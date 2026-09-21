package app

import (
	"encoding/json"
	"testing"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
)

// TestTodo_PROMOUX_013 is the PRIMARY test named in the todo's TEST field.
// It exercises the pure decision functions PROMOUX-013 adds, with no
// database and no live cell: interventionUnavailableAtStage's total mapping
// over every JourneyStage for both kinds, interventionOutcomeFromDisposition's
// total mapping over CancelIntent's own four dispositions, and
// journeyWorkerRefFromIntent's decode of the stored request payload.
func TestTodo_PROMOUX_013(t *testing.T) {
	t.Run("InterventionUnavailableAtStage", func(t *testing.T) {
		allStages := []workspace.JourneyStage{
			workspace.JourneyStageProposed, workspace.JourneyStageBlocked,
			workspace.JourneyStageAwaitingApproval, workspace.JourneyStageCompleted,
			workspace.JourneyStageRejected, workspace.JourneyStageFailed,
			workspace.JourneyStageFinanceApproval, workspace.JourneyStageManagerApproval,
			workspace.JourneyStageWaitingEffectiveDate, workspace.JourneyStageRevalidation,
			workspace.JourneyStageReapproval, workspace.JourneyStageExecuted,
			workspace.JourneyStageObservingEffects, workspace.JourneyStageRecorded,
			workspace.JourneyStageRepairRequired,
		}
		terminal := map[workspace.JourneyStage]bool{
			workspace.JourneyStageCompleted: true, workspace.JourneyStageRejected: true,
			workspace.JourneyStageFailed: true, workspace.JourneyStageRecorded: true,
		}
		for _, stage := range allStages {
			t.Run("Withdraw/"+string(stage), func(t *testing.T) {
				// A journey that never started its workflow. BLOCKED is
				// reachable both ways, so the started fact is what decides
				// there (UXLIVE-026); this row is the unstarted one.
				reason, unavailable := interventionUnavailableAtStage(workspace.JourneyInterventionWithdraw, stage, false)
				wantUnavailable := stage != workspace.JourneyStageProposed && stage != workspace.JourneyStageBlocked
				if unavailable != wantUnavailable {
					t.Fatalf("WITHDRAW at %s: unavailable=%v, want %v", stage, unavailable, wantUnavailable)
				}
				if unavailable && reason == "" {
					t.Fatalf("WITHDRAW at %s: unavailable with no reason", stage)
				}
				if terminal[stage] && reason != reasonInterventionAlreadyTerminal {
					t.Fatalf("WITHDRAW at terminal stage %s: reason=%s, want %s", stage, reason, reasonInterventionAlreadyTerminal)
				}
			})
			t.Run("Cancel/"+string(stage), func(t *testing.T) {
				reason, unavailable := interventionUnavailableAtStage(workspace.JourneyInterventionCancel, stage, false)
				unstarted := stage == workspace.JourneyStageProposed || stage == workspace.JourneyStageBlocked
				committed := stage == workspace.JourneyStageExecuted || stage == workspace.JourneyStageObservingEffects
				wantUnavailable := terminal[stage] || unstarted || committed
				if unavailable != wantUnavailable {
					t.Fatalf("CANCEL at %s: unavailable=%v, want %v", stage, unavailable, wantUnavailable)
				}
				if unavailable && reason == "" {
					t.Fatalf("CANCEL at %s: unavailable with no reason", stage)
				}
			})
		}
		// The same inputs must always produce the same reason -- called twice
		// to prove the answer is a pure function of (kind, stage, started),
		// never incidental to call order or anything else.
		r1, _ := interventionUnavailableAtStage(workspace.JourneyInterventionCancel, workspace.JourneyStageProposed, false)
		r2, _ := interventionUnavailableAtStage(workspace.JourneyInterventionCancel, workspace.JourneyStageProposed, false)
		if r1 != r2 {
			t.Fatalf("interventionUnavailableAtStage is not stable across calls: %q then %q", r1, r2)
		}
	})

	t.Run("InterventionOutcomeFromDisposition", func(t *testing.T) {
		cases := []struct {
			disposition intent.CancellationDisposition
			want        workspace.JourneyInterventionOutcome
		}{
			{intent.DispositionCancelled, workspace.InterventionApplied},
			{intent.DispositionCancellationPending, workspace.InterventionPendingSafePoint},
			{intent.DispositionTooLate, workspace.InterventionTooLate},
			{intent.DispositionRepairRequired, workspace.InterventionRepairRequired},
			{intent.CancellationDisposition("SOMETHING_UNKNOWN"), workspace.InterventionDenied},
		}
		for _, tc := range cases {
			if got := interventionOutcomeFromDisposition(tc.disposition); got != tc.want {
				t.Errorf("interventionOutcomeFromDisposition(%s) = %s, want %s", tc.disposition, got, tc.want)
			}
		}
	})

	t.Run("JourneyWorkerRefFromIntent", func(t *testing.T) {
		payload, err := encodeJourneyPayloadForTest(map[string]any{"worker_ref": "omar-reyes"})
		if err != nil {
			t.Fatalf("encode fixture payload: %v", err)
		}
		msg := &intentsv1.IntentInstance{Request: &intentsv1.TypedPayload{ProtobufWireBytes: payload}}
		ref, err := journeyWorkerRefFromIntent(msg)
		if err != nil {
			t.Fatalf("journeyWorkerRefFromIntent: %v", err)
		}
		if ref != "omar-reyes" {
			t.Fatalf("worker ref = %q, want omar-reyes", ref)
		}

		empty, err := encodeJourneyPayloadForTest(map[string]any{"business_reason": "no worker ref here"})
		if err != nil {
			t.Fatalf("encode fixture payload (no worker_ref): %v", err)
		}
		if _, err := journeyWorkerRefFromIntent(&intentsv1.IntentInstance{
			Request: &intentsv1.TypedPayload{ProtobufWireBytes: empty},
		}); err == nil {
			t.Fatal("journeyWorkerRefFromIntent succeeded on a payload with no worker_ref")
		}
	})
}

// encodeJourneyPayloadForTest builds a wire payload journeyWorkerRefFromIntent
// can decode, the same way journeyRequestPayload does in production: JSON in,
// through protomap.Struct, out as deterministic Protobuf wire bytes.
func encodeJourneyPayloadForTest(fields map[string]any) ([]byte, error) {
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	var payload protomap.Struct
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return protomap.MarshalDeterministic(&payload)
}
