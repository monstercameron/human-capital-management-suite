package intentcontrol_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestTodo_REV_084_02_AcceptanceUsesJourneyIntent proves an ordinary intent
// instance can own its accepted action without an intent_instance_context row.
func TestTodo_REV_084_02_AcceptanceUsesJourneyIntent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "rev08402-journey-acceptance")
	intentID := insertIntent(t, db, tenant, "rev08402-journey-acceptance")
	proposalDigest := insertRevision(t, db, tenant, intentID, 1)
	decision := intentcontrol.Decision{
		TenantID: tenant, DecisionID: uuid.New(), IntentID: intentID, Revision: 1,
		RequirementID: "manager-approval", Kind: intentcontrol.DecisionHumanApproval,
		Outcome: intentcontrol.OutcomeApproved, ProposalDigest: proposalDigest,
		ControlDigest: digestOf("rev08402-control"), MaterialityClass: intentcontrol.Material,
		DecidedBy: "principal:manager", AuthorityRef: "authority:manager",
		Reason: "approved proposal", DecidedAt: fixedInstant,
	}
	conn := appConn(t, db)
	var accepted intentcontrol.AcceptedAction
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := (intentcontrol.DecisionStore{}).Record(ctx, tx, decision); err != nil {
			return err
		}
		var err error
		accepted, err = (intentcontrol.AcceptedActionStore{}).RecordApprovedDecision(
			ctx, tx, decision, "proposal-revision:1", "intent.execute", "execute:"+intentID.String()+":proposal-revision:1")
		return err
	})
	if accepted.DecisionID != decision.DecisionID || accepted.IntentID != intentID || accepted.ProposalDigest != proposalDigest {
		t.Fatalf("accepted action = %+v, does not preserve durable decision and intent identity", accepted)
	}

	// The intent_instance row is sufficient authority; no code inserted the
	// optional context table. Verify a fresh read resolves this acceptance.
	reader := appConn(t, db)
	inTenantTx(t, reader, tenant, func(tx dbport.Tx) error {
		stored, err := (intentcontrol.AcceptedActionStore{}).ByDecision(ctx, tx, tenant, decision.DecisionID)
		if err == nil && stored.AcceptanceDigest != accepted.AcceptanceDigest {
			t.Errorf("freshly loaded acceptance digest = %q, want %q", stored.AcceptanceDigest, accepted.AcceptanceDigest)
		}
		return err
	})
}
