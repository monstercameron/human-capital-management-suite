package positionfacts_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// RevisionsAt is proved against PositionRevisionAt itself: for every position
// in the set, the preloaded reader answers the identical revision, and for
// anything outside the set it defers rather than inventing an absence. The
// vacancy list this exists for re-checks every candidate through the Position
// domain, so a preloaded answer that differed anywhere would be a different
// business verdict rendered on a form.

// countingBeginner counts the transactions a read opens.
type countingBeginner struct {
	inner  dbport.Beginner
	begins int
}

func (c *countingBeginner) Begin(ctx context.Context) (dbport.Tx, error) {
	c.begins++
	return c.inner.Begin(ctx)
}

func positionRefs(tenant values.TenantId, ids []uuid.UUID) []values.EntityRef {
	out := make([]values.EntityRef, 0, len(ids))
	for _, id := range ids {
		out = append(out, values.EntityRef{Tenant: tenant, Kind: position.KindPosition, Id: id.String()})
	}
	return out
}

// seededPositions creates n independent position chains in one tenant.
func seededPositions(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, n int) []uuid.UUID {
	t.Helper()
	out := make([]uuid.UUID, 0, n)
	for i := 0; i < n; i++ {
		suffix := string(rune('A' + i))
		out = append(out, seededPosition(t, db, tenantID, "ENG-MGR-"+suffix, "ENGINEERING-"+suffix))
	}
	return out
}

// TestRevisionsAtAnswersWhatPositionRevisionAtAnswers is the PRIMARY case:
// the preloaded reader's revision for every preloaded position is exactly the
// per-position reader's own.
func TestRevisionsAtAnswersWhatPositionRevisionAtAnswers(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedReaderTenant(t, db, "positionfacts-batch-primary")
	tenant := values.TenantId("positionfacts-batch-primary")
	ids := seededPositions(t, db, tenantID, 4)
	reader := positionfacts.Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }}
	asOf := readerAsOf(t)
	ctx := context.Background()

	preloaded, err := reader.RevisionsAt(ctx, tenant, asOf, positionRefs(tenant, ids))
	if err != nil {
		t.Fatalf("RevisionsAt: %v", err)
	}
	for _, ref := range positionRefs(tenant, ids) {
		q := position.PositionQuery{Position: ref, AsOf: asOf}
		want, wantExists, wantErr := reader.PositionRevisionAt(ctx, q)
		got, gotExists, gotErr := preloaded.PositionRevisionAt(ctx, q)
		if gotExists != wantExists || (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("%s preloaded = %t, %v; per-position = %t, %v", ref.Id, gotExists, gotErr, wantExists, wantErr)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s preloaded revision =\n%+v\nper-position revision =\n%+v", ref.Id, got, want)
		}
		if !gotExists {
			t.Errorf("%s resolved to nothing: the fixture proves nothing", ref.Id)
		}
	}
}

// TestRevisionsAtOpensOneTransactionForTheWholeSet is the PERFORMANCE case:
// the preload costs one transaction whatever the directory's size, while the
// per-position path it replaces costs one each.
func TestRevisionsAtOpensOneTransactionForTheWholeSet(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedReaderTenant(t, db, "positionfacts-batch-cost")
	tenant := values.TenantId("positionfacts-batch-cost")
	ids := seededPositions(t, db, tenantID, 5)
	counting := &countingBeginner{inner: db.Conn}
	reader := positionfacts.Reader{DB: counting, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }}
	asOf := readerAsOf(t)
	ctx := context.Background()
	refs := positionRefs(tenant, ids)

	counting.begins = 0
	if _, err := reader.RevisionsAt(ctx, tenant, asOf, refs); err != nil {
		t.Fatalf("RevisionsAt: %v", err)
	}
	batched := counting.begins

	counting.begins = 0
	for _, ref := range refs {
		if _, _, err := reader.PositionRevisionAt(ctx, position.PositionQuery{Position: ref, AsOf: asOf}); err != nil {
			t.Fatalf("PositionRevisionAt(%s): %v", ref.Id, err)
		}
	}
	perPosition := counting.begins

	if batched != 1 {
		t.Fatalf("preloading %d positions opened %d transactions, want 1", len(refs), batched)
	}
	if perPosition != len(refs) {
		t.Fatalf("the per-position path opened %d transactions for %d positions: the comparison proves nothing",
			perPosition, len(refs))
	}

	// Answering from the preloaded set opens nothing further, which is the
	// property the vacancy list depends on: it asks about each position twice.
	preloaded, err := reader.RevisionsAt(ctx, tenant, asOf, refs)
	if err != nil {
		t.Fatalf("RevisionsAt: %v", err)
	}
	counting.begins = 0
	for i := 0; i < 2; i++ {
		for _, ref := range refs {
			if _, _, err := preloaded.PositionRevisionAt(ctx, position.PositionQuery{Position: ref, AsOf: asOf}); err != nil {
				t.Fatalf("preloaded PositionRevisionAt(%s): %v", ref.Id, err)
			}
		}
	}
	if counting.begins != 0 {
		t.Fatalf("answering %d preloaded questions opened %d transactions, want none", 2*len(refs), counting.begins)
	}
}

