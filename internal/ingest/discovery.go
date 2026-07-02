package ingest

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	storage "semantic-search/internal/storage/sqlite"
)

// Options controls how the crawler traverses a directory tree.
type Options struct {
	IncludeHidden  bool
	FollowSymlinks bool
}

var skippedDirectories = map[string]struct{}{
	".git":         {},
	".hg":          {},
	".svn":         {},
	"node_modules": {},
	"vendor":       {},
	"dist":         {},
	"build":        {},
	".cache":       {},
	".Trash":       {},
}

func DiscoverFiles(root string, options Options) ([]storage.FileMetadata, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rootAbs = filepath.Clean(rootAbs)

	var files []storage.FileMetadata
	err = filepath.WalkDir(rootAbs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			return directoryAction(rootAbs, path, entry, options)
		}

		metadata, ok, err := fileMetadata(path, entry, options)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}

		files = append(files, metadata)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return files, nil
}

// directoryAction decides whether to descend into a directory. The root is always
// traversed even when its own name matches a skip rule.
func directoryAction(rootAbs string, path string, entry fs.DirEntry, options Options) error {
	if path == rootAbs {
		return nil
	}
	if _, skip := skippedDirectories[entry.Name()]; skip {
		return filepath.SkipDir
	}
	if !options.IncludeHidden && isHidden(entry.Name()) {
		return filepath.SkipDir
	}

	return nil
}

// fileMetadata returns the metadata for a file, or ok=false when the entry should be
// skipped (hidden, non-regular, or an unfollowed symlink).
func fileMetadata(path string, entry fs.DirEntry, options Options) (storage.FileMetadata, bool, error) {
	if !options.IncludeHidden && isHidden(entry.Name()) {
		return storage.FileMetadata{}, false, nil
	}

	info, err := fileInfo(path, entry, options)
	if err != nil {
		return storage.FileMetadata{}, false, err
	}
	if !info.Mode().IsRegular() {
		return storage.FileMetadata{}, false, nil
	}

	absolutePath := filepath.Clean(path)
	return storage.FileMetadata{
		AbsolutePath: absolutePath,
		FileID:       fileID(absolutePath, info),
		SizeBytes:    info.Size(),
		ModifiedAtNS: info.ModTime().UnixNano(),
	}, true, nil
}

// fileInfo resolves symlinks to their target only when FollowSymlinks is set;
// otherwise the symlink's own (non-regular) metadata is returned so it is skipped.
func fileInfo(path string, entry fs.DirEntry, options Options) (fs.FileInfo, error) {
	if entry.Type()&fs.ModeSymlink == 0 {
		return entry.Info()
	}
	if !options.FollowSymlinks {
		return entry.Info()
	}

	return os.Stat(path)
}

func isHidden(name string) bool {
	return strings.HasPrefix(name, ".") && name != "." && name != ".."
}
