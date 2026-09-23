package admin_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/lineage"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/explorer"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/admin"
)

// rev028Fixture is a migrated schema with one tenant, one registered payload
// schema, one authority assignment and one worker stream carrying a single
// domain fact at sequence 1, mirroring newRev037Fixture's well-formed setup.
type rev028Fixture struct {
	db       *pgtest.DB
	tenant   uuid.UUID
	digester *hashchain.Digester
	appender *hashchain.Appender
	origin   ledger.AppendReceipt
}

func newRev028Fixture(t *testing.T) *rev028Fixture {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()

	registry, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatalf("build chain-link registry: %v", err)
	}
	digester := hashchain.NewDigester(registry)
	f := &rev028Fixture{db: db, tenant: tenant, digester: digester, appender: hashchain.NewAppender(digester)}

	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'acme', 'cell-local', 'Acme', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant)
	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.people.v1.CompensationBase', 1,
			'hcmnext.people.v1.CompensationBase', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, "hcmnext.people.v1.CompensationBase@1")
	db.Exec(t, `
		INSERT INTO authority_assignment (
			tenant_id, authority_ref, authority_kind, domain_scope, effective_from)
		VALUES ($1, 'authority:hr', 'INTERNAL', 'workforce', timestamptz '2020-01-01T00:00:00Z')`,
		tenant)

	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := ledger.EnsureStream(ctx, tx, tenant, "worker:1", "WORKER", "worker:1"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("ensure stream: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit stream: %v", err)
	}

	at := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	eff := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	appender := ledger.New(ledger.WithClock(func() time.Time { return at }))
	err = f.inTx(func(tx dbport.Tx) error {
		receipt, appendErr := appender.Append(ctx, tx, ledger.AppendRequest{
			Tenant:         tenant,
			StreamKey:      "worker:1",
			ExpectedHead:   0,
			AssertionClass: ledger.DomainFact,
			Authority:      "authority:hr",
			SourceRef:      "test:fixture",
			SchemaRef:      "hcmnext.people.v1.CompensationBase@1",
			Payload:        []byte("120000"),
			OccurredAt:     at,
			EffectiveAt:    eff,
			CorrelationID:  uuid.New(),
			IdempotencyKey: uuid.NewString(),
		})
		if appendErr != nil {
			return appendErr
		}
		f.origin = receipt
		_, appendErr = f.appender.Append(ctx, tx, receipt)
		return appendErr
	})
	if err != nil {
		t.Fatalf("append origin: %v", err)
	}
	return f
}

