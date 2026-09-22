package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
)

var rev013ClockAt = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func rev013Policy(maxAttempts int) outbox.GroupPolicy {
	return outbox.GroupPolicy{
		Group:       "wf-rev-013",
		MaxAttempts: maxAttempts,
		PoisonOwner: "wf-rev-013-owner",
		PoisonTTL:   time.Hour,
		RepairRoute: "wf-rev-013/repair",
		Clock:       func() time.Time { return rev013ClockAt },
	}
}

func rev013Consumer(t *testing.T, maxAttempts int) (*outbox.CompensatingConsumer, *outbox.MemoryCheckpointStore) {
	t.Helper()
	store := outbox.NewMemoryCheckpointStore()
	consumer, err := outbox.NewCompensatingConsumer(rev013Policy(maxAttempts), store)
	if err != nil {
		t.Fatalf("NewCompensatingConsumer: %v", err)
	}
	return consumer, store
}

func rev013Record(tenant uuid.UUID, effect, ordering string) outbox.Record {
	return outbox.Record{
		Tenant:         tenant,
		OutboxID:       uuid.New(),
		EffectIdentity: effect,
		OrderingKey:    ordering,
		Criticality:    "NORMAL",
		SchemaRef:      "hcmnext.test/v1",
		Payload:        []byte("payload:" + effect),
	}
}

func rev013CompensationOf(original string) string { return "reversal:" + original }

// rev013Recorder tallies handler runs by kind. applies counts original
// applications by identity; reversals counts compensation applications by
// the reversed identity; order logs every run so tests can prove a
// compensation never runs before its original.
type rev013Recorder struct {
	applies   map[string]int
	reversals map[string]int
	order     []string
}

func (r *rev013Recorder) handler() outbox.EffectHandler {
	return func(_ context.Context, rec outbox.Record, fencing outbox.Fencing) error {
		if !fencing.AllowExternalEffects {
			return errors.New("rev013: handler ran fenced")
		}
		original, isCompensation := outbox.IdentityCompensationLink(rec)
		if isCompensation {
			r.reversals[original]++
			r.order = append(r.order, "reverse:"+original)
			return nil
		}
		r.applies[rec.EffectIdentity]++
		r.order = append(r.order, "apply:"+rec.EffectIdentity)
		return nil
	}
}

func newRev013Recorder() *rev013Recorder {
	return &rev013Recorder{applies: map[string]int{}, reversals: map[string]int{}}
}

func rev013Dispatch(t *testing.T, consumer *outbox.CompensatingConsumer, rec outbox.Record, handler outbox.EffectHandler) outbox.ApplyOutcome {
	t.Helper()
	outcome, err := consumer.Dispatch(context.Background(), rec, outbox.IdentityCompensationLink, handler)
	if err != nil {
		t.Fatalf("Dispatch %s: %v", rec.EffectIdentity, err)
	}
	return outcome
}

