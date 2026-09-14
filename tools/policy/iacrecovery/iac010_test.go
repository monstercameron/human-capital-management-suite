package iacrecovery

import "testing"

func TestTodo_IAC_010(t *testing.T) {
	store := mustStore(t)
	mustAppend(t, store, Copy{ID: "copy-data-1", SourceID: "prod-pg", Digest: "sha256:data", Locked: true}, "recovery-admin")
	mustAppend(t, store, Copy{ID: "copy-config-1", SourceID: "prod-config", Digest: "sha256:config", Locked: true}, "recovery-admin")

	cell := mustProvision(t, store, "recovery-cell-a",
		[]string{"copy-data-1", "copy-config-1"},
		[]string{"recovery-pg.local", "recovery-object.local"})
	input := ReadinessInput{
		ConfigDigest: "sha256:config",
		DataDigest:   "sha256:data",
		KeyRefs:      []string{"keys/backup-kek@v3"},
		KnownKeyRefs: []string{"keys/backup-kek@v3"},
	}
	receipt := mustReady(t, cell, store, input)
	if receipt.Digest == "" {
		t.Fatal("readiness receipt must carry a digest")
	}
	if got := VerifyReadiness(receipt, cell, input); got != "" {
		t.Fatalf("receipt re-verify: %q", got)
	}
}
