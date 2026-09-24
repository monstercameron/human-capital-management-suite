package commit_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	ledgercommit "github.com/monstercameron/human-capital-management-suite/internal/data/ledger/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/data/provenance"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

const commitSchema = "hcmnext.test.LedgerCommit@1"

type fixture struct {
	db     *pgtest.DB
	tenant uuid.UUID
	stream string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	stream := "workflow:" + uuid.NewString()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Commit Test', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenant, tenant.String())
	db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, $2, 1, $2, 'PROTOBUF', 'LEDGER_EVENT')`, tenant, commitSchema)
	inTx(t, db, func(tx dbport.Tx) error {
		if err := ledger.EnsureStream(context.Background(), tx, tenant, stream, "WORKFLOW_INSTANCE", stream); err != nil {
			return err
		}
		return projection.EnsureProjection(context.Background(), tx, tenant, "workflow.promotion_outcome", stream)
	})
	return fixture{db: db, tenant: tenant, stream: stream}
}

func (f fixture) request(fail ledgercommit.Failpoint) ledgercommit.Request {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	return ledgercommit.Request{
		Append: ledger.AppendRequest{
			Tenant: f.tenant, StreamKey: f.stream, ExpectedHead: 0,
			AssertionClass: ledger.TransactionFact, SourceRef: "hcmnext:test",
			SchemaRef: commitSchema, Payload: []byte("promotion-outcome"),
			OccurredAt: now, EffectiveAt: now, CorrelationID: uuid.New(),
			IdempotencyKey: "commit-" + f.stream,
		},
		Projection: "workflow.promotion_outcome",
		Outbox: outbox.OutboxSpec{
			EffectIdentity: "promotion:" + f.stream,
			OrderingKey:    f.stream, SchemaRef: commitSchema, Payload: []byte("promotion-outcome"),
		},
		Provenance: provenance.PublishRequest{
			Tenant: f.tenant, IntentRef: "intent:" + f.stream,
			SourceAuthority: "hcmnext:test", PrincipalRef: "tester",
			EvidenceIDs: []string{"evidence:" + f.stream},
		},
		Failpoint:  fail,
		RecordedAt: now,
	}
}

func inTx(t *testing.T, db *pgtest.DB, fn func(dbport.Tx) error) {
	t.Helper()
	tx, err := db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("transaction: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func TestTodo_LEDGER_008(t *testing.T) {
	f := newFixture(t)
	tx, err := f.db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	req := f.request(nil)
	receipt, err := ledgercommit.Commit(context.Background(), tx, ledger.New(), req)
	if err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("atomic commit: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if receipt.Ledger.Sequence != 1 || !receipt.Projection.Applied || receipt.Outbox.OutboxID == uuid.Nil {
		t.Fatalf("receipt = %+v, want event, projection and outbox", receipt)
	}
	if receipt.Provenance.RecordID == uuid.Nil || len(receipt.OutboxIDs) != 2 || len(receipt.ProvenanceEndpointIDs) != 1 {
		t.Fatalf("receipt roots = %+v, want two outbox ids and one provenance endpoint", receipt)
	}
	for table, want := range map[string]int{"ledger_event": 1, "projection_checkpoint": 1, "outbox": 2, "provenance_record": 1} {
		var got int
		if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE tenant_id = $1`, f.tenant).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s rows = %d, want %d", table, got, want)
		}
	}
}

func TestTodo_LEDGER_008_Race(t *testing.T) {
	f := newFixture(t)
	req := f.request(nil)
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conn := f.db.NewConn(t)
			<-start
			tx, err := conn.Begin(context.Background())
			if err != nil {
				errs[i] = err
				return
			}
			_, err = ledgercommit.Commit(context.Background(), tx, ledger.New(), req)
			if err == nil {
				err = tx.Commit(context.Background())
			} else {
				_ = tx.Rollback(context.Background())
			}
			errs[i] = err
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}
	var events int
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id=$1`, f.tenant).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("concurrent idempotent commits stored %d events, want one", events)
	}
}

func TestTodo_LEDGER_008_Integration(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		stage string
		want  string
	}{{"after-event", "ledger_event"}, {"after-projection", "projection_checkpoint"}, {"after-outbox", "outbox"}, {"after-provenance", "provenance_record"}} {
		t.Run(tc.stage, func(t *testing.T) {
			tx, err := f.db.Conn.Begin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			req := f.request(func(got string) error {
				if got == tc.stage {
					return fmt.Errorf("injected %s failure", tc.stage)
				}
				return nil
			})
			if _, err := ledgercommit.Commit(context.Background(), tx, ledger.New(), req); err == nil {
				_ = tx.Rollback(context.Background())
				t.Fatal("injected failure was ignored")
			}
			if err := tx.Rollback(context.Background()); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM `+tc.want+` WHERE tenant_id=$1`, f.tenant).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("failed commit left %d rows in %s", count, tc.want)
			}
		})
	}
}

func TestTodo_LEDGER_008_Fault(t *testing.T) {
	f := newFixture(t)
	tx, err := f.db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("provenance unavailable")
	req := f.request(func(stage string) error {
		if stage == "after-provenance" {
			return want
		}
		return nil
	})
	if _, err := ledgercommit.Commit(context.Background(), tx, ledger.New(), req); !errors.Is(err, want) {
		_ = tx.Rollback(context.Background())
		t.Fatalf("commit error = %v, want injected cause", err)
	}
	_ = tx.Rollback(context.Background())
	var count int
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id=$1`, f.tenant).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("faulted transaction left %d events", count)
	}
}

func TestTodo_LEDGER_008_Mutation(t *testing.T) {
	f := newFixture(t)
	req := f.request(nil)
	var first, second ledgercommit.Receipt
	inTx(t, f.db, func(tx dbport.Tx) error {
		var err error
		first, err = ledgercommit.Commit(context.Background(), tx, ledger.New(), req)
		if err != nil {
			return err
		}
		second, err = ledgercommit.Commit(context.Background(), tx, ledger.New(), req)
		return err
	})
	if first.Ledger.EventID != second.Ledger.EventID || second.Projection.Applied || second.Provenance.Published {
		t.Fatalf("replay receipts differ: first=%+v second=%+v", first, second)
	}
}
