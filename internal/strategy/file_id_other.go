//go:build !unix

package strategy

import "io/fs"

// fileID falls back to the absolute path on platforms that do not expose device and
// inode identifiers (for example Windows).
func fileID(absolutePath string, _ fs.FileInfo) string {
	return absolutePath
}
