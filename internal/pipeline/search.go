package pipeline

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/davidbelicza/semantic-search/core/search"
	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
)

const (
	// maxSearchChunks is the fixed number of top-ranked chunks the vector search returns before
	// grouping into documents.
	maxSearchChunks = 1000
	// defaultMinRelevance, defaultMaxDocuments, and defaultMaxChunks apply when the caller leaves
	// them unset in the config.
	defaultMinRelevance = 0.5
	defaultMaxDocuments = 20
	defaultMaxChunks    = 2
)

// SearchStore is the metadata surface search needs — a subset of storage.Storage, which any
// injected store satisfies.
type SearchStore interface {
	ChunkMetadataByIDs(ctx context.Context, chunkIDs []int64) ([]storage.ChunkMetadata, error)
	ChunkDocumentIDs(ctx context.Context, chunkIDs []int64) ([]storage.ChunkDocument, error)
	DocumentsByIDs(ctx context.Context, documentIDs []int64) ([]storage.Document, error)
}

// SearchVectorStore is the vector query surface search needs — a subset of storage.VectorStorage.
type SearchVectorStore interface {
	Search(ctx context.Context, query []float32, limit int) ([]storage.VectorHit, error)
}

// documentSearcher is the default search.Searcher: it holds the search dependencies and groups the
// ranked chunk hits into documents.
type documentSearcher struct {
	store       SearchStore
	vectorStore SearchVectorStore
	model       strategy.EmbeddingModel
	client      strategy.AiClient
}

// NewDocumentSearcher builds the default search.Searcher from the search dependencies.
func NewDocumentSearcher(store SearchStore, vectorStore SearchVectorStore, model strategy.EmbeddingModel, client strategy.AiClient) search.Searcher {
	return documentSearcher{store: store, vectorStore: vectorStore, model: model, client: client}
}

// Search embeds the query, retrieves the top-ranked chunk hits, groups them into documents per the
// config, then hydrates only the surviving chunks' text and their documents' paths.
func (s documentSearcher) Search(ctx context.Context, config search.SearchConfig) ([]search.DocumentResult, error) {
	phrased, err := s.model.BuildQuery(config.Query, config.TaskType)
	if err != nil {
		return nil, err
	}

	vectors, err := s.client.Embed(ctx, []string{phrased})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("expected one query embedding, got %d", len(vectors))
	}

	hits, err := s.vectorStore.Search(ctx, vectors[0], maxSearchChunks)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, nil
	}

	results, err := s.rankByDocument(ctx, hits)
	if err != nil {
		return nil, err
	}

	docs := groupDocuments(filterByRelevance(results, resolveMinRelevance(config)), config)

	return s.hydrate(ctx, docs)
}

// resolveMinRelevance returns the minimum relevance, defaulting when the caller left it unset.
func resolveMinRelevance(config search.SearchConfig) float64 {
	if config.MinRelevance == nil {
		return defaultMinRelevance
	}

	return *config.MinRelevance
}

// filterByRelevance keeps only matches at least as relevant as min. Results are ordered by
// ascending distance (descending relevance), so it stops at the first match below min.
func filterByRelevance(results []search.SearchResult, min float64) []search.SearchResult {
	kept := make([]search.SearchResult, 0, len(results))
	for _, result := range results {
		if relevance(result.Score) < min {
			break
		}
		kept = append(kept, result)
	}

	return kept
}

// relevance maps a distance to a relevance score where higher is closer. Both vector backends use
// a cosine-equivalent metric with distance in [0, 2], so 1 - distance/2 lands in [0, 1].
func relevance(distance float64) float64 {
	return 1 - distance/2
}

// rankByDocument maps ranked hits to their documents (no text), preserving hit order.
func (s documentSearcher) rankByDocument(ctx context.Context, hits []storage.VectorHit) ([]search.SearchResult, error) {
	mapping, err := s.store.ChunkDocumentIDs(ctx, hitChunkIDs(hits))
	if err != nil {
		return nil, err
	}

	return buildRankedResults(hits, mapping), nil
}

