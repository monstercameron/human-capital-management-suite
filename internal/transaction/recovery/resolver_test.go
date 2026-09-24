package recovery_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/recovery"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var recoveryAt = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func TestTodo_TX_005(t *testing.T) {
	db, prepared, tenant := recoveryFixture(t)
	injected := errors.New("lost commit acknowledgement")
	committer := commit.New(db.Conn, commit.Options{
		Clock: func() time.Time { return recoveryAt },
		Failpoint: func(stage string) error {
			if stage == "after-commit" {
				return injected
			}
			return nil
		},
	})
	if _, err := committer.Commit(context.Background(), prepared); !errors.Is(err, injected) {
		t.Fatalf("ambiguous commit error = %v, want injected acknowledgement loss", err)
	}
	result, err := recovery.Resolve(context.Background(), db.Conn, recovery.Request{
		Tenant: tenant,
		PlanID: prepared.PlanID,
		Scope: idempotency.Scope{
			Tenant: tenant, Capability: "transaction.commit",
			EffectScope: prepared.PlanID, Key: prepared.IdempotencyKey,
		},
	})
	if err != nil {
		t.Fatalf("resolve committed acknowledgement: %v", err)
	}
	if result.Outcome != recovery.OutcomeCommitted {
		t.Fatalf("resolved outcome = %q, want COMMITTED", result.Outcome)
	}
	if len(result.Evidence.LedgerEvents) != 1 || !result.Evidence.IdempotencyFound {
		t.Fatalf("evidence = %+v, want one ledger row and idempotency receipt", result.Evidence)
	}
}

func TestTodo_TX_005_Golden(t *testing.T) {
	db, prepared, tenant := recoveryFixture(t)
	result, err := recovery.Resolve(context.Background(), db.Conn, recovery.Request{
		Tenant: tenant, PlanID: prepared.PlanID, Key: prepared.IdempotencyKey,
	})
	if err != nil {
		t.Fatalf("resolve absent transaction: %v", err)
	}
	if result.Outcome != recovery.OutcomeNotCommitted || len(result.Evidence.LedgerEvents) != 0 {
		t.Fatalf("absent outcome = %+v, want NOT_COMMITTED without ledger evidence", result)
	}
}

func TestTodo_TX_005_Race(t *testing.T) {
	// Concurrent readers of one durable snapshot must agree on the recovery
	// decision and evidence, even while callers resolve it at the same time.
	db, prepared, tenant := recoveryFixture(t)
	const workers = 8
	start := make(chan struct{})
	results := make([]recovery.Result, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = recovery.Resolve(context.Background(), db.Conn, recovery.Request{Tenant: tenant, PlanID: prepared.PlanID, Key: prepared.IdempotencyKey})
		}(i)
	}
	close(start)
	wg.Wait()
	for i, result := range results {
		if errs[i] != nil {
			t.Fatalf("concurrent lookup %d: %v", i, errs[i])
		}
		if result.Outcome != recovery.OutcomeNotCommitted || len(result.Evidence.LedgerEvents) != 0 || result.Evidence.IdempotencyFound {
			t.Fatalf("concurrent lookup %d = %+v, want stable absent outcome", i, result)
		}
	}
}

func TestTodo_TX_005_Fault(t *testing.T) {
	db, _, tenant := recoveryFixture(t)
	tx, err := db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := ledger.EnsureStream(context.Background(), tx, tenant, "orphan", "WORKER", "orphan"); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(context.Background(), `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile) VALUES ($1, 'hcmnext.recovery.fault@1', 'hcmnext.recovery.fault', 1, 'hcmnext.recovery.fault', 'PROTOBUF', 'LEDGER_EVENT') ON CONFLICT DO NOTHING`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.New(ledger.WithClock(func() time.Time { return recoveryAt })).Append(context.Background(), tx, ledger.AppendRequest{
		Tenant: tenant, StreamKey: "orphan", ExpectedHead: 0,
		AssertionClass: ledger.TransactionFact, SourceRef: "transaction-plan:orphan-plan:FAULT",
		SchemaRef: "hcmnext.recovery.fault@1", Payload: []byte("durable event without receipt"),
		OccurredAt: recoveryAt, EffectiveAt: recoveryAt, CorrelationID: uuid.New(),
		IdempotencyKey: "orphan-key:event:0",
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	// A durable ledger row without the receipt is deliberately treated as
	// ambiguous. The resolver never calls this a failed commit.
	result, err := recovery.Resolve(context.Background(), db.Conn, recovery.Request{Tenant: tenant, PlanID: "orphan-plan", Key: "orphan-key"})
	if err != nil {
		t.Fatalf("resolve no-evidence boundary: %v", err)
	}
	if result.Outcome != recovery.OutcomeAmbiguousRequiresRecovery {
		t.Fatalf("orphan evidence outcome = %q, want AMBIGUOUS_REQUIRES_RECOVERY", result.Outcome)
	}
}

func TestTodo_TX_005_Mutation(t *testing.T) {
	db, _, tenant := recoveryFixture(t)
	_, err := recovery.Resolve(context.Background(), db.Conn, recovery.Request{Tenant: tenant})
	var invalid recovery.ErrInvalidRequest
	if !errors.As(err, &invalid) {
		t.Fatalf("invalid request error = %v, want ErrInvalidRequest", err)
	}
}

func recoveryFixture(t *testing.T) (*pgtest.DB, plan.TransactionPlan, uuid.UUID) {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1, $2, 'cell-local', 'Recovery test', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenant, "recovery-"+tenant.String())
	db.Exec(t, `SELECT set_config('app.tenant_id', $1, false)`, tenant.String())
	p := plan.TransactionPlan{
		PlanID: "recovery-plan-" + tenant.String(), Tenant: values.TenantId(tenant.String()),
		ProposalRevisionID: "proposal:" + tenant.String(), ProposalDigest: "sha256:" + strings.Repeat("a", 64),
		IdempotencyKey: "recovery:" + tenant.String(), ExpiresAt: values.NewInstant(recoveryAt.Add(time.Hour)),
		Streams: []plan.StreamPlan{{StreamKey: "recovery-stream", ExpectedSequence: 0}},
		Events:  []plan.PlannedEvent{{StreamKey: "recovery-stream", Sequence: 1, EventType: "RECOVERY_FACT", SchemaRef: "hcmnext.recovery.v1", Digest: strings.Repeat("a", 64)}},
	}
	sum := sha256.Sum256(p.CanonicalBytes())
	p.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return db, p, tenant
}
