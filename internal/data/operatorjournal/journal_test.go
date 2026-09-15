package operatorjournal_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/operatorjournal"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var at = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

func seedTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, id, key, "operatorjournal "+key)
	return id
}

func journal(db *pgtest.DB, ids map[values.TenantId]uuid.UUID) *operatorjournal.Journal {
	return &operatorjournal.Journal{DB: db.Conn, TenantIDs: func(t values.TenantId) (uuid.UUID, error) {
		id, ok := ids[t]
		if !ok {
			return uuid.Nil, errors.New("unknown tenant")
		}
		return id, nil
	}}
}

func pending(tenant values.TenantId, key, digest string) operator.Receipt {
	return operator.Receipt{Kind: operator.KindWorkflowCancel, Tenant: tenant, Operator: "operator:ana", IdempotencyKey: key,
		RequestDigest: digest, Outcome: operator.OutcomePending, RecordedAt: at, IntentInstanceID: "intent-" + key}
}

// TestDurableJournalReplaysAcrossRecomposition proves the journal's operator
// contract against PostgreSQL: a pending receipt is recorded once and a second
// Begin replays it; Complete seals the final receipt, which a journal composed
// over a fresh connection (a restart) returns; Complete and Abort are fenced
// on PENDING and the request digest; an aborted key is absent and reusable;
// tenants never see each other's receipts.
func TestDurableJournalReplaysAcrossRecomposition(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	ids := map[values.TenantId]uuid.UUID{"acme": seedTenant(t, db, "opj-acme"), "globex": seedTenant(t, db, "opj-globex")}
	j := journal(db, ids)

	p := pending("acme", "cancel-1", "sha256:req1")
	if got, replay, err := j.Begin(ctx, p); err != nil || replay || got.IdempotencyKey != "cancel-1" {
		t.Fatalf("Begin = %+v, %v, %v", got, replay, err)
	}
	if got, replay, err := j.Begin(ctx, pending("acme", "cancel-1", "sha256:other")); err != nil || !replay || got.RequestDigest != "sha256:req1" {
		t.Fatalf("second Begin = %+v, %v, %v; want replay of the first", got, replay, err)
	}
	wrongDigest := p
	wrongDigest.RequestDigest, wrongDigest.Outcome = "sha256:other", operator.OutcomeApplied
	if err := j.Complete(ctx, wrongDigest); !errors.Is(err, operatorjournal.ErrNoPending) {
		t.Fatalf("Complete with another digest = %v", err)
	}
	final := p
	final.Outcome, final.EffectRef, final.Digest = operator.OutcomeApplied, "workflowcontrol/v1|APPLIED", "sha256:sealed"
	if err := j.Complete(ctx, final); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := j.Complete(ctx, final); !errors.Is(err, operatorjournal.ErrNoPending) {
		t.Fatalf("second Complete = %v, want no pending receipt", err)
	}

	restarted := journal(db, ids)
	got, found, err := restarted.Lookup(ctx, "acme", "cancel-1")
	if err != nil || !found || got.Outcome != operator.OutcomeApplied || got.Digest != "sha256:sealed" || got.EffectRef == "" {
		t.Fatalf("Lookup after recomposition = %+v, %v, %v", got, found, err)
	}
	if _, found, err := restarted.Lookup(ctx, "globex", "cancel-1"); err != nil || found {
		t.Fatalf("another tenant saw the receipt: %v, %v", found, err)
	}

	abort := pending("acme", "retry-1", "sha256:r1")
	if _, _, err := j.Begin(ctx, abort); err != nil {
		t.Fatal(err)
	}
	if err := j.Abort(ctx, abort); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if _, found, err := j.Lookup(ctx, "acme", "retry-1"); err != nil || found {
		t.Fatalf("an aborted receipt is visible: %v, %v", found, err)
	}
	if err := j.Abort(ctx, abort); !errors.Is(err, operatorjournal.ErrNoPending) {
		t.Fatalf("second Abort = %v", err)
	}
	reused := pending("acme", "retry-1", "sha256:r2")
	if got, replay, err := j.Begin(ctx, reused); err != nil || replay || got.RequestDigest != "sha256:r2" {
		t.Fatalf("Begin on an aborted key = %+v, %v, %v; want a fresh pending receipt", got, replay, err)
	}

	for name, bad := range map[string]func() error{
		"no database":    func() error { _, _, err := (&operatorjournal.Journal{}).Lookup(ctx, "acme", "k"); return err },
		"blank key":      func() error { _, _, err := j.Lookup(ctx, "acme", " "); return err },
		"unknown tenant": func() error { _, _, err := j.Lookup(ctx, "initech", "k"); return err },
		"nil tenant id": func() error {
			nilIDs := &operatorjournal.Journal{DB: db.Conn, TenantIDs: func(values.TenantId) (uuid.UUID, error) { return uuid.Nil, nil }}
			_, _, err := nilIDs.Lookup(ctx, "acme", "k")
			return err
		},
		"incomplete receipt": func() error {
			r := pending("acme", "k", "")
			_, _, err := j.Begin(ctx, r)
			return err
		},
	} {
		if err := bad(); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

// TestDurableJournalBeginIsExactUnderConcurrency proves concurrent Begins for
// one key record exactly one pending receipt and every other caller replays
// it.
func TestDurableJournalBeginIsExactUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	ids := map[values.TenantId]uuid.UUID{"acme": seedTenant(t, db, "opj-race")}
	const n = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	fresh := make(chan string, n)
	for i := range n {
		// One independent session per caller: pgtest.DB.Conn is a single
		// connection and would serialize the race away.
		j := &operatorjournal.Journal{DB: db.NewConn(t), TenantIDs: journal(db, ids).TenantIDs}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			got, replay, err := j.Begin(ctx, pending("acme", "race", "sha256:race-"+string(rune('a'+i))))
			if err != nil {
				t.Errorf("Begin %d: %v", i, err)
				return
			}
			if !replay {
				fresh <- got.RequestDigest
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(fresh)
	var winners []string
	for d := range fresh {
		winners = append(winners, d)
	}
	if len(winners) != 1 {
		t.Fatalf("%d concurrent Begins recorded a fresh receipt, want exactly 1", len(winners))
	}
	if got, found, err := journal(db, ids).Lookup(ctx, "acme", "race"); err != nil || !found || got.RequestDigest != winners[0] {
		t.Fatalf("stored receipt = %+v, %v, %v; want the winner %s", got, found, err, winners[0])
	}
}
