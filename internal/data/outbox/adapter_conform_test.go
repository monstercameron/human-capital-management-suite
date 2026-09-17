package outbox

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// conformBroker is the identical conformance suite every Broker backend
// passes: the Postgres backend and the OSS-semantics memory double must
// agree on ordering, dedupe, checkpoints, retry/DLQ, replay and tenant
// isolation, so no broker-native semantic can leak into domain code.
func conformBroker(t *testing.T, broker Broker) {
	t.Helper()
	ctx := context.Background()
	tenant := uuid.New()
	other := uuid.New()

	publish := func(partition, identity, body string) StreamRecord {
		t.Helper()
		record, err := broker.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "payroll", Partition: partition, EffectIdentity: identity, OrderingKey: "order-" + identity, Payload: []byte(body), SchemaRef: "hcm/pay/v1"})
		if err != nil {
			t.Fatal(err)
		}
		return record
	}

	// Per-partition offsets start at 1 and follow publish order, while
	// partitions stay independent.
	first := publish("eu", "eff-1", "one")
	second := publish("eu", "eff-2", "two")
	third := publish("us", "eff-3", "three")
	if first.Offset != 1 || second.Offset != 2 || third.Offset != 1 {
		t.Fatalf("offsets = %d, %d, %d; want partition-scoped 1, 2, 1", first.Offset, second.Offset, third.Offset)
	}
	if first.OrderingKey != "order-eff-1" || first.SchemaRef != "hcm/pay/v1" {
		t.Fatalf("record lost order key or schema: %+v", first)
	}

	// Republishing an identity converges: same offset and digest,
	// Duplicate set, no second record.
	redelivery, err := broker.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "payroll", Partition: "eu", EffectIdentity: "eff-1", Payload: []byte("changed")})
	if err != nil || !redelivery.Duplicate || redelivery.Offset != 1 || redelivery.Digest != first.Digest {
		t.Fatalf("redelivery diverged: %+v err=%v", redelivery, err)
	}
	if !bytes.Equal(redelivery.Payload, []byte("one")) {
		t.Fatalf("redelivery rewrote payload: %q", redelivery.Payload)
	}

	// Poll pages in partition order with identical bytes.
	page, err := broker.Poll(ctx, tenant, "payroll-worker", "payroll", "eu", 0, 1)
	if err != nil || len(page) != 1 || page[0].Offset != 1 || !bytes.Equal(page[0].Payload, []byte("one")) {
		t.Fatalf("poll page = %+v err=%v", page, err)
	}
	rest, err := broker.Poll(ctx, tenant, "payroll-worker", "payroll", "eu", 1, 10)
	if err != nil || len(rest) != 1 || rest[0].Offset != 2 {
		t.Fatalf("poll rest = %+v err=%v", rest, err)
	}

	// Checkpoints gate consumption monotonically.
	committed, err := broker.CommitCheckpoint(ctx, ConsumerCheckpoint{Tenant: tenant, Consumer: "payroll-worker", Stream: "payroll", Partition: "eu", Offset: 2})
	if err != nil || committed.Offset != 2 {
		t.Fatalf("checkpoint = %+v err=%v", committed, err)
	}
	read, found, err := broker.ReadCheckpoint(ctx, tenant, "payroll-worker", "payroll", "eu")
	if err != nil || !found || read.Offset != 2 {
		t.Fatalf("read checkpoint = %+v found=%v err=%v", read, found, err)
	}
	if _, err := broker.CommitCheckpoint(ctx, ConsumerCheckpoint{Tenant: tenant, Consumer: "payroll-worker", Stream: "payroll", Partition: "eu", Offset: 1}); !errors.Is(err, ErrCheckpointRewind) {
		t.Fatalf("checkpoint rewind error = %v", err)
	}
	if _, err := broker.CommitCheckpoint(ctx, ConsumerCheckpoint{Tenant: tenant, Consumer: "payroll-worker", Stream: "payroll", Partition: "eu", Offset: 9}); !errors.Is(err, ErrBrokerOffset) {
		t.Fatalf("checkpoint beyond watermark error = %v", err)
	}

	// Replay rereads history byte-identically without moving the checkpoint.
	replayed, err := broker.Replay(ctx, tenant, "payroll-worker", "payroll", "eu", 0, 10)
	if err != nil || len(replayed) != 2 || !bytes.Equal(replayed[0].Payload, []byte("one")) || !bytes.Equal(replayed[1].Payload, []byte("two")) {
		t.Fatalf("replay = %+v err=%v", replayed, err)
	}
	still, _, err := broker.ReadCheckpoint(ctx, tenant, "payroll-worker", "payroll", "eu")
	if err != nil || still.Offset != 2 {
		t.Fatalf("replay moved checkpoint: %+v err=%v", still, err)
	}

	// Bounded retries park the record, and parking sticks.
	for i := 0; i < 2; i++ {
		moved, err := broker.Fail(ctx, tenant, "payroll", "eff-2", "downstream 500")
		if err != nil || moved {
			t.Fatalf("fail %d moved=%v err=%v", i, moved, err)
		}
	}
	moved, err := broker.Fail(ctx, tenant, "payroll", "eff-2", "downstream 500")
	if err != nil || !moved {
		t.Fatalf("exhausting fail moved=%v err=%v", moved, err)
	}
	parked, err := broker.Fail(ctx, tenant, "payroll", "eff-2", "downstream 500")
	if err != nil || parked {
		t.Fatalf("parked fail moved=%v err=%v", parked, err)
	}
	letters, err := broker.DeadLetters(ctx, tenant, "payroll", "eu", 10)
	if err != nil || len(letters) != 1 || letters[0].EffectIdentity != "eff-2" || !letters[0].DLQ {
		t.Fatalf("dead letters = %+v err=%v", letters, err)
	}
	if err := VerifyRecord(letters[0]); err != nil {
		t.Fatalf("parked seal: %v", err)
	}

	// Tenants are fully isolated: records, checkpoints and failures.
	foreign, err := broker.Poll(ctx, other, "payroll-worker", "payroll", "eu", 0, 10)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("cross-tenant poll = %+v err=%v", foreign, err)
	}
	if _, found, err := broker.ReadCheckpoint(ctx, other, "payroll-worker", "payroll", "eu"); err != nil || found {
		t.Fatalf("cross-tenant checkpoint found=%v err=%v", found, err)
	}
	if _, err := broker.Fail(ctx, other, "payroll", "eff-1", "probe"); !errors.Is(err, ErrBrokerUnknown) {
		t.Fatalf("cross-tenant fail error = %v", err)
	}
	foreignFirst, err := broker.Publish(ctx, PublishRequest{Tenant: other, Stream: "payroll", Partition: "eu", EffectIdentity: "eff-1", Payload: []byte("foreign")})
	if err != nil || foreignFirst.Offset != 1 || foreignFirst.Duplicate {
		t.Fatalf("foreign stream did not start at 1: %+v err=%v", foreignFirst, err)
	}

	// Every record that crossed the port verifies.
	for _, record := range []StreamRecord{first, second, third, redelivery} {
		if err := VerifyRecord(record); err != nil {
			t.Fatalf("seal: %v", err)
		}
	}
}
