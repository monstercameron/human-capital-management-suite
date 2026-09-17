package outbox

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestTodo_EVENT_006_Race proves concurrent promotion evaluations are
// data-race free and converge: every goroutine reaches the identical
// promote decision for one signed dossier.
func TestTodo_EVENT_006_Race(t *testing.T) {
	base := event006ValidDossier()
	const workers = 32
	var wg sync.WaitGroup
	decisions := make([]PromotionDecision, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mine := base
			SignDossier(&mine, event006TestKey)
			decisions[i], errs[i] = EvaluatePromotion(mine, event006TestKey)
		}(i)
	}
	wg.Wait()
	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if !decisions[i].Promote || decisions[i].Transport != TransportBroker {
			t.Fatalf("worker %d diverged: %+v", i, decisions[i])
		}
		if decisions[i].BrokerKind != BrokerRedpandaLite {
			t.Fatalf("worker %d broker = %q", i, decisions[i].BrokerKind)
		}
	}
}

// TestTodo_EVENT_006_Integration reaches the real PostgreSQL store: the
// measured backlog is genuine polled rows, and the gate promotes on a
// breached envelope while staying on Postgres inside it.
func TestTodo_EVENT_006_Integration(t *testing.T) {
	ctx := context.Background()
	broker := postgresBroker(t)
	tenant := uuid.New()
	const records = 4
	for i := 0; i < records; i++ {
		identity := fmt.Sprintf("evt-006-%d", i)
		if _, err := broker.Publish(ctx, PublishRequest{
			Tenant: tenant, Stream: "billing", Partition: "eu",
			EffectIdentity: identity, OrderingKey: fmt.Sprintf("eu/%d", i),
			Payload: []byte(fmt.Sprintf("payload-%d", i)), SchemaRef: "hcm/billing/v1",
		}); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}
	page, err := broker.Poll(ctx, tenant, "promotion-meter", "billing", "eu", 0, 100)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(page) != records {
		t.Fatalf("measured backlog = %d, want %d genuine rows", len(page), records)
	}
	for i, record := range page {
		if err := VerifyRecord(record); err != nil {
			t.Fatalf("record %d seal: %v", i, err)
		}
		if !bytes.Equal(record.Payload, []byte(fmt.Sprintf("payload-%d", i))) {
			t.Fatalf("record %d payload = %q", i, record.Payload)
		}
	}

	envelope := OutboxEnvelope{MaxSustainedEPS: 100, MaxBacklog: 1000, MaxLag: time.Minute}
	measured := MeasuredLoad{SustainedEPS: 40, Backlog: int64(len(page)), P99Lag: 10 * time.Second}

	breached := event006ValidDossier()
	breached.Envelope = OutboxEnvelope{MaxSustainedEPS: 100, MaxBacklog: 2, MaxLag: time.Minute}
	breached.Load = measured
	breached = event006Resign(breached)
	promote, err := EvaluatePromotion(breached, event006TestKey)
	if err != nil || !promote.Promote || promote.Transport != TransportBroker {
		t.Fatalf("breached-on-real-backlog = %+v err=%v", promote, err)
	}

	calm := event006ValidDossier()
	calm.Envelope = envelope
	calm.Load = measured
	calm = event006Resign(calm)
	stay, err := EvaluatePromotion(calm, event006TestKey)
	if err != nil || stay.Promote || stay.Transport != TransportPostgres {
		t.Fatalf("inside-envelope-on-real-backlog = %+v err=%v", stay, err)
	}
}

