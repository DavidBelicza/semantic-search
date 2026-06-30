package vectorstore

import (
	"fmt"

	"github.com/apache/arrow/go/v17/arrow"
)

const (
	chunkVectorsTable = "chunk_vectors"
	chunkIDColumn     = "chunk_id"
	vectorColumn      = "vector"
)

func chunkVectorSchema(dimensions int) *arrow.Schema {
	fields := []arrow.Field{
		{Name: chunkIDColumn, Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: vectorColumn, Type: arrow.FixedSizeListOf(int32(dimensions), arrow.PrimitiveTypes.Float32), Nullable: false},
	}

	return arrow.NewSchema(fields, nil)
}

func validateSchema(schema *arrow.Schema, dimensions int) error {
	expected := chunkVectorSchema(dimensions)
	if schema.NumFields() != expected.NumFields() {
		return fmt.Errorf("LanceDB schema mismatch: want %d fields, got %d", expected.NumFields(), schema.NumFields())
	}

	for i, expectedField := range expected.Fields() {
		field := schema.Field(i)
		if field.Name != expectedField.Name {
			return fmt.Errorf("LanceDB schema mismatch: field %d should be %q, got %q", i, expectedField.Name, field.Name)
		}
		if field.Nullable != expectedField.Nullable {
			return fmt.Errorf("LanceDB schema mismatch: field %q nullable should be %t, got %t", field.Name, expectedField.Nullable, field.Nullable)
		}
		if !arrow.TypeEqual(field.Type, expectedField.Type) {
			return fmt.Errorf("LanceDB schema mismatch: field %q should be %s, got %s", field.Name, expectedField.Type, field.Type)
		}
	}

	return nil
}
