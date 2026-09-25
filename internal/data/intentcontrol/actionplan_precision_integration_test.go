package intentcontrol_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestApprovedDecisionAcceptsASubMicrosecondDecisionInstant is the approval
// that failed on a live server: the journey stamps a decision from a
// nanosecond clock, decided_at stores microseconds, and the acceptance read
// back its own row as a different decision. The same decision must accept,
// and its acceptance must carry the durable instant.
func TestApprovedDecisionAcceptsASubMicrosecondDecisionInstant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "decision-precision")
	intentID := insertIntent(t, db, tenant, "decision-precision")
	proposalDigest := insertRevision(t, db, tenant, intentID, 1)
	decision := approvedDecision(tenant, intentID, proposalDigest, "finance", "principal:finance")
	decision.DecidedAt = fixedInstant.Add(123456789 * time.Nanosecond)
	conn := appConn(t, db)
	var accepted intentcontrol.AcceptedAction
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := (intentcontrol.DecisionStore{}).Record(ctx, tx, decision); err != nil {
			return err
		}
		var err error
		accepted, err = (intentcontrol.AcceptedActionStore{}).RecordApprovedDecision(ctx, tx, decision, "proposal-revision:1", "intent.execute", "execute:"+intentID.String())
		return err
	})
	if !accepted.AcceptedAt.Equal(decision.DecidedAt.Truncate(time.Microsecond)) || accepted.DecisionID != decision.DecisionID {
		t.Fatalf("accepted = %+v, want the durable microsecond instant %s", accepted, decision.DecidedAt.Truncate(time.Microsecond))
	}
}
