package rolloutplan

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/configbundle"
)

func rolloutKillBindings(t *testing.T) (*configbundle.KillSwitchStore, configbundle.KillSwitchTarget) {
	t.Helper()
	store := configbundle.NewKillSwitchStore(killSigner(t))
	return store, configbundle.KillSwitchTarget{TenantID: "tenant-a", Capability: "promotion.execute"}
}

func newTestActivationLedger(t *testing.T) *ActivationLedger {
	t.Helper()
	guard, target := rolloutKillBindings(t)
	return NewActivationLedger(guard, target)
}

func activationRequest(t *testing.T) ActivationRequest {
	t.Helper()
	compiled, err := Compile(validPlan())
	if err != nil {
		t.Fatal(err)
	}
	set, err := FreezeCohorts(cohortRequest())
	if err != nil {
		t.Fatal(err)
	}
	return ActivationRequest{
		Plan:         validPlan(),
		Cohorts:      set,
		Stage:        "canary",
		Artifact:     Artifact{Type: ArtifactWorkflow, Version: "v1.2.0"},
		PlanDigest:   compiled.Digest,
		CohortDigest: set.Cohorts[0].Digest,
		Epoch:        1,
	}
}

func TestTodo_ROLLOUT_003(t *testing.T) {
	ledger := newTestActivationLedger(t)
	receipt, err := ledger.Activate(activationRequest(t))
	if err != nil {
		t.Fatalf("valid canary activation rejected: %v", err)
	}
	if receipt.Stage != "canary" || receipt.Epoch != 1 || receipt.ControlDigest == "" {
		t.Fatalf("receipt incomplete: %+v", receipt)
	}
	explanation, err := ledger.Explain("canary", "sub:1")
	if err != nil {
		t.Fatal(err)
	}
	if !explanation.Member || explanation.Receipt.ControlDigest != receipt.ControlDigest {
		t.Fatalf("subject cannot explain its activation: %+v", explanation)
	}
	outsider, err := ledger.Explain("canary", "sub:9")
	if err != nil {
		t.Fatal(err)
	}
	if outsider.Member {
		t.Fatalf("non-member explained as member: %+v", outsider)
	}

	failures := []struct {
		name   string
		mutate func(*ActivationRequest)
		code   string
	}{
		{"wrong artifact", func(r *ActivationRequest) { r.Artifact.Version = "v9.9.9" }, WrongArtifact},
		{"wrong artifact type", func(r *ActivationRequest) { r.Artifact.Type = ArtifactSchema }, WrongArtifact},
		{"stale plan", func(r *ActivationRequest) { r.PlanDigest = "sha256:stale" }, StalePlan},
		{"stale cohort", func(r *ActivationRequest) { r.CohortDigest = "sha256:stale" }, StaleCohort},
		{"unknown stage", func(r *ActivationRequest) { r.Stage = "void" }, UnknownActivationStage},
	}
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			req := activationRequest(t)
			tc.mutate(&req)
			if _, err := newTestActivationLedger(t).Activate(req); !HasActivationCode(err, tc.code) {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
		})
	}

	t.Run("epoch replays fail", func(t *testing.T) {
		ledger := newTestActivationLedger(t)
		if _, err := ledger.Activate(activationRequest(t)); err != nil {
			t.Fatal(err)
		}
		if _, err := ledger.Activate(activationRequest(t)); !HasActivationCode(err, StaleEpoch) {
			t.Fatalf("replayed epoch accepted: %v", err)
		}
		advanced := activationRequest(t)
		advanced.Epoch = 3
		if _, err := ledger.Activate(advanced); err != nil {
			t.Fatalf("advanced epoch rejected: %v", err)
		}
	})
}
