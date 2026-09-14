package gateb

import (
	"reflect"
	"testing"
)

// healthyManifest is the known-good Gate B continuation manifest: every
// required acceptance bullet bound to exact todo/test/fixture/command,
// a passing result, an evidence digest, an owner, a retention period
// inside its freshness window and a sign-off, plus one retained prior
// decision.
func healthyManifest() Manifest {
	criterion := func(bullet, todo, test, fixture, command, digest, owner string) AcceptanceCriterion {
		return AcceptanceCriterion{
			Bullet: bullet, TodoID: todo, Test: test, Fixture: fixture, Command: command,
			Result: ResultPass, EvidenceDigest: digest, Owner: owner,
			RetentionDays: 365, ObservedUnixMilli: 1_700_000_000_000, MaxAgeMillis: 90 * 24 * 60 * 60 * 1000,
			SignOff: "gatekeeper@example.com",
		}
	}
	return Manifest{
		GateRef:      "GATE_B_LIMITED_WRITE",
		NowUnixMilli: 1_700_000_000_000 + int64(24*60*60*1000),
		Criteria: []AcceptanceCriterion{
			criterion(BulletTransactionCorrectness, "SCENARIO-003", "TestTodo_SCENARIO_003", "reference-scenarios", "go test ./internal/domains/scenario/", "sha256:1111", "txn-owner@example.com"),
			criterion(BulletAccessIsolation, "THREAT-002", "TestTodo_THREAT_002", "abuse-fixtures", "go test ./internal/security/abuse/", "sha256:2222", "sec-owner@example.com"),
			criterion(BulletOperabilityRestore, "RECOVERY-003", "TestTodo_RECOVERY_003", "pilot-restore", "go test ./internal/operations/recovery/", "sha256:3333", "ops-owner@example.com"),
			criterion(BulletOperabilitySupport, "OPS-008", "TestTodo_OPS_008", "runbooks", "go test ./internal/operations/support/", "sha256:4444", "support-owner@example.com"),
			criterion(BulletOperabilityRelease, "TOOL-025", "TestToolchainSupplyChainManifestRejectsUntrackedToolInput", "sbom", "go test ./tools/quality/toolinventory/", "sha256:5555", "rel-owner@example.com"),
			criterion(BulletCustomerEvidence, "PILOT-001", "TestPilotGoNoGoRejectsMissingStaleWaivedOrBelowThresholdEvidence", "pilot-cohort", "go test ./internal/governance/pilot/", "sha256:6666", "pilot-owner@example.com"),
			criterion(BulletPerformanceSoak, "PERF-003", "TestTodo_PERF_003", "soak-harness", "go test ./internal/performance/", "sha256:7777", "perf-owner@example.com"),
			criterion(BulletAssurance, "ASSURANCE-001", "TestTodo_ASSURANCE_001", "pentest-scope", "go test ./internal/security/assurance/", "sha256:8888", "assurance-owner@example.com"),
			criterion(BulletPilotDecision, "PILOT-001", "TestTodo_PILOT_001_Golden", "decision-package", "go test ./internal/governance/pilot/", "sha256:9999", "pilot-owner@example.com"),
			criterion(BulletRetentionDisposition, "PERF-009", "TestLongHorizonRetentionStorageGrowthAndLifecycleStayWithinBudget", "retention-model", "go test ./tools/planning/performance/", "sha256:aaaa", "records-owner@example.com"),
		},
		PriorDecisions: []PriorDecision{
			{GateRef: "GATE_A", Verdict: "GO", Digest: "sha256:0000", DecidedUnixMilli: 1_690_000_000_000},
		},
	}
}

func cloneManifest(manifest Manifest) Manifest {
	out := manifest
	out.Criteria = append([]AcceptanceCriterion(nil), manifest.Criteria...)
	out.PriorDecisions = append([]PriorDecision(nil), manifest.PriorDecisions...)
	for i := range out.Criteria {
		if manifest.Criteria[i].Waiver != nil {
			waiver := *manifest.Criteria[i].Waiver
			out.Criteria[i].Waiver = &waiver
		}
	}
	return out
}

