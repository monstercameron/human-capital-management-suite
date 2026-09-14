package iacdrift

import "testing"

func TestTodo_IAC_011(t *testing.T) {
	desired := []Resource{
		{ID: "cell-pg", Kind: "postgres", Owner: "dataops", Capability: "ledger-store", ConfigDigest: "sha256:pg-v1", Protected: true},
		{ID: "cell-bucket", Kind: "object", Owner: "dataops", Capability: "artifact-store", ConfigDigest: "sha256:bk-v1", Protected: true},
		{ID: "cell-queue", Kind: "queue", Owner: "platform", Capability: "dispatch", ConfigDigest: "sha256:q-v1"},
	}
	observed := []Resource{
		{ID: "cell-pg", Kind: "postgres", Owner: "dataops", Capability: "ledger-store", ConfigDigest: "sha256:pg-v1", Protected: true},
		{ID: "cell-bucket", Kind: "object", Owner: "dataops", Capability: "artifact-store", ConfigDigest: "sha256:bk-v2", Protected: true},
		{ID: "cell-queue", Kind: "queue", Owner: "platform", Capability: "dispatch", ConfigDigest: "sha256:q-v1"},
	}
	report := mustDetect(t, desired, observed)
	if len(report.Entries) != 1 {
		t.Fatalf("expected 1 drift entry, got %d", len(report.Entries))
	}
	entry := report.Entries[0]
	if entry.ResourceID != "cell-bucket" || entry.Owner != "dataops" || entry.Capability != "artifact-store" {
		t.Fatalf("drift entry not mapped to owner/capability: %+v", entry)
	}
	// A protected resource replacement without break-glass refuses.
	mustRefuseDestructive(t, entry, DestructiveChange{Replacement: true})
	// ...and with two-person break-glass plus recovery evidence admits.
	glass := BreakGlass{ResourceID: "cell-bucket", ApproverOne: "alice", ApproverTwo: "bob", Reason: "rotate bucket keys", ExpiresAt: "2026-10-01T00:00:00Z", RecoveryEvidence: "iac010:recovery-cell-a:sha256:ok"}
	mustAdmitDestructive(t, entry, DestructiveChange{Replacement: true}, glass)
}
