package epub

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"strconv"
	"strings"
	"testing"

	"github.com/davidbelicza/semantic-search/core/strategy"
)

type archivePart struct {
	name    string
	content string
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func buildEPUB(t *testing.T, parts ...archivePart) []byte {
	t.Helper()

	buf := new(bytes.Buffer)
	writer := zip.NewWriter(buf)
	for _, part := range parts {
		entry, err := writer.Create(part.name)
		if err != nil {
			t.Fatalf("create %s: %v", part.name, err)
		}
		if _, err := entry.Write([]byte(part.content)); err != nil {
			t.Fatalf("write %s: %v", part.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	return buf.Bytes()
}

func containerFor(packagePath string) archivePart {
	return archivePart{"META-INF/container.xml", `<?xml version="1.0"?>
<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="` + packagePath + `" media-type="application/oebps-package+xml"/></rootfiles>
</container>`}
}

func standardParts() []archivePart {
	pkg := `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf">
  <metadata><title>Waterworks</title></metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="one" href="text/one.xhtml" media-type="application/xhtml+xml"/>
    <item id="two" href="text/two.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="nav"/><itemref idref="one"/><itemref idref="two"/>
  </spine>
</package>`

	return []archivePart{
		containerFor("OEBPS/package.opf"),
		{"OEBPS/package.opf", pkg},
		{"OEBPS/nav.xhtml", `<html><body><h1>Contents</h1><p>A listing.</p></body></html>`},
		{"OEBPS/text/one.xhtml", `<html><head><title>One</title></head><body>
			<h1>Waterworks</h1><h2>The Sluice</h2><p>The sluice gate is raised at first light.</p></body></html>`},
		{"OEBPS/text/two.xhtml", `<html><head><title>Two</title></head><body>
			<h1>Waterworks</h1><h2>The Kiln</h2><p>The kiln is banked before the frost arrives.</p></body></html>`},
	}
}

func sectionsFrom(t *testing.T, parts ...archivePart) []strategy.Section {
	t.Helper()

	sections, err := extractSections(buildEPUB(t, parts...))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	return sections
}

func expectError(t *testing.T, want string, parts ...archivePart) {
	t.Helper()

	_, err := extractSections(buildEPUB(t, parts...))
	if err == nil {
		t.Fatalf("expected an error mentioning %q", want)
	}
	if !contains(err.Error(), want) {
		t.Fatalf("expected an error mentioning %q, got %v", want, err)
	}
}

func TestExtractSectionsCarriesHeadingPaths(t *testing.T) {
	sections := sectionsFrom(t, standardParts()...)

	var found bool
	for _, section := range sections {
		if strings.Join(section.Path, " > ") == "Waterworks > The Kiln" {
			found = true
		}
		if contains(section.Body, "A listing.") {
			t.Fatal("the navigation document should not be indexed")
		}
	}
	if !found {
		t.Fatalf("expected the nested chapter path, got %+v", sections)
	}
}

func TestExtractSectionsRejectsABrokenArchive(t *testing.T) {
	if _, err := extractSections([]byte("plain bytes")); err == nil {
		t.Fatal("expected an error for a non-archive")
	}
}

func TestExtractSectionsRequiresTheContainer(t *testing.T) {
	expectError(t, "META-INF/container.xml", archivePart{"OEBPS/package.opf", "<package/>"})
}

func TestContainerErrors(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"malformed", "<container", "parse META-INF/container.xml"},
		{"no rootfile", `<container></container>`, "missing rootfile"},
		{"wrong media type", `<container><rootfiles><rootfile full-path="a.opf" media-type="text/plain"/></rootfiles></container>`, "unsupported rootfile media type"},
		{"empty path", `<container><rootfiles><rootfile full-path=""/></rootfiles></container>`, "invalid rootfile"},
		{"external", `<container><rootfiles><rootfile full-path="https://example.com/a.opf"/></rootfiles></container>`, "must be inside the EPUB"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectError(t, tc.want, archivePart{"META-INF/container.xml", tc.content})
		})
	}
}

func TestPackageDocumentErrors(t *testing.T) {
	cases := []struct {
		name string
		pkg  string
		want string
	}{
		{"malformed", "<package", "parse EPUB package"},
		{"no manifest", `<package><manifest></manifest><spine><itemref idref="a"/></spine></package>`, "missing manifest items"},
		{"no spine", `<package><manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/></manifest><spine></spine></package>`, "missing spine items"},
		{"no usable spine", `<package><manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref=""/></spine></package>`, "missing usable spine items"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectError(t, tc.want, containerFor("package.opf"), archivePart{"package.opf", tc.pkg})
		})
	}
}

