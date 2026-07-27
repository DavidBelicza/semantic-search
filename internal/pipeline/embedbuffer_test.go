package pipeline

import (
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
)

func item(chunks int) embedItem {
	return embedItem{chunks: make([]storage.Chunk, chunks)}
}

func TestEmbedBufferAddReportsFull(t *testing.T) {
	b := newEmbedBuffer(50)

	if b.add(item(30)) {
		t.Fatal("30 chunks should not fill a limit of 50")
	}
	if !b.add(item(30)) {
		t.Fatal("60 chunks should fill a limit of 50")
	}
}

func TestEmbedBufferEnqueueReturnsBatchWhenFull(t *testing.T) {
	b := newEmbedBuffer(50)

	if batch := b.Enqueue(item(30)); batch != nil {
		t.Fatalf("30 chunks should not flush, got %d items", len(batch))
	}
	if batch := b.Enqueue(item(30)); len(batch) != 2 {
		t.Fatalf("60 chunks should flush both items, got %d", len(batch))
	}
}

// A document's chunks are never split, so a full batch may pass the limit by a whole document.
func TestEmbedBufferOvershootsByOneDocument(t *testing.T) {
	b := newEmbedBuffer(50)

	b.Enqueue(item(49))
	batch := b.Enqueue(item(1000))

	if got := len(extractChunks(batch)); got != 1049 {
		t.Fatalf("flushed chunks: got %d, want 1049", got)
	}
}

func TestEmbedBufferFlushEmpties(t *testing.T) {
	b := newEmbedBuffer(50)
	b.add(item(3))

	if len(b.Flush()) != 1 {
		t.Fatal("flush returns the buffered items")
	}
	if len(b.Flush()) != 0 {
		t.Fatal("buffer is empty after flush")
	}
}
