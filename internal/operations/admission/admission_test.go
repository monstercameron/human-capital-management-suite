package admission

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func base() (Request, Snapshot) {
	return Request{TenantID: "tenant-a", CellID: "cell-1", PlacementEpoch: 7, Criticality: P2, EstimatedCost: 10, RetryBudgetID: "retry-1"}, Snapshot{
		TenantID: "tenant-a", CellID: "cell-1", PlacementEpoch: 7, Quota: Quota{Known: true, Version: "q3", Limit: 100}, Capacity: 100, RetryRemaining: 3,
	}
}

func TestTodo_ADMISSION_001(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Request, *Snapshot)
		want   Outcome
	}{
		{"healthy", func(*Request, *Snapshot) {}, Admit},
		{"unknown quota", func(_ *Request, s *Snapshot) { s.Quota.Known = false }, Defer},
		{"stale placement", func(_ *Request, s *Snapshot) { s.PlacementEpoch = 6 }, Defer},
		{"exhausted quota p2", func(_ *Request, s *Snapshot) { s.Quota.Consumed = 95 }, Queue},
		{"exhausted capacity p3", func(r *Request, s *Snapshot) { r.Criticality = P3; s.Capacity = 5 }, Defer},
		{"noisy p4", func(r *Request, s *Snapshot) { r.Criticality = P4; s.NoisyTenant = true }, Reject},
		{"p0 reservation", func(r *Request, s *Snapshot) { r.Criticality = P0; s.Capacity = 10; s.ReservedP0 = 10 }, Admit},
		{"p0 cannot displace capacity", func(r *Request, s *Snapshot) { r.Criticality = P0; s.Capacity = 5; s.ReservedP0 = 5 }, Defer},
		{"retry exhausted", func(r *Request, s *Snapshot) { r.RetryAttempt = 1; s.RetryRemaining = 0 }, Reject},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, s := base()
			tc.mutate(&r, &s)
			got := Decide(r, s, Policy{})
			if got.Outcome != tc.want {
				t.Fatalf("outcome = %s, want %s (reason %s)", got.Outcome, tc.want, got.Reason)
			}
			if err := got.Validate(); err != nil {
				t.Fatal(err)
			}
			again := Decide(r, s, Policy{})
			if !reflect.DeepEqual(got, again) {
				t.Fatalf("decision is not deterministic:\n%+v\n%+v", got, again)
			}
		})
	}
}

func TestTodo_ADMISSION_001_Golden(t *testing.T) {
	r, s := base()
	got := Decide(r, s, Policy{QueueRetryAfter: 9, DeferRetryAfter: 19, DegradeRetryAfter: 4})
	if got.DecisionID != "adm_13bdf0e5aa0e9cbce9158f4b5865b4cdd573168a97e21a33a881c2f784616c31" {
		t.Fatalf("decision id = %q", got.DecisionID)
	}
	if got.Evidence.TenantID != "tenant-a" || got.Evidence.CellID != "cell-1" || got.Evidence.Criticality != P2 {
		t.Fatalf("missing evidence: %+v", got.Evidence)
	}
}

func TestAdmissionDecisionIDBindsHiddenControlState(t *testing.T) {
	r, s := base()
	s.Draining = true
	known := Decide(r, s, Policy{})
	s.Quota.Known = false
	unknown := Decide(r, s, Policy{})
	if known.Outcome != Defer || unknown.Outcome != Defer || known.Reason != unknown.Reason || known.DecisionID == unknown.DecisionID {
		t.Fatalf("decision identity did not bind quota-known state: known=%+v unknown=%+v", known, unknown)
	}
}

func TestAdmissionDecisionIDUsesUnambiguousScopeFraming(t *testing.T) {
	leftRequest, leftSnapshot := base()
	leftRequest.TenantID, leftRequest.CellID = "a|b", "c"
	leftSnapshot.TenantID, leftSnapshot.CellID = leftRequest.TenantID, leftRequest.CellID
	rightRequest, rightSnapshot := base()
	rightRequest.TenantID, rightRequest.CellID = "a", "b|c"
	rightSnapshot.TenantID, rightSnapshot.CellID = rightRequest.TenantID, rightRequest.CellID
	left, right := Decide(leftRequest, leftSnapshot, Policy{}), Decide(rightRequest, rightSnapshot, Policy{})
	if left.Outcome != Admit || right.Outcome != Admit || left.DecisionID == right.DecisionID {
		t.Fatalf("distinct tenant/cell boundaries collided: left=%+v right=%+v", left, right)
	}
}

func TestAdmissionDecisionIDBindsObservedEpochAndPolicyResult(t *testing.T) {
	r, firstSnapshot := base()
	r.PlacementEpoch = 9
	firstSnapshot.PlacementEpoch = 7
	secondSnapshot := firstSnapshot
	secondSnapshot.PlacementEpoch = 8
	first, second := Decide(r, firstSnapshot, Policy{}), Decide(r, secondSnapshot, Policy{})
	if first.Reason != "STALE_PLACEMENT" || second.Reason != first.Reason || first.DecisionID == second.DecisionID {
		t.Fatalf("observed epochs were not bound: first=%+v second=%+v", first, second)
	}

	r, queueSnapshot := base()
	queueSnapshot.Quota.Consumed = 95
	short, long := Decide(r, queueSnapshot, Policy{QueueRetryAfter: 3}), Decide(r, queueSnapshot, Policy{QueueRetryAfter: 30})
	if short.Outcome != Queue || long.Outcome != Queue || short.Reason != long.Reason || short.RetryAfter == long.RetryAfter || short.DecisionID == long.DecisionID {
		t.Fatalf("policy-derived result was not bound: short=%+v long=%+v", short, long)
	}
}