// TestTodo_EVENT_006_Conformance proves the selected low-cost OSS option
// and Postgres agree: both backends pass the identical adapter suite and
// seal identical publishes byte-identically, so promotion cannot change
// ordering or dedupe results.
func TestTodo_EVENT_006_Conformance(t *testing.T) {
	conformBroker(t, memoryBroker(t))
	conformBroker(t, postgresBroker(t))

	ctx := context.Background()
	tenant := uuid.New()
	memory := memoryBroker(t)
	durable := postgresBroker(t)
	request := PublishRequest{
		Tenant: tenant, Stream: "warranty", Partition: "p",
		EffectIdentity: "parity-1", OrderingKey: "p/1",
		Payload: []byte("same-bytes"), SchemaRef: "hcm/warranty/v1",
	}
	fromMemory, err := memory.Publish(ctx, request)
	if err != nil {
		t.Fatalf("memory publish: %v", err)
	}
	fromDurable, err := durable.Publish(ctx, request)
	if err != nil {
		t.Fatalf("postgres publish: %v", err)
	}
	if fromMemory.Offset != fromDurable.Offset || fromMemory.Offset != 1 {
		t.Fatalf("offsets diverged: memory=%d postgres=%d", fromMemory.Offset, fromDurable.Offset)
	}
	if fromMemory.Digest != fromDurable.Digest {
		t.Fatalf("seals diverged: memory=%q postgres=%q", fromMemory.Digest, fromDurable.Digest)
	}
	if err := VerifyRecord(fromMemory); err != nil {
		t.Fatalf("memory seal: %v", err)
	}
	if err := VerifyRecord(fromDurable); err != nil {
		t.Fatalf("postgres seal: %v", err)
	}
}

// TestTodo_EVENT_006_Recovery proves restore parity across a broker
// restart: records and checkpoints survive on the durable handle, and
// the promotion decision evaluates identically before and after.
func TestTodo_EVENT_006_Recovery(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	before, err := NewPostgresBroker(db.Conn, brokerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if err := before.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	const records = 3
	for i := 0; i < records; i++ {
		if _, err := before.Publish(ctx, PublishRequest{
			Tenant: tenant, Stream: "shipping", Partition: "eu",
			EffectIdentity: fmt.Sprintf("evt-006-restore-%d", i),
			Payload:        []byte(fmt.Sprintf("parcel-%d", i)),
		}); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}
	if _, err := before.CommitCheckpoint(ctx, ConsumerCheckpoint{
		Tenant: tenant, Consumer: "worker", Stream: "shipping", Partition: "eu", Offset: records,
	}); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	dossier := event006ValidDossier()
	preRestart, err := EvaluatePromotion(dossier, event006TestKey)
	if err != nil || !preRestart.Promote {
		t.Fatalf("pre-restart = %+v err=%v", preRestart, err)
	}

	// Restart: a fresh handle over the same database.
	after, err := NewPostgresBroker(db.Conn, brokerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := after.Poll(ctx, tenant, "worker", "shipping", "eu", 0, 10)
	if err != nil || len(restored) != records {
		t.Fatalf("restored poll = %d records err=%v", len(restored), err)
	}
	for i, record := range restored {
		if !bytes.Equal(record.Payload, []byte(fmt.Sprintf("parcel-%d", i))) {
			t.Fatalf("restored record %d = %q", i, record.Payload)
		}
		if err := VerifyRecord(record); err != nil {
			t.Fatalf("restored seal %d: %v", i, err)
		}
	}
	position, found, err := after.ReadCheckpoint(ctx, tenant, "worker", "shipping", "eu")
	if err != nil || !found || position.Offset != records {
		t.Fatalf("restored checkpoint = %+v found=%v err=%v", position, found, err)
	}
	postRestart, err := EvaluatePromotion(dossier, event006TestKey)
	if err != nil || postRestart.Transport != preRestart.Transport ||
		postRestart.Promote != preRestart.Promote ||
		postRestart.BrokerKind != preRestart.BrokerKind ||
		!slices.Equal(postRestart.BreachReasons, preRestart.BreachReasons) {
		t.Fatalf("post-restart = %+v pre=%+v err=%v", postRestart, preRestart, err)
	}
}

// BenchmarkTodo_EVENT_006 reports the promotion-gate decision cost for
// one fully evidenced dossier.
func BenchmarkTodo_EVENT_006(b *testing.B) {
	dossier := event006ValidDossier()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decision, err := EvaluatePromotion(dossier, event006TestKey)
		if err != nil {
			b.Fatal(err)
		}
		if !decision.Promote {
			b.Fatal("fully evidenced breached dossier stopped promoting")
		}
	}
}
