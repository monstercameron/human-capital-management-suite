package outbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// errBrokerStore is a BrokerStore that always fails. It keeps the
// hermetic legs hermetic: constructor validation never touches a
// database.
type errBrokerStore struct{ err error }

func (s errBrokerStore) Exec(context.Context, string, ...any) (int64, error) {
	return 0, s.err
}

func (s errBrokerStore) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, s.err
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

func (s errBrokerStore) QueryRow(context.Context, string, ...any) dbport.Row {
	return errRow{s.err}
}

func brokerPolicy() BrokerPolicy { return BrokerPolicy{MaxAttempts: 3, MaxPollLimit: 100} }

func memoryBroker(t *testing.T) *MemoryBroker {
	t.Helper()
	broker, err := NewMemoryBroker(brokerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return broker
}

// TestTodo_EVENT_005 proves the broker-neutral adapter: a logical stream
// with partition/order keys carries deduped records to consumers whose
// checkpoints, bounded retries, dead letters and replay all program the
// neutral port, never broker-native semantics.
func TestTodo_EVENT_005(t *testing.T) {
	ctx := context.Background()
	broker := memoryBroker(t)
	tenant := uuid.New()

	first, err := broker.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "ledger", Partition: "member-1", EffectIdentity: "evt-1", OrderingKey: "member-1/1", Payload: []byte("debit 100"), SchemaRef: "hcm/ledger/v1"})
	if err != nil || first.Offset != 1 || first.Duplicate {
		t.Fatalf("publish = %+v err=%v", first, err)
	}
	second, err := broker.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "ledger", Partition: "member-1", EffectIdentity: "evt-2", OrderingKey: "member-1/2", Payload: []byte("credit 100"), SchemaRef: "hcm/ledger/v1"})
	if err != nil || second.Offset != 2 {
		t.Fatalf("second publish = %+v err=%v", second, err)
	}
	// One consumer drains the partition, checkpointing as it goes.
	page, err := broker.Poll(ctx, tenant, "projection", "ledger", "member-1", 0, 10)
	if err != nil || len(page) != 2 {
		t.Fatalf("poll = %+v err=%v", page, err)
	}
	if _, err := broker.CommitCheckpoint(ctx, ConsumerCheckpoint{Tenant: tenant, Consumer: "projection", Stream: "ledger", Partition: "member-1", Offset: 2}); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	// A second consumer replays the same history independently.
	replayed, err := broker.Replay(ctx, tenant, "audit", "ledger", "member-1", 0, 10)
	if err != nil || len(replayed) != 2 || !bytes.Equal(replayed[1].Payload, []byte("credit 100")) {
		t.Fatalf("replay = %+v err=%v", replayed, err)
	}
	// A poisoned record exhausts its budget and parks; the partition and
	// the healthy consumer are unaffected.
	for i := 0; i < 3; i++ {
		_, err := broker.Fail(ctx, tenant, "ledger", "evt-1", "projection panic")
		if err != nil {
			t.Fatal(err)
		}
	}
	letters, err := broker.DeadLetters(ctx, tenant, "ledger", "member-1", 10)
	if err != nil || len(letters) != 1 || letters[0].EffectIdentity != "evt-1" {
		t.Fatalf("dead letters = %+v err=%v", letters, err)
	}
	position, found, err := broker.ReadCheckpoint(ctx, tenant, "projection", "ledger", "member-1")
	if err != nil || !found || position.Offset != 2 {
		t.Fatalf("healthy checkpoint moved: %+v found=%v err=%v", position, found, err)
	}
}

