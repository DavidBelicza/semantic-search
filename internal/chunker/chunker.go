package chunker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"

	"semantic-search/internal/storage"
)

const (
	DefaultMaxTokens          = 300
	DefaultAverageTokenLength = 4
	chunkBatchSize            = 1
)

type Input struct {
	Document storage.Document
	Text     string
}

type Strategy interface {
	Chunk(ctx context.Context, input Input) ([]storage.Chunk, error)
}

type Store interface {
	DocumentsByStatus(ctx context.Context, status string, limit int) ([]storage.Document, error)
	ReplaceDocumentChunksAndStatus(ctx context.Context, documentID int64, chunks []storage.Chunk, status string) error
}

type Result struct {
	Chunked int
}

type HardLimitChunker struct {
	MaxTokens          int
	AverageTokenLength int
}

func NewHardLimitChunker(maxTokens int) HardLimitChunker {
	return HardLimitChunker{
		MaxTokens:          maxTokens,
		AverageTokenLength: DefaultAverageTokenLength,
	}
}

func (c HardLimitChunker) Chunk(ctx context.Context, input Input) ([]storage.Chunk, error) {
	maxTokens := c.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}

	averageTokenLength := c.AverageTokenLength
	if averageTokenLength <= 0 {
		averageTokenLength = DefaultAverageTokenLength
	}

	maxRunes := maxTokens * averageTokenLength
	if maxRunes <= 0 {
		return nil, fmt.Errorf("invalid chunk size")
	}

	runes := []rune(input.Text)
	chunks := make([]storage.Chunk, 0, int(math.Ceil(float64(len(runes))/float64(maxRunes))))
	for start := 0; start < len(runes); start += maxRunes {
		end := start + maxRunes
		if end > len(runes) {
			end = len(runes)
		}

		text := string(runes[start:end])
		chunks = append(chunks, storage.Chunk{
			ChunkIndex:  len(chunks),
			Text:        text,
			TokenCount:  EstimateTokenCount(text, averageTokenLength),
			StartOffset: start,
			EndOffset:   end,
			ContentHash: HashText(text),
		})
	}

	return chunks, nil
}

func ProcessScannedDocuments(ctx context.Context, store Store, strategy Strategy) (Result, error) {
	var result Result
	if strategy == nil {
		strategy = NewHardLimitChunker(DefaultMaxTokens)
	}

	for {
		documents, err := store.DocumentsByStatus(ctx, storage.DocumentStatusScanned, chunkBatchSize)
		if err != nil {
			return result, err
		}
		if len(documents) == 0 {
			return result, nil
		}

		for _, document := range documents {
			text, err := readTextFile(document.AbsolutePath)
			if err != nil {
				return result, fmt.Errorf("read document %q: %w", document.AbsolutePath, err)
			}

			chunks, err := strategy.Chunk(ctx, Input{Document: document, Text: text})
			if err != nil {
				return result, fmt.Errorf("chunk document %q: %w", document.AbsolutePath, err)
			}

			if err := store.ReplaceDocumentChunksAndStatus(ctx, document.ID, chunks, storage.DocumentStatusChunked); err != nil {
				return result, fmt.Errorf("store chunks for %q: %w", document.AbsolutePath, err)
			}

			result.Chunked++
		}
	}
}

func EstimateTokenCount(text string, averageTokenLength int) int {
	if text == "" {
		return 0
	}
	if averageTokenLength <= 0 {
		averageTokenLength = DefaultAverageTokenLength
	}

	return int(math.Ceil(float64(len([]rune(text))) / float64(averageTokenLength)))
}

func HashText(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}

func readTextFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}

	return string(content), nil
}
