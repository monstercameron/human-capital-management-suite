package webhookreceiver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerreceipt"
	corewebhook "github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
	"github.com/monstercameron/human-capital-management-suite/internal/data/webhookreceipts"
	transportwebhook "github.com/monstercameron/human-capital-management-suite/internal/transport/webhook"
)

type fakeReceiptStore struct {
	duplicate bool
	parsed    providerreceipt.Parsed
	approval  corewebhook.ReplayApproval
}

func (s *fakeReceiptStore) Record(_ context.Context, _ providerreceipt.Parsed, _ time.Time) (bool, error) {
	return s.duplicate, nil
}

func (s *fakeReceiptStore) Replay(_ context.Context, _ string, approval corewebhook.ReplayApproval) (providerreceipt.Parsed, error) {
	s.approval = approval
	return s.parsed, nil
}

func TestSinkRejectsUnavailableComposition(t *testing.T) {
	if _, err := NewSink(nil, webhookreceipts.Scope{TenantID: uuid.New(), Provider: "payroll", EndpointID: "ep"}); err == nil {
		t.Fatal("NewSink accepted a nil database")
	}
	var sink *Sink
	if _, err := sink.Accept(context.Background(), providerreceipt.Parsed{}, time.Now()); err == nil {
		t.Fatal("nil sink accepted a receipt")
	}
	if _, err := sink.Replay(context.Background(), "event", corewebhook.ReplayApproval{Actor: "operator", Purpose: "recovery", Approved: true}); err == nil {
		t.Fatal("nil sink replayed a receipt")
	} else if errors.Is(err, corewebhook.ErrReplayNotAuthorized) {
		t.Fatalf("availability error was confused with authorization: %v", err)
	}
}

func TestSinkMapsDurableInboxDecisions(t *testing.T) {
	parsed := providerreceipt.Parsed{EventID: "event-1"}
	store := &fakeReceiptStore{parsed: parsed}
	sink := &Sink{store: store}
	decision, err := sink.Accept(context.Background(), parsed, time.Unix(10, 0))
	if err != nil || decision != transportwebhook.DispositionAccepted {
		t.Fatalf("new receipt decision=%q err=%v", decision, err)
	}
	store.duplicate = true
	decision, err = sink.Accept(context.Background(), parsed, time.Unix(11, 0))
	if err != nil || decision != transportwebhook.DispositionDuplicate {
		t.Fatalf("duplicate receipt decision=%q err=%v", decision, err)
	}
	approval := corewebhook.ReplayApproval{Actor: "operator-1", Purpose: "recovery", Approved: true}
	got, err := sink.Replay(context.Background(), "event-1", approval)
	if err != nil || got.EventID != parsed.EventID || store.approval != approval {
		t.Fatalf("replay=%+v approval=%+v err=%v", got, store.approval, err)
	}
}
