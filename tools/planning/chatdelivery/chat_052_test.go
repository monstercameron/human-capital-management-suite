package chatdelivery

import (
	"path/filepath"
	"testing"
)

func regressionGateArtifact(t *testing.T) RegressionGateRecord {
	t.Helper()
	r, err := LoadRegressionGate(filepath.Join(repoRoot(t), RegressionGateArtifactPath))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTodo_CHAT_052_Conformance(t *testing.T) {
	gate := regressionGateArtifact(t)
	if violations := ValidateRegressionGate(gate); len(violations) != 0 {
		t.Fatalf("CHAT-052 regression gate artifact is invalid: %v", violations)
	}
	if gate.Status == "APPROVED" || len(gate.Approval.Signers) > 0 {
		t.Fatal("regression gate must not claim external approval")
	}

	// A conforming mixed-load profile: every budgeted metric present and
	// inside budget. The gate must still refuse to authorize activation
	// because it carries no signed approval.
	observed := []MetricObservation{
		{Name: "workflow_wakeup_p95_seconds", Candidate: 3.2},
		{Name: "chat_send_p99_latency_seconds", Candidate: 0.7},
		{Name: "workflow_admission_start_regression_percent", Baseline: 100, Candidate: 102},
		{Name: "workflow_queue_age_regression_percent", Baseline: 100, Candidate: 101},
		{Name: "cost_regression_percent", Baseline: 100, Candidate: 100},
	}
	authorized, results, violations := EvaluateRegression(gate, observed)
	if authorized {
		t.Fatal("an unsigned regression gate must never authorize activation")
	}
	if len(results) != len(gate.Metrics) {
		t.Fatalf("results = %d, want one per budgeted metric (%d)", len(results), len(gate.Metrics))
	}
	for _, r := range results {
		if !r.WithinBudget {
			t.Fatalf("conforming profile metric %s reported out of budget: %+v", r.Name, r)
		}
	}
	foundUnsigned := false
	for _, v := range violations {
		if v.Field == "approval" {
			foundUnsigned = true
		}
	}
	if !foundUnsigned {
		t.Fatalf("expected an approval violation naming the unsigned gate, got %v", violations)
	}

	// A profile that blows a hard ceiling and a percentage budget must be
	// reported as out of budget on those metrics, independent of the
	// approval gap.
	regressed := []MetricObservation{
		{Name: "workflow_wakeup_p95_seconds", Candidate: 9.1},
		{Name: "chat_send_p99_latency_seconds", Candidate: 0.4},
		{Name: "workflow_admission_start_regression_percent", Baseline: 100, Candidate: 120},
		{Name: "workflow_queue_age_regression_percent", Baseline: 100, Candidate: 101},
		{Name: "cost_regression_percent", Baseline: 100, Candidate: 100},
	}
	authorized, results, violations = EvaluateRegression(gate, regressed)
	if authorized {
		t.Fatal("a regressed profile must never authorize activation")
	}
	breached := map[string]bool{}
	for _, r := range results {
		if !r.WithinBudget {
			breached[r.Name] = true
		}
	}
	if !breached["workflow_wakeup_p95_seconds"] || !breached["workflow_admission_start_regression_percent"] {
		t.Fatalf("expected workflow_wakeup_p95_seconds and workflow_admission_start_regression_percent to breach, got results=%+v", results)
	}
	if breached["chat_send_p99_latency_seconds"] || breached["workflow_queue_age_regression_percent"] || breached["cost_regression_percent"] {
		t.Fatalf("unexpected breach outside the regressed metrics: %+v", results)
	}
	if len(violations) < 3 {
		t.Fatalf("expected violations for both breaches plus the unsigned gate, got %v", violations)
	}
}

func TestTodo_CHAT_052_Conformance_MissingObservation(t *testing.T) {
	gate := regressionGateArtifact(t)
	authorized, _, violations := EvaluateRegression(gate, nil)
	if authorized {
		t.Fatal("no observations must never authorize activation")
	}
	if len(violations) < len(gate.Metrics) {
		t.Fatalf("expected a missing-observation violation per budgeted metric, got %v", violations)
	}
}

func TestChatRegressionGate052Digest(t *testing.T) {
	gate := regressionGateArtifact(t)
	computed, err := DigestRegressionGate(gate)
	if err != nil {
		t.Fatal(err)
	}
	if computed != gate.CanonicalDigest {
		t.Fatalf("digest changed: got %s want %s", computed, gate.CanonicalDigest)
	}
}
