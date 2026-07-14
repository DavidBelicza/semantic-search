//go:build unix

package fs

import (
	iofs "io/fs"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestFileIDReturnsDeviceAndInode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if id := FileID(path, info); !regexp.MustCompile(`^\d+:\d+$`).MatchString(id) {
		t.Fatalf("expected a device:inode id, got %q", id)
	}
}

func TestFileIDStableForSameFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	link := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, link); err != nil {
		t.Skipf("hardlinks unsupported: %v", err)
	}

	fileInfo, _ := os.Stat(path)
	linkInfo, _ := os.Stat(link)
	if FileID(path, fileInfo) != FileID(link, linkInfo) {
		t.Fatal("expected the same id for two paths to one file")
	}
}

func TestFileIDFallsBackToPath(t *testing.T) {
	if got := FileID("/abs/path", fakeFileInfo{}); got != "/abs/path" {
		t.Fatalf("expected the path fallback, got %q", got)
	}
}

// fakeFileInfo has no *syscall.Stat_t, forcing the fallback branch.
type fakeFileInfo struct{}

func (fakeFileInfo) Name() string        { return "fake" }
func (fakeFileInfo) Size() int64         { return 0 }
func (fakeFileInfo) Mode() iofs.FileMode { return 0 }
func (fakeFileInfo) ModTime() time.Time  { return time.Time{} }
func (fakeFileInfo) IsDir() bool         { return false }
func (fakeFileInfo) Sys() any            { return nil }
