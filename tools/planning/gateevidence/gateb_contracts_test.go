package gateevidence

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func TestGateBDecisionBlocksUnresolvedDependency(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	blocked := GateBPilotReceipt{
		SchemaVersion: 1, ManifestTodoID: "NEXT-009",
		Dependencies: []GateBDependency{
			{Name: "RECOVERY-005", State: GateBDependencyResolved},
			{Name: "PILOT-DRILL-3", State: GateBDependencyMissing},
		},
		Scenarios: []GateBScenario{{Name: "failover-db", Passed: true}},
		Security:  GateBSecurityApproval{Approver: "security-captain", ApprovedAt: at.Add(-time.Hour), ValidFor: 24 * time.Hour},
		RPO:       GateBObjective{Met: true}, RTO: GateBObjective{Met: true},
		RepairOwner: "ops-commander",
	}
	record := EvaluateGateBDecision(blocked, at)
	if record.Decision != GateBRemediate || record.WriteAuthority {
		t.Fatalf("unresolved dependency decision = %+v", record)
	}
	clear := blocked
	clear.Dependencies[1].State = GateBDependencyResolved
	proceed := EvaluateGateBDecision(clear, at)
	if proceed.Decision != GateBProceedLimited {
		t.Fatalf("complete evidence decision = %+v", proceed)
	}
	if !proceed.WriteAuthority {
		t.Fatal("limited-write decision carries no write authority")
	}
	if proceed.Scope.Tenant == "" || proceed.Scope.Expiry.IsZero() || proceed.RollbackCondition == "" || proceed.BypassCondition == "" {
		t.Fatalf("limited grant lacks exact scope or conditions: %+v", proceed)
	}
}

func seedGateBReceipt(at time.Time) GateBPilotReceipt {
	return GateBPilotReceipt{
		SchemaVersion: 1, ManifestTodoID: "NEXT-009",
		Dependencies: []GateBDependency{
			{Name: "RECOVERY-005", State: GateBDependencyResolved},
			{Name: "NEXT-009", State: GateBDependencyResolved},
		},
		Scenarios: []GateBScenario{{Name: "failover-db", Passed: true}, {Name: "failback-db", Passed: true}},
		Security:  GateBSecurityApproval{Approver: "security-captain", ApprovedAt: at.Add(-time.Hour), ValidFor: 24 * time.Hour},
		RPO:       GateBObjective{Met: true}, RTO: GateBObjective{Met: true},
		RepairOwner: "ops-commander",
	}
}

func mustSignGateB(t *testing.T, record GateBDecisionRecord) GateBDecisionRecord {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := SignGateBDecision(record, "gate-commander", "2026-10-07", private)
	if err != nil {
		t.Fatalf("SignGateBDecision: %v", err)
	}
	return signed
}

// TestTodo_WEDGE_015_Fault: every RED vector blocks with zero write
// authority and a named blocker.
func TestTodo_WEDGE_015_Fault(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		mutate func(GateBPilotReceipt) GateBPilotReceipt
		block  string
	}{
		{"implied dependency", func(r GateBPilotReceipt) GateBPilotReceipt {
			r.Dependencies[0].State = GateBDependencyImplied
			return r
		}, "DEPENDENCY_IMPLIED"},
		{"failed scenario", func(r GateBPilotReceipt) GateBPilotReceipt {
			r.Scenarios[0].Passed = false
			return r
		}, "SCENARIO_FAILED"},
		{"stale approval", func(r GateBPilotReceipt) GateBPilotReceipt {
			r.Security.ApprovedAt = at.Add(-48 * time.Hour)
			return r
		}, "SECURITY_APPROVAL_STALE"},
		{"unmet RPO", func(r GateBPilotReceipt) GateBPilotReceipt {
			r.RPO.Met = false
			return r
		}, "RPO_UNMET"},
		{"unmet RTO", func(r GateBPilotReceipt) GateBPilotReceipt {
			r.RTO.Met = false
			return r
		}, "RTO_UNMET"},
		{"unowned repair", func(r GateBPilotReceipt) GateBPilotReceipt {
			r.RepairOwner = ""
			return r
		}, "REPAIR_UNOWNED"},
	}
	for _, tc := range cases {
		record := EvaluateGateBDecision(tc.mutate(seedGateBReceipt(at)), at)
		if record.Decision != GateBRemediate || record.WriteAuthority {
			t.Fatalf("%s: %+v", tc.name, record)
		}
		found := false
		for _, blocker := range record.Blockers {
			if strings.HasPrefix(blocker, tc.block) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: blockers %+v lack %s", tc.name, record.Blockers, tc.block)
		}
	}
	// A signed blocked decision still carries no authority.
	blocked := mustSignGateB(t, EvaluateGateBDecision(seedGateBReceipt(at), at.Add(48*time.Hour)))
	if blocked.Decision != GateBRemediate || blocked.WriteAuthority {
		t.Fatalf("signed stale decision: %+v", blocked)
	}
	if ok, err := VerifyGateBDecision(blocked); err != nil || !ok {
		t.Fatalf("signed blocked verify = %v, %v", ok, err)
	}
}

