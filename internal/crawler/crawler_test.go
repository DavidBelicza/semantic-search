package crawler

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

	got, err := CollectFileMetadata(root, Options{})
	if err != nil {
		t.Fatalf("collect file metadata: %v", err)
	}

	wantPaths := []string{readmeFile, entryFile, planFile}
	var gotPaths []string
	for _, file := range got {
		gotPaths = append(gotPaths, file.AbsolutePath)
		if file.FileID == "" {
			t.Fatalf("file id was not set for %q", file.AbsolutePath)
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

func TestCollectFileMetadataSkipsVcsBuildAndHiddenByDefault(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) string {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %q: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %q: %v", rel, err)
		}
		return full
	}

	keep := write("keep.md")
	write(".git/config.md")
	write("node_modules/dep.md")
	hiddenDirFile := write(".hidden/note.md")
	hiddenFile := write(".secret.md")

	got, err := CollectFileMetadata(root, Options{})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(got) != 1 || got[0].AbsolutePath != keep {
		t.Fatalf("expected only keep.md by default, got %#v", collectPaths(got))
	}

	withHidden, err := CollectFileMetadata(root, Options{IncludeHidden: true})
	if err != nil {
		t.Fatalf("collect with hidden: %v", err)
	}
	hiddenPaths := collectPaths(withHidden)
	for _, want := range []string{keep, hiddenFile, hiddenDirFile} {
		if !contains(hiddenPaths, want) {
			t.Fatalf("expected %q included with IncludeHidden, got %#v", want, hiddenPaths)
		}
	}
	for _, path := range hiddenPaths {
		if strings.Contains(path, "node_modules") || strings.Contains(path, ".git") {
			t.Fatalf("skip directories must always be excluded, got %#v", hiddenPaths)
		}
	}
}

func collectPaths(files []FileMetadata) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.AbsolutePath)
	}

	return paths
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}
