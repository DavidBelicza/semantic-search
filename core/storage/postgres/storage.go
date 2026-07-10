// Package postgres implements storage.Storage on PostgreSQL using the pure-Go pgx driver (no
// CGO). It mirrors the sqlite store's behaviour with Postgres-dialect SQL.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"

	storage "github.com/davidbelicza/semantic-search/core/storage"
	postgresmigrations "github.com/davidbelicza/semantic-search/migrations/postgres"
)

// Store satisfies storage.Storage.
var _ storage.Storage = (*Store)(nil)

type Store struct {
	db *sql.DB
}

// Open connects to the PostgreSQL database at dsn (e.g.
// "postgres://user:pass@host:5432/db?sslmode=disable").
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, postgresmigrations.SchemaSQL)
	return err
}

func (s *Store) UpsertDocuments(ctx context.Context, files []storage.FileMetadata) error {
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
) VALUES ($1, $2, $3, $4, 'indexed', NULL, now())
ON CONFLICT (file_id) DO UPDATE SET
	absolute_path = EXCLUDED.absolute_path,
	file_size = EXCLUDED.file_size,
	modified_at_ns = EXCLUDED.modified_at_ns,
	status = CASE
		WHEN documents.file_size != EXCLUDED.file_size
			OR documents.modified_at_ns != EXCLUDED.modified_at_ns
		THEN 'indexed'
		ELSE documents.status
	END,
	deleted_at_unix = NULL,
	updated_at = now()`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, file := range files {
		if _, err := stmt.ExecContext(ctx, file.FileID, file.AbsolutePath, file.SizeBytes, file.ModifiedAtNS); err != nil {
			return fmt.Errorf("upsert document %q: %w", file.AbsolutePath, err)
		}
	}

	return tx.Commit()
}

func (s *Store) DocumentsByStatus(ctx context.Context, status string, afterID int64, limit int) ([]storage.Document, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, file_id, absolute_path, file_size, modified_at_ns, content_hash, scanned_file_size, scanned_modified_at_ns, status, embedded_content_hash
FROM documents
WHERE status = $1 AND id > $2
ORDER BY id
LIMIT $3`, status, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var documents []storage.Document
	for rows.Next() {
		var document storage.Document
		var contentHash sql.NullString
		var scannedFileSize sql.NullInt64
		var scannedModifiedAtNS sql.NullInt64
		var embeddedContentHash sql.NullString
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
			&embeddedContentHash,
		); err != nil {
			return nil, err
		}

		document.ContentHash = contentHash.String
		document.HasHash = contentHash.Valid
		document.ScannedFileSize = scannedFileSize.Int64
		document.ScannedModifiedAtNS = scannedModifiedAtNS.Int64
		document.HasScannedMetadata = scannedFileSize.Valid && scannedModifiedAtNS.Valid
		document.EmbeddedContentHash = embeddedContentHash.String
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
	content_hash = $1,
	scanned_file_size = file_size,
	scanned_modified_at_ns = modified_at_ns,
	status = $2,
	updated_at = now()
WHERE file_id = $3`

	return s.updateDocument(ctx, query, contentHash, status, fileID)
}

func (s *Store) UpdateDocumentStatus(ctx context.Context, fileID string, status string) error {
	query := `
UPDATE documents
SET
	status = $1,
	updated_at = now()
WHERE file_id = $2`

	return s.updateDocument(ctx, query, status, fileID)
}

// MarkDocumentEmbedded advances a document to the embedded status and records the content hash
// that has been fully embedded, so later runs can skip re-embedding unchanged documents.
func (s *Store) MarkDocumentEmbedded(ctx context.Context, fileID string, contentHash string) error {
	query := `
UPDATE documents
SET
	status = 'embedded',
	embedded_content_hash = $1,
	updated_at = now()
WHERE file_id = $2`

	return s.updateDocument(ctx, query, contentHash, fileID)
}

func (s *Store) UpdateDocumentScanCheckpointAndStatus(ctx context.Context, fileID string, status string) error {
	query := `
UPDATE documents
SET
	scanned_file_size = file_size,
	scanned_modified_at_ns = modified_at_ns,
	status = $1,
	updated_at = now()