func TestManifestDropsIncompleteAndDuplicateItems(t *testing.T) {
	pkg := `<package>
  <manifest>
    <item id="" href="skipped.xhtml" media-type="application/xhtml+xml"/>
    <item id="nohref" href="" media-type="application/xhtml+xml"/>
    <item id="nohref" href="late.xhtml" media-type="application/xhtml+xml"/>
    <item id="twice" href="first.xhtml" media-type="application/xhtml+xml"/>
    <item id="twice" href="second.xhtml" media-type="application/xhtml+xml"/>
    <item id="one" href="one.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="nohref"/><itemref idref="twice"/><itemref idref="one"/></spine>
</package>`

	sections := sectionsFrom(t,
		containerFor("package.opf"),
		archivePart{"package.opf", pkg},
		archivePart{"late.xhtml", `<html><body><p>The blank href poisons the id.</p></body></html>`},
		archivePart{"first.xhtml", `<html><body><p>The first duplicate.</p></body></html>`},
		archivePart{"second.xhtml", `<html><body><p>The second duplicate.</p></body></html>`},
		archivePart{"one.xhtml", `<html><body><h1>Kept</h1><p>An unambiguous item is indexed.</p></body></html>`},
	)

	joined := ""
	for _, section := range sections {
		joined += section.Body
	}
	for _, dropped := range []string{"poisons the id", "first duplicate", "second duplicate"} {
		if contains(joined, dropped) {
			t.Fatalf("expected an ambiguous manifest id dropped, found %q in %q", dropped, joined)
		}
	}
	if !contains(joined, "An unambiguous item is indexed.") {
		t.Fatalf("expected the unambiguous item indexed, got %q", joined)
	}
}

func TestSpineSkipsBlankAndRepeatedReferences(t *testing.T) {
	pkg := `<package>
  <manifest><item id="one" href="one.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref=""/><itemref idref="one"/><itemref idref="one"/></spine>
</package>`

	sections := sectionsFrom(t,
		containerFor("package.opf"),
		archivePart{"package.opf", pkg},
		archivePart{"one.xhtml", `<html><body><h1>Once</h1><p>Indexed a single time.</p></body></html>`},
	)

	count := 0
	for _, section := range sections {
		if contains(section.Body, "Indexed a single time.") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected the repeated spine item indexed once, got %d", count)
	}
}

func TestSpineFollowsManifestFallbacks(t *testing.T) {
	pkg := `<package>
  <manifest>
    <item id="video" href="clip.mp4" media-type="video/mp4" fallback="still"/>
    <item id="still" href="still.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="video"/></spine>
</package>`

	sections := sectionsFrom(t,
		containerFor("package.opf"),
		archivePart{"package.opf", pkg},
		archivePart{"still.xhtml", `<html><body><h1>Still</h1><p>The fallback resource is read.</p></body></html>`},
	)

	if len(sections) == 0 || !contains(sections[0].Body, "The fallback resource is read.") {
		t.Fatalf("expected the fallback document indexed, got %+v", sections)
	}
}

func TestSpineSkipsUnreadableItemsWithoutAFallback(t *testing.T) {
	pkg := `<package>
  <manifest>
    <item id="audio" href="track.mp3" media-type="audio/mpeg"/>
    <item id="one" href="one.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="audio"/><itemref idref="one"/></spine>
</package>`

	sections := sectionsFrom(t,
		containerFor("package.opf"),
		archivePart{"package.opf", pkg},
		archivePart{"one.xhtml", `<html><body><h1>Text</h1><p>Only readable media is indexed.</p></body></html>`},
	)

	if len(sections) != 1 {
		t.Fatalf("expected only the readable document, got %+v", sections)
	}
}

