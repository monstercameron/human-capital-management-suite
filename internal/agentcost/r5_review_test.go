package agentcost

import (
	"errors"
	"math"
	"testing"
	"time"
)

// failingOwners is an Owners whose store cannot answer.
type failingOwners struct{ fakeOwners }

func (failingOwners) CheckOwner(string, string, string) (bool, error) {
	return false, errors.New("database is down")
}

// A ceiling near the int64 maximum used to wrap the gate's 80 percent
// arithmetic negative, so the first run looked like 80 percent of the day and
// the owner was told so; it is now refused as invalid.
func TestR5_LimitsPastTheArithmeticAreRefused(t *testing.T) {
	f := newFixture(t)
	for _, limit := range []Limit{
		{TenantID: "t1", AgentID: "policy", MaxRuns: math.MaxInt64},
		{TenantID: "t1", AgentID: "policy", MaxSpendMicros: math.MaxInt64},
		{TenantID: "t1", AgentID: "policy", MaxRuns: MaxRunsPerDay + 1},
		{TenantID: "t1", AgentID: "policy", MaxSpendMicros: MaxSpendMicrosPerDay + 1},
	} {
		if err := f.gate.Set("olive", limit); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Set(%+v) = %v, want ErrInvalid", limit, err)
		}
	}
	if err := f.gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", MaxRuns: MaxRunsPerDay, MaxSpendMicros: MaxSpendMicrosPerDay}); err != nil {
		t.Fatalf("the largest limit was refused: %v", err)
	}
	f.gate.Record(policy("general"), 1, "olive")
	if len(f.notifier.sent) != 0 {
		t.Fatalf("one cheap run against the largest limit sent %v", f.notifier.sent)
	}
}

// When the store cannot say who owns an agent, the answer is "unavailable", not
// "you may not": the owner is not told their own agent is not theirs, and their
// report is not silently drawn without their runs.
func TestR5_AnOwnerLookupThatFailsIsUnavailableNotDenied(t *testing.T) {
	clock := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	owners := failingOwners{fakeOwners{owners: map[string]bool{"t1/policy/olive": true}}}
	gate, err := NewGate(owners, nil, time.UTC, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", MaxRuns: 3}); !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrDenied) {
		t.Fatalf("Set = %v, want ErrUnavailable", err)
	}
	if _, err := gate.Limits("t1", "policy", "olive"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Limits = %v, want ErrUnavailable", err)
	}
	if _, err := gate.AuditTrail("t1", "policy", "olive"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("AuditTrail = %v, want ErrUnavailable", err)
	}
	ledger := &Ledger{}
	ledger.Add(Run{TenantID: "t1", AgentID: "policy", RunID: "r1", At: clock, Kind: KindAnswer, SpendMicros: 5, Answered: true})
	if _, err := ledger.ReportFor(owners, "t1", "olive", clock, time.UTC); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("ReportFor = %v, want ErrUnavailable", err)
	}
}
