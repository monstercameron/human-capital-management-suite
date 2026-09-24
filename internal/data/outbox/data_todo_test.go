package outbox_test

import (
	"context"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
)

func TestTodo_DATA_008(t *testing.T) {
	f := newNext004Fixture(t)
	receipt := f.commitAt(t, "data-008", 0)
	if receipt.Ledger.Sequence != 1 || !receipt.Projection.Applied || receipt.Outbox.Status != outbox.StatusPending {
		t.Fatalf("append receipt = %+v", receipt)
	}
	cp, err := projection.Read(context.Background(), f.db.Conn, f.tenant, next004Projection, next004StreamKey)
	if err != nil || cp.LastAppliedSequence != 1 {
		t.Fatalf("checkpoint = %+v, %v", cp, err)
	}
}

func TestTodo_DATA_008_Race(t *testing.T) {
	f := newNext004Fixture(t)
	f.commitAt(t, "race-seed", 0)
	start := make(chan struct{})
	claimed := make([][]outbox.Record, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range claimed {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			claimed[i], errs[i] = outbox.NewConsumer(f.db.Conn, outbox.WithBatchSize(10)).Poll(context.Background(), f.tenant)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("consumer %d: %v", i, err)
		}
	}
	if len(claimed[0])+len(claimed[1]) != 1 {
		t.Fatalf("consumers claimed %d copies of one pending message: %d and %d", len(claimed[0])+len(claimed[1]), len(claimed[0]), len(claimed[1]))
	}
}

func TestTodo_DATA_008_Fault(t *testing.T) {
	f := newNext004Fixture(t)
	first := f.commitAt(t, "fault-check", 0)
	replay := f.commitAt(t, "fault-check", 0)
	if first.Ledger.EventID != replay.Ledger.EventID || replay.Projection.Applied || replay.Outbox.OutboxID != first.Outbox.OutboxID {
		t.Fatalf("replay did not recover original append identities: %+v / %+v", first, replay)
	}
}

func TestTodo_DATA_008_Mutation(t *testing.T) {
	f := newNext004Fixture(t)
	first := f.commitAt(t, "mutation-check", 0)
	replay := f.commitAt(t, "mutation-check", 0)
	if replay.Ledger.EventID != first.Ledger.EventID || replay.Ledger.Sequence != 1 {
		t.Fatalf("idempotent retry changed the committed event: %+v", replay)
	}
	var count int
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id=$1`, f.tenant).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("mutation attempt left %d ledger events; first event was %s", count, first.Ledger.EventID)
	}
}
