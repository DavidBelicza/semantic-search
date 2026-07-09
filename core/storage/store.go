package storage

import "context"

// Storage is the metadata and chunk persistence surface a document store must provide. The
// index/process pipelines and search depend on this interface; sqlite is the built-in
// implementation, but any backend (e.g. Postgres) can satisfy it.
type Storage interface {
	EnsureSchema(ctx context.Context) error
	Close() error

	UpsertDocuments(ctx context.Context, files []FileMetadata) error
	DocumentsByStatus(ctx context.Context, status string, afterID int64, limit int) ([]Document, error)
	UpdateDocumentContentHashAndStatus(ctx context.Context, fileID, contentHash, status string) error
	UpdateDocumentScanCheckpointAndStatus(ctx context.Context, fileID, status string) error
	UpdateDocumentStatus(ctx context.Context, fileID, status string) error
	MarkDocumentEmbedded(ctx context.Context, fileID, contentHash string) error
	ApplyDocumentChunkReconcile(ctx context.Context, documentID int64, plan ChunkReconcilePlan) ([]Chunk, error)
	ChunksByDocumentID(ctx context.Context, documentID int64) ([]Chunk, error)
	ChunkMetadataByIDs(ctx context.Context, chunkIDs []int64) ([]ChunkMetadata, error)
}

// VectorHit is one nearest-neighbor result: a chunk id and its distance to the query
// (lower is closer).
type VectorHit struct {
	ChunkID  int64
	Distance float64
}

// VectorStorage is the vector persistence and search surface. sqlite-vec is the built-in
// implementation, but any backend (e.g. pgvector) can satisfy it.
type VectorStorage interface {
	EnsureSchema(ctx context.Context) error
	Close() error
	Delete(ctx context.Context, chunkIDs []int64) error
	Replace(ctx context.Context, embeddings []ChunkEmbedding) error
	Search(ctx context.Context, query []float32, limit int) ([]VectorHit, error)
}
