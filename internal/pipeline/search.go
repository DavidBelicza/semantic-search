package pipeline

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/davidbelicza/semantic-search/core/search"
	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
)

// chunkFetchLimit bounds how many ranked chunks the vector store returns before grouping.
// TODO: the proper retrieval bound (enough chunks to satisfy MaxDocuments without a fixed cap)
// is the next step; this constant is a placeholder so grouping can be built and tested first.
const chunkFetchLimit = 1000

// SearchStore is the metadata surface search needs — a subset of storage.Storage, which any
// injected store satisfies.
type SearchStore interface {
	ChunkMetadataByIDs(ctx context.Context, chunkIDs []int64) ([]storage.ChunkMetadata, error)
	DocumentsByIDs(ctx context.Context, documentIDs []int64) ([]storage.Document, error)
}

// SearchVectorStore is the vector query surface search needs — a subset of storage.VectorStorage.
type SearchVectorStore interface {
	Search(ctx context.Context, query []float32, limit int) ([]storage.VectorHit, error)
}

// Searcher runs a search and returns matching documents. It is the internal seam behind the
// facade's Search; promoting it to a public package for caller-supplied implementations is a
// later option.
type Searcher interface {
	Search(ctx context.Context, config search.SearchConfig) ([]search.DocumentResult, error)
}

// documentSearcher is the one Searcher implementation: it holds the search dependencies and
// groups the ranked chunk hits into documents.
type documentSearcher struct {
	store       SearchStore
	vectorStore SearchVectorStore
	model       strategy.EmbeddingModel
	client      strategy.AiClient
}

// NewDocumentSearcher builds the default Searcher from the search dependencies.
func NewDocumentSearcher(store SearchStore, vectorStore SearchVectorStore, model strategy.EmbeddingModel, client strategy.AiClient) Searcher {
	return documentSearcher{store: store, vectorStore: vectorStore, model: model, client: client}
}

// Search runs the chunk query, groups the ranked hits into documents per the config, and fills
// in each document's file name and path.
func (s documentSearcher) Search(ctx context.Context, config search.SearchConfig) ([]search.DocumentResult, error) {
	results, err := Search(ctx, s.store, s.vectorStore, s.model, s.client, config.Query, config.TaskType, chunkFetchLimit)
	if err != nil {
		return nil, err
	}

	docs := groupDocuments(results, config)

	return s.withDocumentPaths(ctx, docs)
}

// withDocumentPaths looks up the grouped documents' paths and sets AbsolutePath and FileName.
func (s documentSearcher) withDocumentPaths(ctx context.Context, docs []search.DocumentResult) ([]search.DocumentResult, error) {
	if len(docs) == 0 {
		return docs, nil
	}

	documents, err := s.store.DocumentsByIDs(ctx, documentIDs(docs))
	if err != nil {
		return nil, err
	}

	pathByID := make(map[int64]string, len(documents))
	for _, document := range documents {
		pathByID[document.ID] = document.AbsolutePath
	}

	for i := range docs {
		path := pathByID[docs[i].DocumentID]
		if path == "" {
			continue
		}
		docs[i].AbsolutePath = path
		docs[i].FileName = filepath.Base(path)
	}

	return docs, nil
}

// groupDocuments collapses ranked chunk results into documents. Results arrive best-first, so a
// document's first-seen chunk is its best and first-seen order is best-score order; no re-sort is
// needed. MaxChunks caps the chunks kept per document, MaxDocuments caps the documents returned.
func groupDocuments(results []search.SearchResult, config search.SearchConfig) []search.DocumentResult {
	order := make([]int64, 0)
	byDoc := make(map[int64]*search.DocumentResult)

	for _, result := range results {
		doc, seen := byDoc[result.DocumentID]
		if !seen {
			order = append(order, result.DocumentID)
			byDoc[result.DocumentID] = &search.DocumentResult{
				DocumentID: result.DocumentID,
				Score:      result.Score,
				Chunks:     []search.SearchResult{result},
			}
			continue
		}
		doc.Chunks = append(doc.Chunks, result)
	}

	docs := make([]search.DocumentResult, 0, len(order))
	for _, id := range order {
		docs = append(docs, *byDoc[id])
	}

	docs = capChunksPerDocument(docs, config.MaxChunks)

	return capDocuments(docs, config.MaxDocuments)
}

// capChunksPerDocument trims each document's chunks to the top max, keeping the highest ranked.
func capChunksPerDocument(docs []search.DocumentResult, max *int) []search.DocumentResult {
	if max == nil {
		return docs
	}

	for i := range docs {
		docs[i].Chunks = limitChunks(docs[i].Chunks, *max)
	}

	return docs
}

// capDocuments trims the document list to the top max, keeping the highest ranked.
func capDocuments(docs []search.DocumentResult, max *int) []search.DocumentResult {
	if max == nil || len(docs) <= *max {
		return docs
	}

	return docs[:*max]
}

// limitChunks returns at most max chunks from the front of the ranked slice.
func limitChunks(chunks []search.SearchResult, max int) []search.SearchResult {
	if len(chunks) <= max {
		return chunks
	}

	return chunks[:max]
}

// documentIDs pulls the document ids out of the grouped results, in order.
func documentIDs(docs []search.DocumentResult) []int64 {
	ids := make([]int64, len(docs))
	for i, doc := range docs {
		ids[i] = doc.DocumentID
	}

	return ids
}

// Search is the chunk query behind the document search: it phrases the query for the model,
// embeds it, runs the vector nearest-neighbor lookup, and resolves the hits back to chunk text and
// metadata. An empty taskType uses the model's default retrieval task. It stays exported until the
// facade delegates to the Searcher (then it becomes internal to this package).
func Search(ctx context.Context, store SearchStore, vectorStore SearchVectorStore, model strategy.EmbeddingModel, client strategy.AiClient, query string, taskType string, limit int) ([]search.SearchResult, error) {
	phrased, err := model.BuildQuery(query, taskType)
	if err != nil {
		return nil, err
	}

	vectors, err := client.Embed(ctx, []string{phrased})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("expected one query embedding, got %d", len(vectors))
	}

	hits, err := vectorStore.Search(ctx, vectors[0], limit)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, nil
	}

	metadata, err := store.ChunkMetadataByIDs(ctx, hitChunkIDs(hits))
	if err != nil {
		return nil, err
	}

	return buildSearchResults(hits, metadata), nil
}

// buildSearchResults resolves vector hits to their chunk metadata, preserving hit order and
// skipping any hit whose metadata is missing.
func buildSearchResults(hits []storage.VectorHit, metadata []storage.ChunkMetadata) []search.SearchResult {
	byID := make(map[int64]storage.ChunkMetadata, len(metadata))
	for _, item := range metadata {
		byID[item.ChunkID] = item
	}

	results := make([]search.SearchResult, 0, len(hits))
	for _, hit := range hits {
		item, ok := byID[hit.ChunkID]
		if !ok {
			continue
		}
		results = append(results, search.SearchResult{
			DocumentID: item.DocumentID,
			ChunkID:    item.ChunkID,
			Title:      item.Title,
			Text:       item.Text,
			Score:      hit.Distance,
		})
	}

	return results
}

// hitChunkIDs pulls the chunk ids out of the hits, in order.
func hitChunkIDs(hits []storage.VectorHit) []int64 {
	ids := make([]int64, len(hits))
	for i, hit := range hits {
		ids[i] = hit.ChunkID
	}

	return ids
}
