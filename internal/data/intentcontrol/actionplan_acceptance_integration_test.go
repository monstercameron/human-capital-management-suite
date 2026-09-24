package intentcontrol_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_REV_084_02_SequentialApprovalsKeepFirstAcceptedAction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "rev08402-first-acceptance")
	intentID := insertIntent(t, db, tenant, "rev08402-first-acceptance")
	proposalDigest := insertRevision(t, db, tenant, intentID, 1)
	semanticKey := "execute:" + intentID.String() + ":proposal-revision:1"
	first := approvedDecision(tenant, intentID, proposalDigest, "manager", "principal:manager")
	second := approvedDecision(tenant, intentID, proposalDigest, "finance", "principal:finance")
	conn := appConn(t, db)
	store := intentcontrol.AcceptedActionStore{}
	var firstAccepted, secondAccepted intentcontrol.AcceptedAction
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := (intentcontrol.DecisionStore{}).Record(ctx, tx, first); err != nil {
			return err
		}
		var err error
		firstAccepted, err = store.RecordApprovedDecision(ctx, tx, first, "proposal-revision:1", "intent.execute", semanticKey)
		return err
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := (intentcontrol.DecisionStore{}).Record(ctx, tx, second); err != nil {
			return err
		}
		var err error
		secondAccepted, err = store.RecordApprovedDecision(ctx, tx, second, "proposal-revision:1", "intent.execute", semanticKey)
		return err
	})
	if secondAccepted.DecisionID != firstAccepted.DecisionID ||
		secondAccepted.AcceptedBy != firstAccepted.AcceptedBy ||
		!secondAccepted.AcceptedAt.Equal(firstAccepted.AcceptedAt) {
		t.Fatalf("second approval replaced the first accepted action: first=%+v second=%+v", firstAccepted, secondAccepted)
	}

	// A key collision across a different intent cannot borrow the first
	// action's acceptance, even though the storage uniqueness conflict resolves.
	otherIntent := insertIntent(t, db, tenant, "rev08402-other-action")
	otherDigest := insertRevision(t, db, tenant, otherIntent, 1)
	otherDecision := approvedDecision(tenant, otherIntent, otherDigest, "manager", "principal:manager")
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		if err := (intentcontrol.DecisionStore{}).Record(ctx, tx, otherDecision); err != nil {
			return err
		}
		_, err := store.RecordApprovedDecision(ctx, tx, otherDecision, "proposal-revision:1", "intent.execute", semanticKey)
		return err
	})
	if !errors.Is(err, intentcontrol.ErrDuplicate) {
		t.Fatalf("cross-intent reuse of semantic key error = %v, want ErrDuplicate", err)
	}
}

func approvedDecision(tenant, intentID uuid.UUID, proposalDigest, requirementID, actor string) intentcontrol.Decision {
	return intentcontrol.Decision{
		TenantID: tenant, DecisionID: uuid.New(), IntentID: intentID, Revision: 1,
		RequirementID: requirementID, Kind: intentcontrol.DecisionHumanApproval,
		Outcome: intentcontrol.OutcomeApproved, ProposalDigest: proposalDigest,
		ControlDigest: digestOf("rev08402-control:" + requirementID), MaterialityClass: intentcontrol.Material,
		DecidedBy: actor, AuthorityRef: "authority:" + requirementID,
		Reason: "approved proposal", DecidedAt: fixedInstant,
	}
}
