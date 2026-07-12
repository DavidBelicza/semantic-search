// Package storage defines the resource entities — the data model the application persists
// and passes between layers. It holds no persistence code and depends on no database, so
// business logic can use these types without coupling to a specific store. Concrete stores
// (for example core/storage/sqlite) live in subpackages and depend on this package.
package storage

const (
	DocumentStatusIndexed  = "indexed"
	DocumentStatusScanned  = "scanned"
	DocumentStatusChunked  = "chunked"
	DocumentStatusEmbedded = "embedded"
)

// Document is a registered file and its indexing state.
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
	EmbeddedContentHash string
}

// Chunk is a retrieval unit: a titled slice of a document's text with its position and a
// content hash for change detection. ID and DocumentID are assigned during persistence.
type Chunk struct {
	ID          int64
	DocumentID  int64
	ChunkIndex  int
	Title       string
	Text        string
	TokenCount  int
	StartOffset int
	EndOffset   int
	ContentHash string
}

// ChunkEmbedding is a chunk's vector, keyed by chunk id.
type ChunkEmbedding struct {
	ChunkID int64
	Vector  []float32
}

// ChunkMetadata is the subset of a chunk resolved for search results.
type ChunkMetadata struct {
	ChunkID    int64
	DocumentID int64
	Title      string
	Text       string
}

// ChunkDocument maps a chunk to its document. It is the light lookup used to group ranked chunk
// hits into documents without loading the chunk text.
type ChunkDocument struct {
	ChunkID    int64
	DocumentID int64
}

// ChunkReconcilePlan describes how a document's stored chunks change on re-index: which are
// kept as-is, which are newly inserted, and which stored ids are removed.
type ChunkReconcilePlan struct {
	Keep      []Chunk
	Insert    []Chunk
	RemoveIDs []int64
}

// ReconcileChunks diffs the incoming chunks against what is stored, matching by content
// hash: unchanged chunks are kept (carrying their ids forward), new ones are inserted, and
// stored chunks no longer present are removed.
func ReconcileChunks(existing []Chunk, incoming []Chunk) ChunkReconcilePlan {
	available := make(map[string][]Chunk)
	for _, chunk := range existing {
		available[chunk.ContentHash] = append(available[chunk.ContentHash], chunk)
	}

	var plan ChunkReconcilePlan
	keptIDs := make(map[int64]struct{})
	for _, chunk := range incoming {
		candidates := available[chunk.ContentHash]
		if len(candidates) == 0 {
			plan.Insert = append(plan.Insert, chunk)
			continue
		}

		existingChunk := candidates[0]
		available[chunk.ContentHash] = candidates[1:]
		chunk.ID = existingChunk.ID
		chunk.DocumentID = existingChunk.DocumentID
		plan.Keep = append(plan.Keep, chunk)
		keptIDs[existingChunk.ID] = struct{}{}
	}

	for _, chunk := range existing {
		if chunk.ID == 0 {
			continue
		}
		if _, ok := keptIDs[chunk.ID]; !ok {
			plan.RemoveIDs = append(plan.RemoveIDs, chunk.ID)
		}
	}

	return plan
}
