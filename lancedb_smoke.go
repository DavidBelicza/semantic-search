//go:build lancedb_smoke

package main

/*
#cgo darwin,arm64 LDFLAGS: ${SRCDIR}/native/lib/darwin_arm64/liblancedb_go.a -framework Security -framework CoreFoundation
#cgo darwin,amd64 LDFLAGS: ${SRCDIR}/native/lib/darwin_amd64/liblancedb_go.a -framework Security -framework CoreFoundation
#cgo linux,arm64 LDFLAGS: ${SRCDIR}/native/lib/linux_arm64/liblancedb_go.a -lm -ldl -lpthread
#cgo linux,amd64 LDFLAGS: ${SRCDIR}/native/lib/linux_amd64/liblancedb_go.a -lm -ldl -lpthread
#cgo windows,amd64 LDFLAGS: ${SRCDIR}/native/lib/windows_amd64/liblancedb_go.a
*/
import "C"

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/apache/arrow/go/v17/arrow"
	"github.com/apache/arrow/go/v17/arrow/array"
	"github.com/apache/arrow/go/v17/arrow/memory"
	. "github.com/lancedb/lancedb-go/pkg/contracts"
	"github.com/lancedb/lancedb-go/pkg/lancedb"
)

const smokeDimensions = 3

func main() {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "semantic-search-lancedb-smoke-")
	if err != nil {
		log.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	conn, err := lancedb.Connect(ctx, dir, nil)
	if err != nil {
		log.Fatalf("connect lancedb: %v", err)
	}
	defer conn.Close()

	table, schema, err := createSmokeTable(ctx, conn)
	if err != nil {
		log.Fatalf("create table: %v", err)
	}
	defer table.Close()

	if err := insertSmokeVector(ctx, table, schema); err != nil {
		log.Fatalf("insert vector: %v", err)
	}

	results, err := table.VectorSearch(ctx, "vector", []float32{0.1, 0.2, 0.3}, 1)
	if err != nil {
		log.Fatalf("vector search: %v", err)
	}

	fmt.Printf("lancedb smoke ok: results=%d db=%s\n", len(results), dir)
}

func createSmokeTable(ctx context.Context, conn IConnection) (ITable, *arrow.Schema, error) {
	fields := []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int32, Nullable: false},
		{Name: "vector", Type: arrow.FixedSizeListOf(smokeDimensions, arrow.PrimitiveTypes.Float32), Nullable: false},
	}
	arrowSchema := arrow.NewSchema(fields, nil)

	schema, err := lancedb.NewSchema(arrowSchema)
	if err != nil {
		return nil, nil, err
	}

	table, err := conn.CreateTable(ctx, "vectors", schema)
	if err != nil {
		return nil, nil, err
	}

	return table, arrowSchema, nil
}

func insertSmokeVector(ctx context.Context, table ITable, schema *arrow.Schema) error {
	pool := memory.NewGoAllocator()

	idBuilder := array.NewInt32Builder(pool)
	idBuilder.AppendValues([]int32{1}, nil)
	idArray := idBuilder.NewArray()
	defer idArray.Release()

	vectorBuilder := array.NewFloat32Builder(pool)
	vectorBuilder.AppendValues([]float32{0.1, 0.2, 0.3}, nil)
	vectorValues := vectorBuilder.NewArray()
	defer vectorValues.Release()

	vectorType := arrow.FixedSizeListOf(smokeDimensions, arrow.PrimitiveTypes.Float32)
	vectorArray := array.NewFixedSizeListData(
		array.NewData(vectorType, 1, []*memory.Buffer{nil}, []arrow.ArrayData{vectorValues.Data()}, 0, 0),
	)
	defer vectorArray.Release()

	record := array.NewRecord(schema, []arrow.Array{idArray, vectorArray}, 1)
	defer record.Release()

	return table.Add(ctx, record, nil)
}
