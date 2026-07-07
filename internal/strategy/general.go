package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"

	"github.com/davidbelicza/semantic-search/internal/embedder"
	"github.com/davidbelicza/semantic-search/internal/fs"
	storage "github.com/davidbelicza/semantic-search/internal/storage/sqlite"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

const generalMaxTokens = 300

// GeneralStrategy is a standalone strategy with format-agnostic behaviour: it claims
// every file, builds metadata from file info, hashes content, treats bytes as UTF-8
// text, chunks by a fixed token budget, and embeds via the injected embedder. Concrete
// strategies compose it and override the parts that differ.
type GeneralStrategy struct {
	embedder Embedder
}

func NewGeneralStrategy(embedder Embedder) GeneralStrategy {
	return GeneralStrategy{embedder: embedder}
}

func (GeneralStrategy) Claims(string) bool {
	return true
}

func (GeneralStrategy) CreateMetadata(file FileRef) (storage.FileMetadata, error) {
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

func (GeneralStrategy) Parse(content []byte) (textproc.ParsedDocument, error) {
	return textproc.ParsedDocument{Sections: []textproc.Section{{Body: string(content)}}}, nil
}

func (GeneralStrategy) Chunk(_ storage.Document, parsed textproc.ParsedDocument) ([]storage.Chunk, error) {
	windows := textproc.HardWindow(joinSectionBodies(parsed.Sections), generalMaxTokens*textproc.DefaultAverageTokenLength)

	parts := make([]textproc.ChunkPart, len(windows))
	for i, window := range windows {
		parts[i] = textproc.ChunkPart{Text: window}
	}

	return textproc.BuildChunks(parts, textproc.DefaultAverageTokenLength), nil
}

// joinSectionBodies concatenates section bodies into one text. GeneralStrategy is format
// agnostic, so it chunks the whole document without heading structure.
func joinSectionBodies(sections []textproc.Section) string {
	bodies := make([]string, len(sections))
	for i, section := range sections {
		bodies[i] = section.Body
	}

	return strings.Join(bodies, "\n")
}

func (s GeneralStrategy) Embed(ctx context.Context, chunks []storage.Chunk) ([][]float32, error) {
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = embedder.DocumentInput(chunk.Title, chunk.Text)
	}

	return s.embedder.Embed(ctx, texts)
}
