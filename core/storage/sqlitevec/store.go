// Package sqlitevec stores chunk embedding vectors in the same SQLite database as the
// document metadata, using the sqlite-vec extension's vec0 virtual table. Search is
// exact (brute-force) K-nearest-neighbour over unit-normalized vectors, so L2 distance
// ranks the same as cosine similarity.
package sqlitevec

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/davidbelicza/semantic-search/core/storage"
)

const (
	chunkVectorsTable = "chunk_vectors"
	chunkIDColumn     = "chunk_id"
	vectorColumn      = "vector"
	distanceColumn    = "distance"
)

func init() {
	// Register sqlite-vec so every mattn connection exposes the vec0 virtual table and
	// the vec_* SQL functions. Auto uses sqlite3_auto_extension, which applies to all
	// connections opened afterwards.
	sqlite_vec.Auto()
}

// Store satisfies storage.VectorStorage.
var _ storage.VectorStorage = (*Store)(nil)

type Store struct {
	db         *sql.DB
	dimensions int
}

// Open connects to the SQLite database at path and ensures the vec0 vector table
// exists. The vectors live in the same file as the documents/chunks tables.
func Open(ctx context.Context, path string, dimensions int) (*Store, error) {
	if dimensions <= 0 {
		return nil, fmt.Errorf("embedding dimensions are required")
	}

	db, err := sql.Open("sqlite3", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite-vec database: %w", err)
	}

	store := &Store{db: db, dimensions: dimensions}
	if err := store.EnsureSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// EnsureSchema creates the vec0 virtual table if it does not exist. The vector column
// is a fixed-length float32 list sized to the configured embedding dimensions.
func (s *Store) EnsureSchema(ctx context.Context) error {
	create := fmt.Sprintf(
		"CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(%s INTEGER PRIMARY KEY, %s FLOAT[%d])",
		chunkVectorsTable, chunkIDColumn, vectorColumn, s.dimensions,
	)
	if _, err := s.db.ExecContext(ctx, create); err != nil {
		return fmt.Errorf("create vec0 table: %w", err)
	}

	return nil
}

func (s *Store) Delete(ctx context.Context, chunkIDs []int64) error {
	if len(chunkIDs) == 0 {
		return nil
	}

	query, args := deleteQuery(chunkIDs)
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete vectors: %w", err)
	}

	return nil
}

// Replace upserts the given chunk vectors. Existing rows for the same chunk ids are
// removed first, then the normalized vectors are inserted, all in one transaction.
func (s *Store) Replace(ctx context.Context, embeddings []storage.ChunkEmbedding) error {
	if len(embeddings) == 0 {
		return nil
	}
	if err := validateEmbeddings(embeddings, s.dimensions); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	deleteSQL, deleteArgs := deleteQuery(chunkIDs(embeddings))
	if _, err := tx.ExecContext(ctx, deleteSQL, deleteArgs...); err != nil {
		return fmt.Errorf("delete vectors: %w", err)
	}

	if err := insertEmbeddings(ctx, tx, embeddings); err != nil {
		return err
	}

	return tx.Commit()
}

func insertEmbeddings(ctx context.Context, tx *sql.Tx, embeddings []storage.ChunkEmbedding) error {
	insert := fmt.Sprintf("INSERT INTO %s(%s, %s) VALUES (?, ?)", chunkVectorsTable, chunkIDColumn, vectorColumn)
	stmt, err := tx.PrepareContext(ctx, insert)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, embedding := range embeddings {
		blob, err := sqlite_vec.SerializeFloat32(normalize(embedding.Vector))
		if err != nil {
			return fmt.Errorf("serialize vector for chunk %d: %w", embedding.ChunkID, err)
		}
		if _, err := stmt.ExecContext(ctx, embedding.ChunkID, blob); err != nil {
			return fmt.Errorf("insert vector for chunk %d: %w", embedding.ChunkID, err)
		}
	}

	return nil
}

// Search returns the limit nearest chunk vectors to the query, closest first. The query
// is normalized so that L2 distance ranks the same as cosine similarity.
func (s *Store) Search(ctx context.Context, query []float32, limit int) ([]storage.VectorHit, error) {
	if limit <= 0 {
		return nil, nil
	}
	if len(query) != s.dimensions {
		return nil, fmt.Errorf("query dimension mismatch: configured %d, got %d", s.dimensions, len(query))
	}

	blob, err := sqlite_vec.SerializeFloat32(normalize(query))
	if err != nil {
		return nil, fmt.Errorf("serialize query vector: %w", err)
	}

	search := fmt.Sprintf(
		"SELECT %s, %s FROM %s WHERE %s MATCH ? ORDER BY %s LIMIT ?",
		chunkIDColumn, distanceColumn, chunkVectorsTable, vectorColumn, distanceColumn,
	)
	rows, err := s.db.QueryContext(ctx, search, blob, limit)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close()

	var hits []storage.VectorHit
	for rows.Next() {
		var hit storage.VectorHit
		if err := rows.Scan(&hit.ChunkID, &hit.Distance); err != nil {
			return nil, err
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return hits, nil
}

func dsn(path string) string {
	// A short busy timeout lets the vector connection wait rather than fail immediately
	// when the metadata connection holds a write lock on the shared file.
	if strings.Contains(path, "?") {
		return path + "&_busy_timeout=5000"
	}

	return path + "?_busy_timeout=5000"
}

func validateEmbeddings(embeddings []storage.ChunkEmbedding, dimensions int) error {
	for _, embedding := range embeddings {
		if embedding.ChunkID == 0 {
			return fmt.Errorf("chunk id is required")
		}
		if len(embedding.Vector) != dimensions {
			return fmt.Errorf("embedding dimension mismatch for chunk %d: configured %d, got %d", embedding.ChunkID, dimensions, len(embedding.Vector))
		}
	}

	return nil
}

func deleteQuery(chunkIDs []int64) (string, []any) {
	placeholders := make([]string, len(chunkIDs))
	args := make([]any, len(chunkIDs))
	for i, chunkID := range chunkIDs {
		placeholders[i] = "?"
		args[i] = chunkID
	}

	return "DELETE FROM " + chunkVectorsTable + " WHERE " + chunkIDColumn + " IN (" + strings.Join(placeholders, ", ") + ")", args
}

func chunkIDs(embeddings []storage.ChunkEmbedding) []int64 {
	ids := make([]int64, len(embeddings))
	for i, embedding := range embeddings {
		ids[i] = embedding.ChunkID
	}

	return ids
}

// normalize scales a vector to unit length so that L2 distance ranks the same as cosine
// similarity. A zero vector is returned unchanged.
func normalize(vector []float32) []float32 {
	var sumSquares float64
	for _, value := range vector {
		sumSquares += float64(value) * float64(value)
	}
	if sumSquares == 0 {
		return vector
	}

	norm := float32(math.Sqrt(sumSquares))
	normalized := make([]float32, len(vector))
	for i, value := range vector {
		normalized[i] = value / norm
	}

	return normalized
}
