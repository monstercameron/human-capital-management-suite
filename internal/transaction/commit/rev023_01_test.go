package commit_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/conflictstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	intentmodel "github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
)

// TestTodo_REV_023_01_Integration proves that a real commit re-runs the
// approval-pinned conflict set and refuses a post-approval collision with
// zero business rows written.
func TestTodo_REV_023_01_Integration(t *testing.T) {
	db, prepared, tenant := commitFixture(t)
	ctx := context.Background()
	resource, err := values.NewResourceKey(values.TenantId(tenant.String()), values.Kind("assignment"), "worker", "9001")
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewInstantInterval(values.NewInstant(commitAt.Add(10*time.Minute)), values.NewInstant(commitAt.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenant.String()); err != nil {
		t.Fatal(err)
	}
	for _, stream := range prepared.Streams {
		if err := ledger.EnsureStream(ctx, tx, tenant, stream.StreamKey, "TRANSACTION", prepared.PlanID); err != nil {
			t.Fatal(err)
		}
	}
	// The approved intent: one footprint and one planned write per stream.
	var pinnedFootprints []conflict.WriteFootprint
	for _, stream := range prepared.Streams {
		revision, revErr := values.NewSequenceRevision(stream.StreamKey, uint64(stream.ExpectedSequence))
		if revErr != nil {
			t.Fatal(revErr)
		}
		suffix := strings.TrimPrefix(stream.StreamKey, "stream:")
		pinnedFootprints = append(pinnedFootprints, conflict.WriteFootprint{
			Resource: resource, Field: conflict.FieldPath("employment.assignment." + suffix), Interval: interval,
			Operation: conflict.OperationUpdate, ExpectedRevision: revision,
			Authority: conflict.AuthorityScope{Domain: "PEOPLE", PolicyRef: "authority.local/v1"},
		})
		prepared.Writes = append(prepared.Writes, intentmodel.PlannedWrite{
			Subject:     intentmodel.SubjectReference{Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE"},
			ResourceKey: resource, FieldPath: "employment.assignment." + suffix, ExpectedRevision: revision,
			SourceAuthorityDecision: "authority.local/v1", Operation: intentmodel.WriteOperationUpdate, EffectiveInterval: interval,
		})
	}
	for _, footprint := range pinnedFootprints {
		prepared.ConflictFootprintDigests = append(prepared.ConflictFootprintDigests, footprint.ScopeDigest())
	}
	store := conflictstore.New()
	approved := conflict.WriteIntent{TenantID: tenant.String(), ID: "approved-" + tenant.String(), ProposalID: prepared.ProposalRevisionID, SnapshotDigest: "snapshot-approved", Footprints: pinnedFootprints}
	if err := store.Register(ctx, tx, approved); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// A competing intent registered after approval contends for the identical
	// scope, so the reevaluation must report an overlap and block.
	peerTx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer peerTx.Rollback(ctx)
	if _, err := peerTx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenant.String()); err != nil {
		t.Fatal(err)
	}
	peerFootprints := append([]conflict.WriteFootprint(nil), pinnedFootprints...)
	peer := conflict.WriteIntent{TenantID: tenant.String(), ID: "peer-" + tenant.String(), ProposalID: "proposal-peer", SnapshotDigest: "snapshot-peer", Footprints: peerFootprints}
	if err := store.Register(ctx, peerTx, peer); err != nil {
		t.Fatal(err)
	}
	if err := peerTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	prepared = bindConflict(prepared, approved.ID, approved.SnapshotDigest)
	committer := transactioncommit.New(db.Conn, transactioncommit.Options{Clock: func() time.Time { return commitAt }, ConflictFence: store})
	_, err = committer.Commit(ctx, prepared)
	if !errors.Is(err, conflict.ErrIntentConflict) || !strings.Contains(err.Error(), string(conflict.PreflightBlocked)) {
		t.Fatalf("post-approval collision commit = %v, want BLOCKED conflict preflight", err)
	}
	for table, want := range map[string]int{"ledger_event": 0, "outbox": 0} {
		var got int
		if err := db.Conn.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", tenant).Scan(&got); err != nil || got != want {
			t.Fatalf("%s rows=%d err=%v, want %d", table, got, err, want)
		}
	}
}