// TestGateBEvidenceManifestRejectsUnboundStaleFailedOrUnsignedCriterion is
// the GATEB-EVID-001 primary: a fully bound manifest continues with zero
// added authority unless granted, while every seeded evidence defect —
// unbound, stale, failed, unsigned, vo-expired, incomplete-waiver —
// returns GATE_BLOCKED and grants nothing.
func TestGateBEvidenceManifestRejectsUnboundStaleFailedOrUnsignedCriterion(t *testing.T) {
	healthy := healthyManifest()
	before := cloneManifest(healthy)
	report, err := Compile(healthy)
	if err != nil {
		t.Fatalf("healthy manifest failed to compile: %v", err)
	}
	if report.Verdict != VerdictContinue {
		t.Fatalf("healthy verdict = %s, want CONTINUE", report.Verdict)
	}
	if len(report.Criteria) != len(RequiredBullets) {
		t.Fatalf("report covers %d bullets, want %d", len(report.Criteria), len(RequiredBullets))
	}
	for _, criterion := range report.Criteria {
		if criterion.Status != StatusPass {
			t.Fatalf("healthy bullet %s status = %s", criterion.Bullet, criterion.Status)
		}
	}
	if report.Grant.Permitted {
		t.Fatalf("healthy compile granted authority unasked: %+v", report.Grant)
	}

	// Authority issues only on a clean CONTINUE with a requested scope.
	requested := cloneManifest(healthy)
	requested.RequestContinuation = true
	requested.ContinuationScope = "one partner, job/level and base pay, one workflow"
	granted, err := Compile(requested)
	if err != nil {
		t.Fatalf("requested compile failed: %v", err)
	}
	if granted.Verdict != VerdictContinue || !granted.Grant.Permitted || granted.Grant.Scope != requested.ContinuationScope {
		t.Fatalf("requested grant = %+v", granted.Grant)
	}
	if len(report.Decisions) != len(healthy.PriorDecisions)+1 {
		t.Fatalf("report retains %d decisions, want %d", len(report.Decisions), len(healthy.PriorDecisions)+1)
	}
	if report.Digest == "" {
		t.Fatal("healthy report has no digest")
	}
	if !reflect.DeepEqual(before, healthy) {
		t.Fatal("Compile mutated its input manifest")
	}

	defects := []struct {
		name   string
		mutate func(*Manifest)
		bullet string
	}{
		{"unbound_test", func(m *Manifest) { m.Criteria[0].Test = "" }, BulletTransactionCorrectness},
		{"unbound_digest", func(m *Manifest) { m.Criteria[1].EvidenceDigest = "" }, BulletAccessIsolation},
		{"unbound_owner", func(m *Manifest) { m.Criteria[2].Owner = "  " }, BulletOperabilityRestore},
		{"unbound_retention", func(m *Manifest) { m.Criteria[3].RetentionDays = 0 }, BulletOperabilitySupport},
		{"stale", func(m *Manifest) { m.Criteria[4].ObservedUnixMilli -= 2 * m.Criteria[4].MaxAgeMillis }, BulletOperabilityRelease},
		{"failed", func(m *Manifest) { m.Criteria[5].Result = ResultFail }, BulletCustomerEvidence},
		{"unsigned", func(m *Manifest) { m.Criteria[6].SignOff = "" }, BulletPerformanceSoak},
		{"expired", func(m *Manifest) { m.Criteria[7].ExpiresUnixMilli = m.NowUnixMilli - 1 }, BulletAssurance},
		{"removed_bullet", func(m *Manifest) { m.Criteria = m.Criteria[:len(m.Criteria)-1] }, BulletRetentionDisposition},
		{"incomplete_waiver", func(m *Manifest) {
			m.Criteria[8].Result = ResultFail
			m.Criteria[8].Waiver = &Waiver{By: "owner@example.com", Reason: "known gap"}
		}, BulletPilotDecision},
	}
	for _, defect := range defects {
		t.Run(defect.name, func(t *testing.T) {
			manifest := cloneManifest(healthy)
			defect.mutate(&manifest)
			before := cloneManifest(manifest)
			report, err := Compile(manifest)
			if err != nil {
				t.Fatalf("evidence defect returned error instead of GATE_BLOCKED: %v", err)
			}
			if report.Verdict != VerdictGateBlocked {
				t.Fatalf("%s verdict = %s, want GATE_BLOCKED", defect.name, report.Verdict)
			}
			found := false
			for _, blocker := range report.Blockers {
				if blocker.Bullet == defect.bullet {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s names no blocker for %s: %+v", defect.name, defect.bullet, report.Blockers)
			}
			if report.Grant.Permitted {
				t.Fatalf("%s granted authority while blocked: %+v", defect.name, report.Grant)
			}
			if !reflect.DeepEqual(before, manifest) {
				t.Fatalf("%s mutated its input manifest", defect.name)
			}
		})
	}

	// A complete waiver names its scope, control and expiry and downgrades
	// to conditional continuation — never a clean pass, never a grant.
	waived := cloneManifest(healthy)
	waived.Criteria[9].Result = ResultFail
	waived.Criteria[9].Waiver = &Waiver{
		By: "records-owner@example.com", Reason: "archive restore drill deferred one cycle",
		Scope: "retention-model restore drill", Control: "manual restore rehearsal logged",
		ExpiresUnixMilli: waived.NowUnixMilli + int64(30*24*60*60*1000),
	}
	report, err = Compile(waived)
	if err != nil {
		t.Fatalf("waived manifest failed to compile: %v", err)
	}
	if report.Verdict != VerdictConditionalContinue {
		t.Fatalf("waived verdict = %s, want CONDITIONAL_CONTINUE", report.Verdict)
	}
	if report.Grant.Permitted {
		t.Fatalf("waiver granted authority: %+v", report.Grant)
	}
}
