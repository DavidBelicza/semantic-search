package crawler

import (
	"io/fs"
	"path/filepath"
)

type FileMetadata struct {
	RootPath     string
	RelativePath string
	AbsolutePath string
	SizeBytes    int64
	ModifiedAtNS int64
}

func CollectFileMetadata(root string) ([]FileMetadata, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rootAbs = filepath.Clean(rootAbs)

	var files []FileMetadata
	err = filepath.WalkDir(rootAbs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		absolutePath := filepath.Clean(path)
		relativePath, err := filepath.Rel(rootAbs, absolutePath)
		if err != nil {
			return err
		}

		files = append(files, FileMetadata{
			RootPath:     rootAbs,
			RelativePath: relativePath,
			AbsolutePath: absolutePath,
			SizeBytes:    info.Size(),
			ModifiedAtNS: info.ModTime().UnixNano(),
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	return files, nil
}
