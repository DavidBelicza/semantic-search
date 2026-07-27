package pipeline

import (
	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
)

// embedItem is one document's chunks waiting to be embedded, kept with the strategy that
// embeds them and the document to mark once they are stored.
type embedItem struct {
	strategy strategy.Strategy
	document storage.Document
	chunks   []storage.Chunk
}

// embedBuffer collects embedItems across documents until they hold at least limit chunks, so
// chunks from several small files go to the embedding server in one request. It owns the
// batching decision — when to hand a batch over — and nothing else; embedding and writing are
// the pipeline's job.
type embedBuffer struct {
	items  []embedItem
	chunks int
	limit  int
}

func newEmbedBuffer(limit int) *embedBuffer {
	return &embedBuffer{limit: limit}
}

// add appends an item and reports whether the buffer has reached its limit. A document's
// chunks are never split, so the buffer may pass the limit by up to one document.
func (b *embedBuffer) add(item embedItem) (full bool) {
	b.items = append(b.items, item)
	b.chunks += len(item.chunks)

	return b.chunks >= b.limit
}

// Enqueue adds an item and returns a batch to embed once the buffer is full, or nil while it
// still has room.
func (b *embedBuffer) Enqueue(item embedItem) []embedItem {
	if full := b.add(item); !full {
		return nil
	}

	return b.Flush()
}

// Flush returns the buffered items and empties the buffer.
func (b *embedBuffer) Flush() []embedItem {
	items := b.items
	b.items, b.chunks = nil, 0

	return items
}
