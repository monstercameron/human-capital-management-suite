package outbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

const (
	retrySchemaPayroll = "hcmnext.events.v1.PayrollPromotion@1"
	retrySchemaIAM     = "hcmnext.events.v1.IAMPromotion@1"
	retrySchemaLegacy  = "hcmnext.events.v1.OutboxEvent@1"
)

type retryFixture struct {
	db     *pgtest.DB
	tenant uuid.UUID
	now    time.Time
}

func newRetryFixture(t *testing.T) *retryFixture {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-retry', 'Retry', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, "retry-"+tenant.String())
	for _, ref := range []string{retrySchemaPayroll, retrySchemaIAM, retrySchemaLegacy} {
		id := ref[:len(ref)-2]
		db.Exec(t, `
			INSERT INTO payload_schema (
				tenant_id, schema_ref, schema_id, schema_version,
				message_full_name, wire_format, canonicalization_profile)
			VALUES ($1, $2, $3, 1, $3, 'PROTOBUF', 'LEDGER_EVENT')`,
			tenant, ref, id)
	}
	return &retryFixture{db: db, tenant: tenant, now: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)}
}

func (f *retryFixture) clock() time.Time { return f.now }

func (f *retryFixture) enqueue(t *testing.T, effect, schemaRef string) outbox.Record {
	t.Helper()
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	rec, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
		Tenant:         f.tenant,
		EffectIdentity: effect,
		OrderingKey:    "worker:" + effect,
		SchemaRef:      schemaRef,
		Payload:        []byte(effect),
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("enqueue %s: %v", effect, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return rec
}

func (f *retryFixture) consumer(opts ...outbox.ConsumerOption) *outbox.Consumer {
	return outbox.NewConsumer(f.db.Conn, append([]outbox.ConsumerOption{
		outbox.WithLease(time.Minute), outbox.WithClock(f.clock),
	}, opts...)...)
}

func (f *retryFixture) pollOne(t *testing.T, c *outbox.Consumer) outbox.Record {
	t.Helper()
	got, err := c.Poll(context.Background(), f.tenant)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("polled %d rows, want 1", len(got))
	}
	return got[0]
}

func (f *retryFixture) pollNone(t *testing.T, c *outbox.Consumer) {
	t.Helper()
	got, err := c.Poll(context.Background(), f.tenant)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("polled %d rows, want 0", len(got))
	}
}

func (f *retryFixture) read(t *testing.T, id uuid.UUID) outbox.Record {
	t.Helper()
	rec, err := outbox.Read(context.Background(), f.db.Conn, f.tenant, id)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return rec
}

func TestConsumer_FailAfterDelaysRedelivery(t *testing.T) {
	f := newRetryFixture(t)
	ctx := context.Background()
	rec := f.enqueue(t, "fail-after", retrySchemaPayroll)
	c := f.consumer()
	claim := f.pollOne(t, c)
	if claim.Attempts != 1 {
		t.Fatalf("claimed attempts = %d, want 1", claim.Attempts)
	}
	retryAt := f.now.Add(30 * time.Second)
	if err := c.FailAfter(ctx, f.tenant, rec.OutboxID, claim.LeaseToken, errors.New("503"), retryAt); err != nil {
		t.Fatalf("FailAfter: %v", err)
	}
	row := f.read(t, rec.OutboxID)
	if row.Status != outbox.StatusPending || !row.AvailableAt.Equal(retryAt) || row.LastError == nil || *row.LastError != "503" {
		t.Fatalf("row after FailAfter = %s %v %v", row.Status, row.AvailableAt, row.LastError)
	}
	f.now = retryAt.Add(-time.Microsecond)
	f.pollNone(t, c)
	f.now = retryAt
	if again := f.pollOne(t, c); again.Attempts != 2 {
		t.Fatalf("re-polled attempts = %d, want 2", again.Attempts)
	}
}

func TestConsumer_FailAfterPastTimeIsImmediate(t *testing.T) {
	f := newRetryFixture(t)
	rec := f.enqueue(t, "fail-after-past", retrySchemaPayroll)
	c := f.consumer()
	claim := f.pollOne(t, c)
	if err := c.FailAfter(context.Background(), f.tenant, rec.OutboxID, claim.LeaseToken, errors.New("x"), f.now.Add(-time.Hour)); err != nil {
		t.Fatalf("FailAfter: %v", err)
	}
	if row := f.read(t, rec.OutboxID); !row.AvailableAt.Equal(f.now) {
		t.Fatalf("available_at = %v, want clamped to now %v", row.AvailableAt, f.now)
	}
	f.pollOne(t, c)
}

