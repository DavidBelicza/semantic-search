package strategy

import (
	"errors"
	"testing"
)

type fakePDFExtractor struct {
	text string
	err  error
}

func (f fakePDFExtractor) Extract([]byte) (string, error) {
	return f.text, f.err
}

func TestPDFStrategyClaimsOnlyPDF(t *testing.T) {
	s := NewPDFStrategy(NewGeneralStrategy(nil), fakePDFExtractor{})

	if !s.Claims("report.PDF") {
		t.Fatal("expected report.PDF to be claimed (case-insensitive)")
	}
	if s.Claims("note.md") {
		t.Fatal("expected note.md not to be claimed")
	}
}

func TestPDFStrategyParseDelegatesToExtractor(t *testing.T) {
	s := NewPDFStrategy(NewGeneralStrategy(nil), fakePDFExtractor{text: "  hello world \n"})

	text, err := s.Parse([]byte("%PDF-1.7"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if text != "hello world" {
		t.Fatalf("expected trimmed extractor output, got %q", text)
	}
}

func TestPDFStrategyParsePropagatesExtractorError(t *testing.T) {
	wantErr := errors.New("boom")
	s := NewPDFStrategy(NewGeneralStrategy(nil), fakePDFExtractor{err: wantErr})

	if _, err := s.Parse([]byte("%PDF-1.7")); !errors.Is(err, wantErr) {
		t.Fatalf("expected extractor error to propagate, got %v", err)
	}
}