func TestTodo_ADMISSION_001_Security(t *testing.T) {
	r, s := base()
	s.TenantID = "tenant-b"
	if got := Decide(r, s, Policy{}); got.Outcome != Reject || got.Reason != "TENANT_OR_CELL_MISMATCH" {
		t.Fatalf("cross-tenant result = %+v", got)
	}
	r.TenantID = ""
	if got := Decide(r, s, Policy{}); got.Outcome != Reject {
		t.Fatalf("empty tenant result = %+v", got)
	}
}

func TestTodo_ADMISSION_001_Race(t *testing.T) {
	r, s := base()
	want := Decide(r, s, Policy{})
	const workers = 32
	var wg sync.WaitGroup
	results := make(chan Decision, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- Decide(r, s, Policy{})
		}()
	}
	wg.Wait()
	close(results)
	for got := range results {
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("concurrent same-snapshot decision = %+v, want %+v", got, want)
		}
	}
}

func TestTodo_ADMISSION_001_Fault(t *testing.T) {
	r, s := base()
	s.Quota.Consumed = int(^uint(0) >> 1)
	s.Quota.Pending = 1
	if got := Decide(r, s, Policy{}); got.Outcome != Reject || got.Reason != "INVALID_CAPACITY_RESERVATION" {
		t.Fatalf("overflow snapshot = %+v", got)
	}
}

func BenchmarkTodo_ADMISSION_001(b *testing.B) {
	r, s := base()
	for i := 0; i < b.N; i++ {
		_ = Decide(r, s, Policy{})
	}
}

func TestAdmissionContractAndDecisionValidation(t *testing.T) {
	if Version() != 1 || Explain() == "" {
		t.Fatalf("contract metadata version=%d explain=%q", Version(), Explain())
	}
	valid := Decide(baseRequestForValidation(), baseSnapshotForValidation(), Policy{})
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Decision)
	}{
		{"missing id", func(d *Decision) { d.DecisionID = "" }},
		{"missing tenant", func(d *Decision) { d.TenantID = "" }},
		{"missing cell", func(d *Decision) { d.CellID = "" }},
		{"invalid criticality", func(d *Decision) { d.Criticality = "P9" }},
		{"missing outcome", func(d *Decision) { d.Outcome = "" }},
		{"unknown outcome", func(d *Decision) { d.Outcome = Outcome("ADMITTED") }},
		{"shed protocol synonym is not a decision outcome", func(d *Decision) { d.Outcome = Shed }},
		{"missing reason", func(d *Decision) { d.Reason = "" }},
		{"negative retry", func(d *Decision) { d.RetryAfter = -1 }},
		{"negative reservation", func(d *Decision) { d.Reservation = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := valid
			tc.mutate(&got)
			if !errors.Is(got.Validate(), ErrInvalidInput) {
				t.Fatalf("Validate=%v", got.Validate())
			}
		})
	}
}

func TestAdmissionDecisionSecurityBranches(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Request, *Snapshot)
		want   Outcome
		reason string
	}{
		{"invalid context", func(r *Request, _ *Snapshot) { r.EstimatedCost = 0 }, Reject, "INVALID_CONTEXT"},
		{"draining", func(_ *Request, s *Snapshot) { s.Draining = true }, Defer, "CELL_DRAINING"},
		{"p0 quota reservation", func(r *Request, s *Snapshot) { r.Criticality = P0; s.Quota.Consumed = 95 }, Defer, "P0_QUOTA_RESERVED"},
		{"p1 pressure", func(r *Request, s *Snapshot) { r.Criticality = P1; s.Capacity = 1 }, Degrade, "PRESSURE_OR_NOISY_TENANT"},
		{"p3 pressure", func(r *Request, s *Snapshot) { r.Criticality = P3; s.Capacity = 1 }, Defer, "PRESSURE_OR_NOISY_TENANT"},
		{"p4 pressure", func(r *Request, s *Snapshot) { r.Criticality = P4; s.Capacity = 1 }, Reject, "BEST_EFFORT_SHED"},
		{"retry exhausted", func(r *Request, s *Snapshot) { r.RetryAttempt = 1; s.RetryRemaining = 0 }, Reject, "RETRY_BUDGET_EXHAUSTED"},
		{"invalid capacity", func(_ *Request, s *Snapshot) { s.Capacity = -1 }, Reject, "INVALID_CAPACITY_RESERVATION"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, s := base()
			tc.mutate(&r, &s)
			got := Decide(r, s, Policy{QueueRetryAfter: 2, DeferRetryAfter: 3, DegradeRetryAfter: 4})
			if got.Outcome != tc.want || got.Reason != tc.reason {
				t.Fatalf("decision=%+v", got)
			}
			if err := got.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func baseRequestForValidation() Request   { r, _ := base(); return r }
func baseSnapshotForValidation() Snapshot { _, s := base(); return s }
