package rebuild_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/data/rebuild"
)

func TestTodo_DATA_010(t *testing.T) {
	f := newFixture(t)
	tenant := insertTenant(t, f.db)
	stream := "data010:" + uuid.NewString()
	ensureStream(t, f.db, tenant, stream, "worker_state")
	f.registerSchema(t, tenant)
	event := f.appendEvent(t, tenant, stream, 0, ledger.TransactionFact, nil)
	f.seedRow(t, tenant, stream, 1, "value-1")
	report, err := rebuild.New(rebuild.NewPGSource(ledger.NewReader()), f.db.Conn).Rebuild(context.Background(), target(tenant, stream), fixtureReducer())
	if err != nil || !report.Promoted || !report.Match || report.SourceHead != event.Sequence {
		t.Fatalf("single-event rebuild = %+v, %v", report, err)
	}
}

func TestTodo_DATA_010_Golden(t *testing.T) {
	f := newFixture(t)
	tenant := insertTenant(t, f.db)
	stream := "data010-golden:" + uuid.NewString()
	ensureStream(t, f.db, tenant, stream, "worker_state")
	f.registerSchema(t, tenant)
	f.appendEvent(t, tenant, stream, 0, ledger.TransactionFact, nil)
	f.seedRow(t, tenant, stream, 1, "value-1")
	executor := rebuild.New(rebuild.NewPGSource(ledger.NewReader()), f.db.Conn)
	one, err := executor.Rebuild(context.Background(), target(tenant, stream), fixtureReducer())
	if err != nil {
		t.Fatal(err)
	}
	two, err := executor.Rebuild(context.Background(), target(tenant, stream), fixtureReducer())
	if err != nil {
		t.Fatal(err)
	}
	if !one.Promoted || !two.Match || one.Current.Digest != two.Current.Digest || one.Shadow.Digest != two.Shadow.Digest {
		t.Fatalf("rebuild digest changed: first=%+v second=%+v", one, two)
	}
}

func TestTodo_DATA_010_Recovery(t *testing.T) {
	f := newFixture(t)
	tenant := insertTenant(t, f.db)
	stream := "data010-recovery:" + uuid.NewString()
	ensureStream(t, f.db, tenant, stream, "worker_state")
	f.registerSchema(t, tenant)
	f.appendEvent(t, tenant, stream, 0, ledger.TransactionFact, nil)
	broken := fixtureReducer()
	broken.Fold = func(context.Context, string, dbport.Tx, ledger.EventRecord) error {
		return errors.New("fold interrupted")
	}
	_, err := rebuild.New(rebuild.NewPGSource(ledger.NewReader()), f.db.Conn).Rebuild(context.Background(), target(tenant, stream), broken)
	if err == nil {
		t.Fatal("interrupted rebuild succeeded")
	}
	cp, readErr := projection.Read(context.Background(), f.db.Conn, tenant, "worker_state", stream)
	if readErr != nil || cp.LastAppliedSequence != 0 {
		t.Fatalf("failed rebuild checkpoint = %+v, %v", cp, readErr)
	}
	if got := f.leftoverShadowTables(t); got != 0 {
		t.Fatalf("failed rebuild left %d shadow tables", got)
	}
}
