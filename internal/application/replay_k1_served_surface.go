package application

import (
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/replay"
)

// ServedReplaySurface exposes the deterministic workflow replay core through
// the application boundary used by hcmnext serve. The replay package remains
// the semantic owner; this surface only carries its value-owned operations
// into the composed application.
type ServedReplaySurface struct {
	New               func(replay.Options) (*replay.Replayer, error)
	NewMemorySource   func(replay.Record) *replay.MemorySource
	NewRecorder       func(replay.Record) *replay.Recorder
	DefaultCandidates func() replay.Candidates
	Recomputable      func(workflow.CompiledNode) bool
	CodeOf            func(error) string
	NodeOf            func(error) string
}

// NewServedReplaySurface returns the replay capabilities reachable from the
// served application. It creates no process-wide or tenant-wide state.
func NewServedReplaySurface() ServedReplaySurface {
	return ServedReplaySurface{
		New:               replay.New,
		NewMemorySource:   replay.NewMemorySource,
		NewRecorder:       replay.NewRecorder,
		DefaultCandidates: replay.DefaultCandidates,
		Recomputable:      replay.Recomputable,
		CodeOf:            replay.CodeOf,
		NodeOf:            replay.NodeOf,
	}
}

// Replay returns the replay capabilities exposed by a composed application. A
// nil application has no served capabilities.
func (a *App) Replay() ServedReplaySurface {
	if a == nil {
		return ServedReplaySurface{}
	}
	return NewServedReplaySurface()
}
