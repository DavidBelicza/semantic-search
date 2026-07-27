package pipeline

// Phase names the stage of an index run. A run moves through them in order.
type Phase string

const (
	PhaseScanning Phase = "scanning"
	PhaseIndexing Phase = "indexing"
	PhaseCleanup  Phase = "cleanup"
)

// Progress receives an index run's counters. It is called synchronously, so a slow callback
// slows the run. total is 0 when the phase has none.
type Progress func(phase Phase, done int, total int)

// ProgressTracker carries the counters across the pipelines of one run. A nil tracker is
// valid and reports nothing.
type ProgressTracker struct {
	report Progress
	phase  Phase
	done   int
	total  int
}

func NewProgressTracker(report Progress) *ProgressTracker {
	return &ProgressTracker{report: report}
}

// Start begins a phase with its total and resets done: each phase counts its own set.
func (t *ProgressTracker) Start(phase Phase, total int) {
	if t == nil {
		return
	}

	t.phase = phase
	t.total = total
	t.done = 0
	t.emit()
}

// Advance records one finished document.
func (t *ProgressTracker) Advance() {
	if t == nil {
		return
	}

	t.done++
	t.emit()
}

func (t *ProgressTracker) emit() {
	if t.report == nil {
		return
	}

	t.report(t.phase, t.done, t.total)
}
