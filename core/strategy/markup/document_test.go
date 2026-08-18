package markup

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/davidbelicza/semantic-search/core/strategy"
)

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func extract(t *testing.T, source string, mode Mode) Document {
	t.Helper()

	document, err := Extract(strings.NewReader(source), mode)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	return document
}

func webSections(t *testing.T, source string) []strategy.Section {
	t.Helper()

	return extract(t, source, WebMode).Sections
}

func publicationSections(t *testing.T, source string) []strategy.Section {
	t.Helper()

	return extract(t, source, PublicationMode).Sections
}

func assertSection(t *testing.T, sections []strategy.Section, path []string, body string) {
	t.Helper()
	for _, section := range sections {
		if equalPath(section.Path, path) && strings.TrimSpace(section.Body) == body {
			return
		}
	}
	t.Fatalf("no section path=%v body=%q; got %+v", path, body, sections)
}

func assertNoText(t *testing.T, sections []strategy.Section, text string) {
	t.Helper()
	for _, section := range sections {
		if strings.Contains(section.Body, text) || strings.Contains(strings.Join(section.Path, " "), text) {
			t.Fatalf("expected %q to be dropped, found it in %+v", text, section)
		}
	}
}

func equalPath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func TestExtractReportsReadError(t *testing.T) {
	if _, err := Extract(errReader{}, WebMode); err == nil {
		t.Fatal("expected a parse error when the reader fails")
	}
}

func TestWebContentRootPrefersMain(t *testing.T) {
	sections := webSections(t, `<body>
		<nav><p>Menu link</p></nav>
		<main><h1>Real</h1><p>Main content.</p></main>
		<div><p>Sidebar noise.</p></div>
	</body>`)

	assertSection(t, sections, []string{"Real"}, "Main content.")
	assertNoText(t, sections, "Sidebar noise.")
}

func TestWebContentRootUsesLoneArticle(t *testing.T) {
	sections := webSections(t, `<body>
		<div><p>Outside the article.</p></div>
		<article><h1>Post</h1><p>Article body.</p></article>
	</body>`)

	assertSection(t, sections, []string{"Post"}, "Article body.")
	assertNoText(t, sections, "Outside the article.")
}

func TestWebContentRootFallsBackToBodyWhenSeveralArticles(t *testing.T) {
	sections := webSections(t, `<body>
		<article><h1>First</h1><p>First body.</p></article>
		<article><h1>Second</h1><p>Second body.</p></article>
	</body>`)

	assertSection(t, sections, []string{"First"}, "First body.")
	assertSection(t, sections, []string{"Second"}, "Second body.")
}

func TestContentRootFallsBackToTheNodeItself(t *testing.T) {
	node := &html.Node{Type: html.ElementNode, Data: "div"}

	if got := ContentRoot(node, WebMode); got != node {
		t.Fatalf("expected the node itself as the web root, got %#v", got)
	}
	if got := ContentRoot(node, PublicationMode); got != node {
		t.Fatalf("expected the node itself as the publication root, got %#v", got)
	}
}

func TestPublicationContentRootKeepsTheWholeBody(t *testing.T) {
	sections := publicationSections(t, `<body>
		<main><h1>Chapter</h1><p>Main body.</p></main>
		<aside><p>Margin note.</p></aside>
		<footer><p>Chapter footnote.</p></footer>
	</body>`)

	assertSection(t, sections, []string{"Chapter"}, "Main body.\n\nMargin note.\n\nChapter footnote.")
}

func TestFindElementReturnsNilWhenAbsent(t *testing.T) {
	node := &html.Node{Type: html.ElementNode, Data: "div"}

	if found := FindElement(node, "main"); found != nil {
		t.Fatalf("expected nil for an absent element, got %#v", found)
	}
	if found := FindElements(node, "article"); len(found) != 0 {
		t.Fatalf("expected no matches, got %d", len(found))
	}
}

