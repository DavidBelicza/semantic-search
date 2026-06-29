package crawler

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"syscall"
)

type FileMetadata struct {
	AbsolutePath string
	FileID       string
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
		fileID, err := fileIDFromInfo(info)
		if err != nil {
			return err
		}

		files = append(files, FileMetadata{
			AbsolutePath: absolutePath,
			FileID:       fileID,
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

func fileIDFromInfo(info fs.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("file %q does not expose syscall stat metadata", info.Name())
	}

	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
