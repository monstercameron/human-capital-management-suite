package agentjourney

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/trigger"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/workflowbridge"
)

// TestTodo_AGENT_047_Integration proves the journey legs share one admission
// and evidence spine: the DM run, the scheduled digest, and the workflow run
// are admitted through the same inbox; the workflow result derives from its
// admitted record; and the approved HCM action submits exactly once.
func TestTodo_AGENT_047_Integration(t *testing.T) {
	world := NewWorld(t)
	world.DriveFull()

	t.Run("mention run is admitted once", func(t *testing.T) {
		record, ok := world.AdmissionRecord(agentrun.SourcePersonaMention)
		if !ok || record.Decision != agentrun.DecisionAccepted {
			t.Fatalf("mention admission=%+v ok=%v, want the accepted run record", record, ok)
		}
		if record.Request.Persona == nil || record.Request.Persona.ID != journeyPersona {
			t.Fatalf("mention admission=%+v, want the pinned persona provenance", record)
		}
		if record.Authority.GrantRef == "" {
			t.Fatalf("mention admission=%+v, want the server-resolved grant reference", record)
		}
	})

	t.Run("schedule receipt matches its admission", func(t *testing.T) {
		record, ok := world.AdmissionRecord(agentrun.SourceSchedule)
		if !ok || record.Decision != agentrun.DecisionAccepted {
			t.Fatalf("schedule admission=%+v ok=%v, want the accepted run record", record, ok)
		}
		if record.ID != world.Digest.Receipt.RunRequestID || record.RequestDigest != world.Digest.Receipt.RequestDigest {
			t.Fatalf("receipt=%+v record=%+v, want the receipt bound to the admission", world.Digest.Receipt, record)
		}
	})

	t.Run("workflow result derives from its admission", func(t *testing.T) {
		record, ok := world.AdmissionRecord(agentrun.SourceWorkflow)
		if !ok || record.Decision != agentrun.DecisionAccepted {
			t.Fatalf("workflow admission=%+v ok=%v, want the accepted run record", record, ok)
		}
		if world.Workflow.RunID != record.ID || world.Workflow.Agent != record.Authority.Agent {
			t.Fatalf("workflow=%+v record=%+v, want the result derived from the admission", world.Workflow, record)
		}
		refused := world.DeriveRefusedWorkflow()
		if refused.Outcome != workflowbridge.Refused || refused.Code == "" {
			t.Fatalf("refused workflow=%+v, want the REFUSED branch with its code", refused)
		}
	})

	t.Run("trigger replay is fenced", func(t *testing.T) {
		if err := world.ReplayTrigger(); !errors.Is(err, trigger.ErrReplay) {
			t.Fatalf("trigger replay err=%v, want %v", err, trigger.ErrReplay)
		}
	})

	t.Run("approval submits exactly once", func(t *testing.T) {
		replay := world.ReplaySubmission()
		if !replay.Replayed {
			t.Fatalf("replay=%+v, want the idempotent replay marker", replay)
		}
		if replay.Digest != world.SubmitReceipt.Digest {
			t.Fatalf("replay=%+v receipt=%+v, want the same approved digest", replay, world.SubmitReceipt)
		}
		if len(world.Journal.Intents) != 1 || len(world.Journal.EffectKeys) != 1 {
			t.Fatalf("intents=%+v effects=%v, want the single carried action", world.Journal.Intents, world.Journal.EffectKeys)
		}
	})
}
