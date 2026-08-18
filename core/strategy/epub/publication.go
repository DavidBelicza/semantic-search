package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	htmlcharset "golang.org/x/net/html/charset"

	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
	"github.com/davidbelicza/semantic-search/core/strategy/markup"
)

const (
	containerPath        = "META-INF/container.xml"
	encryptionPath       = "META-INF/encryption.xml"
	packageMediaType     = "application/oebps-package+xml"
	xhtmlMediaType       = "application/xhtml+xml"
	htmlMediaType        = "text/html"
	svgMediaType         = "image/svg+xml"
	legacyOEBMediaType   = "text/x-oeb1-document"
	legacyXHTMLMediaType = "application/html+xml"
	maxArchiveEntries    = 100_000
	maxMetadataEntrySize = 8 << 20
	maxContentEntrySize  = 64 << 20
	maxTotalContentSize  = 256 << 20
)

type publicationArchive struct {
	files            map[string]*zip.File
	ambiguous        map[string]struct{}
	encrypted        map[string]struct{}
	remainingContent uint64
}

type encryptedDataXML struct {
	CipherReference struct {
		URI string `xml:"URI,attr"`
	} `xml:"CipherData>CipherReference"`
}

type encryptionXML struct {
	EncryptedData []encryptedDataXML `xml:"EncryptedData"`
}

type rootfileXML struct {
	FullPath  string `xml:"full-path,attr"`
	MediaType string `xml:"media-type,attr"`
}

type containerXML struct {
	Rootfiles []rootfileXML `xml:"rootfiles>rootfile"`
}

type manifestItemXML struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Fallback   string `xml:"fallback,attr"`
	Properties string `xml:"properties,attr"`
}

type spineItemXML struct {
	IDRef string `xml:"idref,attr"`
}

type packageXML struct {
	Metadata struct {
		Titles []string `xml:"title"`
	} `xml:"metadata"`
	Manifest struct {
		Items []manifestItemXML `xml:"item"`
	} `xml:"manifest"`
	Spine struct {
		Items []spineItemXML `xml:"itemref"`
	} `xml:"spine"`
}

type manifestItem struct {
	id         string
	href       string
	mediaType  string
	fallback   string
	properties string
}

type spineItem struct {
	idref string
}

type packageDocument struct {
	title    string
	manifest map[string]manifestItem
	spine    []spineItem
}

func extractSections(content []byte) ([]strategy.Section, error) {
	archive, err := openPublicationArchive(content)
	if err != nil {
		return nil, err
	}

	container, err := archive.readMetadata(containerPath)
	if err != nil {
		return nil, err
	}

	packagePath, err := parseContainer(container)
	if err != nil {
		return nil, err
	}

	packageData, err := archive.readMetadata(packagePath)
	if err != nil {
		return nil, err
	}

	publication, err := parsePackageDocument(packageData)
	if err != nil {
		return nil, fmt.Errorf("parse EPUB package %q: %w", packagePath, err)
	}

	return extractSpine(&archive, packagePath, publication)
}

func openPublicationArchive(content []byte) (publicationArchive, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return publicationArchive{}, fmt.Errorf("open EPUB: %w", err)
	}
	if len(reader.File) > maxArchiveEntries {
		return publicationArchive{}, fmt.Errorf("EPUB contains too many archive entries: %d", len(reader.File))
	}

	files := make(map[string]*zip.File, len(reader.File))
	ambiguous := make(map[string]struct{})
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		name, err := normalizeStoredPath(file.Name)
		if err != nil {
			return publicationArchive{}, fmt.Errorf("invalid EPUB entry %q: %w", file.Name, err)
		}
		if _, duplicate := ambiguous[name]; duplicate {
			continue
		}
		if _, exists := files[name]; exists {
			delete(files, name)
			ambiguous[name] = struct{}{}
			continue
		}
		files[name] = file
	}

	archive := publicationArchive{
		files:            files,
		ambiguous:        ambiguous,
		remainingContent: maxTotalContentSize,
	}
	encrypted, err := archive.readEncryptedPaths()
	if err != nil {
		return publicationArchive{}, err
	}
	archive.encrypted = encrypted

	return archive, nil
}

func (a publicationArchive) archiveFile(name string) (*zip.File, error) {
	if _, duplicate := a.ambiguous[name]; duplicate {
		return nil, fmt.Errorf("EPUB: ambiguous duplicate entry %s", name)
	}

	file, ok := a.files[name]
	if !ok {
		return nil, fmt.Errorf("EPUB: missing %s", name)
	}

	return file, nil
}