// TestTodo_WEDGE_015_Security: scope widening requires a new decision;
// a widened record fails validation and signature verification binds
// the exact scope.
func TestTodo_WEDGE_015_Security(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	proceed := mustSignGateB(t, EvaluateGateBDecision(seedGateBReceipt(at), at))
	if ok, err := VerifyGateBDecision(proceed); err != nil || !ok {
		t.Fatalf("signed proceed verify = %v, %v", ok, err)
	}
	if violations := ValidateGateBDecision(proceed, at); len(violations) != 0 {
		t.Fatalf("valid decision violations: %+v", violations)
	}
	// Widening the scope after signing breaks the signature: widening is
	// a new decision, never a mutation. Shape validation still passes —
	// the widened record is well-formed but inauthentic — so Verify is
	// the enforcing check.
	widened := proceed
	widened.Scope.Fields = []string{"job.level", "pay.base"}
	if ok, _ := VerifyGateBDecision(widened); ok {
		t.Fatal("widened scope verifies against the old signature")
	}
	if violations := ValidateGateBDecision(widened, at); len(violations) != 0 {
		t.Fatalf("widened scope shape violations: %+v", violations)
	}
	// An unsigned PROCEED_LIMITED never validates for recording.
	unsigned := EvaluateGateBDecision(seedGateBReceipt(at), at)
	unsigned.Signer, unsigned.SignedAt = "gate-commander", "2026-10-07"
	if violations := ValidateGateBDecision(unsigned, at); len(violations) == 0 {
		t.Fatal("unsigned decision validates")
	}
	// Expired grants fail validation at recording time.
	if violations := ValidateGateBDecision(proceed, at.Add(8*24*time.Hour)); len(violations) == 0 {
		t.Fatal("expired grant validates")
	}
}

// TestTodo_WEDGE_015_Mutation: receipt edges resolve on the documented
// side.
func TestTodo_WEDGE_015_Mutation(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	// Approval exactly at its validity end still counts: expiry is past,
	// not at.
	edge := seedGateBReceipt(at)
	edge.Security.ApprovedAt = at.Add(-24 * time.Hour)
	if record := EvaluateGateBDecision(edge, at); record.Decision != GateBProceedLimited {
		t.Fatalf("at-instant approval: %+v", record)
	}
	// One nanosecond later remediates.
	edge.Security.ApprovedAt = at.Add(-24*time.Hour - time.Nanosecond)
	if record := EvaluateGateBDecision(edge, at); record.Decision != GateBRemediate {
		t.Fatalf("past-instant approval: %+v", record)
	}
	// No declared dependencies or scenarios block rather than proceed.
	bare := seedGateBReceipt(at)
	bare.Dependencies, bare.Scenarios = nil, nil
	if record := EvaluateGateBDecision(bare, at); record.Decision != GateBRemediate {
		t.Fatalf("bare receipt: %+v", record)
	}
}
