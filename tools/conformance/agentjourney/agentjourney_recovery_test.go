package agentjourney

import (
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

// TestTodo_AGENT_047_Recovery proves the journey survives a restart and a
// revocation: the rebuilt world replays the digest firing and the mention
// admission without a second run or post, and after the kill switch and
// suspension engage, new mentions stop and no new write lease is granted.
func TestTodo_AGENT_047_Recovery(t *testing.T) {
	world := NewWorld(t)
	world.DriveFull()

	dir := t.TempDir()
	world.Snapshot(dir)

	t.Run("restart replays without a second run", func(t *testing.T) {
		restored := RestoreWorld(t, filepath.Join(dir, "agentjourney_snapshot.json"))
		replay := restored.PostDigest()
		if !replay.Duplicate {
			t.Fatalf("replay=%+v, want the duplicate firing marker", replay)
		}
		if replay.Receipt != world.Digest.Receipt {
			t.Fatalf("replay=%+v original=%+v, want the same persisted receipt", replay.Receipt, world.Digest.Receipt)
		}
		if got := restored.DigestPostCount(); got != 0 {
			t.Fatalf("restored digest posts=%d, want no second scheduled post", got)
		}
		again, created, err := restored.Readmit(agentrun.SourceSchedule)
		if err != nil {
			t.Fatal(err)
		}
		if created {
			t.Fatalf("re-admission=%+v, want the existing record, not a new run", again)
		}
		if again.ID != world.Digest.Receipt.RunRequestID {
			t.Fatalf("re-admission=%+v receipt=%+v, want the same run", again, world.Digest.Receipt)
		}
	})

	t.Run("revocation fences later work", func(t *testing.T) {
		restored := RestoreWorld(t, filepath.Join(dir, "agentjourney_snapshot.json"))
		restored.RevokeAgent()
		invocations, err := restored.MentionAs(journeyOwner, journeyDM, "still there @"+journeyPersona)
		if err != nil {
			t.Fatalf("revoked mention err=%v, want a refusal, not an error", err)
		}
		if len(invocations) != 0 {
			t.Fatalf("revoked invocations=%+v, want none", invocations)
		}
		if err := restored.GrantWriteLease(); err == nil {
			t.Fatal("a write lease was granted after revocation, want a refusal")
		}
		if len(restored.Journal.Runs) != 0 {
			t.Fatalf("runs=%+v, want no run after revocation", restored.Journal.Runs)
		}
	})
}
