package rolloutplan

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/drain"
)

func pauseThresholds() Thresholds {
	return Thresholds{MaxBurnPerMille: 100}
}

func pauseTelemetry(planDigest string) Telemetry {
	return Telemetry{
		PlanDigest: planDigest, Stage: "canary", Source: "telemetry/canary",
		ObservedSeconds: 600, Requests: 10000, BurnPerMille: 12, Complete: true,
	}
}

func canaryCohortDigest(t *testing.T) string {
	t.Helper()
	set, err := FreezeCohorts(cohortRequest())
	if err != nil {
		t.Fatal(err)
	}
	for _, cohort := range set.Cohorts {
		if cohort.Stage == "canary" {
			return cohort.Digest
		}
	}
	t.Fatal("canary cohort missing")
	return ""
}

func TestTodo_ROLLOUT_005(t *testing.T) {
	plan := validPlan()
	compiled, err := Compile(plan)
	if err != nil {
		t.Fatal(err)
	}
	ledger := newTestActivationLedger(t)
	gate := NewPauseGate(ledger)
	if _, err := gate.Activate(activationRequest(t)); err != nil {
		t.Fatal(err)
	}

	// Pause stops new activations: the same stage at a fresh epoch and
	// expansion out of it are both refused.
	receipt, err := gate.Pause(plan, "canary", 2, "slo-burn-watch")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Stage != "canary" || receipt.Epoch != 2 || receipt.PlanDigest != compiled.Digest || receipt.ControlDigest == "" {
		t.Fatalf("pause receipt is not bound: %+v", receipt)
	}
	retry := activationRequest(t)
	retry.Epoch = 2
	if _, err := gate.Activate(retry); !HasPauseCode(err, PausedStage) {
		t.Fatalf("activation while paused = %v, want PAUSED_STAGE", err)
	}
	set, err := FreezeCohorts(cohortRequest())
	if err != nil {
		t.Fatal(err)
	}
	decision, err := EvaluateHealth(plan, "canary", pauseTelemetry(compiled.Digest), pauseThresholds())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Expand(ExpansionRequest{
		Plan: plan, Cohorts: set, Decision: decision,
		Artifact:     Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
		CohortDigest: broadCohortDigest(t), Epoch: 3,
	}); !HasPauseCode(err, PausedStage) {
		t.Fatalf("expansion while paused = %v, want PAUSED_STAGE", err)
	}

	// Running unsafe work drains at its declared safe points alongside
	// the pause: the fence stops leases, safe work completes, unsafe
	// work reconciles ambiguous, and only then does the stage resume.
	work := drain.NewDrainer()
	if err := work.Acquire("lease/canary-1", "effect/rollout"); err != nil {
		t.Fatal(err)
	}
	workEpoch := work.Epoch()
	fence, err := work.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := work.Heartbeat("lease/canary-1", workEpoch, drain.RegionSafe); err != nil {
		t.Fatal(err)
	}
	if err := work.Complete("lease/canary-1", workEpoch); err != nil {
		t.Fatal(err)
	}
	drainReport, err := work.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(drainReport.Safe) != 0 || len(drainReport.Ambiguous) != 0 || drainReport.Fence != fence {
		t.Fatalf("drain = %+v, want the completed lease retired under its fence", drainReport)
	}

	// Resume revalidates plan, health, authority and cohort, then
	// reactivates at a fresh epoch past the pause.
	resumed, err := gate.Resume(ResumeRequest{
		Plan: plan, Cohorts: set, Stage: "canary", Epoch: 3,
		Telemetry: pauseTelemetry(compiled.Digest), Thresholds: pauseThresholds(),
		Artifact:     Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
		CohortDigest: canaryCohortDigest(t),
	})
	if err != nil {
		t.Fatalf("validated resume refused: %v", err)
	}
	// Chain continuity is plan, stage and artifact with a fenced epoch:
	// the control digest must rotate (it seals the epoch), never repeat.
	if resumed.Stage != "canary" || resumed.Epoch != 3 || resumed.PlanDigest != compiled.Digest ||
		resumed.Artifact != "WORKFLOW" || resumed.Version != "v1.2.0" {
		t.Fatalf("resume receipt breaks the chain: %+v", resumed)
	}
	if resumed.ControlDigest == receipt.ControlDigest {
		t.Fatal("resume reused the pre-pause control digest: the epoch is not fenced in")
	}
	if gate.Paused("canary") {
		t.Fatal("stage still paused after resume")
	}
}

