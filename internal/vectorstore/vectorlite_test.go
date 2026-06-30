package vectorstore

import "testing"

func TestVectorLiteral(t *testing.T) {
	got := vectorLiteral([]float32{0.25, -1.5, 3})
	want := "[0.25,-1.5,3]"

	if got != want {
		t.Fatalf("vector literal mismatch: want %q, got %q", want, got)
	}
}

func TestCreateTableSQLUsesDimensions(t *testing.T) {
	got := createTableSQL(768)
	want := "CREATE VIRTUAL TABLE IF NOT EXISTS chunk_vectors USING vectorlite(embedding float32[768])"

	if got != want {
		t.Fatalf("create table sql mismatch: want %q, got %q", want, got)
	}
}
