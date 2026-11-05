package agentcost

import (
	"math"
	"testing"
)

// An estimate near the int64 maximum must not wrap negative and slip past a
// money limit the call would plainly pass.
func TestGateAdmitHugeEstimateDenied(t *testing.T) {
	f := newFixture(t)
	if err := f.gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", MaxSpendMicros: 250_000}); err != nil {
		t.Fatal(err)
	}
	f.gate.Record(policy(""), 200_000, "olive")
	subject := policy("")
	subject.EstimateMicros = math.MaxInt64
	if decision := f.gate.Admit(subject); decision.Allowed {
		t.Fatalf("estimate near the int64 maximum wrapped past the limit: %+v", decision)
	}
}

// A negative estimate is invalid input at a fail-closed gate: it must stop
// the call, never discount the spend already recorded.
func TestGateAdmitNegativeEstimateDenied(t *testing.T) {
	f := newFixture(t)
	if err := f.gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", MaxSpendMicros: 250_000}); err != nil {
		t.Fatal(err)
	}
	subject := policy("")
	subject.EstimateMicros = -1
	if decision := f.gate.Admit(subject); decision.Allowed {
		t.Fatalf("negative estimate was admitted: %+v", decision)
	}
}

// A negative recording is invalid input: it must not discount the spend
// already recorded.
func TestGateRecordNegativeSpendIgnored(t *testing.T) {
	f := newFixture(t)
	if err := f.gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", MaxSpendMicros: 250_000}); err != nil {
		t.Fatal(err)
	}
	f.gate.Record(policy(""), 200_000, "olive")
	f.gate.Record(policy(""), -100_000, "olive")
	subject := policy("")
	subject.EstimateMicros = 100_000
	if decision := f.gate.Admit(subject); decision.Allowed {
		t.Fatalf("negative recording discounted the limit: %+v", decision)
	}
}

// Negative usage from the store is corrupt data the gate cannot read: the
// call is not admitted, and no limit is claimed to be reached.
func TestGateAdmitNegativeStoredUsageUnavailable(t *testing.T) {
	f := newFixture(t)
	store := newMemoryStore()
	store.runs = append(store.runs, Run{TenantID: "t1", AgentID: "policy", RunID: "bad", At: *f.clock, Kind: KindAnswer, SpendMicros: -500_000})
	if err := store.SaveLimit(AuditEvent{}, Limit{TenantID: "t1", AgentID: "policy", MaxSpendMicros: 250_000}, false); err != nil {
		t.Fatal(err)
	}
	f.gate.WithStore(store)
	if decision := f.gate.Admit(policy("")); decision.Allowed || !decision.Unavailable {
		t.Fatalf("corrupt stored usage was admitted or claimed a limit: %+v", decision)
	}
}

// The boundary itself does not move: exactly filling the limit is allowed,
// one micro over is not.
func TestGateAdmitEstimateBoundary(t *testing.T) {
	f := newFixture(t)
	if err := f.gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", MaxSpendMicros: 250_000}); err != nil {
		t.Fatal(err)
	}
	f.gate.Record(policy(""), 200_000, "olive")
	exact := policy("")
	exact.EstimateMicros = 50_000
	if decision := f.gate.Admit(exact); !decision.Allowed {
		t.Fatalf("estimate exactly filling the limit was denied: %+v", decision)
	}
	over := policy("")
	over.EstimateMicros = 50_001
	if decision := f.gate.Admit(over); decision.Allowed {
		t.Fatalf("estimate one micro over the limit was allowed: %+v", decision)
	}
}
