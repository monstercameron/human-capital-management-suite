package subscription

import (
	"errors"
	"sync"
	"testing"
)

func completenessExpectation() CompletenessExpectation {
	return CompletenessExpectation{SubscriptionID: "sub-delivery", Tenant: "tenant-a", OrderingKey: "worker:1", FromSequence: 1, ThroughSequence: 3}
}

func independentDeliveryRequest(sequence uint64) DeliveryRequest {
	request := deliveryRequest(sequence)
	request.OrderingMode = OrderingIndependent
	return request
}

func TestTodo_SUB_008(t *testing.T) {
	provider := &deliveryProvider{}
	journal := NewDeliveryJournal()
	for _, sequence := range []uint64{1, 2, 4} {
		if _, err := journal.DeliverNow(provider, independentDeliveryRequest(sequence)); err != nil {
			t.Fatalf("deliver %d: %v", sequence, err)
		}
	}
	report, err := ReconcileDelivery(journal, completenessExpectation())
	if err != nil {
		t.Fatal(err)
	}
	if report.Complete() {
		t.Fatal("provider acceptance of sequence 4 satisfied the range while 3 is missing")
	}
	if len(report.Acked) != 2 || len(report.Gaps) != 1 || report.Gaps[0] != 3 {
		t.Fatalf("report=%+v", report)
	}
	if len(report.Unacked) != 0 || len(report.Unknown) != 0 {
		t.Fatalf("report=%+v", report)
	}
	if repair := report.NeedsRepair(); len(repair) != 1 || repair[0] != 3 {
		t.Fatalf("repair=%v", repair)
	}
	if _, err := journal.DeliverNow(provider, independentDeliveryRequest(3)); err != nil {
		t.Fatal(err)
	}
	healed, err := ReconcileDelivery(journal, completenessExpectation())
	if err != nil {
		t.Fatal(err)
	}
	if !healed.Complete() || len(healed.Acked) != 3 {
		t.Fatalf("healed=%+v", healed)
	}
	if healed.Explain() == "" {
		t.Fatal("empty explanation")
	}
}

func TestTodo_SUB_008_Race(t *testing.T) {
	provider := &deliveryProvider{}
	journal := NewDeliveryJournal()
	var wg sync.WaitGroup
	for _, sequence := range []uint64{1, 2, 3, 4, 5} {
		wg.Add(1)
		go func(seq uint64) {
			defer wg.Done()
			if _, err := journal.DeliverNow(provider, independentDeliveryRequest(seq)); err != nil {
				t.Errorf("deliver %d: %v", seq, err)
			}
		}(sequence)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 25; i++ {
			report, err := ReconcileDelivery(journal, CompletenessExpectation{SubscriptionID: "sub-delivery", Tenant: "tenant-a", OrderingKey: "worker:1", FromSequence: 1, ThroughSequence: 5})
			if err != nil {
				t.Errorf("reconcile: %v", err)
				return
			}
			if len(report.Acked)+len(report.Gaps)+len(report.Unacked)+len(report.Unknown) != 5 {
				t.Errorf("inconsistent snapshot=%+v", report)
				return
			}
		}
	}()
	wg.Wait()
	final, err := ReconcileDelivery(journal, CompletenessExpectation{SubscriptionID: "sub-delivery", Tenant: "tenant-a", OrderingKey: "worker:1", FromSequence: 1, ThroughSequence: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !final.Complete() {
		t.Fatalf("final=%+v", final)
	}
}

func TestTodo_SUB_008_Integration(t *testing.T) {
	provider := &deliveryProvider{}
	journal := NewDeliveryJournal()
	if _, err := journal.DeliverNow(provider, deliveryRequest(2)); !errors.Is(err, ErrDeliveryGap) {
		t.Fatalf("gap error=%v", err)
	}
	report, err := ReconcileDelivery(journal, completenessExpectation())
	if err != nil {
		t.Fatal(err)
	}
	if report.Complete() || len(report.Gaps) != 2 || len(report.Unacked) != 1 {
		t.Fatalf("gap report=%+v", report)
	}
	for _, sequence := range []uint64{1, 2, 3} {
		if _, err := journal.DeliverNow(provider, deliveryRequest(sequence)); err != nil {
			t.Fatalf("deliver %d: %v", sequence, err)
		}
	}
	healed, err := ReconcileDelivery(journal, completenessExpectation())
	if err != nil {
		t.Fatal(err)
	}
	if !healed.Complete() {
		t.Fatalf("healed=%+v", healed)
	}
	if len(provider.attempts) != 3 {
		t.Fatalf("provider attempts=%d", len(provider.attempts))
	}
}

func TestTodo_SUB_008_Fault(t *testing.T) {
	journal := NewDeliveryJournal()
	if _, err := journal.DeliverNow(&deliveryProvider{}, deliveryRequest(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.DeliverNow(&deliveryProvider{err: ambiguousDeliveryError{}}, independentDeliveryRequest(2)); !errors.Is(err, ErrDeliveryAmbiguous) {
		t.Fatalf("ambiguous error=%v", err)
	}
	if _, err := journal.DeliverNow(&deliveryProvider{err: errors.New("connection refused")}, independentDeliveryRequest(3)); !errors.Is(err, ErrDeliveryProvider) {
		t.Fatalf("provider error=%v", err)
	}
	report, err := ReconcileDelivery(journal, completenessExpectation())
	if err != nil {
		t.Fatal(err)
	}
	if report.Complete() {
		t.Fatalf("report=%+v", report)
	}
	if len(report.Unknown) != 1 || report.Unknown[0] != 2 {
		t.Fatalf("unknown=%+v", report)
	}
	if len(report.Unacked) != 1 || report.Unacked[0] != 3 {
		t.Fatalf("unacked=%+v", report)
	}
	for _, seq := range report.NeedsRepair() {
		if seq == 2 {
			t.Fatal("ambiguous sequence 2 must never be blind-retried")
		}
	}
	if _, err := ReconcileDelivery(nil, completenessExpectation()); err == nil {
		t.Fatal("nil journal accepted")
	}
	bad := completenessExpectation()
	bad.ThroughSequence = 0
	if _, err := ReconcileDelivery(journal, bad); err == nil {
		t.Fatal("inverted range accepted")
	}
}
