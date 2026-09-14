package workflow

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

// WF-RUN-021 RED: workload admission contract before workload.go exists.
func TestTodo_WF_RUN_021(t *testing.T) {
	policy := WorkloadPolicy{
		Tenant:          "harborcare-demo",
		Capability:      "promotion.execute/v1",
		Criticality:     admission.P1,
		ControlSnapshot: "sha256:snapshot-1",
		Limits: WorkloadLimits{
			MaxBranches: 4, MaxChildren: 8, MaxPayloadBytes: 1 << 20,
			MaxConcurrentActivities: 16, MaxCostUnits: 1000,
		},
	}
	usage := WorkloadUsage{ConcurrentActivities: 2, CostUnits: 100}
	base := WorkloadRequest{
		Tenant:          "harborcare-demo",
		Capability:      "promotion.execute/v1",
		Criticality:     admission.P1,
		ControlSnapshot: "sha256:snapshot-1",
		IntentRef:       "intent-1",
		Branches:        2, Children: 3,
		PayloadBytes: 4096, CostUnits: 50,
		ConcurrentActivities: 2,
	}

	// Small work admits.
	dec, err := AdmitWorkload(base, usage, policy)
	if err != nil {
		t.Fatalf("AdmitWorkload(small): %v", err)
	}
	if dec.Verdict != WorkloadAdmit {
		t.Fatalf("verdict=%q, want ADMIT", dec.Verdict)
	}
	if dec.IntentRef != "intent-1" {
		t.Fatal("admission dropped the durable intent reference")
	}

	// RED: structural excess is OVERLOADED before anything is scheduled.
	tooBig := base
	tooBig.Branches = 5
	dec, err = AdmitWorkload(tooBig, usage, policy)
	if err != nil {
		t.Fatalf("AdmitWorkload(branches): %v", err)
	}
	if dec.Verdict != WorkloadOverloaded {
		t.Fatalf("verdict=%q, want OVERLOADED", dec.Verdict)
	}

	// RED: transient pressure defers with the intent kept visible for
	// retry/queue policy — it must not vanish or partially schedule.
	busy := WorkloadUsage{ConcurrentActivities: 15, CostUnits: 100}
	dec, err = AdmitWorkload(base, busy, policy)
	if err != nil {
		t.Fatalf("AdmitWorkload(busy): %v", err)
	}
	if dec.Verdict != WorkloadDeferred {
		t.Fatalf("verdict=%q, want ADMISSION_DEFERRED", dec.Verdict)
	}
	if dec.IntentRef != "intent-1" {
		t.Fatal("deferral dropped the durable intent reference")
	}
}
