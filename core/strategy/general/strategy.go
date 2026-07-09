// Package general provides GeneralStrategy: the base structured strategy that parses and
// chunks any text file by generic rules. The Markdown and PDF strategies embed it, inherit
// its metadata/fingerprint/embed/chunk behaviour, and override only what their format needs.
package general

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"

	"github.com/davidbelicza/semantic-search/core/embedder"
	"github.com/davidbelicza/semantic-search/internal/fs"
	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

// GeneralStrategy claims plain-text files, builds metadata from file info, hashes content,
// treats bytes as UTF-8 text in a single section, chunks that structure-agnostically
// (paragraphs with overlap), and embeds via the injected embedder. Markdown and PDF embed it
// for these generic per-file steps and override Claims/Parse/Chunk for their format.
type GeneralStrategy struct {
	embedder strategy.Embedder
}

func NewGeneralStrategy(embedder strategy.Embedder) GeneralStrategy {
	return GeneralStrategy{embedder: embedder}
}

// Claims reports whether the path is a plain-text file this strategy handles. It is a fixed
// whitelist, not a catch-all: unknown extensions are left for other strategies (or skipped).
func (GeneralStrategy) Claims(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt", ".text", ".log", ".rst", ".org", ".adoc":
		return true
	default:
		return false
	}
}

func (GeneralStrategy) CreateMetadata(file strategy.FileRef) (storage.FileMetadata, error) {
	absolutePath := filepath.Clean(file.Path)
	return storage.FileMetadata{
		AbsolutePath: absolutePath,
		FileID:       fs.FileID(absolutePath, file.Info),
		SizeBytes:    file.Info.Size(),
		ModifiedAtNS: file.Info.ModTime().UnixNano(),
	}, nil
}

func (GeneralStrategy) Fingerprint(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func (GeneralStrategy) Parse(content []byte) (strategy.ParsedDocument, error) {
	return strategy.ParsedDocument{Sections: []strategy.Section{{Body: textproc.NormalizeText(content)}}}, nil
}

func (GeneralStrategy) Chunk(doc storage.Document, parsed strategy.ParsedDocument) ([]storage.Chunk, error) {
	return ChunkSections(parsed.Sections, SectionChunkConfig{
		MaxTokens:          defaultMaxTokens,
		OverlapTokens:      defaultOverlapTokens,
		AverageTokenLength: textproc.DefaultAverageTokenLength,
		FallbackTitle:      FileTitleFromPath(doc.AbsolutePath),
		SplitIntoParts:     splitParagraphs,
		SplitOversized:     splitOversizedProse,
	}), nil
}

func (s GeneralStrategy) Embed(ctx context.Context, chunks []storage.Chunk) ([][]float32, error) {
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = embedder.DocumentInput(chunk.Title, chunk.Text)
	}

	return s.embedder.Embed(ctx, texts)
}
