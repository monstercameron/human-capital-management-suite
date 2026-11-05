package procurement

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/delivery"
)

func buy003Context() BuyContext {
	return BuyContext{TenantID: "tenant-a", CompanyID: "company-a", SiteID: "site-1", ActorID: "buyer-1", Origin: "human"}
}

func buy003Supplier() SupplierContext {
	return SupplierContext{TenantID: "tenant-a", CompanyID: "company-a", SupplierID: "supplier-acme", ActorID: "supplier-user-1"}
}

func buy003Input() BuyPOInput {
	return BuyPOInput{
		SiteID:   "site-1",
		Supplier: BuySupplierSnapshot{SupplierID: "supplier-acme", Name: "Acme Supply", ContactRef: "contact-acme-9", BankRef: "bank-acme-1"},
		Lines: []BuyLineInput{
			{ItemSKU: "WIDGET-12", Description: "Widget 12-pack", UOM: "each", Qty: "10", UnitPrice: "25.00", TaxRate: "0.20", DeliveryDate: "2026-11-15"},
			{ItemSKU: "SEAL-9", Description: "Seal kit", UOM: "each", Qty: "4", UnitPrice: "12.50", TaxRate: "", DeliveryDate: "2026-11-15"},
		},
		Currency: "USD", CurrencyScale: 2,
		DeliverySite: "site-1", DeliveryRecipient: "receiving-dock-1",
		SourceRequestRef: "req-77", SpendAuthorizationRef: "spend-auth-1",
	}
}

type buy003Store struct {
	mu       sync.Mutex
	attempts map[string]int
}

func (s *buy003Store) Claim(_ context.Context, envelope delivery.Envelope, _ int) (delivery.Claim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attempts == nil {
		s.attempts = make(map[string]int)
	}
	s.attempts[envelope.IdempotencyKey]++
	n := s.attempts[envelope.IdempotencyKey]
	return delivery.Claim{AttemptID: envelope.IdempotencyKey + "#attempt-" + string(rune(48+n)), Attempt: n}, nil
}

func (s *buy003Store) Observe(context.Context, delivery.Envelope, delivery.Claim, delivery.Observation) error {
	return nil
}

type buy003Transport struct {
	mu        sync.Mutex
	mode      string
	calls     int
	envelopes []delivery.Envelope
}

func (t *buy003Transport) Deliver(_ context.Context, envelope delivery.Envelope) (delivery.ProviderResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	t.envelopes = append(t.envelopes, envelope)
	switch t.mode {
	case "ambiguous":
		return delivery.ProviderResult{}, &delivery.ProviderError{Err: errors.New("response lost"), Ambiguous: true}
	case "fail":
		return delivery.ProviderResult{}, errors.New("channel refused")
	default:
		return delivery.ProviderResult{ProviderRef: "prov-1", ObservationRef: "obs-1", Code: "ACCEPTED"}, nil
	}
}

func buy003Runner(transport *buy003Transport, now time.Time) delivery.Runner {
	return delivery.Runner{Store: &buy003Store{}, Transport: transport, Clock: func() time.Time { return now }, MaxAttempts: 1}
}