// TestTodo_WF_REV_013 proves the PRIMARY contract: every compensating
// consumer applies a compensating event exactly once and holds an
// out-of-order compensation until its original arrives.
func TestTodo_WF_REV_013(t *testing.T) {
	t.Run("duplicate compensation before original applies once after it", func(t *testing.T) {
		tenant := uuid.New()
		consumer, _ := rev013Consumer(t, 3)
		recorder := newRev013Recorder()
		handler := recorder.handler()
		original, compensation := "payroll:rev013-a", rev013CompensationOf("payroll:rev013-a")

		if outcome := rev013Dispatch(t, consumer, rev013Record(tenant, compensation, "ledger-a"), handler); outcome != outbox.ApplyCompensationHeld {
			t.Fatalf("early compensation = %s, want COMPENSATION_HELD", outcome)
		}
		if outcome := rev013Dispatch(t, consumer, rev013Record(tenant, compensation, "ledger-a"), handler); outcome != outbox.ApplyCompensationHeld {
			t.Fatalf("duplicate early compensation = %s, want COMPENSATION_HELD", outcome)
		}
		if recorder.applies[original] != 0 || recorder.reversals[original] != 0 {
			t.Fatalf("held compensation ran: applies=%v reversals=%v", recorder.applies, recorder.reversals)
		}
		held := consumer.HeldCompensations(tenant, original)
		if len(held) != 1 || held[0].EffectIdentity != compensation {
			t.Fatalf("held = %+v, want exactly the one compensation", held)
		}

		if outcome := rev013Dispatch(t, consumer, rev013Record(tenant, original, "ledger-a"), handler); outcome != outbox.ApplyApplied {
			t.Fatalf("original = %s, want APPLIED", outcome)
		}
		if recorder.applies[original] != 1 || recorder.reversals[original] != 1 {
			t.Fatalf("applies=%v reversals=%v, want the original and its compensation exactly once", recorder.applies, recorder.reversals)
		}
		if len(recorder.order) != 2 || recorder.order[0] != "apply:"+original || recorder.order[1] != "reverse:"+original {
			t.Fatalf("order = %v, want the original applied before its compensation", recorder.order)
		}
		if held := consumer.HeldCompensations(tenant, original); len(held) != 0 {
			t.Fatalf("held after drain = %+v, want empty", held)
		}

		if outcome := rev013Dispatch(t, consumer, rev013Record(tenant, original, "ledger-a"), handler); outcome != outbox.ApplyDuplicateFenced {
			t.Fatalf("original redelivery = %s, want DUPLICATE_FENCED", outcome)
		}
		if outcome := rev013Dispatch(t, consumer, rev013Record(tenant, compensation, "ledger-a"), handler); outcome != outbox.ApplyDuplicateFenced {
			t.Fatalf("compensation redelivery = %s, want DUPLICATE_FENCED", outcome)
		}
		if recorder.applies[original] != 1 || recorder.reversals[original] != 1 {
			t.Fatalf("redelivery re-ran: applies=%v reversals=%v", recorder.applies, recorder.reversals)
		}
	})

	t.Run("in-order compensation applies immediately", func(t *testing.T) {
		tenant := uuid.New()
		consumer, _ := rev013Consumer(t, 3)
		recorder := newRev013Recorder()
		handler := recorder.handler()
		original := "payroll:rev013-b"
		if outcome := rev013Dispatch(t, consumer, rev013Record(tenant, original, "ledger-b"), handler); outcome != outbox.ApplyApplied {
			t.Fatalf("original = %s, want APPLIED", outcome)
		}
		if outcome := rev013Dispatch(t, consumer, rev013Record(tenant, rev013CompensationOf(original), "ledger-b"), handler); outcome != outbox.ApplyApplied {
			t.Fatalf("compensation = %s, want APPLIED", outcome)
		}
		if recorder.applies[original] != 1 || recorder.reversals[original] != 1 {
			t.Fatalf("applies=%v reversals=%v, want exactly once each", recorder.applies, recorder.reversals)
		}
	})

	t.Run("compensation for an unknown original stays held", func(t *testing.T) {
		tenant := uuid.New()
		consumer, _ := rev013Consumer(t, 3)
		recorder := newRev013Recorder()
		handler := recorder.handler()
		if outcome := rev013Dispatch(t, consumer, rev013Record(tenant, rev013CompensationOf("payroll:never-published"), "ledger-c"), handler); outcome != outbox.ApplyCompensationHeld {
			t.Fatalf("orphan compensation = %s, want COMPENSATION_HELD", outcome)
		}
		if len(recorder.order) != 0 {
			t.Fatalf("orphan compensation ran: %v", recorder.order)
		}
		held := consumer.HeldCompensations(tenant, "payroll:never-published")
		if len(held) != 1 {
			t.Fatalf("held = %+v, want the orphaned compensation", held)
		}
	})

	t.Run("compensations never cross tenants", func(t *testing.T) {
		tenantA, tenantB := uuid.New(), uuid.New()
		consumer, _ := rev013Consumer(t, 3)
		recorder := newRev013Recorder()
		handler := recorder.handler()
		original := "payroll:rev013-shared"

		if outcome := rev013Dispatch(t, consumer, rev013Record(tenantA, original, "ledger-d"), handler); outcome != outbox.ApplyApplied {
			t.Fatalf("tenant-A original = %s, want APPLIED", outcome)
		}
		if outcome := rev013Dispatch(t, consumer, rev013Record(tenantB, rev013CompensationOf(original), "ledger-d"), handler); outcome != outbox.ApplyCompensationHeld {
			t.Fatalf("tenant-B compensation = %s, want COMPENSATION_HELD: tenant A's original must not release it", outcome)
		}
		if recorder.reversals[original] != 0 {
			t.Fatalf("cross-tenant compensation ran: %v", recorder.order)
		}
		if outcome := rev013Dispatch(t, consumer, rev013Record(tenantB, original, "ledger-d"), handler); outcome != outbox.ApplyApplied {
			t.Fatalf("tenant-B original = %s, want APPLIED", outcome)
		}
		if recorder.reversals[original] != 1 {
			t.Fatalf("reversals=%v, want tenant B's compensation applied once its own original arrived", recorder.reversals)
		}
		if held := consumer.HeldCompensations(tenantB, original); len(held) != 0 {
			t.Fatalf("held after tenant-B drain = %+v, want empty", held)
		}
	})

	t.Run("failed original releases nothing", func(t *testing.T) {
		tenant := uuid.New()
		consumer, _ := rev013Consumer(t, 3)
		var originalAttempts int
		handler := func(_ context.Context, rec outbox.Record, _ outbox.Fencing) error {
			if _, isCompensation := outbox.IdentityCompensationLink(rec); isCompensation {
				t.Error("compensation ran while its original never applied")
				return nil
			}
			originalAttempts++
			return errors.New("rev013: original rejected downstream")
		}
		original, compensation := "payroll:rev013-doomed", rev013CompensationOf("payroll:rev013-doomed")
		if outcome := rev013Dispatch(t, consumer, rev013Record(tenant, compensation, "ledger-h"), handler); outcome != outbox.ApplyCompensationHeld {
			t.Fatalf("early compensation = %s, want COMPENSATION_HELD", outcome)
		}
		if outcome := rev013Dispatch(t, consumer, rev013Record(tenant, original, "ledger-h"), handler); outcome != outbox.ApplyTransientFailed {
			t.Fatalf("failing original = %s, want TRANSIENT_FAILED", outcome)
		}
		if originalAttempts != 1 {
			t.Fatalf("original attempts = %d, want exactly the one failed run", originalAttempts)
		}
		if held := consumer.HeldCompensations(tenant, original); len(held) != 1 {
			t.Fatalf("held after failed original = %+v, want the compensation still waiting", held)
		}
	})

	t.Run("checkpoints commit with compensation", func(t *testing.T) {
		ctx := context.Background()
		tenant := uuid.New()
		consumer, _ := rev013Consumer(t, 3)
		recorder := newRev013Recorder()
		handler := recorder.handler()
		original, compensation := "payroll:rev013-e", rev013CompensationOf("payroll:rev013-e")
		rev013Dispatch(t, consumer, rev013Record(tenant, original, "ledger-e"), handler)
		rev013Dispatch(t, consumer, rev013Record(tenant, compensation, "ledger-e"), handler)
		if err := consumer.Group().CommitCheckpoint(ctx, "ledger-e", tenant, compensation); err != nil {
			t.Fatalf("CommitCheckpoint for the applied compensation: %v", err)
		}
		if err := consumer.Group().CommitCheckpoint(ctx, "ledger-e", tenant, rev013CompensationOf("payroll:never-applied")); !errors.Is(err, outbox.ErrCheckpointBeforeEffect) {
			t.Fatalf("checkpoint before effect = %v, want ErrCheckpointBeforeEffect", err)
		}
	})

	t.Run("poisoned compensation isolates without touching the original", func(t *testing.T) {
		tenant := uuid.New()
		consumer, _ := rev013Consumer(t, 2)
		var applies, reversals int
		handler := func(_ context.Context, rec outbox.Record, _ outbox.Fencing) error {
			if _, isCompensation := outbox.IdentityCompensationLink(rec); isCompensation {
				reversals++
				return errors.New("rev013: reversal rejected downstream")
			}
			applies++
			return nil
		}
		original, compensation := "payroll:rev013-f", rev013CompensationOf("payroll:rev013-f")
		rev013Dispatch(t, consumer, rev013Record(tenant, original, "ledger-f"), handler)
		if outcome := rev013Dispatch(t, consumer, rev013Record(tenant, compensation, "ledger-f"), handler); outcome != outbox.ApplyTransientFailed {
			t.Fatalf("first reversal failure = %s, want TRANSIENT_FAILED", outcome)
		}
		if outcome := rev013Dispatch(t, consumer, rev013Record(tenant, compensation, "ledger-f"), handler); outcome != outbox.ApplyPoisonIsolated {
			t.Fatalf("second reversal failure = %s, want POISON_ISOLATED", outcome)
		}
		if outcome := rev013Dispatch(t, consumer, rev013Record(tenant, original, "ledger-f"), handler); outcome != outbox.ApplyDuplicateFenced {
			t.Fatalf("original redelivery = %s, want DUPLICATE_FENCED", outcome)
		}
		if outcome := rev013Dispatch(t, consumer, rev013Record(tenant, compensation, "ledger-f"), handler); outcome != outbox.ApplyPoisonIsolated {
			t.Fatalf("poisoned reversal redelivery = %s, want POISON_ISOLATED", outcome)
		}
		if applies != 1 || reversals != 2 {
			t.Fatalf("applies=%d reversals=%d, want the original once and the reversal never retried past poison", applies, reversals)
		}
		poisoned := consumer.Group().Poisoned("wf-rev-013")
		if len(poisoned) != 1 || poisoned[0].EffectIdentity != compensation {
			t.Fatalf("poisoned = %+v, want the compensation only", poisoned)
		}
	})

	t.Run("malformed compensation is rejected", func(t *testing.T) {
		ctx := context.Background()
		tenant := uuid.New()
		consumer, _ := rev013Consumer(t, 3)
		handler := newRev013Recorder().handler()
		if _, err := consumer.Dispatch(ctx, outbox.Record{}, outbox.IdentityCompensationLink, handler); !errors.Is(err, outbox.ErrInvalidRecord) {
			t.Fatalf("identity-free record = %v, want ErrInvalidRecord", err)
		}
		if _, err := consumer.Dispatch(ctx, rev013Record(tenant, "reversal:", "ledger-g"), outbox.IdentityCompensationLink, handler); !errors.Is(err, outbox.ErrInvalidCompensation) {
			t.Fatalf("empty reversal target = %v, want ErrInvalidCompensation", err)
		}
		if _, err := consumer.Dispatch(ctx, rev013Record(tenant, "payroll:rev013-g", "ledger-g"), nil, handler); err == nil {
			t.Fatal("nil compensation link accepted")
		}
		if _, err := consumer.Dispatch(ctx, rev013Record(tenant, "payroll:rev013-g", "ledger-g"), outbox.IdentityCompensationLink, nil); err == nil {
			t.Fatal("nil handler accepted")
		}
		if _, err := outbox.NewCompensatingConsumer(rev013Policy(3), nil); err == nil {
			t.Fatal("storeless compensating consumer accepted")
		}
		if _, err := outbox.NewCompensatingConsumer(outbox.GroupPolicy{}, outbox.NewMemoryCheckpointStore()); err == nil {
			t.Fatal("empty group policy accepted")
		}
		selfReversing := func(rec outbox.Record) (string, bool) { return rec.EffectIdentity, true }
		if _, err := consumer.Dispatch(ctx, rev013Record(tenant, "payroll:rev013-self", "ledger-g"), selfReversing, handler); !errors.Is(err, outbox.ErrInvalidCompensation) {
			t.Fatalf("self-reversing compensation accepted: %v", err)
		}
	})
}

