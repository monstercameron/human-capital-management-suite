package commit_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	ledgercommit "github.com/monstercameron/human-capital-management-suite/internal/data/ledger/commit"
)

// The ledger commit composition is the landed implementation of DATA-007's
// atomic event/projection/outbox contract.
func TestTodo_DATA_007(t *testing.T) {
	f := newFixture(t)
	var receipt ledgercommit.Receipt
	inTx(t, f.db, func(tx dbport.Tx) error {
		var err error
		receipt, err = ledgercommit.Commit(context.Background(), tx, ledger.New(), f.request(nil))
		return err
	})
	if receipt.Ledger.Sequence != 1 || !receipt.Projection.Applied || receipt.Outbox.OutboxID == uuid.Nil || receipt.Provenance.RecordID == uuid.Nil {
		t.Fatalf("commit receipt omitted a durable component: %+v", receipt)
	}
}

func TestTodo_DATA_007_Race(t *testing.T) {
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

func TestTodo_DATA_007_Mutation(t *testing.T) {
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
	if first.Ledger.EventID != second.Ledger.EventID || second.Projection.Applied || second.Outbox.OutboxID != uuid.Nil {
		t.Fatalf("idempotent replay changed durable identities: first=%+v second=%+v", first, second)
	}
}
