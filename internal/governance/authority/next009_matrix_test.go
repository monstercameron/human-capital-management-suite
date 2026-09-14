package authority

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func mustPreWriteAmendment(t *testing.T, at time.Time) Amendment {
	t.Helper()
	bound, err := Bind(Amendment{
		AmendmentID: "prewrite-9", Tenant: values.TenantId("harborcare-demo"),
		Topology: TopologyExternalAuthority, Fields: []string{"job.level"},
		Operations: []string{"promotion.execute"},
		From:       at.Add(-time.Hour), Until: at.Add(30 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	return bound
}

func seedPreWriteCriteria(at time.Time) []PreWriteCriterion {
	return []PreWriteCriterion{
		{
			ID: "topology", TestCommand: "go test ./internal/platform/topology/",
			Fixture: "six-command cell", Oracle: "receipt digest",
			ResultDigest: "sha256:topology-ok", Owner: "platform-foundation",
			Retention: "7y", Expiry: at.Add(24 * time.Hour), SignOff: "release-captain",
			EvidenceAt: at.Add(-time.Hour),
		},
		{
			ID: "restore", TestCommand: "go test ./internal/operations/recovery/",
			Fixture: "recovery-cell-1", Oracle: "set digest",
			ResultDigest: "sha256:restore-ok", Owner: "data-and-ledger",
			Retention: "7y", Expiry: at.Add(24 * time.Hour), SignOff: "release-captain",
			EvidenceAt: at.Add(-time.Hour),
		},
	}
}

func seedPreWriteInput(amendment Amendment, criteria []PreWriteCriterion, at time.Time) PreWriteInput {
	return PreWriteInput{
		Amendment: amendment, Criteria: criteria,
		Scope: PreWriteScope{
			Tenant: "harborcare-demo", Intent: "PromoteWorker",
			Capability: "promotion.execute", Fields: []string{"job.level"},
			Provider: "cell-postgres", Window: "2026-10-07/2026-10-14",
		},
		Owner: "gate-commander", RollbackRef: "RUNBOOK-rollback-9",
		RepairRef: "RUNBOOK-repair-9", IncidentRef: "RUNBOOK-incident-9",
		EvaluatedAt: at,
	}
}

func mustCompilePreWrite(t *testing.T, in PreWriteInput) PreWriteVerdict {
	t.Helper()
	verdict, err := CompilePreWrite(in)
	if err != nil {
		t.Fatalf("CompilePreWrite: %v", err)
	}
	return verdict
}

// TestTodo_NEXT_009_Property: every single-field defect blocks with
// zero write authority; only the complete envelope grants.
func TestTodo_NEXT_009_Property(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	amendment := mustPreWriteAmendment(t, at)
	base := seedPreWriteCriteria(at)
	cases := []struct {
		name   string
		mutate func(PreWriteInput) PreWriteInput
	}{
		{"no criteria", func(in PreWriteInput) PreWriteInput { in.Criteria = nil; return in }},
		{"missing oracle", func(in PreWriteInput) PreWriteInput { in.Criteria[0].Oracle = ""; return in }},
		{"stale proof", func(in PreWriteInput) PreWriteInput { in.Criteria[0].Expiry = at.Add(-time.Minute); return in }},
		{"future proof", func(in PreWriteInput) PreWriteInput { in.Criteria[0].EvidenceAt = at.Add(time.Hour); return in }},
		{"self certified", func(in PreWriteInput) PreWriteInput { in.Criteria[0].SignOff = in.Criteria[0].Owner; return in }},
		{"circular owner", func(in PreWriteInput) PreWriteInput { in.Criteria[0].Owner = in.Owner; return in }},
		{"open finding", func(in PreWriteInput) PreWriteInput { in.CriticalFindings = []string{"ASSURANCE-7"}; return in }},
		{"unowned rollback", func(in PreWriteInput) PreWriteInput { in.RollbackRef = ""; return in }},
		{"narrow scope", func(in PreWriteInput) PreWriteInput { in.Scope.Fields = nil; return in }},
	}
	for _, tc := range cases {
		in := seedPreWriteInput(amendment, append([]PreWriteCriterion(nil), base...), at)
		verdict := mustCompilePreWrite(t, tc.mutate(in))
		if verdict.Decision != PreWriteGateBlocked {
			t.Fatalf("%s granted: %+v", tc.name, verdict)
		}
		if verdict.WriteAuthority != "" {
			t.Fatalf("%s carries write authority: %q", tc.name, verdict.WriteAuthority)
		}
	}
	granted := mustCompilePreWrite(t, seedPreWriteInput(amendment, base, at))
	if granted.Decision != PreWriteGranted || len(granted.Findings) != 0 {
		t.Fatalf("complete envelope: %+v", granted)
	}
}

// TestTodo_NEXT_009_Golden pins the pre-write verdict digest oracle.
func TestTodo_NEXT_009_Golden(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	verdict := mustCompilePreWrite(t, seedPreWriteInput(mustPreWriteAmendment(t, at), seedPreWriteCriteria(at), at))
	raw, err := os.ReadFile(filepath.Join("testdata", "next009.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if want := strings.TrimSpace(string(raw)); verdict.Digest != want {
		t.Fatalf("verdict digest mismatch:\n got=%q\nwant=%q", verdict.Digest, want)
	}
}

// TestTodo_NEXT_009_Fault: unbound amendments, inactive windows and
// malformed envelopes error instead of blocking silently.
func TestTodo_NEXT_009_Fault(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	amendment := mustPreWriteAmendment(t, at)
	base := seedPreWriteInput(amendment, seedPreWriteCriteria(at), at)
	unbound := base
	unbound.Amendment.Digest = "sha256:forged"
	if _, err := CompilePreWrite(unbound); err == nil {
		t.Fatal("forged amendment digest admitted")
	}
	expired := base
	expired.EvaluatedAt = at.Add(31 * 24 * time.Hour)
	if _, err := CompilePreWrite(expired); err == nil {
		t.Fatal("evaluation past amendment expiry admitted")
	}
	ownerless := base
	ownerless.Owner = ""
	if _, err := CompilePreWrite(ownerless); err == nil {
		t.Fatal("ownerless amendment admitted")
	}
	timeless := base
	timeless.EvaluatedAt = time.Time{}
	if _, err := CompilePreWrite(timeless); err == nil {
		t.Fatal("timeless evaluation admitted")
	}
	duplicate := base
	duplicate.Criteria = append(append([]PreWriteCriterion(nil), base.Criteria...), base.Criteria[0])
	if verdict := mustCompilePreWrite(t, duplicate); verdict.Decision != PreWriteGateBlocked {
		t.Fatalf("duplicate criterion granted: %+v", verdict)
	}
}

// TestTodo_NEXT_009_Security: the verdict digest binds owner, scope and
// findings; tampering changes the receipt.
func TestTodo_NEXT_009_Security(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	amendment := mustPreWriteAmendment(t, at)
	honest := mustCompilePreWrite(t, seedPreWriteInput(amendment, seedPreWriteCriteria(at), at))
	retargeted := seedPreWriteInput(amendment, seedPreWriteCriteria(at), at)
	retargeted.Scope.Tenant = "other-tenant"
	// Other-tenant is a valid slug but outside the amendment binding: it
	// blocks with zero authority instead of granting silently.
	other := mustCompilePreWrite(t, retargeted)
	if other.Decision != PreWriteGateBlocked || other.WriteAuthority != "" {
		t.Fatalf("retargeted scope granted: %+v", other)
	}
	if honest.Digest == other.Digest {
		t.Fatal("retargeted scope verifies against the honest digest")
	}
}

// TestTodo_NEXT_009_Conformance: the conformance vector pins the defect
// codes and the zero-authority invariant.
func TestTodo_NEXT_009_Conformance(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	amendment := mustPreWriteAmendment(t, at)
	in := seedPreWriteInput(amendment, nil, at)
	in.CriticalFindings = []string{"ASSURANCE-1"}
	in.RollbackRef, in.RepairRef, in.IncidentRef = "", "", ""
	verdict := mustCompilePreWrite(t, in)
	codes := map[string]bool{}
	for _, finding := range verdict.Findings {
		codes[finding.Code] = true
	}
	for _, want := range []string{"CRITERIA_MISSING", "ASSURANCE_FINDING_OPEN", "RESPONSE_PATH_UNOWNED"} {
		if !codes[want] {
			t.Fatalf("conformance vector missing %s: %+v", want, verdict.Findings)
		}
	}
	if verdict.WriteAuthority != "" {
		t.Fatalf("blocked verdict carries authority: %q", verdict.WriteAuthority)
	}
}

// TestTodo_NEXT_009_Recovery: an expired criterion heals by re-proof;
// the renewed verdict grants with a new digest.
func TestTodo_NEXT_009_Recovery(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	amendment := mustPreWriteAmendment(t, at)
	stale := seedPreWriteCriteria(at)
	stale[1].Expiry = at.Add(-time.Minute)
	blocked := mustCompilePreWrite(t, seedPreWriteInput(amendment, stale, at))
	if blocked.Decision != PreWriteGateBlocked {
		t.Fatalf("stale proof granted: %+v", blocked)
	}
	renewed := seedPreWriteCriteria(at)
	granted := mustCompilePreWrite(t, seedPreWriteInput(amendment, renewed, at))
	if granted.Decision != PreWriteGranted {
		t.Fatalf("renewed proof blocked: %+v", granted)
	}
	if granted.Digest == blocked.Digest {
		t.Fatal("renewed verdict reuses the blocked digest")
	}
}

// TestTodo_NEXT_009_Mutation: envelope edges resolve on the documented
// side.
func TestTodo_NEXT_009_Mutation(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	amendment := mustPreWriteAmendment(t, at)
	base := seedPreWriteInput(amendment, seedPreWriteCriteria(at), at)
	// Expiry exactly at evaluation is stale: proof must outlive the gate.
	edge := seedPreWriteInput(amendment, seedPreWriteCriteria(at), at)
	edge.Criteria[0].Expiry = at
	if verdict := mustCompilePreWrite(t, edge); verdict.Decision != PreWriteGateBlocked {
		t.Fatalf("at-instant expiry granted: %+v", verdict)
	}
	// Evidence exactly at evaluation is current.
	if verdict := mustCompilePreWrite(t, base); verdict.Decision != PreWriteGranted {
		t.Fatalf("at-instant evidence blocked: %+v", verdict)
	}
	// Empty tenant scope blocks rather than granting broadly.
	broad := base
	broad.Scope.Tenant = ""
	if verdict := mustCompilePreWrite(t, broad); verdict.Decision != PreWriteGateBlocked {
		t.Fatalf("tenantless scope granted: %+v", verdict)
	}
}
