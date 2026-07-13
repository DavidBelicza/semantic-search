package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	sqlitemigrations "github.com/davidbelicza/semantic-search/migrations/sqlite"
	storage "github.com/davidbelicza/semantic-search/core/storage"
)

// Store satisfies storage.Storage.
var _ storage.Storage = (*Store)(nil)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) EnsureSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, sqlitemigrations.SchemaSQL); err != nil {
		return err
	}
	if err := s.ensureDocumentStatusSchema(ctx); err != nil {
		return err
	}
	if err := s.ensureEmbeddedContentHashColumn(ctx); err != nil {
		return err
	}

	return s.ensureChunkTitleColumn(ctx)
}

func (s *Store) ensureEmbeddedContentHashColumn(ctx context.Context) error {
	exists, err := s.tableColumnExists(ctx, "documents", "embedded_content_hash")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	_, err = s.db.ExecContext(ctx, "ALTER TABLE documents ADD COLUMN embedded_content_hash TEXT")
	return err
}

func (s *Store) ensureChunkTitleColumn(ctx context.Context) error {
	exists, err := s.tableColumnExists(ctx, "chunks", "title")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	_, err = s.db.ExecContext(ctx, "ALTER TABLE chunks ADD COLUMN title TEXT NOT NULL DEFAULT ''")
	return err
}

func (s *Store) tableColumnExists(ctx context.Context, table string, column string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid          int
			name         string
			dataType     string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}

	return false, rows.Err()
}

func (s *Store) ensureDocumentStatusSchema(ctx context.Context) error {
	needsMigration, err := s.documentsNeedStatusMigration(ctx)
	if err != nil {
		return err
	}
	if !needsMigration {
		return nil
	}

	if _, err := s.db.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer s.db.ExecContext(context.Background(), "PRAGMA foreign_keys = ON")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
CREATE TABLE documents_new (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	file_id TEXT NOT NULL,
	absolute_path TEXT NOT NULL,
	file_size INTEGER NOT NULL,
	modified_at_ns INTEGER NOT NULL,
	content_hash TEXT,
	scanned_file_size INTEGER,
	scanned_modified_at_ns INTEGER,
	status TEXT NOT NULL DEFAULT 'indexed' CHECK(status IN ('indexed', 'scanned', 'chunked', 'embedded')),
	indexed_at_unix INTEGER,
	deleted_at_unix INTEGER,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(file_id)
)`); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO documents_new (
	id,
	file_id,
	absolute_path,
	file_size,
	modified_at_ns,
	content_hash,
	scanned_file_size,
	scanned_modified_at_ns,
	status,
	indexed_at_unix,
	deleted_at_unix,
	created_at,
	updated_at
)
SELECT
	id,
	file_id,
	absolute_path,
	file_size,
	modified_at_ns,
	content_hash,
	scanned_file_size,
	scanned_modified_at_ns,
	CASE
		WHEN status IN ('indexed', 'scanned', 'chunked', 'embedded') THEN status
		ELSE 'indexed'
	END,
	indexed_at_unix,
	deleted_at_unix,
	created_at,
	updated_at
FROM documents`); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "DROP TABLE documents"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "ALTER TABLE documents_new RENAME TO documents"); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) documentsNeedStatusMigration(ctx context.Context) (bool, error) {
	var createSQL string
	err := s.db.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'documents'").Scan(&createSQL)
	if err != nil {
		return false, err
	}

	return strings.Contains(createSQL, "'done'") || strings.Contains(createSQL, "'failed'"), nil
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
) VALUES (?, ?, ?, ?, 'indexed', NULL, CURRENT_TIMESTAMP)
ON CONFLICT(file_id) DO UPDATE SET
	absolute_path = excluded.absolute_path,
	file_size = excluded.file_size,
	modified_at_ns = excluded.modified_at_ns,
	status = CASE
		WHEN documents.file_size != excluded.file_size
			OR documents.modified_at_ns != excluded.modified_at_ns
		THEN 'indexed'
		ELSE documents.status
	END,
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