// hydrate fills the surviving chunks' text and their documents' paths, the only heavy loads.
func (s documentSearcher) hydrate(ctx context.Context, docs []search.DocumentResult) ([]search.DocumentResult, error) {
	if len(docs) == 0 {
		return docs, nil
	}

	if err := s.hydrateChunks(ctx, docs); err != nil {
		return nil, err
	}
	if err := s.hydratePaths(ctx, docs); err != nil {
		return nil, err
	}

	return docs, nil
}

// hydrateChunks loads text and title for only the surviving chunks and fills them in.
func (s documentSearcher) hydrateChunks(ctx context.Context, docs []search.DocumentResult) error {
	metadata, err := s.store.ChunkMetadataByIDs(ctx, survivorChunkIDs(docs))
	if err != nil {
		return err
	}

	byID := make(map[int64]storage.ChunkMetadata, len(metadata))
	for _, item := range metadata {
		byID[item.ChunkID] = item
	}

	for i := range docs {
		fillChunkText(docs[i].Chunks, byID)
	}

	return nil
}

// hydratePaths looks up the surviving documents' paths and sets AbsolutePath and FileName.
func (s documentSearcher) hydratePaths(ctx context.Context, docs []search.DocumentResult) error {
	documents, err := s.store.DocumentsByIDs(ctx, documentIDs(docs))
	if err != nil {
		return err
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

	return nil
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

	docs = capChunksPerDocument(docs, resolveMaxChunks(config))

	return capDocuments(docs, resolveMaxDocuments(config))
}

// resolveMaxDocuments returns the document cap, defaulting when the caller left it unset.
func resolveMaxDocuments(config search.SearchConfig) int {
	if config.MaxDocuments == nil {
		return defaultMaxDocuments
	}

	return *config.MaxDocuments
}

// resolveMaxChunks returns the per-document chunk cap, defaulting when the caller left it unset.
func resolveMaxChunks(config search.SearchConfig) int {
	if config.MaxChunks == nil {
		return defaultMaxChunks
	}

	return *config.MaxChunks
}

// capChunksPerDocument trims each document's chunks to the top max, keeping the highest ranked.
func capChunksPerDocument(docs []search.DocumentResult, max int) []search.DocumentResult {
	for i := range docs {
		docs[i].Chunks = limitChunks(docs[i].Chunks, max)
	}

	return docs
}

// capDocuments trims the document list to the top max, keeping the highest ranked.
func capDocuments(docs []search.DocumentResult, max int) []search.DocumentResult {
	if len(docs) <= max {
		return docs
	}

	return docs[:max]
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

// survivorChunkIDs collects the chunk ids kept across the grouped documents.
func survivorChunkIDs(docs []search.DocumentResult) []int64 {
	ids := make([]int64, 0)
	for _, doc := range docs {
		ids = appendChunkIDs(ids, doc.Chunks)
	}

	return ids
}

// appendChunkIDs appends each chunk's id to ids.
func appendChunkIDs(ids []int64, chunks []search.SearchResult) []int64 {
	for _, chunk := range chunks {
		ids = append(ids, chunk.ChunkID)
	}

	return ids
}

// fillChunkText copies title and text into each chunk from the hydrated metadata.
func fillChunkText(chunks []search.SearchResult, byID map[int64]storage.ChunkMetadata) {
	for i := range chunks {
		item, ok := byID[chunks[i].ChunkID]
		if !ok {
			continue
		}
		chunks[i].Title = item.Title
		chunks[i].Text = item.Text
	}
}

// buildRankedResults resolves ranked hits to their document ids, preserving hit order and skipping
// any hit with no mapping. Text and title are left empty; they are hydrated for survivors only.
func buildRankedResults(hits []storage.VectorHit, mapping []storage.ChunkDocument) []search.SearchResult {
	docByChunk := make(map[int64]int64, len(mapping))
	for _, item := range mapping {
		docByChunk[item.ChunkID] = item.DocumentID
	}

	results := make([]search.SearchResult, 0, len(hits))
	for _, hit := range hits {
		documentID, ok := docByChunk[hit.ChunkID]
		if !ok {
			continue
		}
		results = append(results, search.SearchResult{
			DocumentID: documentID,
			ChunkID:    hit.ChunkID,
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
