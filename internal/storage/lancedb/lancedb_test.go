package lancedb

import (
	"context"
	"math"
	"testing"

	"github.com/apache/arrow/go/v17/arrow"

	storage "semantic-search/internal/storage/sqlite"
)

func TestNormalizeProducesUnitVector(t *testing.T) {
	got := normalize([]float32{3, 4})
	if math.Abs(float64(got[0])-0.6) > 1e-6 || math.Abs(float64(got[1])-0.8) > 1e-6 {
		t.Fatalf("normalize mismatch: %#v", got)
	}
}

func TestNormalizeLeavesZeroVectorUnchanged(t *testing.T) {
	for _, value := range normalize([]float32{0, 0, 0}) {
		if value != 0 {
			t.Fatal("expected zero vector to be returned unchanged")
		}
	}
}

func TestChunkVectorSchemaStoresOnlyChunkIDAndVector(t *testing.T) {
	schema := chunkVectorSchema(3)

	if schema.NumFields() != 2 {
		t.Fatalf("field count mismatch: want 2, got %d", schema.NumFields())
	}

	chunkIDField := schema.Field(0)
	if chunkIDField.Name != chunkIDColumn {
		t.Fatalf("chunk id field name mismatch: want %q, got %q", chunkIDColumn, chunkIDField.Name)
	}
	if !arrow.TypeEqual(chunkIDField.Type, arrow.PrimitiveTypes.Int64) {
		t.Fatalf("chunk id field type mismatch: want int64, got %s", chunkIDField.Type)
	}

	vectorField := schema.Field(1)
	if vectorField.Name != vectorColumn {
		t.Fatalf("vector field name mismatch: want %q, got %q", vectorColumn, vectorField.Name)
	}
	wantVectorType := arrow.FixedSizeListOf(3, arrow.PrimitiveTypes.Float32)
	if !arrow.TypeEqual(vectorField.Type, wantVectorType) {
		t.Fatalf("vector field type mismatch: want %s, got %s", wantVectorType, vectorField.Type)
	}
}

func TestDeleteFilter(t *testing.T) {
	tests := []struct {
		name string
		ids  []int64
		want string
	}{
		{
			name: "single",
			ids:  []int64{42},
			want: "chunk_id = 42",
		},
		{
			name: "multiple",
			ids:  []int64{42, 43},
			want: "chunk_id IN (42, 43)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deleteFilter(tt.ids)
			if got != tt.want {
				t.Fatalf("delete filter mismatch: want %q, got %q", tt.want, got)
			}
		})
	}
}

func TestValidateEmbeddingsUsesConfiguredDimensions(t *testing.T) {
	err := validateEmbeddings([]storage.ChunkEmbedding{
		{ChunkID: 12, Vector: []float32{0.1, 0.2}},
	}, 3)
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

func TestStoreSearchReturnsNearestChunkIDs(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir(), 3)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	embeddings := []storage.ChunkEmbedding{
		{ChunkID: 1, Vector: []float32{1, 0, 0}},
		{ChunkID: 2, Vector: []float32{0, 1, 0}},
		{ChunkID: 3, Vector: []float32{0, 0, 1}},
	}
	if err := store.Replace(ctx, embeddings); err != nil {
		t.Fatalf("replace embeddings: %v", err)
	}

	hits, err := store.Search(ctx, []float32{0.9, 0.1, 0}, 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hit count mismatch: want 2, got %d", len(hits))
	}
	if hits[0].ChunkID != 1 {
		t.Fatalf("nearest chunk id mismatch: want 1, got %d", hits[0].ChunkID)
	}
}

func TestStoreSearchRejectsWrongDimensions(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir(), 3)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if _, err := store.Search(ctx, []float32{0.1, 0.2}, 5); err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

func TestStoreReplaceDeletesExistingVector(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir(), 3)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	first := []storage.ChunkEmbedding{
		{ChunkID: 10, Vector: []float32{0.1, 0.2, 0.3}},
	}
	if err := store.Replace(ctx, first); err != nil {
		t.Fatalf("replace first vector: %v", err)
	}

	second := []storage.ChunkEmbedding{
		{ChunkID: 10, Vector: []float32{0.4, 0.5, 0.6}},
	}
	if err := store.Replace(ctx, second); err != nil {
		t.Fatalf("replace second vector: %v", err)
	}

	results, err := store.table.SelectWithFilter(ctx, "chunk_id = 10")
	if err != nil {
		t.Fatalf("select vector: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("vector count mismatch after replace: want 1, got %d", len(results))
	}

	if err := store.Delete(ctx, []int64{10}); err != nil {
		t.Fatalf("delete vector: %v", err)
	}

	results, err = store.table.SelectWithFilter(ctx, "chunk_id = 10")
	if err != nil {
		t.Fatalf("select deleted vector: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("vector count mismatch after delete: want 0, got %d", len(results))
	}
}
