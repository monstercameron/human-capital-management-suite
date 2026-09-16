package bootstrap_test

import (
	"context"
	"testing"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// TestTodo_OBS_024_Integration proves OBS-024's GATE_REFUSED/GATE_ADMITTED
// half of the chain against a fully composed cell, over the gRPC transport
// this suite's own [newCell]/[newExecutionCell] publish: a refusal before
// ExecutionAuthority is configured records GATE_REFUSED, and an admitted
// call under a real ExecutionAuthority records GATE_ADMITTED, both readable
// back from [app.Cell.Evidence] — the same capability evidence sink
// CAP-002's gateway already writes invocation/refusal evidence through.
//
// test/workflow's own TestTodo_OBS_024_Integration proves the driver's
// three evidence kinds (APPROVAL_COMPLETED/TASK_SUBMITTED/TERMINAL_WRITTEN)
// the same way, over the driver's own direct call surface; this test proves
// the two kinds only IntentService.ExecuteIntent itself records, over the
// gRPC/edge transports this todo's own TEST MATRIX line names.
func TestTodo_OBS_024_Integration(t *testing.T) {
	t.Run("GATE_REFUSED before simulation", func(t *testing.T) {
		c := newCell(t)
		seedWorkforce(t, c)
		ctx := c.grpcContext(context.Background())

		created, err := c.grpcIntent.CreateIntent(ctx, promoteWorkerRequest(t, "obs024-gate-refused"))
		if err != nil {
			t.Fatalf("CreateIntent: %v", err)
		}
		intentID := created.GetIntent().GetIntentId()

		// This cell carries no ExecutionAuthority, so the gate refuses
		// before it ever re-simulates or looks at the presented approval —
		// exactly the condition OBS-024's own RED clause names ("including
		// refusals that happen before simulation").
		_, err = c.grpcIntent.ExecuteIntent(ctx, &intentsv1.ExecuteIntentRequest{
			IdempotencyKey: "obs024-gate-refused",
			IntentId:       intentID,
			Approval:       &intentsv1.ProposalApproval{ProposalRevisionId: "does-not-matter", Approved: true, ApprovalRef: "approval:x"},
		})
		if err == nil {
			t.Fatal("expected ExecuteIntent to be refused with no ExecutionAuthority configured")
		}
		if _, ok := envelope.FromGRPC(err); !ok {
			t.Fatalf("ExecuteIntent failed with an unowned error: %v", err)
		}

		found := false
		for _, rec := range memoryEvidence(t, c.app).Records() {
			if rec.Decision == app.EvidenceKindGateRefused && rec.SubjectRef == intentID {
				found = true
				if rec.CapabilityID != "workflow.execution_authority_gate" {
					t.Errorf("GATE_REFUSED CapabilityID = %q, want workflow.execution_authority_gate", rec.CapabilityID)
				}
				if rec.EvidenceID == "" {
					t.Error("GATE_REFUSED evidence carries no evidence id")
				}
			}
		}
		if !found {
			t.Fatal("no GATE_REFUSED evidence recorded for the refused intent")
		}
	})

	t.Run("GATE_ADMITTED under ExecutionAuthority", func(t *testing.T) {
		terminal := &recordingTerminalWriter{}
		c := newExecutionCell(t, terminal)
		seedWorkforce(t, c)
		ctx := c.grpcContext(context.Background())

		created, err := c.grpcIntent.CreateIntent(ctx, promoteWorkerRequest(t, "obs024-gate-admitted"))
		if err != nil {
			t.Fatalf("CreateIntent: %v", err)
		}
		intentID := created.GetIntent().GetIntentId()
		simulated, err := c.grpcIntent.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: intentID})
		if err != nil {
			t.Fatalf("SimulateIntent: %v", err)
		}

		executed, err := c.grpcIntent.ExecuteIntent(ctx, &intentsv1.ExecuteIntentRequest{
			IdempotencyKey:          "obs024-gate-admitted",
			IntentId:                intentID,
			ExpectedInstanceVersion: created.GetIntent().GetInstanceVersion(),
			Approval: &intentsv1.ProposalApproval{
				ProposalRevisionId:     simulated.GetSimulation().GetProposalRevisionId(),
				MaterialProposalDigest: simulated.GetSimulation().GetMaterialProposalDigest(),
				Approved:               true, ApprovalRef: "approval:obs024-gate-admitted",
			},
		})
		if err != nil {
			t.Fatalf("ExecuteIntent: %v", err)
		}
		if executed.GetExecution().GetInstanceId() == "" {
			t.Fatal("receipt names no workflow instance")
		}

		found := false
		for _, rec := range memoryEvidence(t, c.app).Records() {
			if rec.Decision == app.EvidenceKindGateAdmitted && rec.SubjectRef == intentID {
				found = true
				if rec.EvidenceID == "" {
					t.Error("GATE_ADMITTED evidence carries no evidence id")
				}
			}
		}
		if !found {
			t.Fatal("no GATE_ADMITTED evidence recorded for the admitted intent")
		}
	})
}
