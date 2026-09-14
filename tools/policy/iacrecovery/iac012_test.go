package iacrecovery

import (
	"strings"
	"testing"
)

func TestTodo_IAC_012(t *testing.T) {
	store := mustStore(t)
	mustAppend(t, store, Copy{ID: "copy-data-1", SourceID: "prod-pg", Digest: "sha256:data", Locked: true}, "recovery-admin")
	mustAppend(t, store, Copy{ID: "copy-config-1", SourceID: "prod-config", Digest: "sha256:config", Locked: true}, "recovery-admin")

	rehearsal := mustDeploy(t, store, DeployInput{
		CellID:     "rehearsal-cell-1",
		CopyIDs:    []string{"copy-data-1", "copy-config-1"},
		Disposable: true,
	})
	mustConform(t, rehearsal)
	mustDrain(t, rehearsal, 2)
	mustFailover(t, rehearsal)
	mustFailback(t, rehearsal)
	evidence := mustTeardown(t, rehearsal)
	if len(evidence.Phases) != 6 {
		t.Fatalf("phases=%d, want 6", len(evidence.Phases))
	}
	if evidence.Digest == "" {
		t.Fatal("rehearsal evidence must carry a digest")
	}
	// Teardown of a non-disposable stack refuses: destroy needs a
	// verified disposable target.
	kept := mustDeploy(t, store, DeployInput{
		CellID:     "kept-cell-1",
		CopyIDs:    []string{"copy-data-1", "copy-config-1"},
		Disposable: false,
	})
	if _, err := Teardown(kept); err == nil {
		t.Fatal("non-disposable stack destroyed")
	} else if !strings.Contains(err.Error(), CodeNotDisposable) {
		t.Fatalf("expected %s, got %v", CodeNotDisposable, err)
	}
}
