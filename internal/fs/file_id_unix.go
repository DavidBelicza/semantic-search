//go:build unix

package fs

import (
	"fmt"
	iofs "io/fs"
	"syscall"
)

// FileID derives a stable identity from the filesystem device and inode so that two paths
// resolving to the same physical file are indexed once. It falls back to the absolute path
// when the platform does not expose that metadata.
func FileID(absolutePath string, info iofs.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return absolutePath
	}

	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
}
