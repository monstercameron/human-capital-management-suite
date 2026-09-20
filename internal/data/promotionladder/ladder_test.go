package promotionladder

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var (
	effectiveFrom = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	knownFrom     = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	recordedAt    = time.Date(2026, time.September, 1, 13, 45, 0, 0, time.UTC)
	readAt        = time.Date(2026, time.September, 19, 0, 0, 0, 0, time.UTC)
)

func newTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2020-01-01T00:00:00Z')`,
		id, key+"-"+id.String()[:8], "promotionladder "+key)
	return id
}

func seedIn(t *testing.T, db *pgtest.DB, tenant uuid.UUID, edges []Edge) Summary {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	summary, err := Seed(ctx, tx, tenant, edges, effectiveFrom, knownFrom, recordedAt)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return summary
}

func sampleEdges() []Edge {
	base := func(ordinal int, target, grade, title, kind string) Edge {
		return Edge{
			PathID: "ladder:care:CARE-CC3->" + target, Revision: "1",
			OrgUnit: "care-coordination", Kind: kind, Ordinal: ordinal,
			SourceProfileID: "profile/CARE-CC3", SourceJobCode: "CARE-CC3", SourceGrade: "P3",
			TargetProfileID: "profile/" + target, TargetJobCode: target, TargetGrade: grade, TargetTitle: title,
			MinimumBaseIncrease: "0.0500", MaximumBaseIncrease: "0.5000",
			Lifecycle: LifecyclePublished, PolicyVersion: "test.ladder/1",
		}
	}
	return []Edge{
		base(1, "CARE-MGR", "M3", "Care Coordination Manager", "UPWARD"),
		base(2, "QLT-PS3", "P4", "Patient Safety Specialist", "CROSS_FAMILY"),
	}
}

// TestSeedRecordsTheLadderAndReaderReadsItBack proves the two halves agree:
// what Seed writes is what Reader publishes, in the published ordinal order,
// with the guardrails read back as the exact decimals they were recorded as.
func TestSeedRecordsTheLadderAndReaderReadsItBack(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenant := newTenant(t, db, "reads-back")
	edges := sampleEdges()
	if summary := seedIn(t, db, tenant, edges); summary.Inserted != len(edges) || summary.Skipped != 0 {
		t.Fatalf("seed summary = %+v, want %d inserts", summary, len(edges))
	}

	reader := Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenant }}
	stored, err := reader.Edges(ctx, "any", readAt)
	if err != nil {
		t.Fatalf("Edges: %v", err)
	}
	if len(stored) != len(edges) {
		t.Fatalf("read back %d edges, want %d", len(stored), len(edges))
	}
	for i := range edges {
		if stored[i].Ordinal != edges[i].Ordinal || stored[i].TargetJobCode != edges[i].TargetJobCode {
			t.Fatalf("edge %d read back as %+v, want %+v", i, stored[i], edges[i])
		}
		if !stored[i].sameMeaning(edges[i]) {
			t.Fatalf("edge %d read back with a different meaning: %+v vs %+v", i, stored[i], edges[i])
		}
	}
}

// TestReaderIsTenantScoped proves one tenant's published ladder is invisible
// to another, and that a tenant with no mapping reads nothing rather than
// everything.
func TestReaderIsTenantScoped(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	owner := newTenant(t, db, "owner")
	other := newTenant(t, db, "other")
	seedIn(t, db, owner, sampleEdges())

	otherReader := Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return other }}
	stored, err := otherReader.Edges(ctx, "other", readAt)
	if err != nil {
		t.Fatalf("Edges: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("a second tenant read %d edges of somebody else's ladder", len(stored))
	}

	unmapped := Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return uuid.Nil }}
	if stored, err := unmapped.Edges(ctx, "unmapped", readAt); err != nil || len(stored) != 0 {
		t.Fatalf("an unmapped tenant read %d edges (err %v), want none", len(stored), err)
	}
	empty := Reader{}
	if stored, err := empty.Edges(ctx, "nowhere", readAt); err != nil || len(stored) != 0 {
		t.Fatalf("a reader with no database read %d edges (err %v), want none", len(stored), err)
	}
}

// TestSeedReplaysAndRefusesAContradiction proves the seeder is idempotent for
// an identical ladder and a hard error for a published edge whose meaning
// somebody changed, rather than silently overwriting append-only evidence.
func TestSeedReplaysAndRefusesAContradiction(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenant := newTenant(t, db, "replay")
	edges := sampleEdges()
	seedIn(t, db, tenant, edges)
	if summary := seedIn(t, db, tenant, edges); summary.Inserted != 0 || summary.Skipped != len(edges) {
		t.Fatalf("replay summary = %+v, want zero inserts and %d skips", summary, len(edges))
	}

	contradiction := append([]Edge(nil), edges...)
	contradiction[0].TargetTitle = "Something Else Entirely"
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	if _, err := Seed(ctx, tx, tenant, contradiction, effectiveFrom, knownFrom, recordedAt); err == nil {
		t.Fatal("a published edge was silently republished with a different meaning")
	}
}

// TestSeedRefusesIncompleteEdges proves validation runs before any row is
// written, so a half-authored ladder cannot be published.
func TestSeedRefusesIncompleteEdges(t *testing.T) {
	ctx := context.Background()
	valid := sampleEdges()[0]

	for name, mutate := range map[string]func(Edge) Edge{
		"no path id":        func(e Edge) Edge { e.PathID = ""; return e },
		"no ordinal":        func(e Edge) Edge { e.Ordinal = 0; return e },
		"same profile":      func(e Edge) Edge { e.TargetProfileID = e.SourceProfileID; return e },
		"inverted range":    func(e Edge) Edge { e.MinimumBaseIncrease = "0.9000"; return e },
		"negative minimum":  func(e Edge) Edge { e.MinimumBaseIncrease = "-0.0100"; return e },
		"inexact guardrail": func(e Edge) Edge { e.MaximumBaseIncrease = "half"; return e },
	} {
		if err := mutate(valid).Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("the valid edge was refused: %v", err)
	}
	if _, err := Seed(ctx, nil, uuid.New(), []Edge{valid}, effectiveFrom, knownFrom, recordedAt); err == nil {
		t.Error("seeding without a transaction was accepted")
	}
	if _, err := Seed(ctx, fakeTx{}, uuid.New(), []Edge{valid}, time.Time{}, knownFrom, recordedAt); err == nil {
		t.Error("seeding with no effective date was accepted")
	}
}

// fakeTx satisfies dbport.Tx so the coordinate checks above can be reached
// without a database. Seed refuses before it issues a statement, so none of
// these methods is ever called.
type fakeTx struct{}

func (fakeTx) Exec(context.Context, string, ...any) (int64, error) { return 0, nil }
func (fakeTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, nil
}
func (fakeTx) QueryRow(context.Context, string, ...any) dbport.Row { return nil }
func (fakeTx) Commit(context.Context) error                        { return nil }
func (fakeTx) Rollback(context.Context) error                      { return nil }
