package semanticsearch

import (
	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/code"
	"github.com/davidbelicza/semantic-search/core/strategy/docx"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
	"github.com/davidbelicza/semantic-search/core/strategy/markdown"
	"github.com/davidbelicza/semantic-search/core/strategy/pdf"
)

// StrategyFactory is a deferred strategy: it declares the extensions the strategy claims (so
// the engine can reject duplicates before indexing) and builds the strategy once the engine
// supplies the shared embedder. Build may return a cleanup the engine runs on Close (e.g. to
// release the PDF extractor); the cleanup is nil when there is nothing to release.
//
// To register a custom strategy, construct a StrategyFactory whose Build returns it.
type StrategyFactory struct {
	Extensions []string
	Build      func(embedder strategy.Embedder) (strategy.Strategy, func() error, error)
}

// NewMarkdownStrategy registers the Markdown strategy.
func NewMarkdownStrategy() StrategyFactory {
	return StrategyFactory{
		Extensions: []string{".md", ".markdown", ".mdown"},
		Build: func(embedder strategy.Embedder) (strategy.Strategy, func() error, error) {
			return markdown.NewMarkdownStrategy(general.NewGeneralStrategy(embedder)), nil, nil
		},
	}
}

// NewPDFStrategy registers the PDF strategy. Each engine gets its own PDFium extractor, which
// the engine releases on Close.
func NewPDFStrategy() StrategyFactory {
	return StrategyFactory{
		Extensions: []string{".pdf"},
		Build: func(embedder strategy.Embedder) (strategy.Strategy, func() error, error) {
			extractor, err := pdf.NewPDFium()
			if err != nil {
				return nil, nil, err
			}

			return pdf.NewPDFStrategy(general.NewGeneralStrategy(embedder), extractor), extractor.Close, nil
		},
	}
}

// NewCodeStrategy registers the source-code strategy.
func NewCodeStrategy() StrategyFactory {
	return StrategyFactory{
		Extensions: []string{".go", ".js", ".ts", ".jsx", ".tsx", ".py", ".php", ".java", ".rb", ".rs", ".c", ".h", ".cpp", ".hpp", ".cs", ".sh", ".sql"},
		Build: func(embedder strategy.Embedder) (strategy.Strategy, func() error, error) {
			return code.NewCodeStrategy(general.NewGeneralStrategy(embedder)), nil, nil
		},
	}
}

// NewDocxStrategy registers the DOCX strategy.
func NewDocxStrategy() StrategyFactory {
	return StrategyFactory{
		Extensions: []string{".docx"},
		Build: func(embedder strategy.Embedder) (strategy.Strategy, func() error, error) {
			return docx.NewDocxStrategy(general.NewGeneralStrategy(embedder)), nil, nil
		},
	}
}

// NewTextStrategy registers the plain-text strategy (the general strategy used directly).
func NewTextStrategy() StrategyFactory {
	return StrategyFactory{
		Extensions: []string{".txt", ".text", ".log", ".rst", ".org", ".adoc"},
		Build: func(embedder strategy.Embedder) (strategy.Strategy, func() error, error) {
			return general.NewGeneralStrategy(embedder), nil, nil
		},
	}
}