// TestPreloadedRevisionsDefersWhatItDoesNotHold proves the preloaded reader
// never answers a question it was not asked to resolve: a position outside
// the set and a different coordinate both reach the underlying reader, while
// a malformed identifier inside the set keeps PositionRevisionAt's own silent
// absence.
func TestPreloadedRevisionsDefersWhatItDoesNotHold(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedReaderTenant(t, db, "positionfacts-batch-defer")
	tenant := values.TenantId("positionfacts-batch-defer")
	ids := seededPositions(t, db, tenantID, 2)
	counting := &countingBeginner{inner: db.Conn}
	reader := positionfacts.Reader{DB: counting, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }}
	asOf := readerAsOf(t)
	ctx := context.Background()

	outside := seededPosition(t, db, tenantID, "ENG-MGR-OUT", "ENGINEERING-OUT")
	malformed := values.EntityRef{Tenant: tenant, Kind: position.KindPosition, Id: "POS-ENG-MGR-101"}
	refs := append(positionRefs(tenant, ids), malformed)

	preloaded, err := reader.RevisionsAt(ctx, tenant, asOf, refs)
	if err != nil {
		t.Fatalf("RevisionsAt: %v", err)
	}

	// A malformed id was preloaded as the absence the per-position reader
	// reports for it, and answering it opens nothing.
	counting.begins = 0
	if rev, exists, err := preloaded.PositionRevisionAt(ctx, position.PositionQuery{Position: malformed, AsOf: asOf}); exists || err != nil {
		t.Fatalf("a malformed identifier answered %+v, %t, %v; want silent absence", rev, exists, err)
	}
	if counting.begins != 0 {
		t.Errorf("answering a preloaded absence opened %d transactions", counting.begins)
	}

	// A position outside the set is deferred to the reader, which resolves it.
	outsideRef := values.EntityRef{Tenant: tenant, Kind: position.KindPosition, Id: outside.String()}
	counting.begins = 0
	got, exists, err := preloaded.PositionRevisionAt(ctx, position.PositionQuery{Position: outsideRef, AsOf: asOf})
	if err != nil || !exists {
		t.Fatalf("a position outside the preloaded set = %t, %v; want the reader's own answer", exists, err)
	}
	if counting.begins != 1 {
		t.Errorf("deferring one position opened %d transactions, want 1", counting.begins)
	}
	want, _, err := reader.PositionRevisionAt(ctx, position.PositionQuery{Position: outsideRef, AsOf: asOf})
	if err != nil {
		t.Fatalf("PositionRevisionAt: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the deferred answer differs from the reader's own:\n%+v\n%+v", got, want)
	}

	// A different business coordinate is a different question and is
	// deferred too, rather than answered from the preloaded day.
	otherDay, err := values.ParseLocalDate("2019-01-01")
	if err != nil {
		t.Fatalf("ParseLocalDate: %v", err)
	}
	counting.begins = 0
	_, exists, err = preloaded.PositionRevisionAt(ctx,
		position.PositionQuery{Position: positionRefs(tenant, ids)[0], AsOf: position.AsOf{EffectiveOn: otherDay, KnownAt: asOf.KnownAt}})
	if err != nil {
		t.Fatalf("a different coordinate: %v", err)
	}
	if exists {
		t.Error("a coordinate before the position existed resolved it anyway")
	}
	if counting.begins != 1 {
		t.Errorf("a different coordinate opened %d transactions, want 1 (deferred to the reader)", counting.begins)
	}
}

// TestRevisionsAtRefusesAnUnusableReaderAndCoordinate proves the batched read
// refuses the same inputs the directory read refuses, and that a preloaded
// reader with no fallback says so rather than reporting a position absent.
func TestRevisionsAtRefusesAnUnusableReaderAndCoordinate(t *testing.T) {
	ctx := context.Background()
	tenant := values.TenantId("positionfacts-batch-refusal")
	asOf := readerAsOf(t)

	if _, err := (positionfacts.Reader{}).RevisionsAt(ctx, tenant, asOf, nil); err == nil {
		t.Error("a reader with no database resolved a set of revisions")
	}
	reader := positionfacts.Reader{DB: unusedBeginner{}, TenantUUID: func(values.TenantId) uuid.UUID { return uuid.New() }}
	if _, err := reader.RevisionsAt(ctx, values.TenantId(""), asOf, nil); err == nil {
		t.Error("an invalid tenant resolved a set of revisions")
	}
	if _, err := reader.RevisionsAt(ctx, tenant, position.AsOf{}, nil); err == nil {
		t.Error("an unset coordinate resolved a set of revisions")
	}

	empty := positionfacts.PreloadedRevisions{}
	_, _, err := empty.PositionRevisionAt(ctx, position.PositionQuery{
		Position: values.EntityRef{Tenant: tenant, Kind: position.KindPosition, Id: uuid.NewString()}, AsOf: asOf})
	if err == nil {
		t.Error("a preloaded reader with no fallback answered a question it never resolved")
	}
}

// unusedBeginner is a dbport.Beginner the refusal cases never reach.
type unusedBeginner struct{}

func (unusedBeginner) Begin(context.Context) (dbport.Tx, error) {
	panic("positionfacts batch test: a refusal case opened a transaction")
}
