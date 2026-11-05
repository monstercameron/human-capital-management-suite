package agentqualification

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"
)

// matrixPlan signs one four-kind qualification plan for a single profile with
// profile-scoped tenants so operations can apply per-profile pressure.
func matrixPlan(t *testing.T, profile string, private ed25519.PrivateKey) SignedPlan {
	t.Helper()
	now := time.Now().UTC()
	plan := Plan{
		ID: "agent-capacity-" + profile + "-v1", Profile: profile, Runtime: "qualification-test",
		Issuer: "capacity-author", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute),
		Concurrency: 4, P99Limit: time.Second,
	}
	for index, kind := range []string{"model", "chat", "workflow", "schedule"} {
		tenant := profile + "-quiet"
		if index%2 == 1 {
			tenant = profile + "-noisy"
		}
		plan.Jobs = append(plan.Jobs, Job{ID: profile + "-" + kind, Tenant: tenant, Kind: kind})
	}
	signed, err := SignPlan(plan, private)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func matrixKeys(t *testing.T) (map[string]SignedPlan, map[string]ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	plans := make(map[string]SignedPlan)
	for _, profile := range MatrixProfiles {
		plans[profile] = matrixPlan(t, profile, private)
	}
	return plans, map[string]ed25519.PublicKey{"capacity-author": public}, private
}

func matrixBudgets() map[string]CapacityBudgets {
	budgets := make(map[string]CapacityBudgets)
	for _, profile := range MatrixProfiles {
		budgets[profile] = CapacityBudgets{
			MaxP99: time.Second, MaxQueueAge: 50 * time.Millisecond, MaxPoolWait: 50 * time.Millisecond,
			MaxTimerLateness: 50 * time.Millisecond, MaxCostMicros: 1000,
		}
	}
	return budgets
}

// matrixOperation applies deterministic per-profile pressure: quiet profiles
// measure near zero, peak congests chat queue age, pooled congests model pool
// wait, and dedicated overspends model cost.
func matrixOperation(_ context.Context, job Job) (Observation, error) {
	observation := completeObservation(job)
	zero := time.Duration(0)
	pressure := 200 * time.Millisecond
	cheap := int64(7)
	expensive := int64(5000)
	observation.QueueAge, observation.PoolWait, observation.TimerLateness = &zero, &zero, &zero
	observation.CostMicros = &cheap
	switch job.Tenant {
	case "peak-quiet", "peak-noisy":
		if job.Kind == "chat" {
			observation.QueueAge = &pressure
		}
	case "pooled-quiet", "pooled-noisy":
		if job.Kind == "model" {
			observation.PoolWait = &pressure
		}
	case "dedicated-quiet", "dedicated-noisy":
		if job.Kind == "model" {
			observation.CostMicros = &expensive
		}
	}
	return observation, nil
}

func matrixOperations() map[string]Operation {
	return map[string]Operation{"model": matrixOperation, "chat": matrixOperation, "workflow": matrixOperation, "schedule": matrixOperation}
}

