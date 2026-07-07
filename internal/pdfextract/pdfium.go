package pdfextract

import (
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/responses"
	"github.com/klippa-app/go-pdfium/webassembly"

	"github.com/davidbelicza/semantic-search/internal/strategy"
)

const instanceTimeout = 30 * time.Second

// PDFium extracts a PDF's text with PDFium (Chrome's PDF engine) compiled to WebAssembly and
// run through the pure-Go wazero runtime. It needs no CGO and no separately installed library
// — the engine is embedded in the binary. It reads the text layer only and does no OCR, so
// scanned (image-only) PDFs yield no text.
//
// PDFium is not thread-safe, so extraction is serialized with a mutex; the indexing pipeline
// processes files one at a time, so this is not a bottleneck.
type PDFium struct {
	mu       sync.Mutex
	pool     pdfium.Pool
	instance pdfium.Pdfium
}

// NewPDFium initializes the embedded PDFium engine. The returned extractor must be closed
// with Close to release the worker and its memory.
func NewPDFium() (*PDFium, error) {
	pool, err := webassembly.Init(webassembly.Config{MinIdle: 1, MaxIdle: 1, MaxTotal: 1})
	if err != nil {
		return nil, err
	}

	instance, err := pool.GetInstance(instanceTimeout)
	if err != nil {
		pool.Close()
		return nil, err
	}

	return &PDFium{pool: pool, instance: instance}, nil
}

// ExtractRuns returns every page's text as positioned, font-annotated runs, in page order.
func (p *PDFium) ExtractRuns(content []byte) ([]strategy.TextRun, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	doc, err := p.instance.OpenDocument(&requests.OpenDocument{File: &content})
	if err != nil {
		return nil, err
	}
	defer p.instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})

	count, err := p.instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return nil, err
	}

	var runs []strategy.TextRun
	for index := 0; index < count.PageCount; index++ {
		pageRuns, err := p.runsForPage(doc.Document, index)
		if err != nil {
			return nil, err
		}
		runs = append(runs, pageRuns...)
	}

	return runs, nil
}

func (p *PDFium) runsForPage(document references.FPDF_DOCUMENT, index int) ([]strategy.TextRun, error) {
	structured, err := p.instance.GetPageTextStructured(&requests.GetPageTextStructured{
		Page:                   requests.Page{ByIndex: &requests.PageByIndex{Document: document, Index: index}},
		Mode:                   requests.GetPageTextStructuredModeRects,
		CollectFontInformation: true,
	})
	if err != nil {
		return nil, err
	}

	runs := make([]strategy.TextRun, 0, len(structured.Rects))
	for _, rect := range structured.Rects {
		runs = append(runs, runFromRect(rect, index))
	}

	return runs, nil
}

func runFromRect(rect *responses.GetPageTextStructuredRect, page int) strategy.TextRun {
	return strategy.TextRun{
		Text:     rect.Text,
		FontSize: rectFontSize(rect),
		X:        rect.PointPosition.Left,
		Y:        rect.PointPosition.Top,
		Page:     page,
	}
}

func rectFontSize(rect *responses.GetPageTextStructuredRect) float64 {
	if rect.FontInformation == nil {
		return 0
	}
	if rect.FontInformation.RenderedSize > 0 {
		return rect.FontInformation.RenderedSize
	}

	return rect.FontInformation.Size
}

// Close releases the PDFium worker and its pool.
func (p *PDFium) Close() error {
	if err := p.instance.Close(); err != nil {
		return err
	}

	return p.pool.Close()
}