func TestWebModeDropsNonProseSubtrees(t *testing.T) {
	sections := webSections(t, `<html><head><title>Title text</title>
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

func TestPublicationModeKeepsSVGTextAndDropsActiveSubtrees(t *testing.T) {
	sections := publicationSections(t, `<body>
		<h1>Plate</h1>
		<svg><text>Diagram label</text></svg>
		<script>var secret = "script text";</script>
		<nav><p>nav text</p></nav>
		<p>Body.</p>
	</body>`)

	assertSection(t, sections, []string{"Plate"}, "Diagram label\n\nBody.")
	for _, dropped := range []string{"script text", "nav text"} {
		assertNoText(t, sections, dropped)
	}
}

func TestPublicationModeDropsHiddenAndPageBreaks(t *testing.T) {
	sections := publicationSections(t, `<body>
		<h1>Chapter</h1>
		<p hidden>hidden text</p>
		<p aria-hidden="true">aria hidden text</p>
		<span epub:type="pagebreak">99</span>
		<span role="doc-pagebreak">100</span>
		<p>Kept body.</p>
	</body>`)

	assertSection(t, sections, []string{"Chapter"}, "Kept body.")
	for _, dropped := range []string{"hidden text", "aria hidden text", "99", "100"} {
		assertNoText(t, sections, dropped)
	}
}

func TestPublicationModeUsesImageAlternativeText(t *testing.T) {
	sections := publicationSections(t, `<body><h1>Figures</h1>
		<p><img src="plate.png" alt="A woodcut of a harbour"></p>
		<p><img src="blank.png" alt="  "></p>
		<p><img src="none.png"></p>
	</body>`)

	assertSection(t, sections, []string{"Figures"}, "A woodcut of a harbour")
}

func TestPublicationImageAlternativeInsideHeading(t *testing.T) {
	sections := publicationSections(t, `<body><h1><img src="t.png" alt="Chapter one"></h1><p>Body.</p></body>`)

	assertSection(t, sections, []string{"Chapter one"}, "Body.")
}

func TestWebModeIgnoresImageAlternativeText(t *testing.T) {
	sections := webSections(t, `<body><h1>Doc</h1><p><img src="p.png" alt="alt text">Body.</p></body>`)

	assertSection(t, sections, []string{"Doc"}, "Body.")
	assertNoText(t, sections, "alt text")
}

func TestHeaderIsKeptBecauseItOftenHoldsTheTitle(t *testing.T) {
	sections := webSections(t, `<body><header><h1>Post title</h1></header><p>Body.</p></body>`)

	assertSection(t, sections, []string{"Post title"}, "Body.")
}

func TestInlineMarkupConcatenatesAndBlocksSeparate(t *testing.T) {
	sections := webSections(t, `<body><p><b>bo</b><i>ld</i> and <a href="#">link</a></p><p>Second.</p></body>`)

	if len(sections) != 1 {
		t.Fatalf("expected one untitled section, got %#v", sections)
	}
	if sections[0].Body != "bold and link\n\nSecond." {
		t.Fatalf("inline/block spacing mismatch: %q", sections[0].Body)
	}
}

func TestWhitespaceAndEntitiesAreNormalized(t *testing.T) {
	sections := webSections(t, "<body><p>a   b\n\tc&nbsp;d &amp; e</p></body>")

	if sections[0].Body != "a b c d & e" {
		t.Fatalf("expected collapsed whitespace and decoded entities, got %q", sections[0].Body)
	}
}

func TestLineBreakSplitsParagraphs(t *testing.T) {
	sections := webSections(t, `<body><p>first<br>second</p></body>`)

	if sections[0].Body != "first\n\nsecond" {
		t.Fatalf("expected a break between the lines, got %q", sections[0].Body)
	}
}

func TestListItemsBecomeSeparateParagraphs(t *testing.T) {
	sections := webSections(t, `<body><ul><li>one</li><li>two</li></ul></body>`)

	if sections[0].Body != "one\n\ntwo" {
		t.Fatalf("expected one paragraph per list item, got %q", sections[0].Body)
	}
}

func TestHeadingTextSkipsNestedNonProse(t *testing.T) {
	sections := webSections(t, `<body><h1>Title<script>script text</script><!--comment text--></h1><p>body</p></body>`)

	assertSection(t, sections, []string{"Title"}, "body")
	assertNoText(t, sections, "script text")
	assertNoText(t, sections, "comment text")
}

func TestBlocksDescendThroughADocumentNode(t *testing.T) {
	document, err := html.Parse(strings.NewReader(`<p>text</p>`))
	if err != nil {
		t.Fatal(err)
	}

	if len(Blocks(document, WebMode)) == 0 {
		t.Fatal("expected the walker to descend through the document node")
	}
	if len(Blocks(nil, WebMode)) != 0 {
		t.Fatal("expected no blocks for a nil root")
	}
}

func TestCommentsAreIgnored(t *testing.T) {
	sections := webSections(t, `<body><p>kept<!-- comment text --></p></body>`)

	assertSection(t, sections, nil, "kept")
	assertNoText(t, sections, "comment text")
}

func TestPreformattedKeepsItsWhitespace(t *testing.T) {
	sections := webSections(t, "<body><pre>line1\n  line2</pre></body>")

	if sections[0].Body != "line1\n  line2" {
		t.Fatalf("expected the pre block verbatim, got %q", sections[0].Body)
	}
}

func TestBlankPreformattedIsDropped(t *testing.T) {
	sections := webSections(t, "<body><pre>   \n  </pre><p>after</p></body>")

	assertSection(t, sections, nil, "after")
	if len(sections) != 1 {
		t.Fatalf("expected only the paragraph, got %#v", sections)
	}
}

func TestEmptyHeadingIsIgnored(t *testing.T) {
	sections := webSections(t, `<body><h1></h1><p>Body.</p></body>`)

	assertSection(t, sections, nil, "Body.")
}

func TestHeadingLevelsNestAndPop(t *testing.T) {
	sections := webSections(t, `<body>
		<h1>A</h1><h2>B</h2><p>under b</p>
		<h3>C</h3><p>under c</p>
		<h2>D</h2><p>under d</p>
	</body>`)

	assertSection(t, sections, []string{"A", "B"}, "under b")
	assertSection(t, sections, []string{"A", "B", "C"}, "under c")
	assertSection(t, sections, []string{"A", "D"}, "under d")
}

func TestDocumentWithoutHeadingsIsOneSection(t *testing.T) {
	sections := webSections(t, `<body><p>only</p><p>text</p></body>`)

	if len(sections) != 1 || len(sections[0].Path) != 0 {
		t.Fatalf("expected a single untitled section, got %#v", sections)
	}
}

func TestLeafHeadingWithoutBodyIsStillIndexed(t *testing.T) {
	sections := webSections(t, `<body><h1>First topic</h1><h1>Second topic</h1></body>`)

	assertSection(t, sections, []string{"First topic"}, "First topic")
	assertSection(t, sections, []string{"Second topic"}, "Second topic")
}

func TestDeepHeadingChainWithoutBodyUsesTheLastHeadingAsText(t *testing.T) {
	sections := webSections(t, `<body><h1>A</h1><h2>B</h2><h3>C</h3></body>`)

	if len(sections) != 1 {
		t.Fatalf("expected exactly one section, got %#v", sections)
	}
	assertSection(t, sections, []string{"A", "B", "C"}, "C")
}

func TestWebModeHasNoDocumentTitle(t *testing.T) {
	document := extract(t, `<html><head><title>Page title</title></head><body><p>Body.</p></body></html>`, WebMode)

	if document.Title != "" {
		t.Fatalf("web extraction should not report a title, got %q", document.Title)
	}
}

func TestPublicationTitleComesFromTheHead(t *testing.T) {
	document := extract(t, `<html><head><title>  Chapter   One  </title></head><body><p>Body.</p></body></html>`, PublicationMode)

	if document.Title != "Chapter One" {
		t.Fatalf("expected the collapsed head title, got %q", document.Title)
	}
}

func TestPublicationTitleFallsBackToAStandaloneSVG(t *testing.T) {
	document := extract(t, `<html><body><svg><title>Cover plate</title><text>Label</text></svg></body></html>`, PublicationMode)

	if document.Title != "Cover plate" {
		t.Fatalf("expected the standalone svg title, got %q", document.Title)
	}
}

func TestPublicationTitleIgnoresEmbeddedSVGFigureTitles(t *testing.T) {
	document := extract(t, `<html><body><p>Prose.</p><svg><title>Figure label</title></svg></body></html>`, PublicationMode)

	if document.Title != "" {
		t.Fatalf("an embedded figure title is not a document title, got %q", document.Title)
	}
}

func TestPublicationTitleIsEmptyWithoutOne(t *testing.T) {
	for _, source := range []string{
		`<html><head></head><body><p>Body.</p></body></html>`,
		`<html><body><svg></svg></body></html>`,
		`<html><body><svg></svg><svg></svg></body></html>`,
	} {
		if document := extract(t, source, PublicationMode); document.Title != "" {
			t.Fatalf("source %q: expected no title, got %q", source, document.Title)
		}
	}
}

func TestPublicationTitleWithoutABody(t *testing.T) {
	document, err := Extract(strings.NewReader(`<title>Only head</title>`), PublicationMode)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if document.Title != "Only head" {
		t.Fatalf("expected the head title, got %q", document.Title)
	}
}

func TestHeadingTextDescendsIntoInlineMarkup(t *testing.T) {
	sections := webSections(t, `<body><h1>Chapter <em>One</em></h1><p>Body.</p></body>`)

	assertSection(t, sections, []string{"Chapter One"}, "Body.")
}

func TestStandaloneSVGTitleIgnoresABodyWithoutElements(t *testing.T) {
	document := extract(t, `<html><head></head><body><!-- only a comment --></body></html>`, PublicationMode)

	if document.Title != "" {
		t.Fatalf("expected no title, got %q", document.Title)
	}
}

func TestTitleLookupHandlesDetachedTrees(t *testing.T) {
	node := &html.Node{Type: html.ElementNode, Data: "div"}

	if found := headTitle(node); found != nil {
		t.Fatalf("expected nil without a head, got %#v", found)
	}
	if found := standaloneSVGTitle(node); found != nil {
		t.Fatalf("expected nil without a body, got %#v", found)
	}
}
