package storage

// FileMetadata is the per-file input for registering a document: where it lives, a stable
// identity, and the size/mtime used for cheap change detection. It is the boundary type
// between file discovery and document storage.
type FileMetadata struct {
	AbsolutePath string
	FileID       string
	SizeBytes    int64
	ModifiedAtNS int64
}
