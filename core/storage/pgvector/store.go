// Package pgvector implements storage.VectorStorage on PostgreSQL using the pgvector extension
// and the pure-Go pgx driver (no CGO). Search uses pgvector's cosine-distance operator, so
// lower scores are closer, matching the sqlite-vec store's contract.
package pgvector

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"

	storage "github.com/davidbelicza/semantic-search/core/storage"
)

// Store satisfies storage.VectorStorage.
var _ storage.VectorStorage = (*Store)(nil)

const chunkVectorsTable = "chunk_vectors"

type Store struct {
	db         *sql.DB
	dimensions int
	// hnsw builds an HNSW index for approximate (ANN) search; when false, search is exact
	// brute-force kNN over a sequential scan.
	hnsw bool
}

// Open connects to the PostgreSQL database at dsn and ensures the pgvector schema exists. The
// server must have the pgvector extension available. When hnsw is true an HNSW index is
// created for approximate search; otherwise search is exact.
func Open(ctx context.Context, dsn string, dimensions int, hnsw bool) (*Store, error) {
	if dimensions <= 0 {
		return nil, fmt.Errorf("embedding dimensions are required")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	store := &Store{db: db, dimensions: dimensions, hnsw: hnsw}
	if err := store.EnsureSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// EnsureSchema installs the pgvector extension and creates the vector table sized to the
// configured embedding dimensions.
func (s *Store) EnsureSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("enable pgvector extension: %w", err)
	}

	create := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (chunk_id BIGINT PRIMARY KEY, embedding vector(%d))",
		chunkVectorsTable, s.dimensions,
	)
	if _, err := s.db.ExecContext(ctx, create); err != nil {
		return fmt.Errorf("create vector table: %w", err)
	}

	return s.ensureIndex(ctx)
}

// ensureIndex creates the HNSW index for approximate search when the store is in HNSW mode.
// The operator class (vector_cosine_ops) matches the cosine operator (<=>) used by Search, so
// Postgres uses the index automatically. In KNN mode there is no index and search is exact.
func (s *Store) ensureIndex(ctx context.Context) error {
	if !s.hnsw {
		return nil
	}

	index := fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s_embedding_hnsw ON %s USING hnsw (embedding vector_cosine_ops)",
		chunkVectorsTable, chunkVectorsTable,
	)
	if _, err := s.db.ExecContext(ctx, index); err != nil {
		return fmt.Errorf("create hnsw index: %w", err)
	}

	return nil
}

func (s *Store) Delete(ctx context.Context, chunkIDs []int64) error {
	if len(chunkIDs) == 0 {
		return nil
	}

	query, args := inQuery("DELETE FROM "+chunkVectorsTable+" WHERE chunk_id IN (", chunkIDs)
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete vectors: %w", err)
	}

	return nil
}

// Replace upserts the given chunk vectors in one transaction.
func (s *Store) Replace(ctx context.Context, embeddings []storage.ChunkEmbedding) error {
	if len(embeddings) == 0 {
		return nil
	}
	if err := s.validate(embeddings); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(
		"INSERT INTO %s (chunk_id, embedding) VALUES ($1, $2::vector) "+
			"ON CONFLICT (chunk_id) DO UPDATE SET embedding = EXCLUDED.embedding",
		chunkVectorsTable,
	))
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, embedding := range embeddings {
		if _, err := stmt.ExecContext(ctx, embedding.ChunkID, formatVector(embedding.Vector)); err != nil {
			return fmt.Errorf("store vector for chunk %d: %w", embedding.ChunkID, err)
		}
	}

	return tx.Commit()
}

// Search returns the limit nearest chunk vectors to the query, closest first, ranked by cosine
// distance.
func (s *Store) Search(ctx context.Context, query []float32, limit int) ([]storage.VectorHit, error) {
	if limit <= 0 {
		return nil, nil
	}
	if len(query) != s.dimensions {
		return nil, fmt.Errorf("query dimension mismatch: configured %d, got %d", s.dimensions, len(query))
	}

	search := fmt.Sprintf(
		"SELECT chunk_id, embedding <=> $1::vector AS distance FROM %s ORDER BY distance LIMIT $2",
		chunkVectorsTable,
	)
	rows, err := s.db.QueryContext(ctx, search, formatVector(query), limit)
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

func (s *Store) validate(embeddings []storage.ChunkEmbedding) error {
	for _, embedding := range embeddings {
		if embedding.ChunkID == 0 {
			return fmt.Errorf("chunk id is required")
		}
		if len(embedding.Vector) != s.dimensions {
			return fmt.Errorf("embedding dimension mismatch for chunk %d: configured %d, got %d", embedding.ChunkID, s.dimensions, len(embedding.Vector))
		}
	}

	return nil
}

// formatVector renders a float slice as pgvector's text input, e.g. "[0.1,0.2,0.3]".
func formatVector(vector []float32) string {
	parts := make([]string, len(vector))
	for i, value := range vector {
		parts[i] = strconv.FormatFloat(float64(value), 'f', -1, 32)
	}

	return "[" + strings.Join(parts, ",") + "]"
}

func inQuery(prefix string, ids []int64) (string, []any) {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	return prefix + strings.Join(placeholders, ", ") + ")", args
}
