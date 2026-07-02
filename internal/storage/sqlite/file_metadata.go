package sqlite

// FileMetadata is the per-file input for registering a document: where it lives, a
// stable identity, and the size/mtime used for cheap change detection. It is the
// boundary type between file discovery and document storage, and lives here (rather than
// in the ingest package) so the storage layer can consume it without an import cycle.
type FileMetadata struct {
	AbsolutePath string
	FileID       string
	SizeBytes    int64
	ModifiedAtNS int64
}
