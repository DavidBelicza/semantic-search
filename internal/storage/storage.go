package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

	_ "github.com/mattn/go-sqlite3"

	"semantic-search/internal/crawler"
)

//go:embed schema.sql
var schemaSQL string

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schemaSQL)
	return err
}

func (s *Store) UpsertDocuments(ctx context.Context, files []crawler.FileMetadata) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO documents (
	root_path,
	relative_path,
	absolute_path,
	file_size,
	modified_at_ns,
	deleted_at_unix,
	updated_at
) VALUES (?, ?, ?, ?, ?, NULL, CURRENT_TIMESTAMP)
ON CONFLICT(root_path, relative_path) DO UPDATE SET
	absolute_path = excluded.absolute_path,
	file_size = excluded.file_size,
	modified_at_ns = excluded.modified_at_ns,
	deleted_at_unix = NULL,
	updated_at = CURRENT_TIMESTAMP;`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, file := range files {
		if _, err := stmt.ExecContext(
			ctx,
			file.RootPath,
			file.RelativePath,
			file.AbsolutePath,
			file.SizeBytes,
			file.ModifiedAtNS,
		); err != nil {
			return fmt.Errorf("upsert document %q: %w", file.AbsolutePath, err)
		}
	}

	return tx.Commit()
}
