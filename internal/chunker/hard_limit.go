package chunker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"

	storage "semantic-search/internal/storage/sqlite"
)

const (
	DefaultMaxTokens          = 300
	DefaultAverageTokenLength = 4
)

type Input struct {
	Document storage.Document
	Text     string
}

type Strategy interface {
	Chunk(ctx context.Context, input Input) ([]storage.Chunk, error)
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
