package workflow

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

func wfrun021Policy() WorkloadPolicy {
	return WorkloadPolicy{
		Tenant: "harborcare-demo", Capability: "promotion.execute/v1",
		Criticality: admission.P1, ControlSnapshot: "sha256:snapshot-1",
		Limits: WorkloadLimits{
			MaxBranches: 4, MaxChildren: 8, MaxPayloadBytes: 1 << 20,
			MaxConcurrentActivities: 16, MaxCostUnits: 1000,
		},
	}
}

func wfrun021Request() WorkloadRequest {
	return WorkloadRequest{
		Tenant: "harborcare-demo", Capability: "promotion.execute/v1",
		Criticality: admission.P1, ControlSnapshot: "sha256:snapshot-1",
		IntentRef: "intent-1",
		Branches:  2, Children: 3, PayloadBytes: 4096,
		ConcurrentActivities: 2, CostUnits: 50,
	}
}

// TestTodo_WF_RUN_021_Race: admission is pure, so concurrent verdicts over
// a shared policy stay race-free.
func TestTodo_WF_RUN_021_Race(t *testing.T) {
	policy := wfrun021Policy()
	usage := WorkloadUsage{ConcurrentActivities: 2, CostUnits: 100}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				req := wfrun021Request()
				req.IntentRef = "intent-race"
				req.ConcurrentActivities = (n + i) % 3
				dec, err := AdmitWorkload(req, usage, policy)
				if err != nil {
					t.Errorf("AdmitWorkload: %v", err)
					return
				}
				if dec.Verdict != WorkloadAdmit || dec.IntentRef != "intent-race" {
					t.Errorf("verdict=%+v", dec)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

// TestTodo_WF_RUN_021_Fault: misconfiguration and scope drift fail closed;
// verdicts stay deterministic.
func TestTodo_WF_RUN_021_Fault(t *testing.T) {
	policy := wfrun021Policy()
	usage := WorkloadUsage{ConcurrentActivities: 2, CostUnits: 100}
	req := wfrun021Request()

	badPolicy := policy
	badPolicy.Limits.MaxBranches = 0
	if _, err := AdmitWorkload(req, usage, badPolicy); !errors.Is(err, ErrWorkload) {
		t.Fatalf("zero limit admitted: %v", err)
	}
	emptyTenant := policy
	emptyTenant.Tenant = ""
	if _, err := AdmitWorkload(req, usage, emptyTenant); !errors.Is(err, ErrWorkload) {
		t.Fatalf("tenantless policy admitted: %v", err)
	}
	noIntent := req
	noIntent.IntentRef = ""
	if _, err := AdmitWorkload(noIntent, usage, policy); !errors.Is(err, ErrWorkload) {
		t.Fatalf("intentless request admitted: %v", err)
	}
	negative := req
	negative.CostUnits = -1
	if _, err := AdmitWorkload(negative, usage, policy); !errors.Is(err, ErrWorkload) {
		t.Fatalf("negative demand admitted: %v", err)
	}
	negUsage := WorkloadUsage{ConcurrentActivities: -1}
	if _, err := AdmitWorkload(req, negUsage, policy); !errors.Is(err, ErrWorkload) {
		t.Fatalf("negative usage admitted: %v", err)
	}
	// Scope drift in any one axis fails closed.
	for _, mutate := range []func(*WorkloadRequest){
		func(r *WorkloadRequest) { r.Tenant = "other-tenant" },
		func(r *WorkloadRequest) { r.Capability = "payroll.run/v1" },
		func(r *WorkloadRequest) { r.Criticality = admission.P4 },
		func(r *WorkloadRequest) { r.ControlSnapshot = "sha256:snapshot-2" },
	} {
		drifted := req
		mutate(&drifted)
		if _, err := AdmitWorkload(drifted, usage, policy); !errors.Is(err, ErrWorkload) {
			t.Fatalf("scope drift admitted: %+v", drifted)
		}
	}
	unknown := req
	unknown.Criticality = "P9"
	if _, err := AdmitWorkload(unknown, usage, policy); !errors.Is(err, ErrWorkload) {
		t.Fatalf("unknown criticality admitted: %v", unknown)
	}
	// Verdict mapping honors the owned vocabulary.
	if WorkloadAdmit.AdmissionOutcome() != admission.Admit ||
		WorkloadDeferred.AdmissionOutcome() != admission.Defer ||
		WorkloadOverloaded.AdmissionOutcome() != admission.Reject {
		t.Fatal("verdict mapping left the owned admission vocabulary")
	}
}

// TestTodo_WF_RUN_021_Mutation: limit edges resolve on the documented
// side — exactly at admits, one past denies, structural before pressure.
func TestTodo_WF_RUN_021_Mutation(t *testing.T) {
	policy := wfrun021Policy()
	usage := WorkloadUsage{}
	atLimit := wfrun021Request()
	atLimit.Branches = 4
	atLimit.Children = 8
	atLimit.PayloadBytes = 1 << 20
	atLimit.ConcurrentActivities = 16
	atLimit.CostUnits = 1000
	dec, err := AdmitWorkload(atLimit, usage, policy)
	if err != nil || dec.Verdict != WorkloadAdmit {
		t.Fatalf("demand exactly at every limit: %+v %v", dec, err)
	}
	// One past on each structural axis overloads.
	for _, mutate := range []func(*WorkloadRequest){
		func(r *WorkloadRequest) { r.Branches = 5 },
		func(r *WorkloadRequest) { r.Children = 9 },
		func(r *WorkloadRequest) { r.PayloadBytes = (1 << 20) + 1 },
	} {
		over := atLimit
		over.ConcurrentActivities = 0
		over.CostUnits = 0
		mutate(&over)
		dec, err := AdmitWorkload(over, usage, policy)
		if err != nil || dec.Verdict != WorkloadOverloaded || dec.Retryable {
			t.Fatalf("structural excess: %+v %v, want non-retryable OVERLOADED", dec, err)
		}
		if dec.IntentRef != "intent-1" || dec.LimitName == "" || dec.Observed <= dec.Allowed {
			t.Fatalf("denial names no binding constraint: %+v", dec)
		}
	}
	// Pressure excess defers retryably, and structural wins ties.
	pressured := wfrun021Request()
	pressured.CostUnits = 1001
	dec, err = AdmitWorkload(pressured, usage, policy)
	if err != nil || dec.Verdict != WorkloadDeferred || !dec.Retryable {
		t.Fatalf("cost excess: %+v %v, want retryable ADMISSION_DEFERRED", dec, err)
	}
	tied := pressured
	tied.Branches = 9
	dec, err = AdmitWorkload(tied, usage, policy)
	if err != nil || dec.Verdict != WorkloadOverloaded || dec.LimitName != "branches" {
		t.Fatalf("structural+pressure tie: %+v %v, want OVERLOADED naming branches", dec, err)
	}
}
