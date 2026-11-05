package agentjourney

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentapproval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/trigger"
)

// TestTodo_AGENT_047_Security proves the journey fails closed: outsiders
// cannot invoke the persona, suspended installs stop mentions, wrong-digest
// approvals are refused, tampered evaluation seals cannot publish, and one
// tenant's schedule firing never replays another tenant's run.
func TestTodo_AGENT_047_Security(t *testing.T) {
	t.Run("outsider mention is refused", func(t *testing.T) {
		world := NewWorld(t)
		world.SelectProvider()
		world.PublishAgent()
		world.InstallAgent()
		invocations, err := world.MentionAs(journeyOutsider, journeyDM, "hello @"+journeyPersona)
		if err != nil {
			t.Fatalf("outsider mention err=%v, want a refusal, not an error", err)
		}
		if len(invocations) != 0 {
			t.Fatalf("outsider invocations=%+v, want none", invocations)
		}
		if len(world.Journal.Refusals) != 1 || world.Journal.Refusals[0].InvokerID != journeyOutsider {
			t.Fatalf("refusals=%+v, want the outsider refusal", world.Journal.Refusals)
		}
		if len(world.Journal.Runs) != 0 {
			t.Fatalf("runs=%+v, want no run for the outsider", world.Journal.Runs)
		}
	})

	t.Run("suspended installation stops mentions", func(t *testing.T) {
		world := NewWorld(t)
		world.SelectProvider()
		world.PublishAgent()
		world.InstallAgent()
		world.SuspendInstallation()
		invocations, err := world.MentionAs(journeyOwner, journeyDM, "are you there @"+journeyPersona)
		if err != nil {
			t.Fatalf("suspended mention err=%v, want a refusal, not an error", err)
		}
		if len(invocations) != 0 {
			t.Fatalf("suspended invocations=%+v, want none", invocations)
		}
		if len(world.Journal.Runs) != 0 {
			t.Fatalf("runs=%+v, want no run against the suspended install", world.Journal.Runs)
		}
	})

	t.Run("wrong digest approval is refused", func(t *testing.T) {
		world := NewWorld(t)
		world.SelectProvider()
		world.PublishAgent()
		world.InstallAgent()
		world.AnswerDM()
		world.ParkForApproval()
		tampered := world.Approval.Digest + "tamper"
		if err := world.ApproveWithDigest(tampered); !errors.Is(err, agentapproval.ErrDigestMismatch) {
			t.Fatalf("tampered approval err=%v, want %v", err, agentapproval.ErrDigestMismatch)
		}
		stored, err := world.ApprovalState()
		if err != nil {
			t.Fatal(err)
		}
		if stored.State != agentapproval.StatePending {
			t.Fatalf("approval state=%q, want the PENDING card untouched", stored.State)
		}
		world.CarryHCMAction()
		if len(world.Journal.Intents) != 1 {
			t.Fatalf("intents=%+v, want the single carried action after the refused tamper", world.Journal.Intents)
		}
	})

	t.Run("tampered evaluation seal cannot publish", func(t *testing.T) {
		world := NewWorld(t)
		if err := world.PublishTamperedEval(); err == nil {
			t.Fatal("the tampered evaluation published, want a refusal")
		} else if !strings.Contains(strings.ToLower(err.Error()), "seal") && !strings.Contains(strings.ToLower(err.Error()), "evaluation") {
			t.Fatalf("tampered publish err=%v, want the broken-seal refusal", err)
		}
	})

	t.Run("schedule firings are tenant isolated", func(t *testing.T) {
		world := NewWorld(t)
		world.SelectProvider()
		world.PublishAgent()
		world.InstallAgent()
		home := world.PostDigest()
		away := world.FireDigestForTenant("designpartner-rival")
		if home.Receipt.SourceKey == "" || away.Receipt.SourceKey == "" {
			t.Fatalf("home=%+v away=%+v, want both firing receipts", home, away)
		}
		if home.Receipt.SourceKey == away.Receipt.SourceKey {
			t.Fatalf("source keys collide: %q", home.Receipt.SourceKey)
		}
		if home.Receipt.RunRequestID == away.Receipt.RunRequestID {
			t.Fatalf("run ids collide: %q", home.Receipt.RunRequestID)
		}
		if got := world.DigestPostCount(); got != 2 {
			t.Fatalf("digest posts=%d, want one per tenant", got)
		}
	})

	t.Run("agent-authored loop event is refused", func(t *testing.T) {
		world := NewWorld(t)
		world.SelectProvider()
		world.PublishAgent()
		world.InstallAgent()
		world.Participate()
		if world.TriggerLoopErr == nil {
			t.Fatal("the agent-authored loop event was admitted, want a refusal")
		}
		if !errors.Is(world.TriggerLoopErr, errTriggerLoopSentinel()) {
			t.Fatalf("loop err=%v, want the trigger loop refusal", world.TriggerLoopErr)
		}
		if len(world.Journal.Runs) != 0 {
			t.Fatalf("runs=%+v, want no run from the loop event", world.Journal.Runs)
		}
	})
}

func errTriggerLoopSentinel() error {
	return trigger.ErrLoop
}
