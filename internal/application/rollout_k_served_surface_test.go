package application_test

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	ve "github.com/monstercameron/human-capital-management-suite/internal/platform/versionexplain"
)

type rolloutExplanationRecorder struct {
	count int
}

func (r *rolloutExplanationRecorder) Record(ve.Explanation) { r.count++ }

func rolloutServedSnapshot() (ve.Snapshot, values.Instant) {
	known := values.NewInstant(time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC))
	effective := values.NewInstant(time.Date(2026, 9, 3, 8, 5, 0, 0, time.UTC))
	return ve.Snapshot{
		TargetID: "svc-payroll",
		Epoch:    7,
		Cohorts: map[string]ve.CohortState{
			"general": {Stage: ve.StageAll, Bundle: "payroll-2026.09", Epoch: 7, Receipt: "rc:general:7"},
		},
		Default:     "general",
		Membership:  map[string]string{"emp-1": "general"},
		KnownAt:     known,
		EffectiveAt: effective,
	}, effective
}

func TestTodo_ROLLOUT_007(t *testing.T) {
	surface := application.NewServedVersionExplanationSurface()
	recorder := &rolloutExplanationRecorder{}
	snapshot, at := rolloutServedSnapshot()

	explanation, err := surface.Explain(recorder, snapshot, "emp-1", at)
	if err != nil {
		t.Fatalf("served explanation failed: %v", err)
	}
	if explanation.TargetID != "svc-payroll" || explanation.CohortID != "general" || explanation.Bundle != "payroll-2026.09" {
		t.Fatalf("served explanation lost rollout state: %+v", explanation)
	}
	if recorder.count != 1 {
		t.Fatalf("served explanation recorded %d rows, want 1", recorder.count)
	}
}

func TestTodo_ROLLOUT_007_Mutation(t *testing.T) {
	var nilApp *application.App
	if surface := nilApp.VersionExplanation(); surface.Explain != nil || surface.NewExplainer != nil || surface.IsRejected != nil {
		t.Fatalf("nil application exposed a served capability: %+v", surface)
	}

	surface := application.NewServedVersionExplanationSurface()
	snapshot, at := rolloutServedSnapshot()
	first, err := surface.Explain(nil, snapshot, "emp-1", at)
	if err != nil {
		t.Fatalf("first served explanation failed: %v", err)
	}
	second, err := surface.Explain(nil, snapshot, "emp-1", at)
	if err != nil {
		t.Fatalf("second served explanation failed: %v", err)
	}
	if first != second {
		t.Fatalf("served explanation changed across identical reads:\nfirst=%+v\nsecond=%+v", first, second)
	}
}