WHERE file_id = $2`

	return s.updateDocument(ctx, query, status, fileID)
}

func (s *Store) ApplyDocumentChunkReconcile(ctx context.Context, documentID int64, plan storage.ChunkReconcilePlan) ([]storage.Chunk, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := deleteChunksTx(ctx, tx, plan.RemoveIDs); err != nil {
		return nil, err
	}

	if err := moveKeptChunksToTemporaryIndexes(ctx, tx, documentID, plan.Keep); err != nil {
		return nil, err
	}
	if err := updateKeptChunks(ctx, tx, documentID, plan.Keep); err != nil {
		return nil, err
	}

	inserted, err := insertChunks(ctx, tx, documentID, plan.Insert)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return inserted, nil
}

// moveKeptChunksToTemporaryIndexes parks kept chunks at unique negative indices first, so the
// final indices (which may reorder kept chunks) never collide with UNIQUE(document_id,
// chunk_index) mid-transaction.
func moveKeptChunksToTemporaryIndexes(ctx context.Context, tx *sql.Tx, documentID int64, kept []storage.Chunk) error {
	if len(kept) == 0 {
		return nil
	}

	stmt, err := tx.PrepareContext(ctx, "UPDATE chunks SET chunk_index = $1 WHERE id = $2 AND document_id = $3")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, chunk := range kept {
		temporaryIndex := -(int64(i) + 1)
		if _, err := stmt.ExecContext(ctx, temporaryIndex, chunk.ID, documentID); err != nil {
			return err
		}
	}

	return nil
}

func updateKeptChunks(ctx context.Context, tx *sql.Tx, documentID int64, kept []storage.Chunk) error {
	if len(kept) == 0 {
		return nil
	}

	stmt, err := tx.PrepareContext(ctx, `
UPDATE chunks
SET
	chunk_index = $1,
	title = $2,
	text = $3,
	token_count = $4,
	start_offset = $5,
	end_offset = $6,
	content_hash = $7
WHERE id = $8 AND document_id = $9`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, chunk := range kept {
		if _, err := stmt.ExecContext(ctx, chunk.ChunkIndex, chunk.Title, chunk.Text, chunk.TokenCount, chunk.StartOffset, chunk.EndOffset, chunk.ContentHash, chunk.ID, documentID); err != nil {
			return err
		}
	}

	return nil
}

func insertChunks(ctx context.Context, tx *sql.Tx, documentID int64, toInsert []storage.Chunk) ([]storage.Chunk, error) {
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO chunks (
	document_id,
	chunk_index,
	title,
	text,
	token_count,
	start_offset,
	end_offset,
	content_hash
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	inserted := make([]storage.Chunk, 0, len(toInsert))
	for _, chunk := range toInsert {
		var chunkID int64
		if err := stmt.QueryRowContext(ctx, documentID, chunk.ChunkIndex, chunk.Title, chunk.Text, chunk.TokenCount, chunk.StartOffset, chunk.EndOffset, chunk.ContentHash).Scan(&chunkID); err != nil {
			return nil, err
		}
		chunk.ID = chunkID
		chunk.DocumentID = documentID
		inserted = append(inserted, chunk)
	}

	return inserted, nil
}

func (s *Store) ChunkMetadataByIDs(ctx context.Context, chunkIDs []int64) ([]storage.ChunkMetadata, error) {
	if len(chunkIDs) == 0 {
		return nil, nil
	}

	query, args := inQuery("SELECT id, document_id, title, text FROM chunks WHERE id IN (", chunkIDs)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metadata []storage.ChunkMetadata
	for rows.Next() {
		var item storage.ChunkMetadata
		if err := rows.Scan(&item.ChunkID, &item.DocumentID, &item.Title, &item.Text); err != nil {
			return nil, err
		}
		metadata = append(metadata, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return metadata, nil
}

func (s *Store) ChunksByDocumentID(ctx context.Context, documentID int64) ([]storage.Chunk, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, document_id, chunk_index, title, text, token_count, start_offset, end_offset, content_hash
FROM chunks
WHERE document_id = $1
ORDER BY chunk_index`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []storage.Chunk
	for rows.Next() {
		var chunk storage.Chunk
		if err := rows.Scan(
			&chunk.ID,
			&chunk.DocumentID,
			&chunk.ChunkIndex,
			&chunk.Title,
			&chunk.Text,
			&chunk.TokenCount,
			&chunk.StartOffset,
			&chunk.EndOffset,
			&chunk.ContentHash,
		); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return chunks, nil
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

func deleteChunksTx(ctx context.Context, tx *sql.Tx, chunkIDs []int64) error {
	if len(chunkIDs) == 0 {
		return nil
	}

	query, args := inQuery("DELETE FROM chunks WHERE id IN (", chunkIDs)
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

// inQuery builds "<prefix>$1, $2, ...)" with the matching args for an IN clause.
func inQuery(prefix string, ids []int64) (string, []any) {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	return prefix + strings.Join(placeholders, ", ") + ")", args
}