func (f *rev028Fixture) inTx(fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// rev028Deps wires the admin lineage ports to the governed explorer adapters
// over the fixture's real store, the same closure shape the production
// composition root uses for ListLedgerStream and VerifyLedgerChain.
func rev028Deps(f *rev028Fixture) admin.Dependencies {
	return admin.Dependencies{
		RecordLedgerCorrection: func(ctx context.Context, tenant uuid.UUID, req explorer.CorrectionRequest) (explorer.RecordCorrectionView, error) {
			var view explorer.RecordCorrectionView
			err := f.inTx(func(tx dbport.Tx) error {
				var err error
				view, err = explorer.RecordCorrection(ctx, tx, f.appender, tenant, req, nil)
				return err
			})
			return view, err
		},
		GetLedgerLineage: func(ctx context.Context, tenant uuid.UUID, ref explorer.EventRef) (explorer.LineageResultView, error) {
			return explorer.Lineage(ctx, f.db.Conn, tenant, ref, nil)
		},
		GetLedgerEffectiveCurrent: func(ctx context.Context, tenant uuid.UUID, ref explorer.EventRef) (explorer.EffectiveCurrentView, error) {
			return explorer.EffectiveCurrent(ctx, f.db.Conn, tenant, ref, nil)
		},
	}
}

// TestTodo_REV_028_01_Integration proves the admin operator surface reaches
// the lineage capability over a real store: recording a correction through
// the RecordLedgerCorrection port invokes lineage.Append, and resolving
// through the GetLedgerEffectiveCurrent and GetLedgerLineage ports reads back
// through lineage.EffectiveCurrent and lineage.Ancestors with results
// identical to the library calls.
func TestTodo_REV_028_01_Integration(t *testing.T) {
	f := newRev028Fixture(t)
	deps := rev028Deps(f)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if deps.RecordLedgerCorrection == nil || deps.GetLedgerLineage == nil || deps.GetLedgerEffectiveCurrent == nil {
		t.Fatal("admin lineage ports are not wired")
	}

	at := time.Date(2026, 2, 2, 9, 0, 0, 0, time.UTC)
	eff := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	recorded, err := deps.RecordLedgerCorrection(ctx, f.tenant, explorer.CorrectionRequest{
		StreamKey:      "worker:1",
		ExpectedHead:   f.origin.Sequence,
		Corrects:       explorer.EventRef{StreamKey: "worker:1", Sequence: f.origin.Sequence},
		Authority:      "authority:hr",
		SourceRef:      "test:rev028-admin",
		SchemaRef:      "hcmnext.people.v1.CompensationBase@1",
		Payload:        []byte("125000"),
		OccurredAt:     at,
		EffectiveAt:    eff,
		CorrelationID:  uuid.New(),
		IdempotencyKey: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("record through admin port: %v", err)
	}
	if recorded.Sequence != f.origin.Sequence+1 || recorded.Digest == "" {
		t.Fatalf("recorded %+v, want next sequence with a digest", recorded)
	}

	current, err := deps.GetLedgerEffectiveCurrent(ctx, f.tenant,
		explorer.EventRef{StreamKey: "worker:1", Sequence: f.origin.Sequence})
	if err != nil {
		t.Fatalf("effective current through admin port: %v", err)
	}
	if current.Current.Ref.Sequence != recorded.Sequence {
		t.Fatalf("port effective current = %d, want correction at %d",
			current.Current.Ref.Sequence, recorded.Sequence)
	}
	direct, directPath, err := lineage.EffectiveCurrent(ctx, f.db.Conn, f.tenant,
		ledger.EventRef{StreamKey: "worker:1", Sequence: f.origin.Sequence})
	if err != nil {
		t.Fatalf("direct effective current: %v", err)
	}
	if !reflect.DeepEqual(direct, current.Current) || !reflect.DeepEqual(directPath, current.Path) {
		t.Fatalf("port result %+v %+v != library result %+v %+v",
			current.Current, current.Path, direct, directPath)
	}

	walk, err := deps.GetLedgerLineage(ctx, f.tenant,
		explorer.EventRef{StreamKey: "worker:1", Sequence: recorded.Sequence})
	if err != nil {
		t.Fatalf("lineage through admin port: %v", err)
	}
	if len(walk.Ancestors) != 1 || walk.Ancestors[0].Ref.Sequence != f.origin.Sequence {
		t.Fatalf("port ancestors = %+v, want the origin", walk.Ancestors)
	}
	if len(walk.Descendants) != 0 {
		t.Fatalf("correction descendants = %+v, want none", walk.Descendants)
	}
	if walk.Digest == "" {
		t.Fatal("lineage digest is empty")
	}

	chain, err := explorer.VerifyChain(ctx, f.db.Conn, f.digester, f.tenant, "worker:1")
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	if !chain.Verified || chain.Head.Sequence != recorded.Sequence {
		t.Fatalf("chain = %+v, want verified at head %d", chain, recorded.Sequence)
	}

	_, err = deps.RecordLedgerCorrection(ctx, f.tenant, explorer.CorrectionRequest{
		StreamKey:      "worker:1",
		ExpectedHead:   recorded.Sequence,
		Corrects:       explorer.EventRef{StreamKey: "worker:1", Sequence: 999},
		Authority:      "authority:hr",
		SourceRef:      "test:rev028-admin",
		SchemaRef:      "hcmnext.people.v1.CompensationBase@1",
		Payload:        []byte("0"),
		OccurredAt:     at,
		EffectiveAt:    eff,
		CorrelationID:  uuid.New(),
		IdempotencyKey: uuid.NewString(),
	})
	var notFound lineage.ErrCorrectionTargetNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("dangling record through admin port: error = %v, want ErrCorrectionTargetNotFound", err)
	}
}
