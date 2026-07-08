package semanticsearch

import (
	"context"

	"github.com/davidbelicza/semantic-search/internal/embedder"
	"github.com/davidbelicza/semantic-search/internal/storage/sqlite"
	"github.com/davidbelicza/semantic-search/internal/storage/sqlitevec"
	"github.com/davidbelicza/semantic-search/internal/strategy"
	"github.com/davidbelicza/semantic-search/internal/strategy/code"
	"github.com/davidbelicza/semantic-search/internal/strategy/docx"
	"github.com/davidbelicza/semantic-search/internal/strategy/general"
	"github.com/davidbelicza/semantic-search/internal/strategy/markdown"
	"github.com/davidbelicza/semantic-search/internal/strategy/pdf"
)

// dependencies is the fully instantiated object graph the pipelines need.
type dependencies struct {
	store        *sqlite.Store
	vectorStore  *sqlitevec.Store
	pool         strategy.Pool
	pdfExtractor *pdf.PDFium
}

// build is the single place that instantiates the whole dependency graph: it opens the
// stores and composes the strategy pool (embedder → GeneralStrategy → Markdown/PDF
// strategies → Pool). The embedder is built here and injected into the strategies, since
// embedding is a per-file operation the strategy owns.
func build(ctx context.Context, dbPath string) (dependencies, error) {
	store, vectorStore, err := openStores(ctx, dbPath)
	if err != nil {
		return dependencies{}, err
	}

	pdfExtractor, err := pdf.NewPDFium()
	if err != nil {
		store.Close()
		vectorStore.Close()
		return dependencies{}, err
	}

	documentEmbedder := embedder.NewEmbeddingGemma300MQATEmbedder(embedder.DefaultBaseURL)
	base := general.NewGeneralStrategy(documentEmbedder)
	markdownStrategy := markdown.NewMarkdownStrategy(base)
	pdfStrategy := pdf.NewPDFStrategy(base, pdfExtractor)
	codeStrategy := code.NewCodeStrategy(base)
	docxStrategy := docx.NewDocxStrategy(base)
	pool := strategy.NewPool(markdownStrategy, pdfStrategy, codeStrategy, docxStrategy, base)

	return dependencies{store: store, vectorStore: vectorStore, pool: pool, pdfExtractor: pdfExtractor}, nil
}

func (d dependencies) close() {
	if d.pdfExtractor != nil {
		d.pdfExtractor.Close()
	}
	if d.vectorStore != nil {
		d.vectorStore.Close()
	}
	if d.store != nil {
		d.store.Close()
	}
}

// openStores opens and prepares the SQLite metadata store and the sqlite-vec vector store,
// both backed by the same database file.
func openStores(ctx context.Context, dbPath string) (*sqlite.Store, *sqlitevec.Store, error) {
	store, err := sqlite.Open(dbPath)
	if err != nil {
		return nil, nil, err
	}
	if err := store.EnsureSchema(ctx); err != nil {
		store.Close()
		return nil, nil, err
	}

	vectorStore, err := sqlitevec.Open(ctx, dbPath, embedder.DefaultDimensions)
	if err != nil {
		store.Close()
		return nil, nil, err
	}

	return store, vectorStore, nil
}
