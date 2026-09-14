package journey_test

import (
	"context"
	"testing"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// TestTodo_PROMOUX_013 is the PRIMARY test named in the todo's TEST field.
// It drives the three new RPCs (EditProposal, PreviewJourneyIntervention,
// RequestJourneyIntervention) through a real gRPC server against a fake
// engine, proving the wire<->port translation both directions: the request
// reaches the port as its own plain Go types, and the port's answer -- an
// intervention outcome, a consequence preview, a governance version --
// comes back whole on the wire.
func TestTodo_PROMOUX_013(t *testing.T) {
	engine := newFakeEngine()
	engine.editSuccessor = fixtureSummary()
	engine.editSuperseded = fixtureIntentID
	engine.preview = workspace.JourneyInterventionPreview{
		Available: true, ConsequenceSummary: "withdrawing now cancels the proposal",
		LikelyOutcome: workspace.InterventionApplied, CurrentGovernanceVersion: 7,
	}
	engine.interveneResult = workspace.JourneyInterventionResult{
		Journey: fixtureSummary(), Outcome: workspace.InterventionPendingSafePoint, RetainedEvidenceRef: "evidence:cancel:1",
	}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	ctx := testContext(t)

	t.Run("EditProposalForwardsFieldsBothWays", func(t *testing.T) {
		resp, err := client.EditProposal(ctx, &journeyv1.EditProposalRequest{
			IntentId: fixtureIntentID, ExpectedInstanceVersion: 3, IdempotencyKey: "idem-edit-1",
			Reason:       "correcting the base pay",
			Target:       &journeyv1.Placement{JobCode: "OPS-HRBP3", Grade: "P3"},
			ProposedBase: "99000.00", EffectiveDate: "2026-06-01", BusinessReason: "edited",
		})
		if err != nil {
			t.Fatalf("EditProposal: %v", err)
		}
		if engine.editCalls != 1 {
			t.Fatalf("engine.editCalls = %d, want 1", engine.editCalls)
		}
		if engine.lastIntentID != fixtureIntentID {
			t.Fatalf("engine saw intent id %q, want %q", engine.lastIntentID, fixtureIntentID)
		}
		if engine.lastEditExpected != 3 || engine.lastEditIdempotency != "idem-edit-1" || engine.lastEditReason != "correcting the base pay" {
			t.Fatalf("engine saw expected=%d idempotency=%q reason=%q, want 3/idem-edit-1/correcting the base pay",
				engine.lastEditExpected, engine.lastEditIdempotency, engine.lastEditReason)
		}
		if engine.lastEditInput.TargetJobCode != "OPS-HRBP3" || engine.lastEditInput.ProposedBase != "99000.00" {
			t.Fatalf("engine saw edit input %+v, want the request's own edited fields", engine.lastEditInput)
		}
		if resp.GetSupersededIntentId() != fixtureIntentID {
			t.Fatalf("superseded_intent_id = %q, want %q", resp.GetSupersededIntentId(), fixtureIntentID)
		}
		if resp.GetJourney().GetIntentId() != fixtureSummary().IntentID {
			t.Fatalf("journey.intent_id = %q, want the successor's own", resp.GetJourney().GetIntentId())
		}
	})

	t.Run("PreviewJourneyInterventionCarriesEveryField", func(t *testing.T) {
		resp, err := client.PreviewJourneyIntervention(ctx, &journeyv1.PreviewJourneyInterventionRequest{
			IntentId: fixtureIntentID, Kind: journeyv1.JourneyInterventionKind_JOURNEY_INTERVENTION_KIND_WITHDRAW,
		})
		if err != nil {
			t.Fatalf("PreviewJourneyIntervention: %v", err)
		}
		if engine.lastPreviewKind != workspace.JourneyInterventionWithdraw {
			t.Fatalf("engine saw kind %q, want WITHDRAW", engine.lastPreviewKind)
		}
		if !resp.GetAvailable() {
			t.Fatal("available = false, want true")
		}
		if resp.GetConsequenceSummary() != engine.preview.ConsequenceSummary {
			t.Fatalf("consequence_summary = %q, want %q", resp.GetConsequenceSummary(), engine.preview.ConsequenceSummary)
		}
		if resp.GetLikelyOutcome() != commonv1.InterventionOutcome_INTERVENTION_OUTCOME_APPLIED {
			t.Fatalf("likely_outcome = %v, want APPLIED", resp.GetLikelyOutcome())
		}
		if resp.GetCurrentGovernanceVersion() != 7 {
			t.Fatalf("current_governance_version = %d, want 7", resp.GetCurrentGovernanceVersion())
		}
	})

	t.Run("RequestJourneyInterventionForwardsAndProjectsOutcome", func(t *testing.T) {
		resp, err := client.RequestJourneyIntervention(ctx, &journeyv1.RequestJourneyInterventionRequest{
			IntentId: fixtureIntentID, Kind: journeyv1.JourneyInterventionKind_JOURNEY_INTERVENTION_KIND_CANCEL,
			ExpectedInstanceVersion: 9, IdempotencyKey: "idem-cancel-1", Reason: "operator requested cancellation",
		})
		if err != nil {
			t.Fatalf("RequestJourneyIntervention: %v", err)
		}
		if engine.interveneCalls != 1 {
			t.Fatalf("engine.interveneCalls = %d, want 1", engine.interveneCalls)
		}
		if engine.lastInterventionReq.Kind != workspace.JourneyInterventionCancel ||
			engine.lastInterventionReq.ExpectedInstanceVersion != 9 ||
			engine.lastInterventionReq.IdempotencyKey != "idem-cancel-1" ||
			engine.lastInterventionReq.Reason != "operator requested cancellation" {
			t.Fatalf("engine saw request %+v, want the wire request's own fields", engine.lastInterventionReq)
		}
		if resp.GetOutcome() != commonv1.InterventionOutcome_INTERVENTION_OUTCOME_PENDING_SAFE_POINT {
			t.Fatalf("outcome = %v, want PENDING_SAFE_POINT", resp.GetOutcome())
		}
		if resp.GetRetainedEvidenceRef() != "evidence:cancel:1" {
			t.Fatalf("retained_evidence_ref = %q, want evidence:cancel:1", resp.GetRetainedEvidenceRef())
		}
	})
}

// TestTodo_PROMOUX_013_Security proves the three new governed RPCs fail
// closed for an unauthenticated caller, exactly like every other write this
// service exposes, and that RequestJourneyIntervention refuses an
// unspecified kind rather than silently defaulting to one of the two real
// ones.
func TestTodo_PROMOUX_013_Security(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))

	t.Run("EditProposalRefusesAnUnauthenticatedCaller", func(t *testing.T) {
		_, err := client.EditProposal(context.Background(), &journeyv1.EditProposalRequest{IntentId: fixtureIntentID})
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
	})
	t.Run("PreviewJourneyInterventionRefusesAnUnauthenticatedCaller", func(t *testing.T) {
		_, err := client.PreviewJourneyIntervention(context.Background(), &journeyv1.PreviewJourneyInterventionRequest{IntentId: fixtureIntentID})
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
	})
	t.Run("RequestJourneyInterventionRefusesAnUnauthenticatedCaller", func(t *testing.T) {
		_, err := client.RequestJourneyIntervention(context.Background(), &journeyv1.RequestJourneyInterventionRequest{IntentId: fixtureIntentID})
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
	})
	t.Run("RequestJourneyInterventionRefusesAnUnspecifiedKindRatherThanDefaulting", func(t *testing.T) {
		engine.interveneErr = workspace.ErrJourneyInput
		defer func() { engine.interveneErr = nil }()
		ctx := testContext(t)
		if _, err := client.RequestJourneyIntervention(ctx, &journeyv1.RequestJourneyInterventionRequest{
			IntentId: fixtureIntentID, Kind: journeyv1.JourneyInterventionKind_JOURNEY_INTERVENTION_KIND_UNSPECIFIED,
			ExpectedInstanceVersion: 1, IdempotencyKey: "idem", Reason: "r",
		}); err == nil {
			t.Fatal("RequestJourneyIntervention with an unspecified kind succeeded")
		}
		if engine.lastInterventionReq.Kind != "" {
			t.Fatalf("engine saw kind %q for an unspecified wire value, want the empty string (never a silently chosen default)",
				engine.lastInterventionReq.Kind)
		}
	})
}