func (a publicationArchive) readMetadata(name string) ([]byte, error) {
	file, err := a.archiveFile(name)
	if err != nil {
		return nil, err
	}

	return readArchiveFile(file, maxMetadataEntrySize)
}

func (a *publicationArchive) readContent(name string) ([]byte, error) {
	file, err := a.archiveFile(name)
	if err != nil {
		return nil, err
	}
	if a.remainingContent == 0 {
		return nil, fmt.Errorf("EPUB content exceeds the %d-byte extraction budget", maxTotalContentSize)
	}

	limit := min(uint64(maxContentEntrySize), a.remainingContent)
	data, err := readArchiveFile(file, limit)
	if err != nil {
		return nil, err
	}
	a.remainingContent -= uint64(len(data))

	return data, nil
}

func readArchiveFile(file *zip.File, limit uint64) ([]byte, error) {
	if file.UncompressedSize64 > limit {
		return nil, fmt.Errorf("EPUB entry %q exceeds the %d-byte extraction limit", file.Name, limit)
	}

	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open EPUB entry %q: %w", file.Name, err)
	}
	defer reader.Close()

	return readLimited(reader, file.Name, limit)
}

// readLimited reads at most limit bytes, treating anything longer as an entry that outgrew the
// size its archive recorded.
func readLimited(reader io.Reader, name string, limit uint64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, fmt.Errorf("read EPUB entry %q: %w", name, err)
	}
	if uint64(len(data)) > limit {
		return nil, fmt.Errorf("EPUB entry %q exceeds the %d-byte extraction limit", name, limit)
	}

	return data, nil
}

func (a publicationArchive) readEncryptedPaths() (map[string]struct{}, error) {
	if !a.contains(encryptionPath) {
		return nil, nil
	}

	content, err := a.readMetadata(encryptionPath)
	if err != nil {
		return nil, err
	}

	var source encryptionXML
	if err := decodeXML(content, &source); err != nil {
		return nil, fmt.Errorf("parse %s: %w", encryptionPath, err)
	}

	encrypted := make(map[string]struct{}, len(source.EncryptedData))
	for _, item := range source.EncryptedData {
		if strings.TrimSpace(item.CipherReference.URI) == "" {
			continue
		}

		resourcePath, external, err := resolveReference("", item.CipherReference.URI)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid encrypted resource: %w", encryptionPath, err)
		}
		if external {
			return nil, fmt.Errorf("%s: encrypted resource must be inside the EPUB", encryptionPath)
		}
		encrypted[resourcePath] = struct{}{}
	}

	return encrypted, nil
}

func (a publicationArchive) contains(name string) bool {
	_, stored := a.files[name]
	_, ambiguous := a.ambiguous[name]

	return stored || ambiguous
}

func (a publicationArchive) isEncrypted(name string) bool {
	_, encrypted := a.encrypted[name]

	return encrypted
}

func parseContainer(content []byte) (string, error) {
	var container containerXML
	if err := decodeXML(content, &container); err != nil {
		return "", fmt.Errorf("parse %s: %w", containerPath, err)
	}
	if len(container.Rootfiles) == 0 {
		return "", fmt.Errorf("%s: missing rootfile", containerPath)
	}

	rootfile := container.Rootfiles[0]
	if mediaType := baseMediaType(rootfile.MediaType); mediaType != "" && mediaType != packageMediaType {
		return "", fmt.Errorf("%s: unsupported rootfile media type %q", containerPath, rootfile.MediaType)
	}

	packagePath, external, err := resolveReference("", rootfile.FullPath)
	if err != nil {
		return "", fmt.Errorf("%s: invalid rootfile: %w", containerPath, err)
	}
	if external {
		return "", fmt.Errorf("%s: rootfile must be inside the EPUB", containerPath)
	}

	return packagePath, nil
}

func parsePackageDocument(content []byte) (packageDocument, error) {
	var source packageXML
	if err := decodeXML(content, &source); err != nil {
		return packageDocument{}, err
	}

	manifest, err := buildManifest(source.Manifest.Items)
	if err != nil {
		return packageDocument{}, err
	}

	spine, err := buildSpine(source.Spine.Items)
	if err != nil {
		return packageDocument{}, err
	}

	return packageDocument{
		title:    firstNonEmpty(source.Metadata.Titles),
		manifest: manifest,
		spine:    spine,
	}, nil
}

