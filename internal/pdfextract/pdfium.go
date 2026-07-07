package pdfextract

import (
	"strings"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

const instanceTimeout = 30 * time.Second

// PDFium extracts a PDF's text layer with PDFium (Chrome's PDF engine) compiled to
// WebAssembly and run through the pure-Go wazero runtime. It needs no CGO and no separately
// installed library — the engine is embedded in the binary. It reads the text layer only
// and does no OCR, so scanned (image-only) PDFs yield no text.
//
// PDFium is not thread-safe, so Extract is serialized with a mutex; the indexing pipeline
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

func (p *PDFium) Extract(content []byte) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	doc, err := p.instance.OpenDocument(&requests.OpenDocument{File: &content})
	if err != nil {
		return "", err
	}
	defer p.instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})

	count, err := p.instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	for index := 0; index < count.PageCount; index++ {
		page, err := p.instance.GetPageText(&requests.GetPageText{
			Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: index}},
		})
		if err != nil {
			return "", err
		}

		builder.WriteString(page.Text)
		builder.WriteString("\n")
	}

	return builder.String(), nil
}

// Close releases the PDFium worker and its pool.
func (p *PDFium) Close() error {
	if err := p.instance.Close(); err != nil {
		return err
	}

	return p.pool.Close()
}
