package pdf

import (
	"errors"
	"strings"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
)

type fakePDFExtractor struct {
	runs []TextRun
	err  error
}

func (f fakePDFExtractor) ExtractRuns([]byte) ([]TextRun, error) {
	return f.runs, f.err
}

func TestPDFStrategyClaimsOnlyPDF(t *testing.T) {
	s := NewPDFStrategy(general.NewGeneralStrategy(nil), fakePDFExtractor{})

	if !s.Claims("report.PDF") {
		t.Fatal("expected report.PDF to be claimed (case-insensitive)")
	}
	if s.Claims("note.md") {
		t.Fatal("expected note.md not to be claimed")
	}
}

func TestPDFStrategyParseBuildsSectionsFromRuns(t *testing.T) {
	s := NewPDFStrategy(general.NewGeneralStrategy(nil), fakePDFExtractor{runs: []TextRun{
		{Text: "Findings", FontSize: 20, X: 10, Y: 700, Page: 0},
		{Text: "The patient is stable.", FontSize: 10, X: 10, Y: 680, Page: 0},
	}})

	parsed, err := s.Parse([]byte("%PDF-1.7"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Sections) != 1 {
		t.Fatalf("section count mismatch: %#v", parsed.Sections)
	}
	if strings.Join(parsed.Sections[0].Path, " > ") != "Findings" {
		t.Fatalf("heading path mismatch: %#v", parsed.Sections[0].Path)
	}
	if parsed.Sections[0].Body != "The patient is stable." {
		t.Fatalf("body mismatch: %q", parsed.Sections[0].Body)
	}
}

func TestPDFStrategyParsePropagatesExtractorError(t *testing.T) {
	wantErr := errors.New("boom")
	s := NewPDFStrategy(general.NewGeneralStrategy(nil), fakePDFExtractor{err: wantErr})

	if _, err := s.Parse([]byte("%PDF-1.7")); !errors.Is(err, wantErr) {
		t.Fatalf("expected extractor error to propagate, got %v", err)
	}
}

func TestPDFStrategyImageOnlyYieldsNoChunks(t *testing.T) {
	s := NewPDFStrategy(general.NewGeneralStrategy(nil), fakePDFExtractor{runs: nil})

	parsed, err := s.Parse([]byte("%PDF-1.7"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	chunks, err := s.Chunk(storage.Document{}, parsed)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected no chunks for image-only PDF, got %d", len(chunks))
	}
}

func TestBuildSectionsFromRunsOrdersByDescendingTop(t *testing.T) {
	sections := buildSectionsFromRuns([]TextRun{
		{Text: "under the heading", FontSize: 10, X: 10, Y: 500, Page: 0},
		{Text: "Diagnosis", FontSize: 18, X: 10, Y: 520, Page: 0},
	})

	if len(sections) != 1 || strings.Join(sections[0].Path, " > ") != "Diagnosis" {
		t.Fatalf("expected one Diagnosis section, got %#v", sections)
	}
	if sections[0].Body != "under the heading" {
		t.Fatalf("body mismatch: %q", sections[0].Body)
	}
}

func TestBuildSectionsFromRunsStripsRepeatedHeadersAndFooters(t *testing.T) {
	sections := buildSectionsFromRuns([]TextRun{
		{Text: "Clinic Footer", FontSize: 8, X: 10, Y: 30, Page: 0},
		{Text: "page one body text here", FontSize: 10, X: 10, Y: 400, Page: 0},
		{Text: "Clinic Footer", FontSize: 8, X: 10, Y: 30, Page: 1},
		{Text: "page two body text here", FontSize: 10, X: 10, Y: 400, Page: 1},
	})

	joined := ""
	for _, section := range sections {
		joined += section.Body + "\n"
	}
	if strings.Contains(joined, "Clinic Footer") {
		t.Fatalf("repeated footer not stripped: %q", joined)
	}
	if !strings.Contains(joined, "page one body") || !strings.Contains(joined, "page two body") {
		t.Fatalf("body text lost: %q", joined)
	}
}

func TestBuildSectionsFromRunsJoinsHyphenatedLineBreaks(t *testing.T) {
	sections := buildSectionsFromRuns([]TextRun{
		{Text: "inter-", FontSize: 10, X: 10, Y: 400, Page: 0},
		{Text: "national guidelines", FontSize: 10, X: 10, Y: 388, Page: 0},
	})

	if len(sections) != 1 || !strings.Contains(sections[0].Body, "international guidelines") {
		t.Fatalf("hyphenated word not rejoined: %#v", sections)
	}
}
