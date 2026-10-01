package governance

import "github.com/monstercameron/human-capital-management-suite/internal/governance/pilot"

// PilotRuntime is the serving boundary for the evidence-bound pilot review.
// It carries no mutable state: each review supplies its complete evidence
// snapshot, so a composed cell cannot accidentally reuse an earlier verdict.
type PilotRuntime struct{}

// NewPilotRuntime returns the pilot review boundary for one composed process.
func NewPilotRuntime() PilotRuntime { return PilotRuntime{} }

// Review evaluates one complete pilot evidence package at the serving
// boundary. The evaluator remains pure and is shared by offline and serving
// callers without adding authority to the review itself.
func (PilotRuntime) Review(input pilot.ReviewInput) (pilot.DecisionPackage, error) {
	return pilot.Review(input)
}

// PilotRuntime exposes the pilot boundary from a composed governance result.
// ComposeResult is already carried by the intent preflight path used by the
// serving cell, making the review reachable without a test-only import.
func (ComposeResult) PilotRuntime() PilotRuntime { return NewPilotRuntime() }