func buildManifest(source []manifestItemXML) (map[string]manifestItem, error) {
	if len(source) == 0 {
		return nil, fmt.Errorf("missing manifest items")
	}

	manifest := make(map[string]manifestItem, len(source))
	ambiguous := make(map[string]struct{})
	for _, raw := range source {
		item := manifestItem{
			id:         strings.TrimSpace(raw.ID),
			href:       strings.TrimSpace(raw.Href),
			mediaType:  baseMediaType(raw.MediaType),
			fallback:   strings.TrimSpace(raw.Fallback),
			properties: raw.Properties,
		}
		if item.id == "" {
			continue
		}
		if _, duplicate := ambiguous[item.id]; duplicate {
			continue
		}
		if item.href == "" {
			delete(manifest, item.id)
			ambiguous[item.id] = struct{}{}
			continue
		}
		if _, exists := manifest[item.id]; exists {
			delete(manifest, item.id)
			ambiguous[item.id] = struct{}{}
			continue
		}
		manifest[item.id] = item
	}

	return manifest, nil
}

func buildSpine(source []spineItemXML) ([]spineItem, error) {
	if len(source) == 0 {
		return nil, fmt.Errorf("missing spine items")
	}

	spine := make([]spineItem, 0, len(source))
	seen := make(map[string]struct{}, len(source))
	for _, raw := range source {
		item := spineItem{idref: strings.TrimSpace(raw.IDRef)}
		if item.idref == "" {
			continue
		}
		if _, exists := seen[item.idref]; exists {
			continue
		}
		seen[item.idref] = struct{}{}
		spine = append(spine, item)
	}
	if len(spine) == 0 {
		return nil, fmt.Errorf("missing usable spine items")
	}

	return spine, nil
}

func extractSpine(archive *publicationArchive, packagePath string, publication packageDocument) ([]strategy.Section, error) {
	var sections []strategy.Section
	var firstFailure error
	var extractedDocuments int
	seenPaths := make(map[string]struct{})
	// Include both linear and non-linear spine items: notes, answer keys, and other auxiliary
	// content are still part of the publication and useful to semantic search.
	for _, reference := range publication.spine {
		item, supported, err := publication.readableItem(reference.idref)
		if err != nil {
			firstFailure = keepFirstError(firstFailure, fmt.Errorf("spine item %q: %w", reference.idref, err))
			continue
		}
		if !supported || hasProperty(item.properties, "nav") {
			continue
		}

		resourcePath, external, err := resolveReference(packagePath, item.href)
		if err != nil {
			firstFailure = keepFirstError(firstFailure, fmt.Errorf("resolve EPUB resource %q: %w", item.href, err))
			continue
		}
		if external {
			continue
		}
		if _, exists := seenPaths[resourcePath]; exists {
			continue
		}
		seenPaths[resourcePath] = struct{}{}

		documentSections, err := extractContentDocument(archive, resourcePath, item, publication.title)
		if err != nil {
			firstFailure = keepFirstError(firstFailure, err)
			continue
		}
		extractedDocuments++
		sections = append(sections, documentSections...)
	}
	if extractedDocuments > 0 || firstFailure == nil {
		return sections, nil
	}

	return nil, fmt.Errorf("EPUB has no readable spine content: %w", firstFailure)
}

func keepFirstError(current, next error) error {
	if current != nil {
		return current
	}

	return next
}

func (p packageDocument) readableItem(id string) (manifestItem, bool, error) {
	seen := make(map[string]struct{})
	for id != "" {
		if _, exists := seen[id]; exists {
			return manifestItem{}, false, fmt.Errorf("manifest fallback cycle at %q", id)
		}
		seen[id] = struct{}{}

		item, ok := p.manifest[id]
		if !ok {
			return manifestItem{}, false, fmt.Errorf("spine references missing manifest item %q", id)
		}
		if isReadableContent(item) {
			return item, true, nil
		}

		id = item.fallback
	}

	return manifestItem{}, false, nil
}