func (s *Store) DocumentsByStatus(ctx context.Context, status string, afterID int64, limit int) ([]storage.Document, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, file_id, absolute_path, file_size, modified_at_ns, content_hash, scanned_file_size, scanned_modified_at_ns, status, embedded_content_hash
FROM documents
WHERE status = ? AND id > ?
ORDER BY id
LIMIT ?`, status, afterID, limit)
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

// DocumentsFromID pages through all documents by id, returning their id and absolute path. Other
// Document fields are left zero; this is the enumeration used by the cleanup pipeline.
func (s *Store) DocumentsFromID(ctx context.Context, fromID int64, limit int) ([]storage.Document, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, absolute_path
FROM documents
WHERE id > ?
ORDER BY id
LIMIT ?`, fromID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var documents []storage.Document
	for rows.Next() {
		var document storage.Document
		if err := rows.Scan(&document.ID, &document.AbsolutePath); err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return documents, nil
}

// DeleteDocument removes a document and its chunks in one transaction. The caller deletes the
// vectors first, so a crash cannot strand a document whose vectors are already gone.
func (s *Store) DeleteDocument(ctx context.Context, documentID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM chunks WHERE document_id = ?", documentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM documents WHERE id = ?", documentID); err != nil {
		return err
	}

	return tx.Commit()
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

// MarkDocumentEmbedded advances a document to the embedded status and records the
// content hash that has been fully embedded. The recorded hash lets later runs skip
// re-embedding unchanged documents and tells the pipeline whether kept chunks
// already have vectors.
func (s *Store) MarkDocumentEmbedded(ctx context.Context, fileID string, contentHash string) error {
	query := `
UPDATE documents
SET
	status = 'embedded',
	embedded_content_hash = ?,
	updated_at = CURRENT_TIMESTAMP
WHERE file_id = ?`

	return s.updateDocument(ctx, query, contentHash, fileID)
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

func moveKeptChunksToTemporaryIndexes(ctx context.Context, tx *sql.Tx, documentID int64, kept []storage.Chunk) error {
	if len(kept) == 0 {
		return nil
	}

	stmt, err := tx.PrepareContext(ctx, "UPDATE chunks SET chunk_index = ? WHERE id = ? AND document_id = ?")
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

func (s *Store) ApplyDocumentChunkReconcile(ctx context.Context, documentID int64, plan storage.ChunkReconcilePlan) ([]storage.Chunk, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := deleteChunksTx(ctx, tx, plan.RemoveIDs); err != nil {
		return nil, err
	}

	updateStmt, err := tx.PrepareContext(ctx, `
UPDATE chunks
SET
	chunk_index = ?,
	title = ?,
	text = ?,
	token_count = ?,
	start_offset = ?,
	end_offset = ?,
	content_hash = ?
WHERE id = ? AND document_id = ?`)
	if err != nil {
		return nil, err
	}
	defer updateStmt.Close()

	// Move kept chunks to unique temporary negative indices first. Final indices may
	// reorder kept chunks (e.g. two identical blocks swap places), and applying them
	// directly could trip UNIQUE(document_id, chunk_index) mid-transaction when one
	// row takes an index still held by a not-yet-updated row.
	if err := moveKeptChunksToTemporaryIndexes(ctx, tx, documentID, plan.Keep); err != nil {
		return nil, err
	}

	for _, chunk := range plan.Keep {
		if _, err := updateStmt.ExecContext(
			ctx,
			chunk.ChunkIndex,
			chunk.Title,
			chunk.Text,
			chunk.TokenCount,
			chunk.StartOffset,
			chunk.EndOffset,
			chunk.ContentHash,
			chunk.ID,
			documentID,
		); err != nil {
			return nil, err
		}
	}

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
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	inserted := make([]storage.Chunk, 0, len(plan.Insert))
	for _, chunk := range plan.Insert {
		result, err := stmt.ExecContext(
			ctx,
			documentID,
			chunk.ChunkIndex,
			chunk.Title,
			chunk.Text,
			chunk.TokenCount,
			chunk.StartOffset,
			chunk.EndOffset,
			chunk.ContentHash,
		)
		if err != nil {
			return nil, err
		}

		chunkID, err := result.LastInsertId()
		if err != nil {
			return nil, err
		}
		chunk.ID = chunkID
		chunk.DocumentID = documentID
		inserted = append(inserted, chunk)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return inserted, nil
}

func (s *Store) ChunkMetadataByIDs(ctx context.Context, chunkIDs []int64) ([]storage.ChunkMetadata, error) {
	if len(chunkIDs) == 0 {
		return nil, nil
	}

	query, args := chunkMetadataQuery(chunkIDs)
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

func chunkMetadataQuery(chunkIDs []int64) (string, []any) {
	placeholders := make([]string, len(chunkIDs))
	args := make([]any, len(chunkIDs))
	for i, chunkID := range chunkIDs {
		placeholders[i] = "?"
		args[i] = chunkID
	}

	return "SELECT id, document_id, title, text FROM chunks WHERE id IN (" + strings.Join(placeholders, ", ") + ")", args
}

// ChunkDocumentIDs maps the given chunks to their documents without loading chunk text; it is the
// light lookup search uses to group ranked hits before hydrating only the survivors.
func (s *Store) ChunkDocumentIDs(ctx context.Context, chunkIDs []int64) ([]storage.ChunkDocument, error) {
	if len(chunkIDs) == 0 {
		return nil, nil
	}

	query, args := chunkDocumentIDsQuery(chunkIDs)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mapping []storage.ChunkDocument
	for rows.Next() {
		var item storage.ChunkDocument
		if err := rows.Scan(&item.ChunkID, &item.DocumentID); err != nil {
			return nil, err
		}
		mapping = append(mapping, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return mapping, nil
}

func chunkDocumentIDsQuery(chunkIDs []int64) (string, []any) {
	placeholders := make([]string, len(chunkIDs))
	args := make([]any, len(chunkIDs))
	for i, chunkID := range chunkIDs {
		placeholders[i] = "?"
		args[i] = chunkID
	}

	return "SELECT id, document_id FROM chunks WHERE id IN (" + strings.Join(placeholders, ", ") + ")", args
}

// DocumentsByIDs returns the identity (id and absolute path) of the given documents. Other
// Document fields are left zero; this is a lookup for search result assembly.
func (s *Store) DocumentsByIDs(ctx context.Context, documentIDs []int64) ([]storage.Document, error) {
	if len(documentIDs) == 0 {
		return nil, nil
	}

	query, args := documentsByIDsQuery(documentIDs)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var documents []storage.Document
	for rows.Next() {
		var document storage.Document
		if err := rows.Scan(&document.ID, &document.AbsolutePath); err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return documents, nil
}

func documentsByIDsQuery(documentIDs []int64) (string, []any) {
	placeholders := make([]string, len(documentIDs))
	args := make([]any, len(documentIDs))
	for i, documentID := range documentIDs {
		placeholders[i] = "?"
		args[i] = documentID
	}

	return "SELECT id, absolute_path FROM documents WHERE id IN (" + strings.Join(placeholders, ", ") + ")", args
}

func (s *Store) ChunksByDocumentID(ctx context.Context, documentID int64) ([]storage.Chunk, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, document_id, chunk_index, title, text, token_count, start_offset, end_offset, content_hash
FROM chunks
WHERE document_id = ?
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

	query, args := deleteChunksQuery(chunkIDs)
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

func deleteChunksQuery(chunkIDs []int64) (string, []any) {
	placeholders := make([]string, len(chunkIDs))
	args := make([]any, len(chunkIDs))
	for i, chunkID := range chunkIDs {
		placeholders[i] = "?"
		args[i] = chunkID
	}

	return "DELETE FROM chunks WHERE id IN (" + strings.Join(placeholders, ", ") + ")", args
}
