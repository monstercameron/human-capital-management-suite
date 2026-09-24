package correction_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/temporal"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/correction"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var (
	correctionOccurred  = time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	correctionEffective = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	correctionRecorded  = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
)

func TestTodo_TX_007(t *testing.T) {
	f := newFixture(t)
	tx := f.dbTx(t)
	result, err := correction.Append(context.Background(), tx, correction.Request{
		Tenant: f.tenant, StreamKey: "worker:promotion", ExpectedHead: 1,
		Target:    ledger.EventRef{StreamKey: "worker:promotion", Sequence: 1},
		Authority: "hcmnext:people", SourceRef: "hcmnext:test:promotion-correction",
		SchemaRef: "hcmnext.people.v1.Promotion@1", Payload: []byte("promotion:principal"),
		OccurredAt: correctionOccurred, EffectiveAt: correctionEffective,
		CorrelationID: uuid.New(), IdempotencyKey: "correction:" + f.tenant.String(),
		Reason: "promotion fact was recorded with the wrong level", CorrectedBy: "steward:people",
		Obligations: []correction.ReconciliationObligation{{
			EffectIdentity: "reconcile:promotion:" + f.tenant.String(), OrderingKey: "promotion:worker",
			SchemaRef: "hcmnext.reconciliation.v1", Payload: []byte("recalculate promotion consumers"),
		}},
	}, func() time.Time { return correctionRecorded })
	if err != nil {
		t.Fatalf("append correction: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result.Correction.Sequence != 2 || result.Correction.Replayed || len(result.Obligations) != 1 {
		t.Fatalf("correction result = %+v, want successor sequence 2 and one obligation", result)
	}

	var total int
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = 'worker:promotion'`, f.tenant).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("promotion ledger rows = %d, want original plus correction", total)
	}
	decision := temporal.Decision{Tenant: f.tenant}
	before, err := temporal.KnownAsOf(context.Background(), f.db.Conn, temporal.Request{
		Tenant: f.tenant, Subject: "worker:promotion", Field: "hcmnext.people.v1.Promotion@1",
		EffectiveAt: correctionEffective, KnownAt: correctionRecorded.Add(-time.Hour),
	}, decision)
	if err != nil {
		t.Fatalf("read promotion before correction: %v", err)
	}
	after, err := temporal.KnownAsOf(context.Background(), f.db.Conn, temporal.Request{
		Tenant: f.tenant, Subject: "worker:promotion", Field: "hcmnext.people.v1.Promotion@1",
		EffectiveAt: correctionEffective, KnownAt: correctionRecorded.Add(time.Hour),
	}, decision)
	if err != nil {
		t.Fatalf("read promotion after correction: %v", err)
	}
	if len(before.Assertions) != 1 || string(before.Assertions[0].Payload) != "promotion:manager" {
		t.Fatalf("before correction = %v, want original promotion fact", before.Assertions)
	}
	if len(after.Assertions) != 1 || string(after.Assertions[0].Payload) != "promotion:principal" {
		t.Fatalf("after correction = %v, want corrected promotion fact", after.Assertions)
	}
	if _, err := f.db.Conn.Exec(context.Background(), `UPDATE ledger_event SET source_ref = 'mutated' WHERE tenant_id = $1`, f.tenant); err == nil {
		t.Fatal("ledger correction path allowed an in-place rewrite")
	}
}

func TestTodo_TX_007_Race(t *testing.T) {
	f := newFixture(t)
	// Competing corrections prepared against the same stream head must elect
	// exactly one successor; every stale writer must fail closed.
	request := correction.Request{
		Tenant: f.tenant, StreamKey: "worker:promotion", ExpectedHead: 1,
		Target:    ledger.EventRef{StreamKey: "worker:promotion", Sequence: 1},
		Authority: "hcmnext:people", SourceRef: "hcmnext:test:promotion-correction",
		SchemaRef: "hcmnext.people.v1.Promotion@1", Payload: []byte("promotion:principal"),
		OccurredAt: correctionOccurred, EffectiveAt: correctionEffective,
		CorrelationID: uuid.New(), IdempotencyKey: "same-correction",
		Reason: "wrong level", CorrectedBy: "steward:people",
	}
	const workers = 8
	start := make(chan struct{})
	results := make([]correction.Result, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			tx, err := f.db.Conn.Begin(context.Background())
			if err != nil {
				errs[i] = err
				return
			}
			results[i], errs[i] = correction.Append(context.Background(), tx, request, func() time.Time { return correctionRecorded })
			if errs[i] != nil {
				_ = tx.Rollback(context.Background())
				return
			}
			errs[i] = tx.Commit(context.Background())
		}(i)
	}
	close(start)
	wg.Wait()
	var committed correction.Result
	commits := 0
	for i, result := range results {
		if errs[i] == nil {
			commits++
			committed = result
			continue
		}
		var stale ledger.ErrStaleStream
		if !errors.As(errs[i], &stale) {
			t.Fatalf("concurrent correction %d error = %v, want stale stream head", i, errs[i])
		}
	}
	if commits != 1 || committed.Correction.Sequence != 2 || committed.Correction.EventID == uuid.Nil {
		t.Fatalf("concurrent correction commits=%d result=%+v, want exactly one successor", commits, committed.Correction)
	}
	var total int
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = 'worker:promotion'`, f.tenant).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("concurrent correction ledger rows = %d, want original plus one correction", total)
	}
}

