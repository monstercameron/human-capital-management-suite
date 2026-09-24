package rolloutplan

import (
	"strings"
	"testing"
)

func healthThresholds() Thresholds {
	return Thresholds{
		MaxErrors: 0, MaxCorrectnessFailures: 0, MaxReconciliationFails: 0,
		MaxSecurityFindings: 0, MaxBurnPerMille: 100,
	}
}

func healthyTelemetry(planDigest string) Telemetry {
	return Telemetry{
		PlanDigest: planDigest, Stage: "canary", Source: "telemetry/canary",
		ObservedSeconds: 600, Requests: 10000, Errors: 0,
		CorrectnessFailures: 0, ReconciliationFailures: 0, SecurityFindings: 0,
		BurnPerMille: 12, Complete: true,
	}
}

func broadCohortDigest(t *testing.T) string {
	t.Helper()
	set, err := FreezeCohorts(cohortRequest())
	if err != nil {
		t.Fatal(err)
	}
	for _, cohort := range set.Cohorts {
		if cohort.Stage == "broad" {
			return cohort.Digest
		}
	}
	t.Fatal("broad cohort missing")
	return ""
}

func TestTodo_ROLLOUT_004(t *testing.T) {
	plan := validPlan()
	compiled, err := Compile(plan)
	if err != nil {
		t.Fatal(err)
	}
	ledger := newTestActivationLedger(t)
	canary, err := ledger.Activate(activationRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if canary.Stage != "canary" {
		t.Fatalf("activated %+v, want the canary stage first", canary)
	}

	// A clean health window stores its metrics, thresholds and evidence
	// and expands deterministically to the next planned stage.
	decision, err := EvaluateHealth(plan, "canary", healthyTelemetry(compiled.Digest), healthThresholds())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Verdict != VerdictExpand {
		t.Fatalf("verdict = %s, want EXPAND", decision.Verdict)
	}
	for _, evidence := range []string{decision.PlanDigest, decision.TelemetryDigest, decision.ThresholdDigest, decision.Digest} {
		if evidence == "" {
			t.Fatalf("decision stores no evidence: %+v", decision)
		}
	}
	if decision.Requests != 10000 || decision.ObservedSeconds != 600 {
		t.Fatalf("decision drops metrics: %+v", decision)
	}
	if err := decision.Verify(); err != nil {
		t.Fatalf("fresh decision does not verify: %v", err)
	}

	set, err := FreezeCohorts(cohortRequest())
	if err != nil {
		t.Fatal(err)
	}
	broad, err := ledger.Expand(ExpansionRequest{
		Plan: plan, Cohorts: set, Decision: decision,
		Artifact:     Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
		CohortDigest: broadCohortDigest(t), Epoch: 2,
	})
	if err != nil {
		t.Fatalf("healthy expansion refused: %v", err)
	}
	if broad.Stage != "broad" || broad.Epoch != 2 || broad.PlanDigest != compiled.Digest {
		t.Fatalf("expansion receipt is not the deterministic next stage: %+v", broad)
	}

	// Every RED dimension blocks expansion with its exact code or verdict.
	redCases := []struct {
		name   string
		mutate func(*Telemetry)
		code   string
	}{
		{"unknown telemetry", func(tel *Telemetry) { tel.Complete = false }, UnknownTelemetry},
		{"sourceless telemetry", func(tel *Telemetry) { tel.Source = "" }, UnknownTelemetry},
		{"negative telemetry", func(tel *Telemetry) { tel.Errors = -1 }, UnknownTelemetry},
		{"errors exceed requests", func(tel *Telemetry) { tel.Errors = tel.Requests + 1 }, UnknownTelemetry},
		{"wrong stage telemetry", func(tel *Telemetry) { tel.Stage = "broad" }, TelemetryMismatch},
		{"wrong plan telemetry", func(tel *Telemetry) { tel.PlanDigest = "sha256:other" }, TelemetryMismatch},
		{"insufficient window", func(tel *Telemetry) { tel.ObservedSeconds = 599 }, InsufficientWindow},
	}
	for _, tc := range redCases {
		t.Run(tc.name, func(t *testing.T) {
			tel := healthyTelemetry(compiled.Digest)
			tc.mutate(&tel)
			if _, err := EvaluateHealth(plan, "canary", tel, healthThresholds()); !HasHealthCode(err, tc.code) {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
		})
	}

	holdCases := []struct {
		name   string
		mutate func(*Telemetry)
	}{
		{"window budget spent", func(tel *Telemetry) { tel.Errors = 1; tel.Requests = 10001 }},
		{"slo burn", func(tel *Telemetry) { tel.BurnPerMille = 101 }},
	}
	for _, tc := range holdCases {
		t.Run(tc.name, func(t *testing.T) {
			tel := healthyTelemetry(compiled.Digest)
			tc.mutate(&tel)
			decision, err := EvaluateHealth(plan, "canary", tel, healthThresholds())
			if err != nil {
				t.Fatal(err)
			}
			if decision.Verdict != VerdictHold {
				t.Fatalf("verdict = %s, want HOLD", decision.Verdict)
			}
			set, err := FreezeCohorts(cohortRequest())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ledger.Expand(ExpansionRequest{
				Plan: plan, Cohorts: set, Decision: decision,
				Artifact:     Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
				CohortDigest: broadCohortDigest(t), Epoch: 3,
			}); !HasHealthCode(err, HealthBlocked) {
				t.Fatalf("held expansion = %v, want HEALTH_BLOCKED", err)
			}
		})
	}

	stopCases := []struct {
		name   string
		mutate func(*Telemetry)
	}{
		{"correctness regression", func(tel *Telemetry) { tel.CorrectnessFailures = 1 }},
		{"reconciliation regression", func(tel *Telemetry) { tel.ReconciliationFailures = 1 }},
		{"security regression", func(tel *Telemetry) { tel.SecurityFindings = 1 }},
	}
	for _, tc := range stopCases {
		t.Run(tc.name, func(t *testing.T) {
			tel := healthyTelemetry(compiled.Digest)
			tc.mutate(&tel)
			decision, err := EvaluateHealth(plan, "canary", tel, healthThresholds())
			if err != nil {
				t.Fatal(err)
			}
			if decision.Verdict != VerdictStop {
				t.Fatalf("verdict = %s, want STOP", decision.Verdict)
			}
		})
	}
}

func TestTodo_ROLLOUT_004_Security(t *testing.T) {
	plan := validPlan()
	compiled, err := Compile(plan)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := EvaluateHealth(plan, "canary", healthyTelemetry(compiled.Digest), healthThresholds())
	if err != nil {
		t.Fatal(err)
	}

	// A verdict flipped after evaluation no longer verifies: stored
	// decisions cannot be upgraded to EXPAND.
	flipped := decision
	flipped.Errors = 5
	if err := flipped.Verify(); !HasHealthCode(err, TamperedDecision) {
		t.Fatalf("edited metrics verify = %v, want TAMPERED_DECISION", err)
	}
	// A HOLD verdict relabeled EXPAND keeps the old digest and fails.
	held, err := EvaluateHealth(plan, "canary", func() Telemetry {
		tel := healthyTelemetry(compiled.Digest)
		tel.BurnPerMille = 500
		return tel
	}(), healthThresholds())
	if err != nil {
		t.Fatal(err)
	}
	forged := held
	forged.Verdict = VerdictExpand
	if _, err := newTestActivationLedger(t).Expand(ExpansionRequest{
		Plan: plan, Decision: forged,
		Artifact:     Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
		CohortDigest: broadCohortDigest(t), Epoch: 9,
	}); !HasHealthCode(err, TamperedDecision) {
		t.Fatalf("forged EXPAND = %v, want TAMPERED_DECISION", err)
	}

	// Telemetry minted for another plan never evaluates here.
	foreign := healthyTelemetry("sha256:" + strings.Repeat("0", 64))
	if _, err := EvaluateHealth(plan, "canary", foreign, healthThresholds()); !HasHealthCode(err, TelemetryMismatch) {
		t.Fatalf("foreign telemetry = %v, want TELEMETRY_MISMATCH", err)
	}

	// Expansion past the last stage is refused even with a clean bill.
	broadTel := healthyTelemetry(compiled.Digest)
	broadTel.Stage = "broad"
	broadTel.Source = "telemetry/broad"
	broadTel.ObservedSeconds = 1800
	broadDecision, err := EvaluateHealth(plan, "broad", broadTel, Thresholds{
		MaxErrors: 2, MaxCorrectnessFailures: 0, MaxReconciliationFails: 0,
		MaxSecurityFindings: 0, MaxBurnPerMille: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if broadDecision.Verdict != VerdictExpand {
		t.Fatalf("broad verdict = %s, want EXPAND", broadDecision.Verdict)
	}
	set, err := FreezeCohorts(cohortRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newTestActivationLedger(t).Expand(ExpansionRequest{
		Plan: plan, Cohorts: set, Decision: broadDecision,
		Artifact:     Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
		CohortDigest: broadCohortDigest(t), Epoch: 3,
	}); !HasHealthCode(err, NoFurtherStage) {
		t.Fatalf("past-last expansion = %v, want NO_FURTHER_STAGE", err)
	}
}
