package iacrecovery

import (
	"strings"
	"testing"
)

func mustDeploy(t *testing.T, store *Store, in DeployInput) *Rehearsal {
	t.Helper()
	rehearsal, err := Deploy(store, in)
	if err != nil {
		t.Fatalf("Deploy(%s): %v", in.CellID, err)
	}
	return rehearsal
}

func seedRehearsalInput() ReadinessInput {
	return ReadinessInput{
		ConfigDigest: "sha256:config",
		DataDigest:   "sha256:data",
		KeyRefs:      []string{"keys/backup-kek@v3"},
		KnownKeyRefs: []string{"keys/backup-kek@v3"},
	}
}

func mustConform(t *testing.T, rehearsal *Rehearsal) {
	t.Helper()
	if err := Conform(rehearsal, seedRehearsalInput()); err != nil {
		t.Fatalf("Conform: %v", err)
	}
}

func mustDrain(t *testing.T, rehearsal *Rehearsal, inFlight int) {
	t.Helper()
	if err := Drain(rehearsal, inFlight); err != nil {
		t.Fatalf("Drain: %v", err)
	}
}

func mustFailover(t *testing.T, rehearsal *Rehearsal) {
	t.Helper()
	if err := Failover(rehearsal); err != nil {
		t.Fatalf("Failover: %v", err)
	}
}

func mustFailback(t *testing.T, rehearsal *Rehearsal) {
	t.Helper()
	if err := Failback(rehearsal); err != nil {
		t.Fatalf("Failback: %v", err)
	}
}

func mustTeardown(t *testing.T, rehearsal *Rehearsal) RehearsalEvidence {
	t.Helper()
	evidence, err := Teardown(rehearsal)
	if err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if got := VerifyEvidence(evidence); got != "" {
		t.Fatalf("VerifyEvidence: %q", got)
	}
	return evidence
}

func seedRehearsal(t *testing.T) (*Store, *Rehearsal) {
	t.Helper()
	store := mustStore(t)
	mustAppend(t, store, Copy{ID: "copy-data-1", SourceID: "prod-pg", Digest: "sha256:data", Locked: true}, "recovery-admin")
	mustAppend(t, store, Copy{ID: "copy-config-1", SourceID: "prod-config", Digest: "sha256:config", Locked: true}, "recovery-admin")
	return store, mustDeploy(t, store, DeployInput{CellID: "rehearsal-cell-1", CopyIDs: []string{"copy-data-1", "copy-config-1"}, Disposable: true})
}

// TestTodo_IAC_012_Integration: a second cell rehearses end to end and
// its evidence verifies independently.
func TestTodo_IAC_012_Integration(t *testing.T) {
	store, rehearsal := seedRehearsal(t)
	mustConform(t, rehearsal)
	mustDrain(t, rehearsal, 0)
	mustFailover(t, rehearsal)
	mustFailback(t, rehearsal)
	first := mustTeardown(t, rehearsal)
	secondCell := mustDeploy(t, store, DeployInput{CellID: "rehearsal-cell-2", CopyIDs: []string{"copy-data-1", "copy-config-1"}, Disposable: true})
	mustConform(t, secondCell)
	mustDrain(t, secondCell, 5)
	mustFailover(t, secondCell)
	mustFailback(t, secondCell)
	second := mustTeardown(t, secondCell)
	if first.Digest == second.Digest {
		t.Fatal("distinct cells share rehearsal evidence")
	}
}

