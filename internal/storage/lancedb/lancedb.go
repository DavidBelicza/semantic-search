package lancedb

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/apache/arrow/go/v17/arrow"
	"github.com/apache/arrow/go/v17/arrow/array"
	"github.com/apache/arrow/go/v17/arrow/memory"
	"github.com/lancedb/lancedb-go/pkg/contracts"
	"github.com/lancedb/lancedb-go/pkg/lancedb"

	storage "semantic-search/internal/storage/sqlite"
)

type Store struct {
	conn       contracts.IConnection
	table      contracts.ITable
	dimensions int
}

func Open(ctx context.Context, path string, dimensions int) (*Store, error) {
	if dimensions <= 0 {
		return nil, fmt.Errorf("embedding dimensions are required")
	}

	conn, err := lancedb.Connect(ctx, path, nil)
	if err != nil {
		return nil, fmt.Errorf("connect lancedb: %w", err)
	}

	store := &Store{conn: conn, dimensions: dimensions}
	if err := store.EnsureSchema(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	var err error
	if s.table != nil {
		err = s.table.Close()
	}
	if s.conn != nil {
		if closeErr := s.conn.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}

	return err
}

func (s *Store) EnsureSchema(ctx context.Context) error {
	table, err := s.openOrCreateTable(ctx)
	if err != nil {
		return err
	}

	schema, err := table.Schema(ctx)
	if err != nil {
		_ = table.Close()
		return fmt.Errorf("load LanceDB schema: %w", err)
	}
	if err := validateSchema(schema, s.dimensions); err != nil {
		_ = table.Close()
		return err
	}

	s.table = table
	return nil
}

func (s *Store) Delete(ctx context.Context, chunkIDs []int64) error {
	if len(chunkIDs) == 0 {
		return nil
	}

	table, err := s.currentTable(ctx)
	if err != nil {
		return err
	}

	if err := table.Delete(ctx, deleteFilter(chunkIDs)); err != nil {
		return fmt.Errorf("delete vectors: %w", err)
	}

	return nil
}

func (s *Store) Replace(ctx context.Context, embeddings []storage.ChunkEmbedding) error {
	if len(embeddings) == 0 {
		return nil
	}
	if err := validateEmbeddings(embeddings, s.dimensions); err != nil {
		return err
	}
	if err := s.Delete(ctx, chunkIDs(embeddings)); err != nil {
		return err
	}

	table, err := s.currentTable(ctx)
	if err != nil {
		return err
	}

	record, err := embeddingsRecord(embeddings, s.dimensions)
	if err != nil {
		return err
	}
	defer record.Release()

	if err := table.Add(ctx, record, nil); err != nil {
		return fmt.Errorf("insert vectors: %w", err)
	}

	return nil
}

func (s *Store) openOrCreateTable(ctx context.Context) (contracts.ITable, error) {
	if s.conn == nil {
		return nil, fmt.Errorf("lancedb connection is required")
	}

	names, err := s.conn.TableNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("list LanceDB tables: %w", err)
	}
	for _, name := range names {
		if name == chunkVectorsTable {
			table, err := s.conn.OpenTable(ctx, chunkVectorsTable)
			if err != nil {
				return nil, fmt.Errorf("open LanceDB table %q: %w", chunkVectorsTable, err)
			}

			return table, nil
		}
	}

	schema, err := lancedb.NewSchema(chunkVectorSchema(s.dimensions))
	if err != nil {
		return nil, fmt.Errorf("create LanceDB schema: %w", err)
	}

	table, err := s.conn.CreateTable(ctx, chunkVectorsTable, schema)
	if err != nil {
		return nil, fmt.Errorf("create LanceDB table %q: %w", chunkVectorsTable, err)
	}

	return table, nil
}

func (s *Store) currentTable(ctx context.Context) (contracts.ITable, error) {
	if s.table != nil {
		return s.table, nil
	}
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}

	return s.table, nil
}

func validateEmbeddings(embeddings []storage.ChunkEmbedding, dimensions int) error {
	for _, embedding := range embeddings {
		if embedding.ChunkID == 0 {
			return fmt.Errorf("chunk id is required")
		}
		if len(embedding.Vector) != dimensions {
			return fmt.Errorf("embedding dimension mismatch for chunk %d: configured %d, got %d", embedding.ChunkID, dimensions, len(embedding.Vector))
		}
	}

	return nil
}

func embeddingsRecord(embeddings []storage.ChunkEmbedding, dimensions int) (arrow.Record, error) {
	pool := memory.NewGoAllocator()
	schema := chunkVectorSchema(dimensions)

	chunkIDBuilder := array.NewInt64Builder(pool)
	defer chunkIDBuilder.Release()

	vectorBuilder := array.NewFixedSizeListBuilder(pool, int32(dimensions), arrow.PrimitiveTypes.Float32)
	defer vectorBuilder.Release()

	valueBuilder, ok := vectorBuilder.ValueBuilder().(*array.Float32Builder)
	if !ok {
		return nil, fmt.Errorf("unexpected vector value builder type")
	}

	for _, embedding := range embeddings {
		chunkIDBuilder.Append(embedding.ChunkID)
		vectorBuilder.Append(true)
		valueBuilder.AppendValues(embedding.Vector, nil)
	}

	chunkIDArray := chunkIDBuilder.NewArray()
	defer chunkIDArray.Release()

	vectorArray := vectorBuilder.NewArray()
	defer vectorArray.Release()

	return array.NewRecord(schema, []arrow.Array{chunkIDArray, vectorArray}, int64(len(embeddings))), nil
}

func deleteFilter(chunkIDs []int64) string {
	if len(chunkIDs) == 1 {
		return chunkIDColumn + " = " + strconv.FormatInt(chunkIDs[0], 10)
	}

	values := make([]string, len(chunkIDs))
	for i, chunkID := range chunkIDs {
		values[i] = strconv.FormatInt(chunkID, 10)
	}

	return chunkIDColumn + " IN (" + strings.Join(values, ", ") + ")"
}

func chunkIDs(embeddings []storage.ChunkEmbedding) []int64 {
	ids := make([]int64, len(embeddings))
	for i, embedding := range embeddings {
		ids[i] = embedding.ChunkID
	}

	return ids
}
