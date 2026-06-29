package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"

	_ "github.com/mattn/go-sqlite3"

	"semantic-search/internal/crawler"
)

//go:embed schema.sql
var schemaSQL string

type Store struct {
	db *sql.DB
}

const (
	DocumentStatusIndexed  = "indexed"
	DocumentStatusScanned  = "scanned"
	DocumentStatusChunked  = "chunked"
	DocumentStatusEmbedded = "embedded"
	DocumentStatusDone     = "done"
	DocumentStatusFailed   = "failed"
)

type Document struct {
	ID                  int64
	FileID              string
	AbsolutePath        string
	FileSize            int64
	ModifiedAtNS        int64
	ContentHash         string
	HasHash             bool
	ScannedFileSize     int64
	ScannedModifiedAtNS int64
	HasScannedMetadata  bool
	Status              string
}

type Chunk struct {
	ChunkIndex  int
	Text        string
	TokenCount  int
	StartOffset int
	EndOffset   int
	ContentHash string
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
	file_id,
	absolute_path,
	file_size,
	modified_at_ns,
	status,
	deleted_at_unix,
	updated_at
) VALUES (?, ?, ?, ?, 'indexed', NULL, CURRENT_TIMESTAMP)
ON CONFLICT(file_id) DO UPDATE SET
	absolute_path = excluded.absolute_path,
	file_size = excluded.file_size,
	modified_at_ns = excluded.modified_at_ns,
	status = 'indexed',
	deleted_at_unix = NULL,
	updated_at = CURRENT_TIMESTAMP
;`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, file := range files {
		if _, err := stmt.ExecContext(
			ctx,
			file.FileID,
			file.AbsolutePath,
			file.SizeBytes,
			file.ModifiedAtNS,
		); err != nil {
			return fmt.Errorf("upsert document %q: %w", file.AbsolutePath, err)
		}
	}

	return tx.Commit()
}

func (s *Store) DocumentsByStatus(ctx context.Context, status string, limit int) ([]Document, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, file_id, absolute_path, file_size, modified_at_ns, content_hash, scanned_file_size, scanned_modified_at_ns, status
FROM documents
WHERE status = ?
ORDER BY id
LIMIT ?`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var documents []Document
	for rows.Next() {
		var document Document
		var contentHash sql.NullString
		var scannedFileSize sql.NullInt64
		var scannedModifiedAtNS sql.NullInt64
		if err := rows.Scan(
			&document.ID,
			&document.FileID,
			&document.AbsolutePath,
			&document.FileSize,
			&document.ModifiedAtNS,
			&contentHash,
			&scannedFileSize,
			&scannedModifiedAtNS,
			&document.Status,
		); err != nil {
			return nil, err
		}

		document.ContentHash = contentHash.String
		document.HasHash = contentHash.Valid
		document.ScannedFileSize = scannedFileSize.Int64
		document.ScannedModifiedAtNS = scannedModifiedAtNS.Int64
		document.HasScannedMetadata = scannedFileSize.Valid && scannedModifiedAtNS.Valid
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return documents, nil
}

func (s *Store) UpdateDocumentContentHashAndStatus(ctx context.Context, fileID string, contentHash string, status string) error {
	query := `
UPDATE documents
SET
	content_hash = ?,
	scanned_file_size = file_size,
	scanned_modified_at_ns = modified_at_ns,
	status = ?,
	updated_at = CURRENT_TIMESTAMP
WHERE file_id = ?`

	return s.updateDocument(ctx, query, contentHash, status, fileID)
}

func (s *Store) UpdateDocumentStatus(ctx context.Context, fileID string, status string) error {
	query := `
UPDATE documents
SET
	status = ?,
	updated_at = CURRENT_TIMESTAMP
WHERE file_id = ?`

	return s.updateDocument(ctx, query, status, fileID)
}

func (s *Store) UpdateDocumentScanCheckpointAndStatus(ctx context.Context, fileID string, status string) error {
	query := `
UPDATE documents
SET
	scanned_file_size = file_size,
	scanned_modified_at_ns = modified_at_ns,
	status = ?,
	updated_at = CURRENT_TIMESTAMP
WHERE file_id = ?`

	return s.updateDocument(ctx, query, status, fileID)
}

func (s *Store) ReplaceDocumentChunksAndStatus(ctx context.Context, documentID int64, chunks []Chunk, status string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM chunks WHERE document_id = ?", documentID); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO chunks (
	document_id,
	chunk_index,
	text,
	token_count,
	start_offset,
	end_offset,
	content_hash
) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, chunk := range chunks {
		if _, err := stmt.ExecContext(
			ctx,
			documentID,
			chunk.ChunkIndex,
			chunk.Text,
			chunk.TokenCount,
			chunk.StartOffset,
			chunk.EndOffset,
			chunk.ContentHash,
		); err != nil {
			return err
		}
	}

	result, err := tx.ExecContext(ctx, `
UPDATE documents
SET
	status = ?,
	updated_at = CURRENT_TIMESTAMP
WHERE id = ?`, status, documentID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return errors.New("document not found")
	}

	return tx.Commit()
}

func (s *Store) updateDocument(ctx context.Context, query string, args ...any) error {
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return errors.New("document not found")
	}

	return nil
}
