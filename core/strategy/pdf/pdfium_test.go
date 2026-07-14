package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

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
