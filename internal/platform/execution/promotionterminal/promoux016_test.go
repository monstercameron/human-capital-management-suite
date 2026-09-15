package promotionterminal_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionterminal"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

func newMaterialDigest(hexDigest string) digest.Reference {
	return digest.Reference{Digest: hexDigest}
}

// ---------------------------------------------------------------------------
// INTEGRATION
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_016_Integration drives the composed terminal end to end
// against real PostgreSQL: resolve, mutate and ledger fact in one committed
// transaction, effective-dated reads showing old state before the promotion
// date and new state after it, and a second proposal resolving to its own
// changed revision rather than replaying the first.
func TestTodo_PROMOUX_016_Integration(t *testing.T) {
	db, resolver, req, ids, tenantID, _ := setup016(t)
	at, _ := promoux016Times()

	var terminalCalls int
	w := &promotionterminal.Writer{
		ApprovedTerminalCode: approvedTerminalCode,
		Resolver:             resolver,
		Mutation:             promotioncommit.Writer{},
		Next: terminalFunc(func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
			terminalCalls++
			return idempotency.ResultIdentity{ResultRef: "complete"}, nil
		}),
	}
	must016Err(t, in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		_, err := w.Write(ctxOf(t), tx, req)
		return err
	}))
	if terminalCalls != 1 {
		t.Fatalf("terminal calls = %d, want 1", terminalCalls)
	}

	// Effective-dated reads: before the promotion date the old world shows
	// (SWE3/P3, 165000 base, no occupancy); at the date the new one does.
	// "Before" is the hour before the effective start (midnight), not the
	// hour before the noon instant: the committed rows take effect at the
	// start date, so an hour before noon already sees them.
	y, m, d := at.Date()
	before := time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Add(-time.Hour)
	occupancyID := must016(uuid.Parse(resolveInTx(t, db, tenantID, resolver, req).PositionOccupancyID))
	must016Err(t, in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		ctx := ctxOf(t)
		people, organization, compensation := aggregates.PeopleStore{}, aggregates.OrganizationStore{}, aggregates.CompensationStore{}
		// KnownAsOf at the revision's ProducedAt: the commit superseded
		// these rows, so a current read at `before` would miss the old
		// versions -- exactly why the resolver pins the frozen horizon.
		oldAssignment, err := people.KnownAsOfAssignment(ctx, tx, tenantID, ids.assignment, before, at)
		if err != nil {
			return err
		}
		if oldAssignment.JobCode != "ENG-SWE3" || oldAssignment.Grade != "P3" {
			t.Fatalf("pre-effectivity assignment = %q/%q, want the old ENG-SWE3/P3",
				oldAssignment.JobCode, oldAssignment.Grade)
		}
		oldBase, err := compensation.KnownAsOfCompensationComponent(ctx, tx, tenantID, ids.base, before, at)
		if err != nil {
			return err
		}
		if oldBase.Amount != "165000.0000" {
			t.Fatalf("pre-effectivity base = %q, want 165000.0000", oldBase.Amount)
		}
		if _, err := organization.CurrentPositionOccupancy(ctx, tx, tenantID, occupancyID, before); err == nil {
			t.Fatal("pre-effectivity occupancy exists: the position must be empty before the date")
		}
		newAssignment, err := people.CurrentAssignment(ctx, tx, tenantID, ids.assignment, at)
		if err != nil {
			return err
		}
		if newAssignment.JobCode != "ENG-MGR1" || newAssignment.Grade != "M1" {
			t.Fatalf("post-effectivity assignment = %q/%q, want ENG-MGR1/M1",
				newAssignment.JobCode, newAssignment.Grade)
		}
		return nil
	}))

	// A new proposal resolves to its own changed revision: seed a second
	// approved revision for the same intent with a higher base, resolve it,
	// and the command carries the new values -- never the first revision's.
	secondDigest := seed016SecondRevision(t, db, tenantID, ids, at)
	req2 := req
	req2.Proposal.Revision.Revision = 2
	req2.Proposal.Revision.MaterialDigest = newMaterialDigest(secondDigest)
	req2.IdempotencyKey = "promoux016-terminal-second"
	cmd2 := resolveInTx(t, db, tenantID, resolver, req2)
	if cmd2.BasePay.Amount().String() != "190000.00" {
		t.Fatalf("second-revision pay = %q, want 190000.00: the resolver must not replay the first revision", cmd2.BasePay.String())
	}
	if cmd2.ProposalDigest != secondDigest {
		t.Fatalf("second-revision digest = %q, want %q", cmd2.ProposalDigest, secondDigest)
	}
}