func extractContentDocument(archive *publicationArchive, resourcePath string, item manifestItem, publicationTitle string) ([]strategy.Section, error) {
	if archive.isEncrypted(resourcePath) {
		return nil, fmt.Errorf("EPUB content %q is encrypted and cannot be indexed", resourcePath)
	}

	content, err := archive.readContent(resourcePath)
	if err != nil {
		return nil, err
	}

	return extractFromReader(bytes.NewReader(content), item.mediaType, resourcePath, publicationTitle)
}

// extractFromReader decodes one content document to text and sections it. It takes a reader so
// the decode and parse failures stay reachable independently of the archive.
func extractFromReader(reader io.Reader, mediaType, resourcePath, publicationTitle string) ([]strategy.Section, error) {
	decoded, err := htmlcharset.NewReader(reader, mediaType)
	if err != nil {
		return nil, fmt.Errorf("decode EPUB content %q: %w", resourcePath, err)
	}

	document, err := markup.Extract(decoded, markup.PublicationMode)
	if err != nil {
		return nil, fmt.Errorf("parse EPUB content %q: %w", resourcePath, err)
	}

	label := contentLabel(document.Title, publicationTitle, resourcePath)

	return addFallbackPath(document.Sections, label), nil
}

func contentLabel(documentTitle, publicationTitle, resourcePath string) string {
	if title := strings.TrimSpace(documentTitle); title != "" {
		return title
	}
	if title := strings.TrimSpace(publicationTitle); title != "" {
		return title
	}

	return general.FileTitleFromPath(resourcePath)
}

func addFallbackPath(sections []strategy.Section, label string) []strategy.Section {
	if label == "" {
		return sections
	}

	for i := range sections {
		if len(sections[i].Path) == 0 {
			sections[i].Path = []string{label}
		}
	}

	return sections
}

func isReadableContent(item manifestItem) bool {
	switch item.mediaType {
	case xhtmlMediaType, htmlMediaType, svgMediaType, legacyOEBMediaType, legacyXHTMLMediaType:
		return true
	case "":
		return hasReadableExtension(item.href)
	default:
		return false
	}
}

func hasReadableExtension(reference string) bool {
	parsed, err := url.Parse(reference)
	if err != nil {
		return false
	}

	switch strings.ToLower(path.Ext(parsed.Path)) {
	case ".xhtml", ".html", ".htm", ".svg":
		return true
	default:
		return false
	}
}

func hasProperty(properties, wanted string) bool {
	for _, property := range strings.Fields(properties) {
		if property == wanted {
			return true
		}
	}

	return false
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

func baseMediaType(value string) string {
	mediaType, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(value)), ";")

	return strings.TrimSpace(mediaType)
}

func decodeXML(content []byte, target any) error {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	decoder.CharsetReader = htmlcharset.NewReaderLabel

	return decoder.Decode(target)
}

// resolveReference applies a package-relative URL to the container namespace. external is true
// for absolute URLs; callers skip those rather than fetching data during local indexing.
func resolveReference(baseFile, reference string) (resolved string, external bool, err error) {
	value := strings.TrimSpace(reference)
	if value == "" {
		return "", false, fmt.Errorf("empty reference")
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", false, err
	}
	if parsed.IsAbs() || parsed.Host != "" {
		return "", true, nil
	}
	if parsed.Path == "" {
		return "", false, fmt.Errorf("reference has no path")
	}

	decoded := parsed.Path
	if strings.Contains(decoded, "\\") || strings.ContainsRune(decoded, '\x00') {
		return "", false, fmt.Errorf("invalid container path %q", decoded)
	}
	if path.IsAbs(decoded) {
		return "", false, fmt.Errorf("container path must be relative: %q", decoded)
	}

	baseDir := ""
	if baseFile != "" {
		baseDir = path.Dir(baseFile)
	}
	joined := path.Clean(path.Join(baseDir, decoded))
	if escapesContainer(joined) {
		return "", false, fmt.Errorf("container path escapes the root: %q", decoded)
	}

	return joined, false, nil
}

func normalizeStoredPath(name string) (string, error) {
	if strings.Contains(name, "\\") || strings.ContainsRune(name, '\x00') {
		return "", fmt.Errorf("invalid path")
	}
	if path.IsAbs(name) {
		return "", fmt.Errorf("path must be relative")
	}

	cleaned := path.Clean(name)
	if escapesContainer(cleaned) {
		return "", fmt.Errorf("path escapes the container")
	}

	return cleaned, nil
}

func escapesContainer(name string) bool {
	return name == "." || name == ".." || strings.HasPrefix(name, "../")
}
