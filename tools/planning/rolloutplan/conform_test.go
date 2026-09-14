package rolloutplan

import "testing"

// ROLLOUT-008 RED: service/configuration rollout conformance before
// conform.go exists.
func TestTodo_ROLLOUT_008(t *testing.T) {
	healthy := ConformSnapshot{
		Kind: ConformService, Target: "pilot-cell", Version: "cell-1.4.0",
		Epoch: 7, Healthy: true, Paused: false,
		RollbackVersion: "cell-1.3.2", Explain: "routine cell update",
		ActivationHook: "service:restart-cell",
	}
	if err := CheckConformance(healthy); err != nil {
		t.Fatalf("CheckConformance(healthy service): %v", err)
	}

	// RED seeded defect: a configuration snapshot resolving an
	// inconsistent version must reject with field/state/version, and a
	// paused service snapshot must not activate under pause.
	badVersion := healthy
	badVersion.Kind = ConformConfiguration
	badVersion.Version = "cell-1.4.0 "
	badVersion.ActivationHook = "config:apply-flags"
	err := CheckConformance(badVersion)
	rej, ok := AsRejection(err)
	if !ok {
		t.Fatalf("err=%v, want ROLLOUT_008_REJECTED", err)
	}
	if rej.Code != "ROLLOUT_008_REJECTED" || rej.Field == "" || rej.Version == "" {
		t.Fatalf("rejection=%+v, want code with offending field and version", rej)
	}
	paused := healthy
	paused.Kind = ConformService
	paused.Paused = true
	if err := CheckConformance(paused); err == nil {
		t.Fatal("paused service snapshot activated, want rejection")
	}
	// Purity: the check persists nothing and mutates no input.
	if healthy.Version != "cell-1.4.0" || healthy.Epoch != 7 {
		t.Fatal("conformance check mutated its snapshot")
	}
}
