package html

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/davidbelicza/semantic-search/core/strategy"
)

// errReader fails on the first read so the parser surfaces an error.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestExtractSectionsReportsReadError(t *testing.T) {
	if _, err := extractSections(errReader{}); err == nil {
		t.Fatal("expected a parse error when the reader fails")
	}
}

// --- content root ---

func TestContentRootPrefersMain(t *testing.T) {
	sections := parseSections(t, `<body>
		<nav><p>Menu link</p></nav>
		<main><h1>Real</h1><p>Main content.</p></main>
		<div><p>Sidebar noise.</p></div>
	</body>`)

	assertSection(t, sections, []string{"Real"}, "Main content.")
	assertNoText(t, sections, "Sidebar noise.")
}

func TestContentRootUsesLoneArticle(t *testing.T) {
	sections := parseSections(t, `<body>
		<div><p>Outside the article.</p></div>
		<article><h1>Post</h1><p>Article body.</p></article>
	</body>`)

	assertSection(t, sections, []string{"Post"}, "Article body.")
	assertNoText(t, sections, "Outside the article.")
}

func TestContentRootFallsBackToBodyWhenSeveralArticles(t *testing.T) {
	// A listing page: the first article is not the document, so the whole body is indexed.
	sections := parseSections(t, `<body>
		<article><h1>First</h1><p>First body.</p></article>
		<article><h1>Second</h1><p>Second body.</p></article>
	</body>`)

	assertSection(t, sections, []string{"First"}, "First body.")
	assertSection(t, sections, []string{"Second"}, "Second body.")
}

func TestContentRootFallsBackToTheNodeItself(t *testing.T) {
	// A hand-built fragment with no main, article, or body: the node is its own root.
	node := &html.Node{Type: html.ElementNode, Data: "div"}
	if got := contentRoot(node); got != node {
		t.Fatalf("expected the node itself as the root, got %#v", got)
	}
}

func TestFindElementReturnsNilWhenAbsent(t *testing.T) {
	node := &html.Node{Type: html.ElementNode, Data: "div"}
	if found := findElement(node, "main"); found != nil {
		t.Fatalf("expected nil for an absent element, got %#v", found)
	}
	if found := findElements(node, "article"); len(found) != 0 {
		t.Fatalf("expected no matches, got %d", len(found))
	}
}

// --- skipped subtrees ---

func TestParseDropsNonProseSubtrees(t *testing.T) {
	sections := parseSections(t, `<html><head><title>Title text</title>
		<style>body{color:red}</style></head><body>
		<h1>Doc</h1>
		<script>var secret = "script text";</script>
		<noscript>noscript text</noscript>
		<template>template text</template>
		<svg><text>svg text</text></svg>
		<canvas>canvas text</canvas>
		<nav><p>nav text</p></nav>
		<footer><p>footer text</p></footer>
		<aside><p>aside text</p></aside>
		<p>Kept body.</p>
	</body></html>`)

	assertSection(t, sections, []string{"Doc"}, "Kept body.")
	for _, dropped := range []string{
		"script text", "color:red", "noscript text", "template text", "svg text",
		"canvas text", "nav text", "footer text", "aside text", "Title text",
	} {
		assertNoText(t, sections, dropped)
	}
}

func TestHeaderIsKeptBecauseItOftenHoldsTheTitle(t *testing.T) {
	sections := parseSections(t, `<body><header><h1>Post title</h1></header><p>Body.</p></body>`)

	assertSection(t, sections, []string{"Post title"}, "Body.")
}

// --- inline vs block text ---

func TestInlineMarkupConcatenatesAndBlocksSeparate(t *testing.T) {
	sections := parseSections(t, `<body><p><b>bo</b><i>ld</i> and <a href="#">link</a></p><p>Second.</p></body>`)

	if len(sections) != 1 {
		t.Fatalf("expected one untitled section, got %#v", sections)
	}
	if sections[0].Body != "bold and link\n\nSecond." {
		t.Fatalf("inline/block spacing mismatch: %q", sections[0].Body)
	}
}

func TestWhitespaceAndEntitiesAreNormalized(t *testing.T) {
	sections := parseSections(t, "<body><p>a   b\n\tc&nbsp;d &amp; e</p></body>")

	if sections[0].Body != "a b c d & e" {
		t.Fatalf("expected collapsed whitespace and decoded entities, got %q", sections[0].Body)
	}
}

func TestLineBreakSplitsParagraphs(t *testing.T) {
	sections := parseSections(t, `<body><p>first<br>second</p></body>`)

	if sections[0].Body != "first\n\nsecond" {
		t.Fatalf("expected a break between the lines, got %q", sections[0].Body)
	}
}

func TestListItemsBecomeSeparateParagraphs(t *testing.T) {
	sections := parseSections(t, `<body><ul><li>one</li><li>two</li></ul></body>`)

	if sections[0].Body != "one\n\ntwo" {
		t.Fatalf("expected one paragraph per list item, got %q", sections[0].Body)
	}
}

func TestHeadingTextSkipsNestedNonProse(t *testing.T) {
	// The heading's own text is collected directly, so a nested script or comment inside it is
	// dropped rather than becoming part of the path.
	sections := parseSections(t, `<body><h1>Title<script>script text</script><!--comment text--></h1><p>body</p></body>`)

	assertSection(t, sections, []string{"Title"}, "body")
	assertNoText(t, sections, "script text")
	assertNoText(t, sections, "comment text")
}