func TestTodo_ROLLOUT_005_Mutation(t *testing.T) {
	plan := validPlan()
	compiled, err := Compile(plan)
	if err != nil {
		t.Fatal(err)
	}
	fresh := func(t *testing.T) *PauseGate {
		t.Helper()
		gate := NewPauseGate(newTestActivationLedger(t))
		if _, err := gate.Activate(activationRequest(t)); err != nil {
			t.Fatal(err)
		}
		return gate
	}

	// Mutant 1: pausing a stage that never activated is refused.
	gate := NewPauseGate(newTestActivationLedger(t))
	if _, err := gate.Pause(plan, "canary", 2, "reason"); !HasPauseCode(err, UnknownActivationStage) {
		t.Fatalf("pause-before-activation = %v, want UNKNOWN_ACTIVATION_STAGE", err)
	}
	// Mutant 2: pausing twice is refused.
	gate = fresh(t)
	if _, err := gate.Pause(plan, "canary", 2, "reason"); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Pause(plan, "canary", 3, "reason"); !HasPauseCode(err, AlreadyPaused) {
		t.Fatalf("double pause = %v, want ALREADY_PAUSED", err)
	}
	// Mutant 3: resuming a stage that is not paused is refused.
	if _, err := fresh(t).Resume(ResumeRequest{Plan: plan, Stage: "canary", Epoch: 3}); !HasPauseCode(err, NotPaused) {
		t.Fatalf("resume-without-pause = %v", err)
	}
	// Mutant 4: resume under a changed plan is stale.
	gate = fresh(t)
	if _, err := gate.Pause(plan, "canary", 2, "reason"); err != nil {
		t.Fatal(err)
	}
	changed := plan
	changed.Owner = "someone-else"
	set, err := FreezeCohorts(cohortRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Resume(ResumeRequest{
		Plan: changed, Cohorts: set, Stage: "canary", Epoch: 3,
		Telemetry: pauseTelemetry(compiled.Digest), Thresholds: pauseThresholds(),
		Artifact:     Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
		CohortDigest: canaryCohortDigest(t),
	}); !HasPauseCode(err, StalePlan) {
		t.Fatalf("changed-plan resume = %v, want STALE_PLAN", err)
	}
	// Mutant 5: resume on HOLD health stays fenced.
	gate = fresh(t)
	if _, err := gate.Pause(plan, "canary", 2, "reason"); err != nil {
		t.Fatal(err)
	}
	held := pauseTelemetry(compiled.Digest)
	held.BurnPerMille = 500
	if _, err := gate.Resume(ResumeRequest{
		Plan: plan, Cohorts: set, Stage: "canary", Epoch: 3,
		Telemetry: held, Thresholds: pauseThresholds(),
		Artifact:     Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
		CohortDigest: canaryCohortDigest(t),
	}); !HasHealthCode(err, HealthBlocked) {
		t.Fatalf("held-health resume = %v, want HEALTH_BLOCKED", err)
	}
	if !gate.Paused("canary") {
		t.Fatal("failed resume unpaused the stage")
	}
	// Mutant 6: resume at or below the pause epoch is stale.
	gate = fresh(t)
	if _, err := gate.Pause(plan, "canary", 2, "reason"); err != nil {
		t.Fatal(err)
	}
	for _, epoch := range []uint64{1, 2} {
		if _, err := gate.Resume(ResumeRequest{
			Plan: plan, Cohorts: set, Stage: "canary", Epoch: epoch,
			Telemetry: pauseTelemetry(compiled.Digest), Thresholds: pauseThresholds(),
			Artifact:     Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
			CohortDigest: canaryCohortDigest(t),
		}); !HasPauseCode(err, StaleResumeEpoch) {
			t.Fatalf("resume(%d) = %v, want STALE_RESUME_EPOCH", epoch, err)
		}
	}
	// Mutant 7: resume under the wrong artifact refuses authority.
	gate = fresh(t)
	if _, err := gate.Pause(plan, "canary", 2, "reason"); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Resume(ResumeRequest{
		Plan: plan, Cohorts: set, Stage: "canary", Epoch: 3,
		Telemetry: pauseTelemetry(compiled.Digest), Thresholds: pauseThresholds(),
		Artifact:     Artifact{Type: ArtifactWorkflow, Version: "v9.9.9"},
		CohortDigest: canaryCohortDigest(t),
	}); !HasPauseCode(err, StaleResumeAuthority) {
		t.Fatalf("wrong-artifact resume = %v, want STALE_RESUME_AUTHORITY", err)
	}
	// Mutant 8: resume against a foreign cohort is refused.
	gate = fresh(t)
	if _, err := gate.Pause(plan, "canary", 2, "reason"); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Resume(ResumeRequest{
		Plan: plan, Cohorts: set, Stage: "canary", Epoch: 3,
		Telemetry: pauseTelemetry(compiled.Digest), Thresholds: pauseThresholds(),
		Artifact:     Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
		CohortDigest: "sha256:foreign",
	}); !HasPauseCode(err, StaleResumeCohort) {
		t.Fatalf("foreign-cohort resume = %v, want STALE_RESUME_COHORT", err)
	}
}
