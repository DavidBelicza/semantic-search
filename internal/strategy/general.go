package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/davidbelicza/semantic-search/internal/embedder"
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
		FileID:       fileID(absolutePath, file.Info),
		SizeBytes:    file.Info.Size(),
		ModifiedAtNS: file.Info.ModTime().UnixNano(),
	}, nil
}

func (GeneralStrategy) Fingerprint(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func (GeneralStrategy) Parse(content []byte) (string, error) {
	return string(content), nil
}

func (GeneralStrategy) Chunk(_ storage.Document, text string) ([]storage.Chunk, error) {
	windows := textproc.HardWindow(text, generalMaxTokens*textproc.DefaultAverageTokenLength)

	chunks := make([]storage.Chunk, 0, len(windows))
	offset := 0
	for _, window := range windows {
		chunks = append(chunks, storage.Chunk{
			ChunkIndex:  len(chunks),
			Text:        window,
			TokenCount:  textproc.EstimateTokenCount(window, textproc.DefaultAverageTokenLength),
			StartOffset: offset,
			EndOffset:   offset + len([]rune(window)),
			ContentHash: textproc.HashText(window),
		})
		offset += len([]rune(window))
	}

	return chunks, nil
}

func (s GeneralStrategy) Embed(ctx context.Context, chunks []storage.Chunk) ([][]float32, error) {
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = embedder.DocumentInput(chunk.Title, chunk.Text)
	}

	return s.embedder.Embed(ctx, texts)
}
