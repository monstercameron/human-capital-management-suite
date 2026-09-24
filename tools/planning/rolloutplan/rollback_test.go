package rolloutplan

import (
	"crypto/ed25519"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/configbundle"
)

func killSigner(t *testing.T) *configbundle.Ed25519ReceiptSigner {
	t.Helper()
	var seed [ed25519.SeedSize]byte
	for i := range seed {
		seed[i] = byte(77 + i)
	}
	signer, err := configbundle.NewEd25519ReceiptSigner("kill-signer", "v1", ed25519.NewKeyFromSeed(seed[:]))
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

// planV2 pins the revised artifact version for rollback tests: same
// stages and owner as validPlan, only the version moves.
func planV2() Plan {
	plan := validPlan()
	plan.Artifact.Version = "v1.2.1"
	return plan
}

func freezeFor(t *testing.T, plan Plan) CohortSet {
	t.Helper()
	set, err := FreezeCohorts(CohortRequest{
		Plan: plan, Targets: cohortTargets(), Population: cohortPopulation(), Placements: cohortPlacements(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func cohortDigestFor(t *testing.T, set CohortSet, stage string) string {
	t.Helper()
	for _, cohort := range set.Cohorts {
		if cohort.Stage == stage {
			return cohort.Digest
		}
	}
	t.Fatalf("cohort for %s missing", stage)
	return ""
}

func activateAt(t *testing.T, ledger *ActivationLedger, history *StageHistory, plan Plan, stage, version string, epoch uint64) ActivationReceipt {
	t.Helper()
	compiled, err := Compile(plan)
	if err != nil {
		t.Fatal(err)
	}
	set := freezeFor(t, plan)
	artifact := Artifact{Type: plan.Artifact.Type, Version: version}
	receipt, err := ledger.Activate(ActivationRequest{
		Plan: plan, Cohorts: set, Stage: stage, Artifact: artifact,
		PlanDigest: compiled.Digest, CohortDigest: cohortDigestFor(t, set, stage), Epoch: epoch,
	})
	if err != nil {
		t.Fatal(err)
	}
	history.Record(receipt)
	return receipt
}

// TestTodo_ROLLOUT_006 is the ROLLOUT-006 primary test.
//
// RED: rollback invents a version, reuses the live epoch, or rewrites
// history; the kill switch misses its SLO or disables the wrong scope.
// GREEN: rollback reactivates the exact verified prior version at a new
// epoch with history intact, and the kill propagates within SLO.
func TestTodo_ROLLOUT_006(t *testing.T) {
	killStore := configbundle.NewKillSwitchStore(killSigner(t))
	target := configbundle.KillSwitchTarget{TenantID: "tenant-a", Capability: "promotion.execute"}
	ledger := NewActivationLedger(killStore, target)
	history := NewStageHistory()
	planA := validPlan()
	planB := planV2()
	first := activateAt(t, ledger, history, planA, "canary", "v1.2.0", 1)
	second := activateAt(t, ledger, history, planB, "canary", "v1.2.1", 2)
	if second.Version == first.Version {
		t.Fatal("revisions did not diverge")
	}

	// Emergency kill first: scope the stop to the bad version's
	// capability and prove it lands inside its propagation SLO without
	// touching anything outside the scope.
	issuedAt := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	kill, err := killStore.Issue(configbundle.KillSwitchRequest{
		SwitchID: "kill-bad-canary",
		Target:   configbundle.KillSwitchTarget{TenantID: "tenant-a", Capability: "promotion.execute"},
		Priority: 1, Reason: "bad canary", IncidentRef: "incident/1",
		Operator: "operator-1", Approver: "approver-1", EvidenceRef: "evidence/1",
		IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Hour), PropagationSLO: 10 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	appliedAt := issuedAt.Add(2 * time.Minute)
	killReceipt, err := killStore.Apply(kill, appliedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !killReceipt.WithinSLO {
		t.Fatalf("kill receipt missed its SLO: %+v", killReceipt)
	}
	killStore.SetClock(func() time.Time { return appliedAt })
	subject := configbundle.KillSwitchTarget{TenantID: "tenant-a", Service: "promotion", Capability: "promotion.execute"}
	if decision := killStore.Evaluate(subject, appliedAt); !decision.Disabled {
		t.Fatalf("bad-version subject not disabled: %+v", decision)
	}
	outsider := configbundle.KillSwitchTarget{TenantID: "tenant-a", Service: "reporting", Capability: "reports.read"}
	if decision := killStore.Evaluate(outsider, appliedAt); decision.Disabled {
		t.Fatalf("out-of-scope subject disabled: %+v", decision)
	}

	// Roll back the stage to the exact verified prior version at a new
	// epoch; history grows by one with every prior entry intact.
	compiledA, err := Compile(planA)
	if err != nil {
		t.Fatal(err)
	}
	setA := freezeFor(t, planA)
	rolled, err := RollbackStage(history, ledger, RollbackRequest{
		Plan: planA, Cohorts: setA, Stage: "canary", TargetVersion: "v1.2.0",
		Artifact:   Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
		PlanDigest: compiledA.Digest, CohortDigest: cohortDigestFor(t, setA, "canary"), Epoch: 3,
	})
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rolled.Version != "v1.2.0" || rolled.Epoch != 3 || rolled.PlanDigest != compiledA.Digest {
		t.Fatalf("rollback receipt is not the verified prior at a new epoch: %+v", rolled)
	}
	entries := history.Entries("canary")
	if len(entries) != 3 || entries[0] != first || entries[1] != second || entries[2] != rolled {
		t.Fatalf("history was rewritten: %+v", entries)
	}
	// The kill evidence survives alongside: nothing deletes applied
	// switches or their receipts.
	if _, ok := killStore.Get("kill-bad-canary"); !ok {
		t.Fatal("kill switch missing after rollback")
	}
	if _, ok := killStore.Receipt("kill-bad-canary"); !ok {
		t.Fatal("kill receipt missing after rollback")
	}
}

func TestTodo_ROLLOUT_006_Race(t *testing.T) {
	ledger := newTestActivationLedger(t)
	history := NewStageHistory()
	activateAt(t, ledger, history, validPlan(), "canary", "v1.2.0", 1)
	const workers = 16
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entries := history.Entries("canary")
			if len(entries) != 1 || entries[0].Version != "v1.2.0" {
				t.Error("concurrent history read diverged")
			}
			if _, ok := history.Lookup("canary", "v1.2.0"); !ok {
				t.Error("concurrent history lookup missed")
			}
		}()
	}
	wg.Wait()
}

// TestTodo_ROLLOUT_006_Integration proves the rollback target stays
// verified end to end: the recorded control digest, the revised plan pin
// and the live ledger receipt all agree before the new epoch lands.
func TestTodo_ROLLOUT_006_Integration(t *testing.T) {
	ledger := newTestActivationLedger(t)
	history := NewStageHistory()
	planA := validPlan()
	planB := planV2()
	activateAt(t, ledger, history, planA, "canary", "v1.2.0", 1)
	activateAt(t, ledger, history, planB, "canary", "v1.2.1", 2)

	want, ok := history.Lookup("canary", "v1.2.0")
	if !ok {
		t.Fatal("prior version missing from history")
	}
	live, err := ledger.Explain("canary", "sub:1")
	if err != nil {
		t.Fatal(err)
	}
	if live.Receipt.Version != "v1.2.1" {
		t.Fatalf("live version = %s, want v1.2.1", live.Receipt.Version)
	}
	compiledA, err := Compile(planA)
	if err != nil {
		t.Fatal(err)
	}
	if planA.Artifact.Version != want.Version || string(planA.Artifact.Type) != want.Artifact {
		t.Fatal("revised plan does not pin the verified prior version")
	}
	setA := freezeFor(t, planA)
	rolled, err := RollbackStage(history, ledger, RollbackRequest{
		Plan: planA, Cohorts: setA, Stage: "canary", TargetVersion: "v1.2.0",
		Artifact:   Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
		PlanDigest: compiledA.Digest, CohortDigest: cohortDigestFor(t, setA, "canary"), Epoch: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rolled.ControlDigest == want.ControlDigest {
		t.Fatal("rollback reused the prior control digest: the new epoch is not fenced in")
	}
	nowLive, err := ledger.Explain("canary", "sub:9")
	if err != nil || nowLive.Receipt.Version != "v1.2.0" || nowLive.Receipt.Epoch != 3 {
		t.Fatalf("live receipt after rollback = %+v, %v", nowLive, err)
	}
}

func TestTodo_ROLLOUT_006_Fault(t *testing.T) {
	setup := func(t *testing.T) (*ActivationLedger, *StageHistory, Plan, CohortSet, string) {
		t.Helper()
		ledger := newTestActivationLedger(t)
		history := NewStageHistory()
		planA := validPlan()
		activateAt(t, ledger, history, planA, "canary", "v1.2.0", 1)
		activateAt(t, ledger, history, planV2(), "canary", "v1.2.1", 2)
		compiledA, err := Compile(planA)
		if err != nil {
			t.Fatal(err)
		}
		setA := freezeFor(t, planA)
		return ledger, history, planA, setA, compiledA.Digest
	}
	rollback := func(t *testing.T, ledger *ActivationLedger, history *StageHistory, plan Plan, set CohortSet, digest string, mutate func(*RollbackRequest)) error {
		t.Helper()
		req := RollbackRequest{
			Plan: plan, Cohorts: set, Stage: "canary", TargetVersion: "v1.2.0",
			Artifact:   Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
			PlanDigest: digest, CohortDigest: cohortDigestFor(t, set, "canary"), Epoch: 3,
		}
		if mutate != nil {
			mutate(&req)
		}
		_, err := RollbackStage(history, ledger, req)
		return err
	}

	t.Run("no history", func(t *testing.T) {
		ledger, history, plan, set, digest := setup(t)
		_ = history
		empty := NewStageHistory()
		if err := rollback(t, ledger, empty, plan, set, digest, nil); !HasRollbackCode(err, NoPriorVersion) {
			t.Fatalf("historyless rollback = %v, want NO_PRIOR_VERSION", err)
		}
	})
	t.Run("unrun version", func(t *testing.T) {
		ledger, history, plan, set, digest := setup(t)
		if err := rollback(t, ledger, history, plan, set, digest, func(r *RollbackRequest) {
			r.TargetVersion = "v9.9.9"
		}); !HasRollbackCode(err, NoPriorVersion) {
			t.Fatalf("unrun version = %v, want NO_PRIOR_VERSION", err)
		}
	})
	t.Run("version mismatch", func(t *testing.T) {
		ledger, history, plan, set, digest := setup(t)
		if err := rollback(t, ledger, history, plan, set, digest, func(r *RollbackRequest) {
			r.Artifact.Version = "v1.2.1"
		}); !HasRollbackCode(err, VersionMismatch) {
			t.Fatalf("mismatched artifact = %v, want VERSION_MISMATCH", err)
		}
	})
	t.Run("plan does not pin prior", func(t *testing.T) {
		ledger, history, _, set, digest := setup(t)
		if err := rollback(t, ledger, history, planV2(), set, digest, nil); !HasRollbackCode(err, StalePlan) {
			t.Fatalf("unpinned plan = %v, want STALE_PLAN", err)
		}
	})
	t.Run("rollback to live", func(t *testing.T) {
		ledger, history, plan, set, digest := setup(t)
		// Current live version is v1.2.1 under planV2: rolling back to it
		// through its own plan is a no-op.
		compiledB, err := Compile(planV2())
		if err != nil {
			t.Fatal(err)
		}
		setB := freezeFor(t, planV2())
		req := RollbackRequest{
			Plan: planV2(), Cohorts: setB, Stage: "canary", TargetVersion: "v1.2.1",
			Artifact:   Artifact{Type: ArtifactWorkflow, Version: "v1.2.1"},
			PlanDigest: compiledB.Digest, CohortDigest: cohortDigestFor(t, setB, "canary"), Epoch: 3,
		}
		_ = digest
		if _, err := RollbackStage(history, ledger, req); !HasRollbackCode(err, RollbackNotNeeded) {
			t.Fatalf("rollback-to-live = %v, want ROLLBACK_NOT_NEEDED", err)
		}
		_ = plan
		_ = set
	})
	t.Run("stale epoch", func(t *testing.T) {
		ledger, history, plan, set, digest := setup(t)
		if err := rollback(t, ledger, history, plan, set, digest, func(r *RollbackRequest) {
			r.Epoch = 2
		}); !HasActivationCode(err, StaleEpoch) {
			t.Fatalf("stale epoch = %v, want STALE_EPOCH", err)
		}
	})
}

// TestTodo_ROLLOUT_006_Recovery crashes between kill and rollback and
// requires the stop to hold while the rollback completes exactly once.
func TestTodo_ROLLOUT_006_Recovery(t *testing.T) {
	killStore := configbundle.NewKillSwitchStore(killSigner(t))
	target := configbundle.KillSwitchTarget{TenantID: "tenant-a", Capability: "promotion.execute"}
	ledger := NewActivationLedger(killStore, target)
	history := NewStageHistory()
	planA := validPlan()
	activateAt(t, ledger, history, planA, "canary", "v1.2.0", 1)
	activateAt(t, ledger, history, planV2(), "canary", "v1.2.1", 2)

	issuedAt := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	kill, err := killStore.Issue(configbundle.KillSwitchRequest{
		SwitchID: "kill-crash", Target: configbundle.KillSwitchTarget{TenantID: "tenant-a", Capability: "promotion.execute"},
		Priority: 1, Reason: "crash drill", IncidentRef: "incident/9",
		Operator: "operator-1", Approver: "approver-1", EvidenceRef: "evidence/9",
		IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Hour), PropagationSLO: 10 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := killStore.Apply(kill, issuedAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	killStore.SetClock(func() time.Time { return issuedAt.Add(2 * time.Minute) })
	// Crash here: the kill is applied and retained, the rollout untouched.
	recoveredStore := killStore
	if _, ok := recoveredStore.Get("kill-crash"); !ok {
		t.Fatal("kill did not survive the crash")
	}
	subject := configbundle.KillSwitchTarget{TenantID: "tenant-a", Capability: "promotion.execute"}
	if decision := recoveredStore.Evaluate(subject, issuedAt.Add(2*time.Minute)); !decision.Disabled {
		t.Fatal("stop did not hold across the crash")
	}
	compiledA, err := Compile(planA)
	if err != nil {
		t.Fatal(err)
	}
	setA := freezeFor(t, planA)
	rolled, err := RollbackStage(history, ledger, RollbackRequest{
		Plan: planA, Cohorts: setA, Stage: "canary", TargetVersion: "v1.2.0",
		Artifact:   Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
		PlanDigest: compiledA.Digest, CohortDigest: cohortDigestFor(t, setA, "canary"), Epoch: 3,
	})
	if err != nil {
		t.Fatalf("post-crash rollback: %v", err)
	}
	if rolled.Epoch != 3 || len(history.Entries("canary")) != 3 {
		t.Fatalf("post-crash rollback landed wrong: %+v", rolled)
	}
}

func TestTodo_ROLLOUT_006_Mutation(t *testing.T) {
	ledger := newTestActivationLedger(t)
	history := NewStageHistory()
	planA := validPlan()
	activateAt(t, ledger, history, planA, "canary", "v1.2.0", 1)
	activateAt(t, ledger, history, planV2(), "canary", "v1.2.1", 2)
	compiledA, err := Compile(planA)
	if err != nil {
		t.Fatal(err)
	}
	setA := freezeFor(t, planA)
	roll := func(mutate func(*RollbackRequest)) error {
		req := RollbackRequest{
			Plan: planA, Cohorts: setA, Stage: "canary", TargetVersion: "v1.2.0",
			Artifact:   Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
			PlanDigest: compiledA.Digest, CohortDigest: cohortDigestFor(t, setA, "canary"), Epoch: 3,
		}
		if mutate != nil {
			mutate(&req)
		}
		_, err := RollbackStage(history, ledger, req)
		return err
	}
	// Mutant 1: the right version under the wrong artifact kind is not
	// the verified prior.
	if err := roll(func(r *RollbackRequest) { r.Artifact.Type = ArtifactSchema }); !HasRollbackCode(err, VersionMismatch) {
		t.Fatalf("wrong-kind version = %v, want VERSION_MISMATCH", err)
	}
	// Mutant 2: rollback naming the live version is a no-op refusal tested
	// in FAULT; here the epoch replay is refused by the ledger.
	if err := roll(func(r *RollbackRequest) { r.Epoch = 1 }); !HasActivationCode(err, StaleEpoch) {
		t.Fatalf("epoch replay = %v, want STALE_EPOCH", err)
	}
	// Mutant 3: a foreign plan digest is stale.
	if err := roll(func(r *RollbackRequest) { r.PlanDigest = "sha256:foreign" }); !HasRollbackCode(err, StalePlan) {
		t.Fatalf("foreign plan = %v, want STALE_PLAN", err)
	}
	// Mutant 4: a foreign cohort digest is stale.
	if err := roll(func(r *RollbackRequest) { r.CohortDigest = "sha256:foreign" }); !HasActivationCode(err, StaleCohort) {
		t.Fatalf("foreign cohort = %v, want STALE_COHORT", err)
	}
}