func TestTodo_AGENT_048(t *testing.T) {
	plans, keys, _ := matrixKeys(t)
	ordered := []SignedPlan{plans["tiny"], plans["peak"], plans["pooled"], plans["dedicated"]}
	matrix, err := RunMatrix(context.Background(), "agent-capacity-matrix-v1", ordered, matrixBudgets(), keys, matrixOperations())
	if err != nil {
		t.Fatal(err)
	}
	if matrix.Status != "MEASURED" || len(matrix.Capacities) != 4 {
		t.Fatalf("matrix=%+v err=%v", matrix, err)
	}
	decisions := make(map[string]ScaleDecision)
	for _, capacity := range matrix.Capacities {
		decisions[capacity.Profile] = capacity.Decision
		if capacity.P50 < 0 || capacity.P50 > capacity.P95 || capacity.P95 > capacity.P99 {
			t.Fatalf("%s: latency percentiles inconsistent: p50=%s p95=%s p99=%s", capacity.Profile, capacity.P50, capacity.P95, capacity.P99)
		}
		if capacity.QueueAge == nil || capacity.PoolWait == nil || capacity.TimerLateness == nil || capacity.TotalCost == nil {
			t.Fatalf("%s: queue age, pool wait, timer lateness or cost unrecorded: %+v", capacity.Profile, capacity)
		}
		if len(capacity.KindP99) != 4 {
			t.Fatalf("%s: per-kind p99 missing: %+v", capacity.Profile, capacity.KindP99)
		}
		if workflow := capacity.KindP99["workflow"]; workflow > capacity.Budgets.MaxP99 {
			t.Fatalf("%s: workflow regressed: p99=%s budget=%s", capacity.Profile, workflow, capacity.Budgets.MaxP99)
		}
		if chat := capacity.KindP99["chat"]; chat > capacity.Budgets.MaxP99 {
			t.Fatalf("%s: chat regressed: p99=%s budget=%s", capacity.Profile, chat, capacity.Budgets.MaxP99)
		}
		if len(capacity.Admissions) != 2 {
			t.Fatalf("%s: fair admission unrecorded: %+v", capacity.Profile, capacity.Admissions)
		}
		for _, admission := range capacity.Admissions {
			if admission.Admitted < 1 {
				t.Fatalf("%s: tenant %s starved: %+v", capacity.Profile, admission.Tenant, admission)
			}
		}
		if capacity.Decision.Action == "" || capacity.Decision.Reason == "" {
			t.Fatalf("%s: scale-out/move threshold undecided: %+v", capacity.Profile, capacity.Decision)
		}
	}
	want := map[string]string{"tiny": "hold", "peak": "scale-out", "pooled": "move", "dedicated": "shed-optional"}
	for profile, action := range want {
		if decisions[profile].Action != action {
			t.Fatalf("%s: decision=%+v want action=%s", profile, decisions[profile], action)
		}
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := SignMatrix(matrix, "independent-reviewer", private)
	if err != nil {
		t.Fatal(err)
	}
	reviewers := map[string]ed25519.PublicKey{"independent-reviewer": public}
	if err := VerifyMatrix(signed, keys, reviewers, time.Now()); err != nil {
		t.Fatal(err)
	}
	signed.Matrix.Capacities[1].Decision.Action = "hold"
	if err := VerifyMatrix(signed, keys, reviewers, time.Now()); !errors.Is(err, ErrEvidence) {
		t.Fatalf("tampered decision accepted: %v", err)
	}
}

func TestTodo_AGENT_048_RejectsSingleProfileScaleClaim(t *testing.T) {
	plans, keys, _ := matrixKeys(t)
	single := []SignedPlan{plans["pooled"]}
	if _, err := RunMatrix(context.Background(), "agent-capacity-matrix-v1", single, matrixBudgets(), keys, matrixOperations()); !errors.Is(err, ErrEvidence) {
		t.Fatalf("single pooled profile accepted as scalable: %v", err)
	}
	duplicated := []SignedPlan{plans["tiny"], plans["tiny"], plans["pooled"], plans["dedicated"]}
	if _, err := RunMatrix(context.Background(), "agent-capacity-matrix-v1", duplicated, matrixBudgets(), keys, matrixOperations()); !errors.Is(err, ErrEvidence) {
		t.Fatalf("matrix without peak profile accepted: %v", err)
	}
	ordered := []SignedPlan{plans["tiny"], plans["peak"], plans["pooled"], plans["dedicated"]}
	budgets := matrixBudgets()
	delete(budgets, "peak")
	if _, err := RunMatrix(context.Background(), "agent-capacity-matrix-v1", ordered, budgets, keys, matrixOperations()); !errors.Is(err, ErrEvidence) {
		t.Fatalf("matrix without peak budgets accepted: %v", err)
	}
	budgets = matrixBudgets()
	budgets["tiny"] = CapacityBudgets{}
	if _, err := RunMatrix(context.Background(), "agent-capacity-matrix-v1", ordered, budgets, keys, matrixOperations()); !errors.Is(err, ErrEvidence) {
		t.Fatalf("matrix with zero budgets accepted: %v", err)
	}
	if _, err := RunMatrix(nil, "agent-capacity-matrix-v1", ordered, matrixBudgets(), keys, matrixOperations()); !errors.Is(err, ErrEvidence) {
		t.Fatalf("nil context accepted: %v", err)
	}
	if _, err := RunMatrix(context.Background(), "", ordered, matrixBudgets(), keys, matrixOperations()); !errors.Is(err, ErrEvidence) {
		t.Fatalf("unidentified matrix accepted: %v", err)
	}
	matrix, err := RunMatrix(context.Background(), "agent-capacity-matrix-v1", ordered, matrixBudgets(), keys, matrixOperations())
	if err != nil {
		t.Fatal(err)
	}
	_, reviewer, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SignMatrix(matrix, matrix.Plans[0].Plan.Issuer, reviewer); !errors.Is(err, ErrEvidence) {
		t.Fatalf("self review accepted: %v", err)
	}
	matrix.Capacities = matrix.Capacities[:3]
	if _, err := SignMatrix(matrix, "independent-reviewer", reviewer); !errors.Is(err, ErrEvidence) {
		t.Fatalf("truncated matrix signed: %v", err)
	}
}

func TestTodo_AGENT_048_UnknownWhenProfileUnmeasured(t *testing.T) {
	plans, keys, _ := matrixKeys(t)
	ordered := []SignedPlan{plans["tiny"], plans["peak"], plans["pooled"], plans["dedicated"]}
	operations := matrixOperations()
	operations["model"] = func(_ context.Context, job Job) (Observation, error) {
		return Observation{}, errors.New("provider quota exhausted")
	}
	matrix, err := RunMatrix(context.Background(), "agent-capacity-matrix-v1", ordered, matrixBudgets(), keys, operations)
	if err != nil || matrix.Status != "UNKNOWN" {
		t.Fatalf("failed profile measured: matrix=%+v err=%v", matrix, err)
	}
	if len(matrix.Findings) == 0 {
		t.Fatal("failed profile produced no finding")
	}
	for _, capacity := range matrix.Capacities {
		if capacity.Decision.Action != "hold" {
			t.Fatalf("%s: capacity decided on unmeasured evidence: %+v", capacity.Profile, capacity.Decision)
		}
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := SignMatrix(matrix, "independent-reviewer", private)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyMatrix(signed, keys, map[string]ed25519.PublicKey{"independent-reviewer": public}, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkTodo_AGENT_048_Benchmark(b *testing.B) {
	plan, keys := qualificationPlan(b)
	operations := map[string]Operation{"model": matrixOperation, "chat": matrixOperation, "workflow": matrixOperation, "schedule": matrixOperation}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		report, err := Run(context.Background(), plan, keys, operations)
		if err != nil || report.Status != "MEASURED" {
			b.Fatalf("report=%+v err=%v", report, err)
		}
	}
}
