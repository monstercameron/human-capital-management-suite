package agentsecurity

import "testing"

func TestTodo_AGENTP_016_PersonaRunFenceBindsDistinctIdentifiers(t *testing.T) {
	switchBoard := NewKillSwitch()
	if err := switchBoard.Grant(Lease{ID: "security-lease-9", PersonaID: "p1", InstallationID: "room-a", Tenant: "tenant-a"}); err != nil {
		t.Fatal(err)
	}
	fence := NewPersonaRunFence(switchBoard)
	runID := PersonaRunID("run-admission-42")
	leaseID := KillSwitchLeaseID("security-lease-9")
	if err := fence.Bind(runID, leaseID); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := fence.Bind(runID, leaseID); err != nil {
		t.Fatalf("exact-pair Bind retry: %v", err)
	}

	started := 0
	if _, err := fence.RunStep(runID, func() error { started++; return nil }); err != nil {
		t.Fatalf("bound run step: %v", err)
	}
	if started != 1 {
		t.Fatalf("step started %d times", started)
	}

	// A credential/security lease identifier cannot stand in for the distinct
	// run identifier, even when both are represented as strings at a boundary.
	if _, err := fence.RunStep(PersonaRunID(leaseID), func() error { started++; return nil }); err == nil {
		t.Fatal("security lease identifier was accepted as an unbound run ID")
	}
	if err := fence.Bind(PersonaRunID("run-admission-43"), leaseID); err == nil {
		t.Fatal("one kill-switch lease was rebound to another run")
	}
	if err := fence.Bind(runID, KillSwitchLeaseID("unknown")); err == nil {
		t.Fatal("unknown kill-switch lease was bound")
	}

	if revoked, _ := switchBoard.Disable(DisableScope{PersonaID: "p1", InstallationID: "room-a", Tenant: "tenant-a"}); revoked != 1 {
		t.Fatalf("revoked=%d, want 1", revoked)
	}
	_, err := fence.RunStep(runID, func() error { started++; return nil })
	if err == nil {
		t.Fatal("bound run started a step after suspension")
	}
	if started != 1 {
		t.Fatalf("post-suspend step ran; total starts=%d", started)
	}
	if err := fence.Bind(runID, leaseID); err == nil {
		t.Fatal("revoked exact pair was rebound")
	}
}