// seed016SecondRevision stores revision 2 of the same intent: same worker
// and window, higher base, its own material digest.
func seed016SecondRevision(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, ids promoux016IDs, at time.Time) string {
	t.Helper()
	sum := sha256.Sum256([]byte("promoux016-material-2:" + ids.proposal.String()))
	digest := hex.EncodeToString(sum[:])
	dto := build016DTO(t, tenantID, ids, at, digest, "190000.00", 2)
	store016Revision(t, db, tenantID, ids, 2, at, dto, digest)
	return digest
}

// ---------------------------------------------------------------------------
// RECOVERY
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_016_Recovery proves a replayed terminal write cannot
// duplicate effects: the second identical write fails closed on the already
// committed occupancy instead of recording a second set of facts, and the
// outbox holds exactly one record per effect identity.
func TestTodo_PROMOUX_016_Recovery(t *testing.T) {
	db, resolver, req, ids, tenantID, _ := setup016(t)
	at, _ := promoux016Times()
	// The occupancy entity is deterministic per proposal, so the id resolved
	// here names the row the writes below commit -- and the row the replay
	// must leave exactly as it found it.
	occupancyID := must016(uuid.Parse(resolveInTx(t, db, tenantID, resolver, req).PositionOccupancyID))

	var terminalCalls int
	w := &promotionterminal.Writer{
		ApprovedTerminalCode: approvedTerminalCode,
		Resolver:             resolver,
		Mutation:             promotioncommit.Writer{},
		Next: terminalFunc(func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
			terminalCalls++
			return idempotency.ResultIdentity{ResultRef: "complete"}, nil
		}),
	}
	must016Err(t, in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		_, err := w.Write(ctxOf(t), tx, req)
		return err
	}))
	if terminalCalls != 1 {
		t.Fatalf("first write terminal calls = %d, want 1", terminalCalls)
	}

	// The replayed write -- same request, same resolved command -- fails on
	// the committed occupancy instead of duplicating anything. The whole
	// second transaction rolls back, so no partial second facts survive it.
	replayed := false
	err := in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		_, err := w.Write(ctxOf(t), tx, req)
		if err != nil {
			replayed = true
			return err
		}
		return nil
	})
	if err == nil || !replayed {
		t.Fatal("replayed terminal write = nil, want the occupancy-conflict refusal")
	}
	if terminalCalls != 1 {
		t.Fatalf("terminal calls after replay = %d, want still 1: Next must not run behind a failed mutation", terminalCalls)
	}

	// Exactly one set of facts survives: one occupancy, one outbox record
	// per effect identity, base pay moved exactly once.
	must016Err(t, in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		ctx := ctxOf(t)
		organization, compensation := aggregates.OrganizationStore{}, aggregates.CompensationStore{}
		occupancy, err := organization.CurrentPositionOccupancy(ctx, tx, tenantID, occupancyID, at)
		if err != nil {
			return err
		}
		if occupancy.PositionRef != ids.position || occupancy.WorkerRef == nil || *occupancy.WorkerRef != ids.worker {
			t.Fatalf("occupancy = %v/%v, want exactly the committed %v/%v",
				occupancy.PositionRef, occupancy.WorkerRef, ids.position, ids.worker)
		}
		var outboxCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE tenant_id=$1 AND effect_identity LIKE $2`,
			tenantID, "%"+ids.proposal.String()).Scan(&outboxCount); err != nil {
			return err
		}
		if outboxCount != 2 {
			t.Fatalf("outbox records = %d, want exactly 2 (one per effect identity)", outboxCount)
		}
		base, err := compensation.CurrentCompensationComponent(ctx, tx, tenantID, ids.base, at)
		if err != nil {
			return err
		}
		if base.Amount != "180000.0000" {
			t.Fatalf("base pay after replay = %q, want exactly one move to 180000.0000", base.Amount)
		}
		return nil
	}))
}