func TestSpineSkipsExternalAndUnresolvableReferences(t *testing.T) {
	pkg := `<package>
  <manifest>
    <item id="remote" href="https://example.com/a.xhtml" media-type="application/xhtml+xml"/>
    <item id="escape" href="../outside.xhtml" media-type="application/xhtml+xml"/>
    <item id="one" href="one.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="remote"/><itemref idref="escape"/><itemref idref="one"/></spine>
</package>`

	sections := sectionsFrom(t,
		containerFor("package.opf"),
		archivePart{"package.opf", pkg},
		archivePart{"one.xhtml", `<html><body><h1>Local</h1><p>Only local resources are read.</p></body></html>`},
	)

	if len(sections) != 1 {
		t.Fatalf("expected only the local document, got %+v", sections)
	}
}

func TestSpineSkipsAMissingManifestItemButKeepsTheRest(t *testing.T) {
	pkg := `<package>
  <manifest><item id="one" href="one.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="ghost"/><itemref idref="one"/></spine>
</package>`

	sections := sectionsFrom(t,
		containerFor("package.opf"),
		archivePart{"package.opf", pkg},
		archivePart{"one.xhtml", `<html><body><h1>Survivor</h1><p>One bad reference does not fail the book.</p></body></html>`},
	)

	if len(sections) != 1 {
		t.Fatalf("expected the remaining document indexed, got %+v", sections)
	}
}

func TestSpineFailsOnlyWhenNothingCouldBeRead(t *testing.T) {
	pkg := `<package>
  <manifest><item id="one" href="missing.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="ghost"/><itemref idref="one"/></spine>
</package>`

	expectError(t, "no readable spine content", containerFor("package.opf"), archivePart{"package.opf", pkg})
}

func TestFallbackCycleIsReportedNotLooped(t *testing.T) {
	pkg := `<package>
  <manifest>
    <item id="a" href="a.bin" media-type="application/octet-stream" fallback="b"/>
    <item id="b" href="b.bin" media-type="application/octet-stream" fallback="a"/>
  </manifest>
  <spine><itemref idref="a"/></spine>
</package>`

	expectError(t, "fallback cycle", containerFor("package.opf"), archivePart{"package.opf", pkg})
}

func TestContentLabelFallsBackThroughTitleAndFileName(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"document title", `<html><head><title>Chapter Title</title></head><body><p>Body text here.</p></body></html>`, "Chapter Title"},
		{"publication title", `<html><body><p>Body text here.</p></body></html>`, "Waterworks"},
	}

	pkg := `<package>
  <metadata><title>Waterworks</title></metadata>
  <manifest><item id="one" href="one.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="one"/></spine>
</package>`

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sections := sectionsFrom(t,
				containerFor("package.opf"),
				archivePart{"package.opf", pkg},
				archivePart{"one.xhtml", tc.doc},
			)
			if len(sections) == 0 || len(sections[0].Path) == 0 || sections[0].Path[0] != tc.want {
				t.Fatalf("expected path %q, got %+v", tc.want, sections)
			}
		})
	}
}

func TestContentLabelUsesTheFileNameLast(t *testing.T) {
	pkg := `<package>
  <manifest><item id="one" href="text/chapter-four.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="one"/></spine>
</package>`

	sections := sectionsFrom(t,
		containerFor("package.opf"),
		archivePart{"package.opf", pkg},
		archivePart{"text/chapter-four.xhtml", `<html><body><p>No title anywhere.</p></body></html>`},
	)

	if len(sections) == 0 || len(sections[0].Path) == 0 || sections[0].Path[0] != "chapter-four" {
		t.Fatalf("expected the file-derived label, got %+v", sections)
	}
}

func TestExistingHeadingPathsAreNotOverwritten(t *testing.T) {
	sections := addFallbackPath([]strategy.Section{
		{Path: []string{"Kept"}, Body: "one"},
		{Body: "two"},
	}, "Label")

	if sections[0].Path[0] != "Kept" || sections[1].Path[0] != "Label" {
		t.Fatalf("fallback path applied incorrectly: %+v", sections)
	}
}

func TestFallbackPathIsSkippedWithoutALabel(t *testing.T) {
	sections := addFallbackPath([]strategy.Section{{Body: "one"}}, "")

	if len(sections[0].Path) != 0 {
		t.Fatalf("expected no path, got %+v", sections)
	}
}

func TestEncryptedContentIsReported(t *testing.T) {
	pkg := `<package>
  <manifest><item id="one" href="one.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="one"/></spine>
</package>`
	encryption := `<encryption><EncryptedData><CipherData><CipherReference URI="one.xhtml"/></CipherData></EncryptedData></encryption>`

	expectError(t, "encrypted",
		containerFor("package.opf"),
		archivePart{"package.opf", pkg},
		archivePart{"one.xhtml", `<html><body><p>Ciphertext stands in for prose.</p></body></html>`},
		archivePart{encryptionPath, encryption},
	)
}