func TestTodo_EVENT_005_Race(t *testing.T) {
	ctx := context.Background()
	broker := memoryBroker(t)
	tenant := uuid.New()

	// Concurrent publishers on one partition mint every offset exactly
	// once with no gaps.
	const publishers = 32
	var wg sync.WaitGroup
	offsets := make([]int64, publishers)
	errs := make([]error, publishers)
	for i := 0; i < publishers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			record, err := broker.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "race", Partition: "p", EffectIdentity: fmt.Sprintf("eff-%d", i), Payload: []byte("x")})
			if err == nil {
				offsets[i] = record.Offset
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	seen := map[int64]bool{}
	for i := 0; i < publishers; i++ {
		if errs[i] != nil {
			t.Fatalf("publisher %d: %v", i, errs[i])
		}
		if offsets[i] < 1 || seen[offsets[i]] {
			t.Fatalf("publisher %d offset %d is missing or duplicated", i, offsets[i])
		}
		seen[offsets[i]] = true
	}
	// Concurrent redeliveries of one identity converge on one offset.
	const redeliveries = 16
	digests := make([]string, redeliveries)
	for i := 0; i < redeliveries; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			record, err := broker.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "race", Partition: "p", EffectIdentity: "eff-once", Payload: []byte("once")})
			if err == nil {
				digests[i] = record.Digest
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i := 0; i < redeliveries; i++ {
		if errs[i] != nil || digests[i] != digests[0] {
			t.Fatalf("redelivery %d diverged: %q err=%v", i, digests[i], errs[i])
		}
	}
	// Concurrent failures against a deep budget count exactly once each.
	deep, err := NewMemoryBroker(BrokerPolicy{MaxAttempts: 1 << 20, MaxPollLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deep.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "race", Partition: "q", EffectIdentity: "eff-fail", Payload: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	const failures = 32
	for i := 0; i < failures; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = deep.Fail(ctx, tenant, "race", "eff-fail", "boom")
		}()
	}
	wg.Wait()
	records, err := deep.Poll(ctx, tenant, "c", "race", "q", 0, 10)
	if err != nil || len(records) != 1 || records[0].Attempts != failures {
		t.Fatalf("concurrent failures = %+v err=%v", records, err)
	}
	// Concurrent commits of one offset all hold.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = broker.CommitCheckpoint(ctx, ConsumerCheckpoint{Tenant: tenant, Consumer: "racer", Stream: "race", Partition: "p", Offset: 1})
		}()
	}
	wg.Wait()
	position, found, err := broker.ReadCheckpoint(ctx, tenant, "racer", "race", "p")
	if err != nil || !found || position.Offset != 1 {
		t.Fatalf("raced checkpoint = %+v found=%v err=%v", position, found, err)
	}
}