func TestWalkerHandlesADocumentNode(t *testing.T) {
	// contentRoot yields the document itself for a tree with no body, so the walker must
	// descend through a document node.
	document, err := html.Parse(strings.NewReader(`<p>text</p>`))
	if err != nil {
		t.Fatal(err)
	}

	walk := &walker{}
	walk.node(document)
	if len(walk.blocks) == 0 {
		t.Fatal("expected the walker to descend through the document node")
	}
}

func TestCommentsAreIgnored(t *testing.T) {
	sections := parseSections(t, `<body><p>kept<!-- comment text --></p></body>`)

	assertSection(t, sections, nil, "kept")
	assertNoText(t, sections, "comment text")
}

// --- preformatted ---

func TestPreformattedKeepsItsWhitespace(t *testing.T) {
	sections := parseSections(t, "<body><pre>line1\n  line2</pre></body>")

	if sections[0].Body != "line1\n  line2" {
		t.Fatalf("expected the pre block verbatim, got %q", sections[0].Body)
	}
}

func TestBlankPreformattedIsDropped(t *testing.T) {
	sections := parseSections(t, "<body><pre>   \n  </pre><p>after</p></body>")

	assertSection(t, sections, nil, "after")
	if strings.Count(sections[0].Body, "after") != 1 || len(sections) != 1 {
		t.Fatalf("expected only the paragraph, got %#v", sections)
	}
}

// --- headings ---

func TestEmptyHeadingIsIgnored(t *testing.T) {
	sections := parseSections(t, `<body><h1></h1><p>Body.</p></body>`)

	assertSection(t, sections, nil, "Body.")
}

func TestHeadingLevelsNestAndPop(t *testing.T) {
	sections := parseSections(t, `<body>
		<h1>A</h1><h2>B</h2><p>under b</p>
		<h3>C</h3><p>under c</p>
		<h2>D</h2><p>under d</p>
	</body>`)

	assertSection(t, sections, []string{"A", "B"}, "under b")
	assertSection(t, sections, []string{"A", "B", "C"}, "under c")
	assertSection(t, sections, []string{"A", "D"}, "under d")
}

func TestSkippedHeadingLevelKeepsPath(t *testing.T) {
	sections := parseSections(t, `<body><h1>A</h1><h4>Deep</h4><p>text</p></body>`)

	assertSection(t, sections, []string{"A", "Deep"}, "text")
}

func TestDocumentWithoutHeadingsIsOneSection(t *testing.T) {
	sections := parseSections(t, `<body><p>only</p><p>text</p></body>`)

	if len(sections) != 1 || len(sections[0].Path) != 0 {
		t.Fatalf("expected a single untitled section, got %#v", sections)
	}
}

// --- headings that never receive a body ---

func TestLeafHeadingWithoutBodyIsStillIndexed(t *testing.T) {
	// A page whose prose sits in its headings: each leaf heading becomes its own section
	// instead of being dropped.
	sections := parseSections(t, `<body><h1>First topic</h1><h1>Second topic</h1></body>`)

	assertSection(t, sections, []string{"First topic"}, "First topic")
	assertSection(t, sections, []string{"Second topic"}, "Second topic")
}

func TestParentHeadingIsLeftToItsChildren(t *testing.T) {
	// h1 only introduces the deeper h2, so it does not become a section of its own.
	sections := parseSections(t, `<body><h1>Parent</h1><h2>Child</h2><p>body</p></body>`)

	if len(sections) != 1 {
		t.Fatalf("expected only the child's section, got %#v", sections)
	}
	assertSection(t, sections, []string{"Parent", "Child"}, "body")
}

func TestDeepHeadingChainWithoutBodyUsesTheLastHeadingAsText(t *testing.T) {
	// h1 > h2 > h3 and no prose: the deepest heading becomes the body, and the whole chain
	// stays as the path. The two outer headings only introduce deeper ones, so they do not
	// repeat as sections of their own.
	sections := parseSections(t, `<body><h1>A</h1><h2>B</h2><h3>C</h3></body>`)

	if len(sections) != 1 {
		t.Fatalf("expected exactly one section, got %#v", sections)
	}
	assertSection(t, sections, []string{"A", "B", "C"}, "C")
}

func TestDeepHeadingChainPrefersRealBodyText(t *testing.T) {
	sections := parseSections(t, `<body><h1>A</h1><h2>B</h2><h3>C</h3><p>real body</p></body>`)

	if len(sections) != 1 {
		t.Fatalf("expected exactly one section, got %#v", sections)
	}
	assertSection(t, sections, []string{"A", "B", "C"}, "real body")
}

func TestSiblingBranchesEachGetTheirOwnLeaf(t *testing.T) {
	sections := parseSections(t, `<body><h1>A</h1><h2>B</h2><h3>C</h3><h2>D</h2></body>`)

	assertSection(t, sections, []string{"A", "B", "C"}, "C")
	assertSection(t, sections, []string{"A", "D"}, "D")
}

func TestTrailingLeafHeadingIsEmittedAtEnd(t *testing.T) {
	sections := parseSections(t, `<body><h1>Topic</h1><p>body</p><h2>Trailing</h2></body>`)

	assertSection(t, sections, []string{"Topic"}, "body")
	assertSection(t, sections, []string{"Topic", "Trailing"}, "Trailing")
}

// --- helper ---

func assertNoText(t *testing.T, sections []strategy.Section, text string) {
	t.Helper()
	for _, s := range sections {
		if strings.Contains(s.Body, text) || strings.Contains(strings.Join(s.Path, " "), text) {
			t.Fatalf("expected %q to be dropped, found it in %+v", text, s)
		}
	}
}
