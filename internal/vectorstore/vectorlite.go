package vectorstore

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"semantic-search/internal/storage"
)

const chunkVectorsTable = "chunk_vectors"

type Store struct {
	db         *sql.DB
	dimensions int
}

func New(db *sql.DB, dimensions int) Store {
	return Store{db: db, dimensions: dimensions}
}

func (s Store) EnsureSchema(ctx context.Context) error {
	if s.dimensions <= 0 {
		return fmt.Errorf("embedding dimensions are required")
	}

	_, err := s.db.ExecContext(ctx, createTableSQL(s.dimensions))
	return err
}

func (s Store) Delete(ctx context.Context, chunkIDs []int64) error {
	if len(chunkIDs) == 0 {
		return nil
	}

	stmt, err := s.db.PrepareContext(ctx, "DELETE FROM chunk_vectors WHERE rowid = ?")
	if err != nil {
		if isMissingVectorTable(err) {
			return nil
		}
		return err
	}
	defer stmt.Close()

	for _, chunkID := range chunkIDs {
		if _, err := stmt.ExecContext(ctx, chunkID); err != nil {
			return fmt.Errorf("delete vector for chunk %d: %w", chunkID, err)
		}
	}

	return nil
}

func (s Store) Replace(ctx context.Context, embeddings []storage.ChunkEmbedding) error {
	if len(embeddings) == 0 {
		return nil
	}

	dimensions := len(embeddings[0].Vector)
	if dimensions == 0 {
		return fmt.Errorf("embedding dimensions are required")
	}
	if dimensions != s.dimensions {
		return fmt.Errorf("embedding dimension mismatch: configured %d, got %d", s.dimensions, dimensions)
	}

	for _, embedding := range embeddings {
		if len(embedding.Vector) != dimensions {
			return fmt.Errorf("embedding dimension mismatch for chunk %d: want %d, got %d", embedding.ChunkID, dimensions, len(embedding.Vector))
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, "INSERT OR REPLACE INTO chunk_vectors(rowid, embedding) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, embedding := range embeddings {
		if _, err := stmt.ExecContext(ctx, embedding.ChunkID, vectorLiteral(embedding.Vector)); err != nil {
			return fmt.Errorf("insert vector for chunk %d: %w", embedding.ChunkID, err)
		}
	}

	return tx.Commit()
}

func createTableSQL(dimensions int) string {
	return fmt.Sprintf(
		"CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vectorlite(embedding float32[%d])",
		chunkVectorsTable,
		dimensions,
	)
}

func vectorLiteral(vector []float32) string {
	values := make([]string, len(vector))
	for i, value := range vector {
		values[i] = strconv.FormatFloat(float64(value), 'g', -1, 32)
	}

	return "[" + strings.Join(values, ",") + "]"
}

func isMissingVectorTable(err error) bool {
	return strings.Contains(err.Error(), "no such table: "+chunkVectorsTable)
}
