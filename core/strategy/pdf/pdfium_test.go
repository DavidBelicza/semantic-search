package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/responses"
)

var errTest = errors.New("engine failure")

// minimalPDF builds a tiny but valid single-page PDF containing one line of text, with a
// correct cross-reference table, so the real PDFium engine can open and extract from it.
func minimalPDF() []byte {
	var buf bytes.Buffer
	offsets := make([]int, 0, 5)
	obj := func(body string) {
		offsets = append(offsets, buf.Len())
		buf.WriteString(body)
	}

	buf.WriteString("%PDF-1.4\n")
	obj("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	obj("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	obj("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")
	stream := "BT /F1 24 Tf 72 700 Td (Hello PDF world) Tj ET\n"
	obj(fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%sendstream\nendobj\n", len(stream), stream))
	obj("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	xrefPos := buf.Len()
	buf.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF", xrefPos)

	return buf.Bytes()
}

func TestPDFiumExtractsTextRuns(t *testing.T) {
	extractor, err := NewPDFium()
	if err != nil {
		t.Fatalf("init pdfium: %v", err)
	}
	defer extractor.Close()

	runs, err := extractor.ExtractRuns(minimalPDF())
	if err != nil {
		t.Fatalf("extract runs: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("expected at least one text run")
	}

	var text string
	for _, run := range runs {
		text += run.Text
	}
	if !strings.Contains(strings.ToLower(text), "hello") {
		t.Fatalf("expected the extracted text to contain 'hello', got %q", text)
	}
}

func TestPDFiumRejectsInvalidPDF(t *testing.T) {
	extractor, err := NewPDFium()
	if err != nil {
		t.Fatalf("init pdfium: %v", err)
	}
	defer extractor.Close()

	if _, err := extractor.ExtractRuns([]byte("this is not a pdf")); err == nil {
		t.Fatal("expected an error for invalid PDF bytes")
	}
}

func TestRectFontSizePrefersRenderedThenSizeThenZero(t *testing.T) {
	none := &responses.GetPageTextStructuredRect{}
	if got := rectFontSize(none); got != 0 {
		t.Fatalf("no font information should yield 0, got %v", got)
	}

	sizeOnly := &responses.GetPageTextStructuredRect{FontInformation: &responses.FontInformation{Size: 11}}
	if got := rectFontSize(sizeOnly); got != 11 {
		t.Fatalf("with no rendered size, Size should be used, got %v", got)
	}

	rendered := &responses.GetPageTextStructuredRect{FontInformation: &responses.FontInformation{Size: 11, RenderedSize: 14}}
	if got := rectFontSize(rendered); got != 14 {
		t.Fatalf("rendered size should win, got %v", got)
	}
}

func TestRunFromRectCopiesFields(t *testing.T) {
	rect := &responses.GetPageTextStructuredRect{
		Text:            "hi",
		PointPosition:   responses.CharPosition{Left: 3, Top: 7},
		FontInformation: &responses.FontInformation{Size: 9},
	}
	run := runFromRect(rect, 2)
	if run.Text != "hi" || run.FontSize != 9 || run.X != 3 || run.Y != 7 || run.Page != 2 {
		t.Fatalf("run fields not copied: %#v", run)
	}
}

// fakeInstance embeds the Pdfium interface (nil) so it satisfies the type while overriding only
// the methods PDFium calls; each error field, when set, makes the matching call fail.
type fakeInstance struct {
	pdfium.Pdfium
	openErr  error
	countErr error
	count    int
	textErr  error
	closeErr error
}

func (f fakeInstance) OpenDocument(*requests.OpenDocument) (*responses.OpenDocument, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	return &responses.OpenDocument{}, nil
}

func (fakeInstance) FPDF_CloseDocument(*requests.FPDF_CloseDocument) (*responses.FPDF_CloseDocument, error) {
	return &responses.FPDF_CloseDocument{}, nil
}

func (f fakeInstance) FPDF_GetPageCount(*requests.FPDF_GetPageCount) (*responses.FPDF_GetPageCount, error) {
	if f.countErr != nil {
		return nil, f.countErr
	}
	return &responses.FPDF_GetPageCount{PageCount: f.count}, nil
}

func (f fakeInstance) GetPageTextStructured(*requests.GetPageTextStructured) (*responses.GetPageTextStructured, error) {
	if f.textErr != nil {
		return nil, f.textErr
	}
	return &responses.GetPageTextStructured{}, nil
}

func (f fakeInstance) Close() error { return f.closeErr }

type fakePool struct {
	pdfium.Pool
	closeErr error
}

func (f fakePool) Close() error { return f.closeErr }

func TestExtractRunsPropagatesEngineErrors(t *testing.T) {
	openFail := &PDFium{instance: fakeInstance{openErr: errTest}, pool: fakePool{}}
	if _, err := openFail.ExtractRuns([]byte("x")); err == nil {
		t.Fatal("expected an open-document error")
	}

	countFail := &PDFium{instance: fakeInstance{countErr: errTest}, pool: fakePool{}}
	if _, err := countFail.ExtractRuns([]byte("x")); err == nil {
		t.Fatal("expected a page-count error")
	}

	textFail := &PDFium{instance: fakeInstance{count: 1, textErr: errTest}, pool: fakePool{}}
	if _, err := textFail.ExtractRuns([]byte("x")); err == nil {
		t.Fatal("expected a page-text error")
	}
}

func TestCloseReleasesInstanceThenPool(t *testing.T) {
	if err := (&PDFium{instance: fakeInstance{closeErr: errTest}, pool: fakePool{}}).Close(); err == nil {
		t.Fatal("expected the instance close error")
	}
	if err := (&PDFium{instance: fakeInstance{}, pool: fakePool{closeErr: errTest}}).Close(); err == nil {
		t.Fatal("expected the pool close error")
	}
	if err := (&PDFium{instance: fakeInstance{}, pool: fakePool{}}).Close(); err != nil {
		t.Fatalf("clean close should succeed: %v", err)
	}
}
