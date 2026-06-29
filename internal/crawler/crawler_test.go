package crawler

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCollectFileMetadataReturnsRecursiveRegularFiles(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "notes", "daily")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create nested directory: %v", err)
	}

	readmeFile := filepath.Join(root, "README.md")
	planFile := filepath.Join(root, "notes", "plan.md")
	entryFile := filepath.Join(nested, "entry.md")
	files := []string{readmeFile, planFile, entryFile}
	for _, file := range files {
		if err := os.WriteFile(file, []byte("test"), 0o644); err != nil {
			t.Fatalf("write test file %q: %v", file, err)
		}
	}

	got, err := CollectFileMetadata(root)
	if err != nil {
		t.Fatalf("collect file metadata: %v", err)
	}

	wantPaths := []string{readmeFile, entryFile, planFile}
	var gotPaths []string
	for _, file := range got {
		gotPaths = append(gotPaths, file.AbsolutePath)
		if file.RootPath != root {
			t.Fatalf("root path mismatch: want %q, got %q", root, file.RootPath)
		}
		if file.SizeBytes != 4 {
			t.Fatalf("size mismatch for %q: want 4, got %d", file.AbsolutePath, file.SizeBytes)
		}
		if file.ModifiedAtNS == 0 {
			t.Fatalf("modified time was not set for %q", file.AbsolutePath)
		}
	}

	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("paths mismatch\nwant: %#v\n got: %#v", wantPaths, gotPaths)
	}
}