func TestConsumer_FailAfterExhaustionAbandons(t *testing.T) {
	f := newRetryFixture(t)
	ctx := context.Background()
	rec := f.enqueue(t, "fail-after-exhaust", retrySchemaPayroll)
	c := f.consumer(outbox.WithMaxAttempts(2))
	claim := f.pollOne(t, c)
	if err := c.FailAfter(ctx, f.tenant, rec.OutboxID, claim.LeaseToken, errors.New("one"), f.now); err != nil {
		t.Fatalf("FailAfter 1: %v", err)
	}
	claim = f.pollOne(t, c)
	if err := c.FailAfter(ctx, f.tenant, rec.OutboxID, claim.LeaseToken, errors.New("two"), f.now.Add(time.Hour)); err != nil {
		t.Fatalf("FailAfter 2: %v", err)
	}
	row := f.read(t, rec.OutboxID)
	if row.Status != outbox.StatusAbandoned || *row.LastError != "two" {
		t.Fatalf("exhausted row = %s %v", row.Status, *row.LastError)
	}
	f.now = f.now.Add(2 * time.Hour)
	f.pollNone(t, c)
}

func TestConsumer_DeferDoesNotConsumeAttempt(t *testing.T) {
	f := newRetryFixture(t)
	ctx := context.Background()
	rec := f.enqueue(t, "defer", retrySchemaIAM)
	// maxAttempts 2: had Defer consumed attempts, the final real Fail would abandon.
	c := f.consumer(outbox.WithMaxAttempts(2))
	for i := 0; i < 3; i++ {
		claim := f.pollOne(t, c)
		if claim.Attempts != 1 {
			t.Fatalf("round %d: attempts = %d, want 1 (Defer must not consume)", i, claim.Attempts)
		}
		retryAt := f.now.Add(10 * time.Second)
		if err := c.Defer(ctx, f.tenant, rec.OutboxID, claim.LeaseToken, retryAt, "breaker open"); err != nil {
			t.Fatalf("Defer: %v", err)
		}
		row := f.read(t, rec.OutboxID)
		if row.Status != outbox.StatusPending || row.Attempts != 0 || !row.AvailableAt.Equal(retryAt) ||
			row.LastError == nil || *row.LastError != "deferred: breaker open" {
			t.Fatalf("round %d: row = %s attempts=%d at=%v err=%v", i, row.Status, row.Attempts, row.AvailableAt, row.LastError)
		}
		f.pollNone(t, c)
		f.now = retryAt
	}
	// A real failure after deferrals still has its full budget.
	claim := f.pollOne(t, c)
	if err := c.FailLease(ctx, f.tenant, rec.OutboxID, claim.LeaseToken, errors.New("real")); err != nil {
		t.Fatalf("FailLease: %v", err)
	}
	if row := f.read(t, rec.OutboxID); row.Status != outbox.StatusPending {
		t.Fatalf("status after first real failure = %s, want PENDING", row.Status)
	}
}

func TestConsumer_AbandonParks(t *testing.T) {
	f := newRetryFixture(t)
	rec := f.enqueue(t, "abandon", retrySchemaPayroll)
	c := f.consumer()
	claim := f.pollOne(t, c)
	if err := c.Abandon(context.Background(), f.tenant, rec.OutboxID, claim.LeaseToken, errors.New("422 invalid grade")); err != nil {
		t.Fatalf("Abandon: %v", err)
	}
	row := f.read(t, rec.OutboxID)
	if row.Status != outbox.StatusAbandoned || row.LastError == nil || *row.LastError != "422 invalid grade" {
		t.Fatalf("row = %s %v", row.Status, row.LastError)
	}
	f.now = f.now.Add(time.Hour)
	f.pollNone(t, c)
}

func TestConsumer_StaleTokenRefused(t *testing.T) {
	f := newRetryFixture(t)
	ctx := context.Background()
	rec := f.enqueue(t, "stale", retrySchemaPayroll)
	c := f.consumer()
	stale := f.pollOne(t, c)
	// Lease expires; another poller reclaims with a new token.
	f.now = f.now.Add(2 * time.Minute)
	fresh := f.pollOne(t, c)
	if fresh.LeaseToken == stale.LeaseToken {
		t.Fatal("reclaim kept the old token")
	}
	calls := map[string]func(token uuid.UUID) error{
		"FailAfter": func(tok uuid.UUID) error {
			return c.FailAfter(ctx, f.tenant, rec.OutboxID, tok, errors.New("x"), f.now.Add(time.Minute))
		},
		"Defer": func(tok uuid.UUID) error {
			return c.Defer(ctx, f.tenant, rec.OutboxID, tok, f.now.Add(time.Minute), "open")
		},
		"Abandon": func(tok uuid.UUID) error {
			return c.Abandon(ctx, f.tenant, rec.OutboxID, tok, errors.New("x"))
		},
	}
	for name, call := range calls {
		if err := call(stale.LeaseToken); !errors.Is(err, outbox.ErrLeaseFence) {
			t.Fatalf("%s with stale token: err = %v, want ErrLeaseFence", name, err)
		}
		if err := call(uuid.Nil); err == nil {
			t.Fatalf("%s with nil token succeeded", name)
		}
	}
	row := f.read(t, rec.OutboxID)
	if row.Status != outbox.StatusInFlight || row.LeaseToken != fresh.LeaseToken || row.Attempts != 2 {
		t.Fatalf("fresh claim disturbed: %s token=%v attempts=%d", row.Status, row.LeaseToken, row.Attempts)
	}
	// The live owner still settles.
	if err := c.Abandon(ctx, f.tenant, rec.OutboxID, fresh.LeaseToken, errors.New("done")); err != nil {
		t.Fatalf("owner Abandon: %v", err)
	}
	// Nothing to settle any more: all three are fenced.
	for name, call := range calls {
		if err := call(fresh.LeaseToken); !errors.Is(err, outbox.ErrLeaseFence) {
			t.Fatalf("%s on settled row: err = %v, want ErrLeaseFence", name, err)
		}
	}
}