// TestTodo_SMB_BUY_003 proves the purchase order lifecycle: version-bound
// approval, dispatch to a verified recipient through durable delivery,
// unknown outcomes staying unknown until evidence, amendments fenced on
// receipts, duplicate-safe cancellation of the remainder, and
// least-privilege supplier acknowledgement and proposals.
func TestTodo_SMB_BUY_003(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	op := buy003Context()
	transport := &buy003Transport{}
	coordinator := NewBuyCoordinator(buy003Runner(transport, now))

	po, err := coordinator.CreatePurchaseOrder(ctx, op, buy003Input(), "po-create")
	if err != nil {
		t.Fatal(err)
	}
	if po.Status != BuyDraft || po.Revision != 1 || po.Total != "350.00" {
		t.Fatalf("created PO = %+v, want DRAFT rev 1 total 350.00", po)
	}
	if _, err := coordinator.DispatchPurchaseOrder(ctx, op, po.POID, DispatchInput{Recipient: "contact-acme-9", Channel: delivery.ChannelEmail, VerificationEvidence: "verified-1", At: now}, "dispatch-early"); err == nil {
		t.Fatal("dispatch of unapproved PO succeeded, want refusal")
	}
	if _, err := coordinator.SubmitPurchaseOrder(ctx, op, po.POID, "submit-1"); err != nil {
		t.Fatal(err)
	}
	approved, err := coordinator.ApprovePurchaseOrder(ctx, op, po.POID, "buyer-2", "budget-checked", "approve-1")
	if err != nil || approved.Status != BuyApproved {
		t.Fatalf("approval = %+v err=%v, want APPROVED", approved, err)
	}

	dispatch, err := coordinator.DispatchPurchaseOrder(ctx, op, po.POID, DispatchInput{Recipient: "contact-acme-9", Channel: delivery.ChannelEmail, VerificationEvidence: "verified-1", At: now}, "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.State != BuyDispatchAccepted || len(dispatch.Attempts) != 1 {
		t.Fatalf("dispatch = %+v, want one accepted attempt", dispatch)
	}
	sent, err := coordinator.POLookup(op, po.POID)
	if err != nil || sent.Status != BuyOpen {
		t.Fatalf("dispatched PO = %+v err=%v, want OPEN", sent, err)
	}
	if len(transport.envelopes) != 1 {
		t.Fatalf("transport saw %d envelopes, want 1", len(transport.envelopes))
	}
	envelopeJSON, _ := json.Marshal(transport.envelopes[0])
	if strings.Contains(strings.ToLower(string(envelopeJSON)), "bank") {
		t.Fatalf("dispatch envelope carries bank destination: %s", envelopeJSON)
	}

	supplier := buy003Supplier()
	ack, err := coordinator.AcknowledgePurchaseOrder(ctx, supplier, po.POID, 1, "confirmed", "ack-ev-1", "ack-1")
	if err != nil || ack.Status != AckAcknowledged {
		t.Fatalf("supplier ack = %+v err=%v, want ACKNOWLEDGED", ack, err)
	}

	if _, err := coordinator.ApplyReceiptLine(ctx, op, po.POID, 1, "6", "recv-1", "receipt-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ApplyInvoiceLine(ctx, op, po.POID, 1, "6", "inv-1", "invoice-1"); err != nil {
		t.Fatal(err)
	}

	low, err := coordinator.ProposePOAmendment(ctx, op, po.POID, []BuyLineInput{
		{ItemSKU: "WIDGET-12", Description: "Widget 12-pack", UOM: "each", Qty: "4", UnitPrice: "25.00", TaxRate: "0.20", DeliveryDate: "2026-11-15"},
		{ItemSKU: "SEAL-9", Description: "Seal kit", UOM: "each", Qty: "4", UnitPrice: "12.50", TaxRate: "", DeliveryDate: "2026-11-15"},
	}, "reduce scope", "cut line 1", "amend-low", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.CommitPOAmendment(ctx, op, low.AmendmentID, "buyer-2", "ok-low", "commit-low"); !errors.Is(err, ErrBuyAmend) {
		t.Fatalf("amend below receipts err=%v, want refusal without return path", err)
	}
	withReturn, err := coordinator.ProposePOAmendment(ctx, op, po.POID, []BuyLineInput{
		{ItemSKU: "WIDGET-12", Description: "Widget 12-pack", UOM: "each", Qty: "4", UnitPrice: "27.00", TaxRate: "0.20", DeliveryDate: "2026-11-20"},
		{ItemSKU: "SEAL-9", Description: "Seal kit", UOM: "each", Qty: "4", UnitPrice: "12.50", TaxRate: "", DeliveryDate: "2026-11-15"},
	}, "reduce scope", "cut line 1 with return", "amend-ok", "return-1")
	if err != nil {
		t.Fatal(err)
	}
	revised, err := coordinator.CommitPOAmendment(ctx, op, withReturn.AmendmentID, "buyer-2", "ok-return", "commit-ok")
	if err != nil {
		t.Fatal(err)
	}
	if revised.Revision != 2 || revised.Status != BuyNeedsApproval {
		t.Fatalf("amended PO = %+v, want rev 2 needing approval", revised)
	}
	history, err := coordinator.PORevisions(op, po.POID)
	if err != nil || len(history) != 2 || history[0].Total != "350.00" {
		t.Fatalf("revision history = %+v err=%v, want rev 1 total preserved", history, err)
	}
	lines, err := coordinator.POLookup(op, po.POID)
	if err != nil {
		t.Fatal(err)
	}
	if lines.Lines[0].InvoicedUnitPrice != "25.00" || lines.Lines[0].InvoicedQty != "6" {
		t.Fatalf("invoiced snapshot = %+v, want old price pinned", lines.Lines[0])
	}
	if _, err := coordinator.ApprovePurchaseOrder(ctx, op, po.POID, "buyer-2", "budget-checked-2", "approve-2"); err != nil {
		t.Fatal(err)
	}

	stale, err := coordinator.AcknowledgePurchaseOrder(ctx, supplier, po.POID, 1, "late confirm", "ack-ev-2", "ack-2")
	if err != nil || stale.Status != AckSuperseded {
		t.Fatalf("outdated ack = %+v err=%v, want SUPERSEDED_ACK evidence", stale, err)
	}
	flagged, err := coordinator.POLookup(op, po.POID)
	if err != nil || !flagged.NeedsReview {
		t.Fatalf("PO after outdated ack = %+v err=%v, want NeedsReview", flagged, err)
	}
	otherSupplier := supplier
	otherSupplier.SupplierID = "supplier-rival"
	if _, err := coordinator.AcknowledgePurchaseOrder(ctx, otherSupplier, po.POID, 2, "hijack", "ack-ev-3", "ack-3"); !errors.Is(err, ErrBuySupplier) {
		t.Fatalf("rival ack err=%v, want supplier refusal", err)
	}

	proposal, err := coordinator.ProposeDeliveryChange(ctx, supplier, po.POID, 2, 1, "2026-11-25", "", "truck delay", "prop-1")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Status != ProposalPending {
		t.Fatalf("proposal = %+v, want PENDING", proposal)
	}
	unchanged, err := coordinator.POLookup(op, po.POID)
	if err != nil || unchanged.Revision != 2 || unchanged.Lines[0].DeliveryDate != "2026-11-20" {
		t.Fatalf("proposal rewrote committed terms: %+v err=%v", unchanged, err)
	}
	adopted, err := coordinator.AdoptDeliveryChange(ctx, op, proposal.ProposalID, "buyer-2", "ok-date", "adopt-1")
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Revision != 3 {
		t.Fatalf("adopted PO rev = %d, want 3", adopted.Revision)
	}
	if retry, err := coordinator.AdoptDeliveryChange(ctx, op, proposal.ProposalID, "buyer-2", "ok-date", "adopt-1"); err != nil || retry.Revision != 3 {
		t.Fatalf("adopt retry = %+v err=%v, want same outcome", retry, err)
	}

	cancel, err := coordinator.RequestPOCancellation(ctx, op, po.POID, "project descoped", "cancel-1")
	if err != nil {
		t.Fatal(err)
	}
	if cancel.Status != CancelPending || cancel.Lines[0].CancelledQty != "0" {
		t.Fatalf("cancellation = %+v, want pending with line 1 fully received", cancel)
	}
	duplicate, err := coordinator.RequestPOCancellation(ctx, op, po.POID, "project descoped", "cancel-2")
	if err != nil || duplicate.CancellationID != cancel.CancellationID {
		t.Fatalf("duplicate cancellation = %+v err=%v, want one outcome", duplicate, err)
	}
	resolved, err := coordinator.ResolvePOCancellation(ctx, op, cancel.CancellationID, CancelAccept, "supplier-acme", "supplier-agrees", "resolve-1")
	if err != nil || resolved.Status != CancelEffective {
		t.Fatalf("cancel resolution = %+v err=%v, want EFFECTIVE", resolved, err)
	}
	closed, err := coordinator.POLookup(op, po.POID)
	if err != nil || closed.Status != BuyCancelled {
		t.Fatalf("cancelled PO = %+v err=%v", closed, err)
	}
	if closed.Lines[0].ReceivedQty != "6" || closed.Lines[0].InvoicedQty != "6" {
		t.Fatalf("cancellation moved received/payable: %+v", closed.Lines[0])
	}
	if _, err := coordinator.ApplyReceiptLine(ctx, op, po.POID, 2, "1", "recv-late", "receipt-late"); err == nil {
		t.Fatal("receipt after cancellation succeeded, want refusal")
	}
}

// TestTodo_SMB_BUY_003_Integration proves unknown dispatch stays unknown
// until evidence, resends link explicitly with reconciliation, dispatches
// are idempotent per business key, supplier views leak no line detail, and
// rejected cancellations leave the order untouched.
func TestTodo_SMB_BUY_003_Integration(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	op := buy003Context()

	ambiguous := &buy003Transport{mode: "ambiguous"}
	shadow := NewBuyCoordinator(buy003Runner(ambiguous, now))
	po, err := shadow.CreatePurchaseOrder(ctx, op, buy003Input(), "po-shadow")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := shadow.SubmitPurchaseOrder(ctx, op, po.POID, "submit-shadow"); err != nil {
		t.Fatal(err)
	}
	if _, err := shadow.ApprovePurchaseOrder(ctx, op, po.POID, "buyer-2", "ok", "approve-shadow"); err != nil {
		t.Fatal(err)
	}
	lost, err := shadow.DispatchPurchaseOrder(ctx, op, po.POID, DispatchInput{Recipient: "contact-acme-9", Channel: delivery.ChannelEmail, VerificationEvidence: "verified-1", At: now}, "dispatch-shadow")
	if err != nil {
		t.Fatal(err)
	}
	if lost.State != BuyDispatchUnknown {
		t.Fatalf("lost-response dispatch = %+v, want UNKNOWN", lost)
	}
	first, err := shadow.GetDispatchOutcome(op, lost.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := shadow.SupplierDispatchView(buy003Supplier(), po.POID)
	if err != nil {
		t.Fatal(err)
	}
	if first.OperationID != lost.OperationID || len(second) != 1 || second[0].OperationID != lost.OperationID || second[0].State != BuyDispatchUnknown {
		t.Fatalf("retry lookup = %+v supplier view = %+v, want one UNKNOWN operation", first, second)
	}
	if _, err := shadow.ResendDispatch(ctx, op, lost.OperationID, false, "", "resend-raw"); !errors.Is(err, ErrBuyDispatch) {
		t.Fatalf("unreconciled resend err=%v, want reconciliation refusal", err)
	}
	resent, err := shadow.ResendDispatch(ctx, op, lost.OperationID, true, "supplier confirms nothing arrived", "resend-1")
	if err != nil {
		t.Fatal(err)
	}
	if resent.OperationID != lost.OperationID || len(resent.Attempts) != 2 {
		t.Fatalf("resend = %+v, want same operation with two linked attempts", resent)
	}
	delivered, err := shadow.RecordDispatchEvidence(ctx, op, lost.OperationID, true, "prov-9", "portal-shows-delivered", "evidence-1")
	if err != nil || delivered.State != BuyDispatchDelivered {
		t.Fatalf("evidence recording = %+v err=%v, want DELIVERED", delivered, err)
	}
	if _, err := shadow.ResendDispatch(ctx, op, lost.OperationID, true, "again", "resend-2"); !errors.Is(err, ErrBuyDispatch) {
		t.Fatalf("resend after delivery err=%v, want refusal", err)
	}

	failing := &buy003Transport{mode: "fail"}
	retrying := NewBuyCoordinator(buy003Runner(failing, now))
	po2, err := retrying.CreatePurchaseOrder(ctx, op, buy003Input(), "po-retry")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retrying.SubmitPurchaseOrder(ctx, op, po2.POID, "submit-retry"); err != nil {
		t.Fatal(err)
	}
	if _, err := retrying.ApprovePurchaseOrder(ctx, op, po2.POID, "buyer-2", "ok", "approve-retry"); err != nil {
		t.Fatal(err)
	}
	failed, err := retrying.DispatchPurchaseOrder(ctx, op, po2.POID, DispatchInput{Recipient: "contact-acme-9", Channel: delivery.ChannelEmail, VerificationEvidence: "verified-1", At: now}, "dispatch-retry")
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != BuyDispatchFailed {
		t.Fatalf("refused dispatch = %+v, want FAILED", failed)
	}
	failing.mode = ""
	healed, err := retrying.ResendDispatch(ctx, op, failed.OperationID, false, "", "resend-healed")
	if err != nil || healed.State != BuyDispatchAccepted || len(healed.Attempts) != 2 {
		t.Fatalf("healed resend = %+v err=%v, want accepted second attempt", healed, err)
	}

	transport := &buy003Transport{}
	coordinator := NewBuyCoordinator(buy003Runner(transport, now))
	po3, err := coordinator.CreatePurchaseOrder(ctx, op, buy003Input(), "po-idem")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.SubmitPurchaseOrder(ctx, op, po3.POID, "submit-idem"); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ApprovePurchaseOrder(ctx, op, po3.POID, "buyer-2", "ok", "approve-idem"); err != nil {
		t.Fatal(err)
	}
	input := DispatchInput{Recipient: "contact-acme-9", Channel: delivery.ChannelEmail, VerificationEvidence: "verified-1", At: now}
	one, err := coordinator.DispatchPurchaseOrder(ctx, op, po3.POID, input, "dispatch-idem")
	if err != nil {
		t.Fatal(err)
	}
	two, err := coordinator.DispatchPurchaseOrder(ctx, op, po3.POID, input, "dispatch-idem")
	if err != nil || two.OperationID != one.OperationID {
		t.Fatalf("dispatch replay = %+v err=%v, want same operation", two, err)
	}
	if transport.calls != 1 {
		t.Fatalf("transport called %d times, want once per business key", transport.calls)
	}
	changed := input
	changed.Channel = delivery.ChannelSMS
	other, err := coordinator.DispatchPurchaseOrder(ctx, op, po3.POID, changed, "dispatch-other-channel")
	if err != nil || other.OperationID == one.OperationID {
		t.Fatalf("other-channel dispatch = %+v err=%v, want a distinct operation", other, err)
	}

	supplier := buy003Supplier()
	commitment, err := coordinator.GetSupplierCommitment(supplier, po3.POID)
	if err != nil {
		t.Fatal(err)
	}
	commitmentJSON, _ := json.Marshal(commitment)
	if strings.Contains(string(commitmentJSON), "Widget 12-pack") || strings.Contains(string(commitmentJSON), "Seal kit") {
		t.Fatalf("supplier commitment enumerates lines: %s", commitmentJSON)
	}
	ack, err := coordinator.AcknowledgePurchaseOrder(ctx, supplier, po3.POID, 1, "ok", "ev", "ack-enum")
	if err != nil {
		t.Fatal(err)
	}
	ackJSON, _ := json.Marshal(ack)
	if strings.Contains(string(ackJSON), "Widget 12-pack") {
		t.Fatalf("ack response enumerates lines: %s", ackJSON)
	}

	rejectPO, err := coordinator.CreatePurchaseOrder(ctx, op, buy003Input(), "po-reject")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.SubmitPurchaseOrder(ctx, op, rejectPO.POID, "submit-reject"); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ApprovePurchaseOrder(ctx, op, rejectPO.POID, "buyer-2", "ok", "approve-reject"); err != nil {
		t.Fatal(err)
	}
	cancel, err := coordinator.RequestPOCancellation(ctx, op, rejectPO.POID, "maybe", "cancel-reject")
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := coordinator.ResolvePOCancellation(ctx, op, cancel.CancellationID, CancelReject, "supplier-acme", "already shipped", "resolve-reject")
	if err != nil || rejected.Status != CancelRejected {
		t.Fatalf("cancel rejection = %+v err=%v", rejected, err)
	}
	stillOpen, err := coordinator.POLookup(op, rejectPO.POID)
	if err != nil || stillOpen.Status == BuyCancelled {
		t.Fatalf("rejected cancellation closed the PO: %+v err=%v", stillOpen, err)
	}
	fresh, err := coordinator.RequestPOCancellation(ctx, op, rejectPO.POID, "again", "cancel-reject-2")
	if err != nil || fresh.CancellationID == cancel.CancellationID {
		t.Fatalf("second cancellation = %+v err=%v, want a new record after rejection", fresh, err)
	}
}

// TestTodo_SMB_BUY_003_Race proves parallel dispatches, amendment commits
// and acknowledgements converge on one outcome each.
func TestTodo_SMB_BUY_003_Race(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	op := buy003Context()
	transport := &buy003Transport{}
	coordinator := NewBuyCoordinator(buy003Runner(transport, now))

	po, err := coordinator.CreatePurchaseOrder(ctx, op, buy003Input(), "po-race")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.SubmitPurchaseOrder(ctx, op, po.POID, "submit-race"); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ApprovePurchaseOrder(ctx, op, po.POID, "buyer-2", "ok", "approve-race"); err != nil {
		t.Fatal(err)
	}

	const racers = 8
	var wg sync.WaitGroup
	operations := make([]string, racers)
	for i := range operations {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dispatch, err := coordinator.DispatchPurchaseOrder(ctx, op, po.POID, DispatchInput{Recipient: "contact-acme-9", Channel: delivery.ChannelEmail, VerificationEvidence: "verified-1", At: now}, "dispatch-race")
			if err != nil {
				t.Errorf("race dispatch err=%v", err)
				return
			}
			operations[i] = dispatch.OperationID
		}(i)
	}
	wg.Wait()
	for _, id := range operations {
		if id == "" || id != operations[0] {
			t.Fatalf("parallel dispatches diverged: %v", operations)
		}
	}
	if transport.calls != 1 {
		t.Fatalf("transport called %d times, want exactly once", transport.calls)
	}

	amend, err := coordinator.ProposePOAmendment(ctx, op, po.POID, []BuyLineInput{
		{ItemSKU: "WIDGET-12", Description: "Widget 12-pack", UOM: "each", Qty: "10", UnitPrice: "26.00", TaxRate: "0.20", DeliveryDate: "2026-11-15"},
		{ItemSKU: "SEAL-9", Description: "Seal kit", UOM: "each", Qty: "4", UnitPrice: "12.50", TaxRate: "", DeliveryDate: "2026-11-15"},
	}, "price update", "supplier increase", "amend-race", "")
	if err != nil {
		t.Fatal(err)
	}
	revisions := make([]int, racers)
	var commitWG sync.WaitGroup
	for i := range revisions {
		commitWG.Add(1)
		go func(i int) {
			defer commitWG.Done()
			revised, err := coordinator.CommitPOAmendment(ctx, op, amend.AmendmentID, "buyer-2", "ok", "commit-race")
			if err != nil {
				t.Errorf("race commit err=%v", err)
				return
			}
			revisions[i] = revised.Revision
		}(i)
	}
	commitWG.Wait()
	for _, rev := range revisions {
		if rev != 2 {
			t.Fatalf("parallel amendment commits diverged: %v", revisions)
		}
	}

	acks := make([]string, racers)
	var ackWG sync.WaitGroup
	for i := range acks {
		ackWG.Add(1)
		go func(i int) {
			defer ackWG.Done()
			ack, err := coordinator.AcknowledgePurchaseOrder(ctx, buy003Supplier(), po.POID, 2, "ok", "ev", "ack-race")
			if err != nil {
				t.Errorf("race ack err=%v", err)
				return
			}
			acks[i] = ack.AckID
		}(i)
	}
	ackWG.Wait()
	for _, id := range acks {
		if id == "" || id != acks[0] {
			t.Fatalf("parallel acks diverged: %v", acks)
		}
	}
}

// TestTodo_SMB_BUY_003_Security proves tenant and company isolation,
// origin authentication and supplier-context authentication on every
// purchase order read and write.
func TestTodo_SMB_BUY_003_Security(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	op := buy003Context()
	coordinator := NewBuyCoordinator(buy003Runner(&buy003Transport{}, now))

	po, err := coordinator.CreatePurchaseOrder(ctx, op, buy003Input(), "po-sec")
	if err != nil {
		t.Fatal(err)
	}
	foreignTenant := op
	foreignTenant.TenantID = "tenant-evil"
	if _, err := coordinator.POLookup(foreignTenant, po.POID); !errors.Is(err, ErrBuyScope) {
		t.Fatalf("cross-tenant lookup err=%v, want scope refusal", err)
	}
	if _, err := coordinator.PORevisions(foreignTenant, po.POID); !errors.Is(err, ErrBuyScope) {
		t.Fatalf("cross-tenant history err=%v, want scope refusal", err)
	}
	foreignCompany := op
	foreignCompany.CompanyID = "company-evil"
	if _, err := coordinator.POLookup(foreignCompany, po.POID); !errors.Is(err, ErrBuyScope) {
		t.Fatalf("cross-company lookup err=%v, want scope refusal", err)
	}
	badOrigin := op
	badOrigin.Origin = "scan-beep"
	if _, err := coordinator.CreatePurchaseOrder(ctx, badOrigin, buy003Input(), "po-bad"); !errors.Is(err, ErrBuyContext) {
		t.Fatalf("bad origin err=%v, want context refusal", err)
	}
	foreignSupplier := buy003Supplier()
	foreignSupplier.TenantID = "tenant-evil"
	if _, err := coordinator.AcknowledgePurchaseOrder(ctx, foreignSupplier, po.POID, 1, "x", "ev", "ack-evil"); !errors.Is(err, ErrBuyScope) {
		t.Fatalf("cross-tenant ack err=%v, want scope refusal", err)
	}
	if _, err := coordinator.GetSupplierCommitment(foreignSupplier, po.POID); !errors.Is(err, ErrBuyScope) {
		t.Fatalf("cross-tenant commitment err=%v, want scope refusal", err)
	}
}

// TestTodo_SMB_BUY_003_Recovery proves a snapshot round-trip preserves
// orders, revisions, dispatches, amendments, cancellations and
// acknowledgements across restart.
func TestTodo_SMB_BUY_003_Recovery(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	op := buy003Context()
	coordinator := NewBuyCoordinator(buy003Runner(&buy003Transport{}, now))

	po, err := coordinator.CreatePurchaseOrder(ctx, op, buy003Input(), "po-rec")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.SubmitPurchaseOrder(ctx, op, po.POID, "submit-rec"); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ApprovePurchaseOrder(ctx, op, po.POID, "buyer-2", "ok", "approve-rec"); err != nil {
		t.Fatal(err)
	}
	dispatch, err := coordinator.DispatchPurchaseOrder(ctx, op, po.POID, DispatchInput{Recipient: "contact-acme-9", Channel: delivery.ChannelEmail, VerificationEvidence: "verified-1", At: now}, "dispatch-rec")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.RecordDispatchEvidence(ctx, op, dispatch.OperationID, true, "prov-1", "delivered", "evidence-rec"); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ApplyReceiptLine(ctx, op, po.POID, 1, "3", "recv-rec", "receipt-rec"); err != nil {
		t.Fatal(err)
	}
	amend, err := coordinator.ProposePOAmendment(ctx, op, po.POID, []BuyLineInput{
		{ItemSKU: "WIDGET-12", Description: "Widget 12-pack", UOM: "each", Qty: "10", UnitPrice: "25.00", TaxRate: "0.20", DeliveryDate: "2026-12-01"},
		{ItemSKU: "SEAL-9", Description: "Seal kit", UOM: "each", Qty: "4", UnitPrice: "12.50", TaxRate: "", DeliveryDate: "2026-11-15"},
	}, "date move", "push line 1", "amend-rec", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.CommitPOAmendment(ctx, op, amend.AmendmentID, "buyer-2", "ok", "commit-rec"); err != nil {
		t.Fatal(err)
	}
	cancel, err := coordinator.RequestPOCancellation(ctx, op, po.POID, "descoped", "cancel-rec")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.AcknowledgePurchaseOrder(ctx, buy003Supplier(), po.POID, 2, "ok", "ev", "ack-rec"); err != nil {
		t.Fatal(err)
	}

	bytes, err := coordinator.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewBuyCoordinator(buy003Runner(&buy003Transport{}, now))
	if err := restarted.Restore(bytes); err != nil {
		t.Fatal(err)
	}
	restored, err := restarted.POLookup(op, po.POID)
	if err != nil || restored.Revision != 2 || restored.Lines[0].ReceivedQty != "3" {
		t.Fatalf("post-restart PO = %+v err=%v", restored, err)
	}
	outcome, err := restarted.GetDispatchOutcome(op, dispatch.OperationID)
	if err != nil || outcome.State != BuyDispatchDelivered {
		t.Fatalf("post-restart dispatch = %+v err=%v", outcome, err)
	}
	if _, err := restarted.ResolvePOCancellation(ctx, op, cancel.CancellationID, CancelAccept, "supplier-acme", "agrees", "resolve-rec"); err != nil {
		t.Fatalf("post-restart cancel resolution err=%v", err)
	}
	if _, err := restarted.DispatchPurchaseOrder(ctx, op, po.POID, DispatchInput{Recipient: "contact-acme-9", Channel: delivery.ChannelEmail, VerificationEvidence: "verified-1", At: now}, "dispatch-rec"); !errors.Is(err, ErrBuyConflict) {
		t.Fatalf("post-restart duplicate dispatch key err=%v, want idempotency conflict on changed payload", err)
	}
	if err := restarted.Restore(bytes); err == nil {
		t.Fatal("restore into a populated coordinator succeeded, want refusal")
	}
	if err := NewBuyCoordinator(buy003Runner(&buy003Transport{}, now)).Restore([]byte("{broken")); err == nil {
		t.Fatal("restore of corrupt bytes succeeded, want refusal")
	}
}

// TestTodo_SMB_BUY_003_ReviewPaths covers the buyer review surface: rejecting
// amendments and supplier proposals, clearing the outdated-acknowledgement
// flag, revalidating supplier contacts, and the per-order audit trace.
func TestTodo_SMB_BUY_003_ReviewPaths(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	op := buy003Context()
	supplier := buy003Supplier()
	coordinator := NewBuyCoordinator(buy003Runner(&buy003Transport{}, now))

	po, err := coordinator.CreatePurchaseOrder(ctx, op, buy003Input(), "rev-po")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.SubmitPurchaseOrder(ctx, op, po.POID, "rev-submit"); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ApprovePurchaseOrder(ctx, op, po.POID, "buyer-2", "ok", "rev-approve"); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.DispatchPurchaseOrder(ctx, op, po.POID, DispatchInput{Recipient: "contact-acme-9", Channel: delivery.ChannelEmail, VerificationEvidence: "verified-1", At: now}, "rev-dispatch"); err != nil {
		t.Fatal(err)
	}

	amendment, err := coordinator.ProposePOAmendment(ctx, op, po.POID, []BuyLineInput{
		{ItemSKU: "WIDGET-12", Description: "Widget 12-pack", UOM: "each", Qty: "10", UnitPrice: "25.00", TaxRate: "0.20", DeliveryDate: "2026-12-01"},
		{ItemSKU: "SEAL-9", Description: "Seal kit", UOM: "each", Qty: "4", UnitPrice: "12.50", TaxRate: "", DeliveryDate: "2026-11-15"},
	}, "slip date", "buyer asked", "rev-amend", "")
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := coordinator.RejectPOAmendment(ctx, op, amendment.AmendmentID, "budget freeze", "rev-reject")
	if err != nil || rejected.Status != AmendRejected {
		t.Fatalf("rejected amendment = %+v err=%v, want REJECTED", rejected, err)
	}
	if again, err := coordinator.RejectPOAmendment(ctx, op, amendment.AmendmentID, "budget freeze", "rev-reject"); err != nil || again.AmendmentID != amendment.AmendmentID {
		t.Fatalf("reject retry = %+v err=%v, want same outcome", again, err)
	}
	if _, err := coordinator.CommitPOAmendment(ctx, op, amendment.AmendmentID, "buyer-2", "sneaky", "rev-commit"); err == nil {
		t.Fatal("committed a rejected amendment")
	}

	proposal, err := coordinator.ProposeDeliveryChange(ctx, supplier, po.POID, 1, 1, "2026-11-25", "", "truck delay", "rev-prop")
	if err != nil {
		t.Fatal(err)
	}
	declined, err := coordinator.RejectDeliveryChange(ctx, op, proposal.ProposalID, "dock booked", "rev-decline")
	if err != nil || declined.Status != ProposalRejected {
		t.Fatalf("rejected proposal = %+v err=%v, want REJECTED", declined, err)
	}
	if _, err := coordinator.AdoptDeliveryChange(ctx, op, proposal.ProposalID, "buyer-2", "sneaky", "rev-adopt"); err == nil {
		t.Fatal("adopted a rejected proposal")
	}

	amend2, err := coordinator.ProposePOAmendment(ctx, op, po.POID, []BuyLineInput{
		{ItemSKU: "WIDGET-12", Description: "Widget 12-pack", UOM: "each", Qty: "10", UnitPrice: "25.00", TaxRate: "0.20", DeliveryDate: "2026-12-01"},
		{ItemSKU: "SEAL-9", Description: "Seal kit", UOM: "each", Qty: "4", UnitPrice: "12.50", TaxRate: "", DeliveryDate: "2026-11-15"},
	}, "slip date", "buyer asked again", "rev-amend2", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.CommitPOAmendment(ctx, op, amend2.AmendmentID, "buyer-2", "ok-2", "rev-commit2"); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ApprovePurchaseOrder(ctx, op, po.POID, "buyer-2", "ok-again", "rev-approve2"); err != nil {
		t.Fatal(err)
	}
	stale, err := coordinator.AcknowledgePurchaseOrder(ctx, supplier, po.POID, 1, "late confirm", "ack-ev-9", "rev-ack")
	if err != nil || stale.Status != AckSuperseded {
		t.Fatalf("outdated ack = %+v err=%v, want SUPERSEDED_ACK", stale, err)
	}
	cleared, err := coordinator.ClearAckReview(ctx, op, po.POID, "rev-clear")
	if err != nil || cleared.NeedsReview {
		t.Fatalf("cleared PO = %+v err=%v, want review flag cleared", cleared, err)
	}

	revalidated, err := coordinator.RevalidateSupplier(ctx, op, po.POID, "contact-acme-10", "bank-acme-2", "kyc-refresh", "rev-reval")
	if err != nil || revalidated.Supplier.ContactRef != "contact-acme-10" || revalidated.Supplier.BankRef != "bank-acme-2" {
		t.Fatalf("revalidated PO = %+v err=%v, want new contacts", revalidated, err)
	}

	trace, err := coordinator.POTrace(op, po.POID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, event := range trace {
		seen[event.Kind] = true
	}
	for _, want := range []string{BuyEventCreated, BuyEventSubmitted, BuyEventApproved, BuyEventDispatched, BuyEventAmended, BuyEventAcked} {
		if !seen[want] {
			t.Errorf("trace missing %s: %+v", want, trace)
		}
	}
}