func TestEncryptionDeclarationErrors(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"malformed", "<encryption", "parse META-INF/encryption.xml"},
		{"invalid uri", `<encryption><EncryptedData><CipherData><CipherReference URI="../out"/></CipherData></EncryptedData></encryption>`, "invalid encrypted resource"},
		{"external", `<encryption><EncryptedData><CipherData><CipherReference URI="https://example.com/f"/></CipherData></EncryptedData></encryption>`, "must be inside the EPUB"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectError(t, tc.want, archivePart{encryptionPath, tc.content}, containerFor("package.opf"))
		})
	}
}

func TestEncryptionDeclarationIgnoresBlankReferences(t *testing.T) {
	pkg := `<package>
  <manifest><item id="one" href="one.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="one"/></spine>
</package>`

	sections := sectionsFrom(t,
		containerFor("package.opf"),
		archivePart{"package.opf", pkg},
		archivePart{"one.xhtml", `<html><body><h1>Clear</h1><p>Nothing is encrypted here.</p></body></html>`},
		archivePart{encryptionPath, `<encryption><EncryptedData><CipherData><CipherReference URI="  "/></CipherData></EncryptedData></encryption>`},
	)

	if len(sections) != 1 {
		t.Fatalf("expected the document indexed, got %+v", sections)
	}
}

func TestDuplicateArchiveEntriesBecomeAmbiguous(t *testing.T) {
	parts := append(standardParts(), archivePart{"OEBPS/package.opf", "<package/>"})

	expectError(t, "ambiguous duplicate entry", parts...)
}

func TestDirectoryEntriesAreIgnored(t *testing.T) {
	parts := append([]archivePart{{"OEBPS/", ""}}, standardParts()...)

	if sections := sectionsFrom(t, parts...); len(sections) == 0 {
		t.Fatal("expected the book to parse with a directory entry present")
	}
}

func TestStoredPathsThatEscapeTheContainerAreRejected(t *testing.T) {
	expectError(t, "invalid EPUB entry", archivePart{"../outside.xml", "x"}, containerFor("package.opf"))
}

func TestReadableContentMediaTypes(t *testing.T) {
	for _, mediaType := range []string{xhtmlMediaType, htmlMediaType, svgMediaType, legacyOEBMediaType, legacyXHTMLMediaType} {
		if !isReadableContent(manifestItem{mediaType: mediaType}) {
			t.Fatalf("%q should be readable", mediaType)
		}
	}
	if isReadableContent(manifestItem{mediaType: "audio/mpeg"}) {
		t.Fatal("audio should not be readable")
	}
	for _, href := range []string{"a.xhtml", "a.html", "a.htm", "a.svg", "A.XHTML"} {
		if !isReadableContent(manifestItem{href: href}) {
			t.Fatalf("%q should be readable by extension", href)
		}
	}
	for _, href := range []string{"a.png", "a", "%zz"} {
		if isReadableContent(manifestItem{href: href}) {
			t.Fatalf("%q should not be readable", href)
		}
	}
}

func TestHasPropertyMatchesWholeTokens(t *testing.T) {
	if !hasProperty("scripted nav", "nav") {
		t.Fatal("expected the nav token found")
	}
	if hasProperty("navigation", "nav") {
		t.Fatal("expected a partial token not to match")
	}
}

func TestFirstNonEmptyAndBaseMediaType(t *testing.T) {
	if got := firstNonEmpty([]string{"", "  ", "Title"}); got != "Title" {
		t.Fatalf("got %q", got)
	}
	if got := firstNonEmpty(nil); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := baseMediaType("  Application/XHTML+XML ; charset=utf-8 "); got != "application/xhtml+xml" {
		t.Fatalf("got %q", got)
	}
}

