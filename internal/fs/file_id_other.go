//go:build !unix

package fs

import iofs "io/fs"

// FileID falls back to the absolute path on platforms that do not expose device and inode
// identifiers (for example Windows).
func FileID(absolutePath string, _ iofs.FileInfo) string {
	return absolutePath
}