func TestTodo_TX_007_Mutation(t *testing.T) {
	f := newFixture(t)
	tx := f.dbTx(t)
	_, err := correction.Append(context.Background(), tx, correction.Request{
		Tenant: f.tenant, StreamKey: "worker:promotion", ExpectedHead: 1,
		Target: ledger.EventRef{StreamKey: "worker:other", Sequence: 1}, Authority: "hcmnext:people",
		SourceRef: "test", SchemaRef: "schema", Payload: []byte("corrected"),
		OccurredAt: correctionOccurred, EffectiveAt: correctionEffective,
		CorrelationID: uuid.New(), IdempotencyKey: "bad-target", Reason: "reason", CorrectedBy: "steward",
	}, func() time.Time { return correctionRecorded })
	if !errors.Is(err, correction.ErrTargetMismatch) {
		t.Fatalf("cross-stream target error = %v, want ErrTargetMismatch", err)
	}
	_ = tx.Rollback(context.Background())
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := pgtest.New(t)
	f := &fixture{db: db, tenant: uuid.New()}
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1, $2, 'cell-local', 'Promotion correction', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, f.tenant, "promotion-"+f.tenant.String())
	db.Exec(t, `SELECT set_config('app.tenant_id', $1, false)`, f.tenant.String())
	db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile) VALUES ($1, 'hcmnext.people.v1.Promotion@1', 'hcmnext.people.v1.Promotion', 1, 'hcmnext.people.v1.Promotion', 'PROTOBUF', 'LEDGER_EVENT')`, f.tenant)
	db.Exec(t, `INSERT INTO authority_assignment (tenant_id, authority_ref, authority_kind, domain_scope, effective_from) VALUES ($1, 'hcmnext:people', 'INTERNAL', 'people', timestamptz '2026-01-01T00:00:00Z')`, f.tenant)
	db.Exec(t, `INSERT INTO ledger_stream (tenant_id, stream_key, stream_kind, subject_ref) VALUES ($1, 'worker:promotion', 'WORKER', 'worker:promotion')`, f.tenant)
	db.Exec(t, `INSERT INTO stream_head (tenant_id, stream_key, head_sequence) VALUES ($1, 'worker:promotion', 0)`, f.tenant)
	f.inTx(t, func(tx dbport.Tx) error {
		_, err := ledger.New(ledger.WithClock(func() time.Time { return correctionOccurred })).Append(context.Background(), tx, ledger.AppendRequest{
			Tenant: f.tenant, StreamKey: "worker:promotion", ExpectedHead: 0,
			AssertionClass: ledger.DomainFact, Authority: "hcmnext:people", SourceRef: "hcmnext:test:promotion",
			SchemaRef: "hcmnext.people.v1.Promotion@1", Payload: []byte("promotion:manager"),
			OccurredAt: correctionOccurred, EffectiveAt: correctionEffective,
			CorrelationID: uuid.New(), IdempotencyKey: "promotion-original",
		})
		return err
	})
	return f
}

type fixture struct {
	db     *pgtest.DB
	tenant uuid.UUID
}

func (f *fixture) dbTx(t *testing.T) dbport.Tx {
	t.Helper()
	tx, err := f.db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

func (f *fixture) inTx(t *testing.T, fn func(dbport.Tx) error) {
	t.Helper()
	tx := f.dbTx(t)
	if err := fn(tx); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Keep the temporal package in this lane's fixture contract: the original and
// correction are later read through its bitemporal API by the promotion fact
// acceptance test in the owning data lane.
var _ = temporal.ModeHistory
var _ = projection.StatusCurrent