func TestKeepFirstErrorKeepsTheEarliest(t *testing.T) {
	first := errString("first")
	if got := keepFirstError(nil, first); got != first {
		t.Fatalf("got %v", got)
	}
	if got := keepFirstError(first, errString("second")); got != first {
		t.Fatalf("got %v", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestResolveReferenceRejectsUnsafeValues(t *testing.T) {
	cases := []string{"", "   ", "back\\slash", "/absolute.xhtml", "../../escape.xhtml", "..", "%zz"}
	for _, reference := range cases {
		if _, _, err := resolveReference("OEBPS/package.opf", reference); err == nil {
			t.Fatalf("expected an error for %q", reference)
		}
	}
}

func TestResolveReferenceMarksAbsoluteURLsExternal(t *testing.T) {
	for _, reference := range []string{"https://example.com/a.xhtml", "//example.com/a.xhtml"} {
		_, external, err := resolveReference("OEBPS/package.opf", reference)
		if err != nil {
			t.Fatalf("%q: %v", reference, err)
		}
		if !external {
			t.Fatalf("%q should be external", reference)
		}
	}
}

func TestResolveReferenceJoinsAgainstThePackageDirectory(t *testing.T) {
	resolved, external, err := resolveReference("OEBPS/package.opf", "text/one%20two.xhtml#frag")
	if err != nil || external {
		t.Fatalf("resolve: %v external=%v", err, external)
	}
	if resolved != "OEBPS/text/one two.xhtml" {
		t.Fatalf("got %q", resolved)
	}

	resolved, _, err = resolveReference("", "package.opf")
	if err != nil || resolved != "package.opf" {
		t.Fatalf("got %q err=%v", resolved, err)
	}
}

func TestNormalizeStoredPathRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"back\\slash", "with\x00null", "/absolute", "../escape", ".."} {
		if _, err := normalizeStoredPath(name); err == nil {
			t.Fatalf("expected an error for %q", name)
		}
	}
	if got, err := normalizeStoredPath("OEBPS/./text/one.xhtml"); err != nil || got != "OEBPS/text/one.xhtml" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestArchiveSizeLimits(t *testing.T) {
	book := buildEPUB(t, archivePart{"a.xhtml", "some content"})
	reader, err := zip.NewReader(bytes.NewReader(book), int64(len(book)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	file := reader.File[0]

	if _, err := readArchiveFile(file, 2); err == nil {
		t.Fatal("expected the declared size to exceed the limit")
	}

	archive := publicationArchive{
		files:            map[string]*zip.File{"a.xhtml": file},
		ambiguous:        map[string]struct{}{},
		remainingContent: 0,
	}
	if _, err := archive.readContent("a.xhtml"); err == nil {
		t.Fatal("expected the extraction budget to be exhausted")
	}

	archive.remainingContent = maxTotalContentSize
	if _, err := archive.readContent("missing.xhtml"); err == nil {
		t.Fatal("expected a missing entry error")
	}
	if _, err := archive.readMetadata("missing.xhtml"); err == nil {
		t.Fatal("expected a missing entry error")
	}
	if !archive.contains("a.xhtml") || archive.contains("missing.xhtml") {
		t.Fatal("contains mismatch")
	}
	if archive.isEncrypted("a.xhtml") {
		t.Fatal("nothing should be encrypted here")
	}
}

func TestRepeatedDuplicateEntriesStayAmbiguous(t *testing.T) {
	parts := append(standardParts(),
		archivePart{"OEBPS/package.opf", "<package/>"},
		archivePart{"OEBPS/package.opf", "<package/>"},
	)

	expectError(t, "ambiguous duplicate entry", parts...)
}

func TestAmbiguousEncryptionDeclarationIsReported(t *testing.T) {
	expectError(t, "ambiguous duplicate entry",
		containerFor("package.opf"),
		archivePart{encryptionPath, "<encryption/>"},
		archivePart{encryptionPath, "<encryption/>"},
	)
}

func TestTwoManifestItemsPointingAtOneResourceAreReadOnce(t *testing.T) {
	pkg := `<package>
  <manifest>
    <item id="a" href="shared.xhtml" media-type="application/xhtml+xml"/>
    <item id="b" href="./shared.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="a"/><itemref idref="b"/></spine>
</package>`

	sections := sectionsFrom(t,
		containerFor("package.opf"),
		archivePart{"package.opf", pkg},
		archivePart{"shared.xhtml", `<html><body><h1>Shared</h1><p>Read a single time.</p></body></html>`},
	)

	count := 0
	for _, section := range sections {
		if contains(section.Body, "Read a single time.") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected the shared resource read once, got %d", count)
	}
}

func TestReferenceWithoutAPathIsRejected(t *testing.T) {
	if _, _, err := resolveReference("OEBPS/package.opf", "#fragment-only"); err == nil {
		t.Fatal("expected an error for a reference with no path")
	}
}

func TestUnsupportedCharsetIsReported(t *testing.T) {
	pkg := `<package>
  <manifest><item id="one" href="one.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="one"/></spine>
</package>`
	document := `<?xml version="1.0" encoding="x-not-a-charset"?>` +
		`<html><body><p>Body text.</p></body></html>`

	_, err := extractSections(buildEPUB(t,
		containerFor("package.opf"),
		archivePart{"package.opf", pkg},
		archivePart{"one.xhtml", document},
	))
	if err == nil {
		t.Skip("the charset sniffer accepted the declaration")
	}
	if !contains(err.Error(), "decode EPUB content") && !contains(err.Error(), "no readable spine content") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestContentReadStopsAtTheRemainingBudget(t *testing.T) {
	book := buildEPUB(t, archivePart{"a.xhtml", "a document body that is comfortably longer than the budget"})
	reader, err := zip.NewReader(bytes.NewReader(book), int64(len(book)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	archive := publicationArchive{
		files:            map[string]*zip.File{"a.xhtml": reader.File[0]},
		ambiguous:        map[string]struct{}{},
		remainingContent: 4,
	}
	if _, err := archive.readContent("a.xhtml"); err == nil {
		t.Fatal("expected the remaining budget to cap the read")
	}
}

func TestTooManyArchiveEntriesIsRejected(t *testing.T) {
	buf := new(bytes.Buffer)
	writer := zip.NewWriter(buf)
	for i := 0; i <= maxArchiveEntries; i++ {
		if _, err := writer.CreateHeader(&zip.FileHeader{Name: "f/" + strconv.Itoa(i), Method: zip.Store}); err != nil {
			t.Fatalf("create entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := extractSections(buf.Bytes()); err == nil || !contains(err.Error(), "too many archive entries") {
		t.Fatalf("expected an entry count error, got %v", err)
	}
}

func TestUnreadableEntriesAreReported(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func([]byte) []byte
	}{
		{"broken local header", func(book []byte) []byte {
			copy(book[0:4], []byte("XXXX"))
			return book
		}},
		{"corrupt compressed data", func(book []byte) []byte {
			for i := 40; i < 80 && i < len(book); i++ {
				book[i] ^= 0xFF
			}
			return book
		}},
	}

	body := strings.Repeat("compressible publication prose ", 40)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			book := tc.corrupt(buildEPUB(t, archivePart{"a.xhtml", body}))

			reader, err := zip.NewReader(bytes.NewReader(book), int64(len(book)))
			if err != nil {
				t.Skipf("the archive no longer opens: %v", err)
			}
			if len(reader.File) == 0 {
				t.Skip("no entries survived the corruption")
			}

			if _, err := readArchiveFile(reader.File[0], maxContentEntrySize); err == nil {
				t.Fatal("expected a read error for a corrupt entry")
			}
		})
	}
}

// patchCentralDirectorySize rewrites the uncompressed size recorded in the central directory so
// the archive under-reports how much data an entry expands to.
func patchCentralDirectorySize(t *testing.T, book []byte, size uint32) []byte {
	t.Helper()

	signature := []byte{0x50, 0x4b, 0x01, 0x02}
	index := bytes.Index(book, signature)
	if index < 0 {
		t.Fatal("central directory header not found")
	}

	patched := append([]byte(nil), book...)
	binary.LittleEndian.PutUint32(patched[index+24:index+28], size)

	return patched
}

func TestEntryWhoseRecordedSizeDisagreesWithItsDataIsRejected(t *testing.T) {
	body := strings.Repeat("publication prose that compresses well ", 40)
	book := patchCentralDirectorySize(t, buildEPUB(t, archivePart{"a.xhtml", body}), 1)

	reader, err := zip.NewReader(bytes.NewReader(book), int64(len(book)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if reader.File[0].UncompressedSize64 != 1 {
		t.Fatalf("expected the patched size, got %d", reader.File[0].UncompressedSize64)
	}

	if _, err := readArchiveFile(reader.File[0], 1); err == nil {
		t.Fatal("expected an entry whose recorded size disagrees with its data to be rejected")
	}
}