func TestTodo_EVENT_005_Fault(t *testing.T) {
	ctx := context.Background()
	broker := memoryBroker(t)
	tenant := uuid.New()
	good := PublishRequest{Tenant: tenant, Stream: "s", Partition: "p", EffectIdentity: "e", Payload: []byte("x")}

	cases := []struct {
		name  string
		apply func() error
		want  error
	}{
		{"nil tenant publish", func() error { r := good; r.Tenant = uuid.Nil; _, err := broker.Publish(ctx, r); return err }, ErrBrokerInvalid},
		{"blank stream", func() error { r := good; r.Stream = ""; _, err := broker.Publish(ctx, r); return err }, ErrBrokerInvalid},
		{"padded partition", func() error { r := good; r.Partition = " p"; _, err := broker.Publish(ctx, r); return err }, ErrBrokerInvalid},
		{"blank identity", func() error { r := good; r.EffectIdentity = ""; _, err := broker.Publish(ctx, r); return err }, ErrBrokerInvalid},
		{"empty payload", func() error { r := good; r.Payload = nil; _, err := broker.Publish(ctx, r); return err }, ErrBrokerInvalid},
		{"blank consumer poll", func() error { _, err := broker.Poll(ctx, tenant, "", "s", "p", 0, 1); return err }, ErrBrokerInvalid},
		{"negative poll offset", func() error { _, err := broker.Poll(ctx, tenant, "c", "s", "p", -1, 1); return err }, ErrBrokerInvalid},
		{"zero poll limit", func() error { _, err := broker.Poll(ctx, tenant, "c", "s", "p", 0, 0); return err }, ErrBrokerInvalid},
		{"oversize poll limit", func() error { _, err := broker.Poll(ctx, tenant, "c", "s", "p", 0, 101); return err }, ErrBrokerInvalid},
		{"negative checkpoint", func() error {
			_, err := broker.CommitCheckpoint(ctx, ConsumerCheckpoint{Tenant: tenant, Consumer: "c", Stream: "s", Partition: "p", Offset: -1})
			return err
		}, ErrBrokerInvalid},
		{"unknown fail", func() error { _, err := broker.Fail(ctx, tenant, "s", "missing", "boom"); return err }, ErrBrokerUnknown},
		{"blank fail reason", func() error { _, err := broker.Fail(ctx, tenant, "s", "e", ""); return err }, ErrBrokerInvalid},
		{"zero dead-letter limit", func() error { _, err := broker.DeadLetters(ctx, tenant, "s", "p", 0); return err }, ErrBrokerInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.apply(); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := NewMemoryBroker(BrokerPolicy{}); !errors.Is(err, ErrBrokerPolicy) {
		t.Fatalf("empty policy error = %v", err)
	}
	if _, err := NewPostgresBroker(nil, brokerPolicy()); !errors.Is(err, ErrBrokerInvalid) {
		t.Fatalf("nil store error = %v", err)
	}
	if _, err := NewPostgresBroker(errBrokerStore{errors.New("boom")}, BrokerPolicy{}); !errors.Is(err, ErrBrokerPolicy) {
		t.Fatalf("postgres empty policy error = %v", err)
	}
	// Polling beyond the watermark is out of range, not empty.
	if _, err := broker.Publish(ctx, good); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Poll(ctx, tenant, "c", "s", "p", 2, 1); !errors.Is(err, ErrBrokerOffset) {
		t.Fatalf("beyond-watermark poll error = %v", err)
	}
	// A forged record never verifies.
	forged, err := broker.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "s", Partition: "q", EffectIdentity: "forged", Payload: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	forged.Payload = []byte("rewritten")
	if err := VerifyRecord(forged); !errors.Is(err, ErrBrokerInvalid) {
		t.Fatalf("forged record verified: %v", err)
	}
}

func TestTodo_EVENT_005_Recovery(t *testing.T) {
	ctx := context.Background()
	broker := memoryBroker(t)
	tenant := uuid.New()
	for _, identity := range []string{"r-1", "r-2", "r-3"} {
		if _, err := broker.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "ledger", Partition: "p", EffectIdentity: identity, Payload: []byte(identity)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := broker.CommitCheckpoint(ctx, ConsumerCheckpoint{Tenant: tenant, Consumer: "projection", Stream: "ledger", Partition: "p", Offset: 2}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := broker.Fail(ctx, tenant, "ledger", "r-3", "poison"); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := broker.Snapshot()
	restarted, err := ResumeBroker(snapshot, brokerPolicy())
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	// Offsets, checkpoints, retries and dead letters resume exactly.
	page, err := restarted.Poll(ctx, tenant, "projection", "ledger", "p", 2, 10)
	if err != nil || len(page) != 1 || page[0].EffectIdentity != "r-3" || !page[0].DLQ {
		t.Fatalf("resumed poll = %+v err=%v", page, err)
	}
	position, found, err := restarted.ReadCheckpoint(ctx, tenant, "projection", "ledger", "p")
	if err != nil || !found || position.Offset != 2 {
		t.Fatalf("resumed checkpoint = %+v found=%v err=%v", position, found, err)
	}
	letters, err := restarted.DeadLetters(ctx, tenant, "ledger", "p", 10)
	if err != nil || len(letters) != 1 || letters[0].Attempts != 3 {
		t.Fatalf("resumed dead letters = %+v err=%v", letters, err)
	}
	// Post-restart behavior is identical: redeliveries converge and the
	// checkpoint cursor still refuses rewinds.
	redelivery, err := restarted.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "ledger", Partition: "p", EffectIdentity: "r-1", Payload: []byte("changed")})
	if err != nil || !redelivery.Duplicate || !bytes.Equal(redelivery.Payload, []byte("r-1")) {
		t.Fatalf("post-restart redelivery diverged: %+v err=%v", redelivery, err)
	}
	if _, err := restarted.CommitCheckpoint(ctx, ConsumerCheckpoint{Tenant: tenant, Consumer: "projection", Stream: "ledger", Partition: "p", Offset: 1}); !errors.Is(err, ErrCheckpointRewind) {
		t.Fatalf("post-restart rewind error = %v", err)
	}
	// A snapshot carrying a broken seal, a gapped offset chain or a
	// checkpoint beyond its watermark never resumes. Each case starts
	// from a fresh snapshot: snapshots share nothing with the live
	// broker, and cases must not share with each other either.
	broken := broker.Snapshot()
	broken.Records[0].Payload = []byte("rewritten")
	if _, err := ResumeBroker(broken, brokerPolicy()); !errors.Is(err, ErrBrokerInvalid) {
		t.Fatalf("broken seal resumed: %v", err)
	}
	fresh := broker.Snapshot()
	gapped := BrokerSnapshot{Records: append([]StreamRecord(nil), fresh.Records[0], fresh.Records[2]), Checkpoints: fresh.Checkpoints}
	if _, err := ResumeBroker(gapped, brokerPolicy()); !errors.Is(err, ErrBrokerOffset) {
		t.Fatalf("gapped chain resumed: %v", err)
	}
	ahead := broker.Snapshot()
	ahead.Checkpoints[0].Offset = 99
	if _, err := ResumeBroker(ahead, brokerPolicy()); !errors.Is(err, ErrBrokerOffset) {
		t.Fatalf("ahead checkpoint resumed: %v", err)
	}
	if _, err := ResumeBroker(snapshot, BrokerPolicy{}); !errors.Is(err, ErrBrokerPolicy) {
		t.Fatalf("empty policy resumed: %v", err)
	}
}

func TestTodo_EVENT_005_Security(t *testing.T) {
	ctx := context.Background()
	broker := memoryBroker(t)
	tenantA, tenantB := uuid.New(), uuid.New()
	if _, err := broker.Publish(ctx, PublishRequest{Tenant: tenantA, Stream: "payroll", Partition: "eu", EffectIdentity: "a-1", Payload: []byte("secret")}); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.CommitCheckpoint(ctx, ConsumerCheckpoint{Tenant: tenantA, Consumer: "worker", Stream: "payroll", Partition: "eu", Offset: 1}); err != nil {
		t.Fatal(err)
	}
	// Tenant B observes nothing of tenant A through any port leg.
	if page, err := broker.Poll(ctx, tenantB, "worker", "payroll", "eu", 0, 10); err != nil || len(page) != 0 {
		t.Fatalf("cross-tenant poll = %+v err=%v", page, err)
	}
	if replayed, err := broker.Replay(ctx, tenantB, "worker", "payroll", "eu", 0, 10); err != nil || len(replayed) != 0 {
		t.Fatalf("cross-tenant replay = %+v err=%v", replayed, err)
	}
	if _, found, err := broker.ReadCheckpoint(ctx, tenantB, "worker", "payroll", "eu"); err != nil || found {
		t.Fatalf("cross-tenant checkpoint found=%v err=%v", found, err)
	}
	if letters, err := broker.DeadLetters(ctx, tenantB, "payroll", "eu", 10); err != nil || len(letters) != 0 {
		t.Fatalf("cross-tenant dead letters = %+v err=%v", letters, err)
	}
	if _, err := broker.Fail(ctx, tenantB, "payroll", "a-1", "probe"); !errors.Is(err, ErrBrokerUnknown) {
		t.Fatalf("cross-tenant fail error = %v", err)
	}
	// Tenant B's own checkpoint namespace is independent.
	if _, err := broker.CommitCheckpoint(ctx, ConsumerCheckpoint{Tenant: tenantB, Consumer: "worker", Stream: "payroll", Partition: "eu", Offset: 0}); err != nil {
		t.Fatalf("tenant-B zero checkpoint: %v", err)
	}
	position, _, err := broker.ReadCheckpoint(ctx, tenantA, "worker", "payroll", "eu")
	if err != nil || position.Offset != 1 {
		t.Fatalf("tenant-A checkpoint moved: %+v err=%v", position, err)
	}
}

func TestTodo_EVENT_005_Conformance(t *testing.T) {
	t.Run("memory", func(t *testing.T) { conformBroker(t, memoryBroker(t)) })
	t.Run("postgres", func(t *testing.T) { conformBroker(t, postgresBroker(t)) })
}