// TestTodo_WF_REV_013_Property covers duplicate and reordered delivery:
// every randomized sequence of original and compensating deliveries ends
// with each delivered compensation applied exactly once after its
// original, or held exactly once when the original never arrives.
func TestTodo_WF_REV_013_Property(t *testing.T) {
	for seed := int64(0); seed < 200; seed++ {
		t.Run(fmt.Sprintf("seed/%d", seed), func(t *testing.T) {
			rev013PropertySeed(t, seed)
		})
	}
}

func rev013PropertySeed(t *testing.T, seed int64) {
	t.Helper()
	ctx := context.Background()
	tenant := uuid.New()
	consumer, _ := rev013Consumer(t, 5)
	rng := rand.New(rand.NewSource(seed))

	originals := []string{"prop-original-1", "prop-original-2"}
	compensationOf := func(original string) string { return "reversal:" + original }
	// Each identity keeps one fixed partition (the group's applied fence
	// is per partition, as for plain dispatches); the compensation may
	// use a different partition than its original, which the consumer
	// correlates without partition affinity.
	originalPartition := map[string]string{}
	compensationPartition := map[string]string{}
	for _, original := range originals {
		originalPartition[original] = fmt.Sprintf("prop-p%d", 1+rng.Intn(2))
		compensationPartition[original] = fmt.Sprintf("prop-p%d", 1+rng.Intn(2))
	}

	recorder := newRev013Recorder()
	handler := recorder.handler()
	deliveredOriginal := map[string]bool{}
	deliveredCompensation := map[string]bool{}
	for i, n := 0, 4+rng.Intn(7); i < n; i++ {
		original := originals[rng.Intn(len(originals))]
		if rng.Intn(2) == 0 {
			outcome, err := consumer.Dispatch(ctx, rev013Record(tenant, original, originalPartition[original]), outbox.IdentityCompensationLink, handler)
			if err != nil {
				t.Fatalf("seed %d op %d: Dispatch %s: %v", seed, i, original, err)
			}
			if outcome != outbox.ApplyApplied && outcome != outbox.ApplyDuplicateFenced {
				t.Fatalf("seed %d op %d: original outcome = %s, want APPLIED or DUPLICATE_FENCED", seed, i, outcome)
			}
			deliveredOriginal[original] = true
			continue
		}
		outcome, err := consumer.Dispatch(ctx, rev013Record(tenant, compensationOf(original), compensationPartition[original]), outbox.IdentityCompensationLink, handler)
		if err != nil {
			t.Fatalf("seed %d op %d: Dispatch %s: %v", seed, i, compensationOf(original), err)
		}
		if outcome != outbox.ApplyApplied && outcome != outbox.ApplyDuplicateFenced && outcome != outbox.ApplyCompensationHeld {
			t.Fatalf("seed %d op %d: compensation outcome = %s, want APPLIED, DUPLICATE_FENCED or COMPENSATION_HELD", seed, i, outcome)
		}
		deliveredCompensation[original] = true
	}
	// Fixpoint: redeliver every delivered identity once more, so held
	// compensations whose original arrived always drain.
	for _, original := range originals {
		if deliveredOriginal[original] {
			if _, err := consumer.Dispatch(ctx, rev013Record(tenant, original, originalPartition[original]), outbox.IdentityCompensationLink, handler); err != nil {
				t.Fatalf("seed %d: fixpoint Dispatch %s: %v", seed, original, err)
			}
		}
	}
	for _, original := range originals {
		if deliveredCompensation[original] {
			if _, err := consumer.Dispatch(ctx, rev013Record(tenant, compensationOf(original), compensationPartition[original]), outbox.IdentityCompensationLink, handler); err != nil {
				t.Fatalf("seed %d: fixpoint Dispatch %s: %v", seed, compensationOf(original), err)
			}
		}
	}

	positionOf := func(tag string) int {
		for i, entry := range recorder.order {
			if entry == tag {
				return i
			}
		}
		return -1
	}
	for _, original := range originals {
		switch {
		case deliveredCompensation[original] && deliveredOriginal[original]:
			if recorder.applies[original] != 1 {
				t.Fatalf("seed %d: %s applied %d times, want exactly once", seed, original, recorder.applies[original])
			}
			if recorder.reversals[original] != 1 {
				t.Fatalf("seed %d: %s reversed %d times, want exactly once", seed, original, recorder.reversals[original])
			}
			if positionOf("reverse:"+original) < positionOf("apply:"+original) {
				t.Fatalf("seed %d: order = %v, want the compensation after its original", seed, recorder.order)
			}
			if held := consumer.HeldCompensations(tenant, original); len(held) != 0 {
				t.Fatalf("seed %d: held after fixpoint = %+v, want empty", seed, held)
			}
		case deliveredCompensation[original]:
			if recorder.reversals[original] != 0 {
				t.Fatalf("seed %d: %s reversed without its original: %v", seed, original, recorder.order)
			}
			held := consumer.HeldCompensations(tenant, original)
			if len(held) != 1 || held[0].EffectIdentity != compensationOf(original) {
				t.Fatalf("seed %d: held = %+v, want the one compensation exactly once", seed, held)
			}
		default:
			if recorder.reversals[original] != 0 {
				t.Fatalf("seed %d: undelivered compensation ran: %v", seed, recorder.order)
			}
		}
		if !deliveredOriginal[original] && recorder.applies[original] != 0 {
			t.Fatalf("seed %d: undelivered original applied: %v", seed, recorder.order)
		}
	}
}