func TestConsumer_ArgumentValidation(t *testing.T) {
	c := outbox.NewConsumer(nil)
	ctx := context.Background()
	id, tok := uuid.New(), uuid.New()
	if err := c.FailAfter(ctx, id, id, tok, nil, time.Now()); err == nil {
		t.Fatal("FailAfter nil cause accepted")
	}
	if err := c.FailAfter(ctx, id, id, tok, errors.New("x"), time.Time{}); err == nil {
		t.Fatal("FailAfter zero retryAt accepted")
	}
	if err := c.Defer(ctx, id, id, tok, time.Time{}, "r"); err == nil {
		t.Fatal("Defer zero retryAt accepted")
	}
	if err := c.Defer(ctx, id, id, tok, time.Now(), ""); err == nil {
		t.Fatal("Defer empty reason accepted")
	}
	if err := c.Abandon(ctx, id, id, tok, nil); err == nil {
		t.Fatal("Abandon nil cause accepted")
	}
}

func TestConsumer_SchemaFilters(t *testing.T) {
	f := newRetryFixture(t)
	ctx := context.Background()
	payroll := f.enqueue(t, "payroll", retrySchemaPayroll)
	iam := f.enqueue(t, "iam", retrySchemaIAM)
	legacy := f.enqueue(t, "legacy", retrySchemaLegacy)

	idsOf := func(recs []outbox.Record) map[uuid.UUID]bool {
		out := map[uuid.UUID]bool{}
		for _, r := range recs {
			out[r.OutboxID] = true
		}
		return out
	}
	// Legacy sweep excludes the provider schemas.
	sweep := f.consumer(outbox.WithoutSchemaRefs(retrySchemaPayroll), outbox.WithoutSchemaRefs(retrySchemaIAM))
	got, err := sweep.Poll(ctx, f.tenant)
	if err != nil {
		t.Fatalf("sweep poll: %v", err)
	}
	if ids := idsOf(got); len(ids) != 1 || !ids[legacy.OutboxID] {
		t.Fatalf("sweep claimed %v, want only legacy", ids)
	}
	// Provider role claims only its schemas.
	provider := f.consumer(outbox.WithSchemaRefs(retrySchemaPayroll, retrySchemaIAM))
	got, err = provider.Poll(ctx, f.tenant)
	if err != nil {
		t.Fatalf("provider poll: %v", err)
	}
	if ids := idsOf(got); len(ids) != 2 || !ids[payroll.OutboxID] || !ids[iam.OutboxID] {
		t.Fatalf("provider claimed %v, want payroll+iam", ids)
	}
	for _, r := range got {
		if err := provider.Defer(ctx, f.tenant, r.OutboxID, r.LeaseToken, f.now, "reset"); err != nil {
			t.Fatalf("defer: %v", err)
		}
	}
	// Include and exclude combine.
	both := f.consumer(outbox.WithSchemaRefs(retrySchemaPayroll, retrySchemaIAM), outbox.WithoutSchemaRefs(retrySchemaIAM))
	got, err = both.Poll(ctx, f.tenant)
	if err != nil {
		t.Fatalf("combined poll: %v", err)
	}
	if ids := idsOf(got); len(ids) != 1 || !ids[payroll.OutboxID] {
		t.Fatalf("combined claimed %v, want only payroll", ids)
	}
	// An explicit empty allow-list claims nothing.
	f.pollNone(t, f.consumer(outbox.WithSchemaRefs()))
	// Empty refs are rejected.
	if _, err := f.consumer(outbox.WithSchemaRefs("")).Poll(ctx, f.tenant); err == nil {
		t.Fatal("empty include ref accepted")
	}
	if _, err := f.consumer(outbox.WithoutSchemaRefs("")).Poll(ctx, f.tenant); err == nil {
		t.Fatal("empty exclude ref accepted")
	}
	// No filters: the remaining IAM row is claimable by the default consumer.
	if rec := f.pollOne(t, f.consumer()); rec.OutboxID != iam.OutboxID {
		t.Fatalf("unfiltered poll claimed %s, want iam", rec.OutboxID)
	}
}
