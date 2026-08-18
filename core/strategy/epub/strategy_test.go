package epub

import (
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
)

func newEPUB() strategy.Strategy {
	return NewEPUBStrategy(general.NewGeneralStrategy(nil, nil))
}

func TestClaimsOnlyEPUBExtension(t *testing.T) {
	s := newEPUB()

	for _, path := range []string{"book.epub", "BOOK.EPUB", "a/b/Book.ePub"} {
		if !s.Claims(path) {
			t.Fatalf("should claim %q", path)
		}
	}
	for _, path := range []string{"book.epub3", "book.zip", "book.xhtml", "book.pdf", "noext"} {
		if s.Claims(path) {
			t.Fatalf("should not claim %q", path)
		}
	}
}

func TestParseReturnsSectionsInSpineOrder(t *testing.T) {
	book := buildEPUB(t, standardParts()...)

	parsed, err := newEPUB().Parse(book)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Sections) < 2 {
		t.Fatalf("expected sections from both spine documents, got %d", len(parsed.Sections))
	}

	first := parsed.Sections[0].Body
	last := parsed.Sections[len(parsed.Sections)-1].Body
	if !contains(first, "sluice") || !contains(last, "kiln") {
		t.Fatalf("spine order not preserved: first=%q last=%q", first, last)
	}
}

func TestParseSurfacesContainerErrors(t *testing.T) {
	if _, err := newEPUB().Parse([]byte("not a zip archive")); err == nil {
		t.Fatal("expected an error for a non-archive")
	}
}

func TestChunkIsInheritedAndTitledFromTheHeadingPath(t *testing.T) {
	s := newEPUB()

	parsed, err := s.Parse(buildEPUB(t, standardParts()...))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	chunks, err := s.Chunk(storage.Document{AbsolutePath: "/books/almanac.epub"}, parsed)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	if chunks[0].Title != "Waterworks > The Sluice" {
		t.Fatalf("expected the nested heading path as the title, got %q", chunks[0].Title)
	}
}
