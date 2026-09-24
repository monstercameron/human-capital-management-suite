package rolloutplan

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/configbundle"
)

func appliedRolloutKill(t *testing.T, store *configbundle.KillSwitchStore, id, capability string, at time.Time) {
	t.Helper()
	store.SetClock(func() time.Time { return at.Add(2 * time.Minute) })
	control, err := store.Issue(configbundle.KillSwitchRequest{
		SwitchID: id, Target: configbundle.KillSwitchTarget{TenantID: "tenant-a", Capability: capability},
		Priority: 2, Reason: "rollout stop", IncidentRef: "incident/" + id,
		Operator: "operator-1", Approver: "approver-1", EvidenceRef: "evidence/" + id,
		IssuedAt: at, ExpiresAt: at.Add(time.Hour), PropagationSLO: 10 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(control, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_REV_043_03(t *testing.T) {
	request := activationRequest(t)
	store := configbundle.NewKillSwitchStore(killSigner(t))
	target := request.Plan.KillTarget
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	appliedRolloutKill(t, store, "stop-promotion", target.Capability, now)
	if stale := store.Evaluate(target, now.Add(2*time.Hour)); stale.Disabled {
		t.Fatalf("expired-time fixture should show why caller-supplied evaluation time is unsafe: %+v", stale)
	}
	ledger := NewActivationLedger(store, target)
	if _, err := ledger.Activate(request); !HasActivationCode(err, CapabilityKilled) {
		t.Fatalf("activation with applied kill = %v, want CAPABILITY_KILLED", err)
	}
	if _, err := ledger.Explain(request.Stage, ""); !HasActivationCode(err, UnknownActivationStage) {
		t.Fatalf("killed activation changed the stage ledger: %v", err)
	}

	other := request
	other.Plan.KillTarget = configbundle.KillSwitchTarget{TenantID: "tenant-a", Capability: "reports.read"}
	compiled, err := Compile(other.Plan)
	if err != nil {
		t.Fatal(err)
	}
	other.PlanDigest = compiled.Digest
	other.Epoch = 1
	if _, err := ledger.Activate(other); !HasActivationCode(err, KillTargetMismatch) {
		t.Fatalf("caller changed frozen capability subject = %v, want KILL_TARGET_MISMATCH", err)
	}
}

func TestTodo_REV_043_03_Security(t *testing.T) {
	request := activationRequest(t)
	if _, err := NewActivationLedger(nil, request.Plan.KillTarget).Activate(request); !HasActivationCode(err, KillStateRequired) {
		t.Fatalf("activation without CP-008 state = %v, want KILL_STATE_REQUIRED", err)
	}
	wrong := configbundle.KillSwitchTarget{TenantID: "tenant-a", Capability: "reports.read"}
	if _, err := NewActivationLedger(configbundle.NewKillSwitchStore(killSigner(t)), wrong).Activate(request); !HasActivationCode(err, KillTargetMismatch) {
		t.Fatalf("activation under a different trusted subject = %v, want KILL_TARGET_MISMATCH", err)
	}
}

func TestTodo_REV_043_03_Integration(t *testing.T) {
	plan := validPlan()
	compiled, err := Compile(plan)
	if err != nil {
		t.Fatal(err)
	}
	set, err := FreezeCohorts(cohortRequest())
	if err != nil {
		t.Fatal(err)
	}
	store, target := rolloutKillBindings(t)
	ledger := NewActivationLedger(store, target)
	if _, err := ledger.Activate(ActivationRequest{
		Plan: plan, Cohorts: set, Stage: "canary", Artifact: plan.Artifact,
		PlanDigest: compiled.Digest, CohortDigest: cohortDigestFor(t, set, "canary"), Epoch: 1,
	}); err != nil {
		t.Fatal(err)
	}
	decision, err := EvaluateHealth(plan, "canary", healthyTelemetry(compiled.Digest), healthThresholds())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	appliedRolloutKill(t, store, "stop-before-expand", target.Capability, now)
	_, err = ledger.Expand(ExpansionRequest{
		Plan: plan, Cohorts: set, Decision: decision, Artifact: plan.Artifact,
		CohortDigest: cohortDigestFor(t, set, "broad"), Epoch: 2,
	})
	if !HasActivationCode(err, CapabilityKilled) {
		t.Fatalf("expand after CP-008 apply = %v, want CAPABILITY_KILLED", err)
	}
	if _, err := ledger.Explain("broad", ""); !HasActivationCode(err, UnknownActivationStage) {
		t.Fatalf("killed expansion changed the stage ledger: %v", err)
	}
}

func TestTodo_REV_043_03_Fault(t *testing.T) {
	request := activationRequest(t)
	store, target := rolloutKillBindings(t)
	ledger := NewActivationLedger(store, target)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	control, err := store.Issue(configbundle.KillSwitchRequest{
		SwitchID: "issued-only", Target: target, Priority: 1, Reason: "not applied",
		IncidentRef: "incident/issued-only", Operator: "operator-1", Approver: "approver-1",
		EvidenceRef: "evidence/issued-only", IssuedAt: now, ExpiresAt: now.Add(time.Hour), PropagationSLO: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Receipt(control.Request.SwitchID); ok {
		t.Fatal("issued switch unexpectedly has an applied receipt")
	}
	store.SetClock(func() time.Time { return now.Add(time.Minute) })
	if _, err := ledger.Activate(request); err != nil {
		t.Fatalf("unapplied switch blocked activation: %v", err)
	}
	guardEntered := make(chan struct{})
	releaseGuard := make(chan struct{})
	guardDone := make(chan error, 1)
	go func() {
		guardDone <- store.Guard(target, func(decision configbundle.KillDecision) error {
			if decision.Disabled {
				return &ActivationError{Code: CapabilityKilled, Detail: "unexpected pre-apply decision"}
			}
			close(guardEntered)
			<-releaseGuard
			return nil
		})
	}()
	<-guardEntered
	type applyResult struct {
		receipt configbundle.AppliedKillSwitchReceipt
		err     error
	}
	applyStarted := make(chan struct{})
	applyDone := make(chan applyResult, 1)
	go func() {
		close(applyStarted)
		receipt, err := store.Apply(control, now.Add(20*time.Minute))
		applyDone <- applyResult{receipt, err}
	}()
	<-applyStarted
	select {
	case result := <-applyDone:
		t.Fatalf("CP-008 apply passed an in-flight guarded transition: %v", result.err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseGuard)
	if err := <-guardDone; err != nil {
		t.Fatal(err)
	}
	result := <-applyDone
	if result.err != nil || result.receipt.WithinSLO || result.receipt.Status != "APPLIED_LATE" {
		t.Fatalf("late apply receipt = %+v, %v", result.receipt, result.err)
	}
	store.SetClock(func() time.Time { return now.Add(21 * time.Minute) })
	if _, err := ledger.Activate(request); !HasActivationCode(err, CapabilityKilled) {
		t.Fatalf("late applied switch did not fail closed: %v", err)
	}
}

func TestTodo_REV_043_03_Recovery(t *testing.T) {
	request := activationRequest(t)
	store := configbundle.NewKillSwitchStore(killSigner(t))
	target := request.Plan.KillTarget
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	appliedRolloutKill(t, store, "temporary-stop", target.Capability, now)
	ledger := NewActivationLedger(store, target)
	if _, err := ledger.Activate(request); !HasActivationCode(err, CapabilityKilled) {
		t.Fatalf("initial killed activation = %v", err)
	}
	store.SetClock(func() time.Time { return now.Add(2 * time.Hour) })
	recovered, err := ledger.Activate(request)
	if err != nil || recovered.Epoch != request.Epoch {
		t.Fatalf("activation after CP-008 expiry = %+v, %v", recovered, err)
	}
	if _, ok := store.Receipt("temporary-stop"); !ok {
		t.Fatal("recovery replaced or deleted the applied CP-008 receipt")
	}
}
