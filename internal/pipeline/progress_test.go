package pipeline

import (
	"context"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
)

type progressCall struct {
	phase Phase
	done  int
	total int
}

// recorder collects every reported call so a test can assert the whole sequence.
func recorder(calls *[]progressCall) Progress {
	return func(phase Phase, done int, total int) {
		*calls = append(*calls, progressCall{phase: phase, done: done, total: total})
	}
}

func TestProgressTrackerStartsAndAdvances(t *testing.T) {
	var calls []progressCall
	tracker := NewProgressTracker(recorder(&calls))

	tracker.Start(PhaseIndexing, 2)
	tracker.Advance()
	tracker.Advance()

	want := []progressCall{
		{PhaseIndexing, 0, 2},
		{PhaseIndexing, 1, 2},
		{PhaseIndexing, 2, 2},
	}
	if len(calls) != len(want) {
		t.Fatalf("call count: got %d, want %d (%v)", len(calls), len(want), calls)
	}
	for i, call := range calls {
		if call != want[i] {
			t.Fatalf("call %d: got %+v, want %+v", i, call, want[i])
		}
	}
}

// Each phase counts its own set of documents, so starting one resets done.
func TestProgressTrackerStartResetsDone(t *testing.T) {
	var calls []progressCall
	tracker := NewProgressTracker(recorder(&calls))

	tracker.Start(PhaseIndexing, 5)
	tracker.Advance()
	tracker.Start(PhaseCleanup, 3)

	last := calls[len(calls)-1]
	if last != (progressCall{PhaseCleanup, 0, 3}) {
		t.Fatalf("after Start: got %+v, want {cleanup 0 3}", last)
	}
}

// A nil tracker and a nil callback are both valid: the pipelines pass whatever they are
// given straight through, so neither may panic.
func TestProgressTrackerNilIsSafe(t *testing.T) {
	var tracker *ProgressTracker
	tracker.Start(PhaseIndexing, 1)
	tracker.Advance()

	NewProgressTracker(nil).Advance()
}

// A document is counted where it finishes: markEmbedded, at the end of the process pipeline.
func TestProcessAdvancesProgressOnEmbedded(t *testing.T) {
	path := writeFile(t, "abcdefg")
	store := &memoryStore{
		documents: []storage.Document{{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusScanned}},
		chunks:    map[int64][]storage.Chunk{},
		nextID:    100,
	}
	var calls []progressCall
	tracker := NewProgressTracker(recorder(&calls))
	tracker.Start(PhaseIndexing, 1)

	if err := Process(context.Background(), store, &memoryVectorStore{}, strategy.NewPool(fakeStrategy{maxRunes: 3}), false, tracker); err != nil {
		t.Fatalf("process: %v", err)
	}

	last := calls[len(calls)-1]
	if last != (progressCall{PhaseIndexing, 1, 1}) {
		t.Fatalf("after process: got %+v, want {indexing 1 1}", last)
	}
}

// A document that fails is left for the next run, so it is never counted as finished and the
// phase ends below its total.
func TestProcessDoesNotAdvanceProgressOnFailure(t *testing.T) {
	store := &memoryStore{
		documents: []storage.Document{{ID: 42, FileID: "1:100", AbsolutePath: "/nonexistent/note.md", Status: storage.DocumentStatusScanned}},
		chunks:    map[int64][]storage.Chunk{},
		nextID:    100,
	}
	var calls []progressCall
	tracker := NewProgressTracker(recorder(&calls))
	tracker.Start(PhaseIndexing, 1)

	if err := Process(context.Background(), store, &memoryVectorStore{}, strategy.NewPool(fakeStrategy{maxRunes: 3}), false, tracker); err == nil {
		t.Fatal("process: want an error for a missing file")
	}

	last := calls[len(calls)-1]
	if last != (progressCall{PhaseIndexing, 0, 1}) {
		t.Fatalf("after failed process: got %+v, want {indexing 0 1}", last)
	}
}

// Cleanup reports no total — the set of stored documents is only known to the database — so it
// enters with 0 and done just rises as documents are stat'ed.
func TestCleanupStartsPhaseAndCountsEveryDocument(t *testing.T) {
	present := writeFile(t, "still here")
	store := &fakeCleanupStore{
		documents: []storage.Document{
			{ID: 1, AbsolutePath: present},
			{ID: 2, AbsolutePath: "/nonexistent/gone.md"},
		},
		chunks: map[int64][]storage.Chunk{},
	}
	var calls []progressCall

	if err := Cleanup(context.Background(), store, &fakeCleanupVectorStore{}, false, NewProgressTracker(recorder(&calls))); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if calls[0] != (progressCall{PhaseCleanup, 0, 0}) {
		t.Fatalf("on enter: got %+v, want {cleanup 0 0}", calls[0])
	}
	last := calls[len(calls)-1]
	if last != (progressCall{PhaseCleanup, 2, 0}) {
		t.Fatalf("after cleanup: got %+v, want {cleanup 2 0}", last)
	}
}
