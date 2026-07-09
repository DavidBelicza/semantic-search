package semanticsearch

// IndexOptions configures an index run.
type IndexOptions struct {
	// FailFast aborts on the first per-document error instead of collecting and continuing.
	FailFast bool
	// IncludeHidden indexes hidden files and directories.
	IncludeHidden bool
	// FollowSymlinks resolves and indexes symlink targets.
	FollowSymlinks bool
}