// TestTodo_IAC_012_Fault: skipped phases, unknown copies, failed
// conformance and wrong-environment teardown refuse with exact codes.
func TestTodo_IAC_012_Fault(t *testing.T) {
	store, rehearsal := seedRehearsal(t)
	// Failover before drain refuses: phases run in order.
	if err := Failover(rehearsal); err == nil {
		t.Fatal("unordered failover admitted")
	} else if !strings.Contains(err.Error(), CodePhaseOrder) {
		t.Fatalf("expected %s, got %v", CodePhaseOrder, err)
	}
	// Unknown copies refuse at deploy.
	if _, err := Deploy(store, DeployInput{CellID: "ghost", CopyIDs: []string{"copy-missing"}, Disposable: true}); err == nil {
		t.Fatal("unknown-copy deploy admitted")
	}
	// Failed readiness fails conformance, never silently.
	bad := seedRehearsalInput()
	bad.DataDigest = "sha256:tampered"
	if err := Conform(rehearsal, bad); err == nil {
		t.Fatal("tampered conformance admitted")
	} else if !strings.Contains(err.Error(), CodeConformFailed) {
		t.Fatalf("expected %s, got %v", CodeConformFailed, err)
	}
	// Teardown mid-rehearsal refuses: the chain must complete.
	if _, err := Teardown(rehearsal); err == nil {
		t.Fatal("mid-rehearsal teardown admitted")
	}
	// Post-teardown operations refuse.
	mustConform(t, rehearsal)
	mustDrain(t, rehearsal, 1)
	mustFailover(t, rehearsal)
	mustFailback(t, rehearsal)
	mustTeardown(t, rehearsal)
	if _, err := Teardown(rehearsal); err == nil {
		t.Fatal("double teardown admitted")
	}
	if err := Conform(rehearsal, seedRehearsalInput()); err == nil {
		t.Fatal("post-teardown conform admitted")
	}
}

// TestTodo_IAC_012_Conformance: the conformance vector pins phase
// order, chain integrity and cell binding.
func TestTodo_IAC_012_Conformance(t *testing.T) {
	_, rehearsal := seedRehearsal(t)
	mustConform(t, rehearsal)
	mustDrain(t, rehearsal, 3)
	mustFailover(t, rehearsal)
	mustFailback(t, rehearsal)
	evidence := mustTeardown(t, rehearsal)
	want := []string{PhaseDeploy, PhaseConform, PhaseDrain, PhaseFailover, PhaseFailback, PhaseTeardown}
	for i, receipt := range evidence.Phases {
		if receipt.Phase != want[i] || receipt.CellID != "rehearsal-cell-1" {
			t.Fatalf("phase %d: %+v", i, receipt)
		}
	}
	// A forged detail breaks the chain.
	forged := evidence
	forged.Phases[2].Detail = "drained=999"
	if got := VerifyEvidence(forged); got == "" {
		t.Fatal("forged phase verifies")
	}
	// A reordered chain breaks verification.
	reordered := evidence
	reordered.Phases[1], reordered.Phases[2] = reordered.Phases[2], reordered.Phases[1]
	if got := VerifyEvidence(reordered); got == "" {
		t.Fatal("reordered chain verifies")
	}
}

// TestTodo_IAC_012_Recovery: a failed conformance heals by re-proof;
// the completed rehearsal tears down receipt-stable.
func TestTodo_IAC_012_Recovery(t *testing.T) {
	_, rehearsal := seedRehearsal(t)
	bad := seedRehearsalInput()
	bad.DataDigest = "sha256:tampered"
	if err := Conform(rehearsal, bad); err == nil {
		t.Fatal("tampered conformance admitted")
	}
	mustConform(t, rehearsal)
	mustDrain(t, rehearsal, 1)
	mustFailover(t, rehearsal)
	mustFailback(t, rehearsal)
	first := mustTeardown(t, rehearsal)
	// Re-running the identical rehearsal reproduces identical evidence.
	_, again := seedRehearsal(t)
	mustConform(t, again)
	mustDrain(t, again, 1)
	mustFailover(t, again)
	mustFailback(t, again)
	second := mustTeardown(t, again)
	if first.Digest != second.Digest {
		t.Fatalf("rehearsal drift:\n got=%q\nwant=%q", second.Digest, first.Digest)
	}
}
