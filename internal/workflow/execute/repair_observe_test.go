package execute

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
)

type failingRepairEvidence struct{ calls int }

func (f *failingRepairEvidence) RecordRepairEvidence(context.Context, RepairEvidence) error {
	f.calls++
	return errors.New("evidence store unavailable")
}

// TestRepairEvidenceWriteFailuresAreObservable proves a failed durable
// evidence write does not fail the repair but is recorded as a FAILURE
// operation for every stage, never silently discarded.
func TestRepairEvidenceWriteFailuresAreObservable(t *testing.T) {
	var rec observetest.Recorder
	sink := &failingRepairEvidence{}
	executor := newRepairExecutor(t, &repairEffectDouble{}, nil, passingRepairDecision())
	executor.opts.Evidence = sink
	result, err := executor.Execute(rec.Context(context.Background()), executeRepairRequest(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)))
	if err != nil || result.Status != RepairCompleted || len(result.Evidence) != 6 {
		t.Fatalf("repair = %+v, %v", result, err)
	}
	writes := rec.Named("workflow.repair.record_evidence")
	if sink.calls != 6 || len(writes) != 6 {
		t.Fatalf("evidence writes = %d, recorded ops = %d", sink.calls, len(writes))
	}
	for _, op := range writes {
		if op.Outcome != observe.OutcomeFailure {
			t.Fatalf("evidence write outcome = %s, want FAILURE", op.Outcome)
		}
	}
}
