package productui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/delivery"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/procurement"
)

type smbBuy003Store struct{}

func (smbBuy003Store) Claim(_ context.Context, envelope delivery.Envelope, attempt int) (delivery.Claim, error) {
	return delivery.Claim{AttemptID: envelope.IdempotencyKey + "#1", Attempt: attempt}, nil
}

func (smbBuy003Store) Observe(context.Context, delivery.Envelope, delivery.Claim, delivery.Observation) error {
	return nil
}

type smbBuy003Transport struct{ ambiguous bool }

func (t smbBuy003Transport) Deliver(_ context.Context, _ delivery.Envelope) (delivery.ProviderResult, error) {
	if t.ambiguous {
		return delivery.ProviderResult{}, &delivery.ProviderError{Err: context.DeadlineExceeded, Ambiguous: true}
	}
	return delivery.ProviderResult{ProviderRef: "prov-1", ObservationRef: "obs-1", Code: "ACCEPTED"}, nil
}

func smbBuy003Fixture(t *testing.T, ambiguous bool) (procurement.BuyPO, *procurement.BuyDispatch, *procurement.BuyAck) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	op := procurement.BuyContext{TenantID: "tenant-a", CompanyID: "company-a", SiteID: "site-1", ActorID: "buyer-1", Origin: "human"}
	runner := delivery.Runner{Store: smbBuy003Store{}, Transport: smbBuy003Transport{ambiguous: ambiguous}, Clock: func() time.Time { return now }, MaxAttempts: 1}
	coordinator := procurement.NewBuyCoordinator(runner)
	po, err := coordinator.CreatePurchaseOrder(ctx, op, procurement.BuyPOInput{
		SiteID:   "site-1",
		Supplier: procurement.BuySupplierSnapshot{SupplierID: "supplier-acme", Name: "Acme Supply", ContactRef: "contact-acme-9", BankRef: "bank-acme-1"},
		Lines: []procurement.BuyLineInput{
			{ItemSKU: "WIDGET-12", Description: "Widget 12-pack", UOM: "each", Qty: "10", UnitPrice: "25.00", TaxRate: "0.20", DeliveryDate: "2026-11-15"},
		},
		Currency: "USD", CurrencyScale: 2,
		DeliverySite: "site-1", DeliveryRecipient: "receiving-dock-1",
		SourceRequestRef: "req-77", SpendAuthorizationRef: "spend-auth-1",
	}, "card-po")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.SubmitPurchaseOrder(ctx, op, po.POID, "card-submit"); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ApprovePurchaseOrder(ctx, op, po.POID, "buyer-2", "ok", "card-approve"); err != nil {
		t.Fatal(err)
	}
	dispatch, err := coordinator.DispatchPurchaseOrder(ctx, op, po.POID, procurement.DispatchInput{Recipient: "contact-acme-9", Channel: delivery.ChannelEmail, VerificationEvidence: "verified-1", At: now}, "card-dispatch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ApplyReceiptLine(ctx, op, po.POID, 1, "6", "recv-1", "card-receipt"); err != nil {
		t.Fatal(err)
	}
	supplier := procurement.SupplierContext{TenantID: "tenant-a", CompanyID: "company-a", SupplierID: "supplier-acme", ActorID: "supplier-user-1"}
	ack, err := coordinator.AcknowledgePurchaseOrder(ctx, supplier, po.POID, 1, "confirmed", "ack-ev-1", "card-ack")
	if err != nil {
		t.Fatal(err)
	}
	po, err = coordinator.POLookup(op, po.POID)
	if err != nil {
		t.Fatal(err)
	}
	return po, &dispatch, &ack
}

// TestTodo_SMB_BUY_003_Browser renders the buyer workbench card the way the
// purchasing surface serves it: identity, supplier, totals, per-line
// ordered/received/invoiced progress, dispatch state with the unknown
// banner, amendments, cancellation and acknowledgement, localized across
// en-US, de-DE and ar RTL.
func TestTodo_SMB_BUY_003_Browser(t *testing.T) {
	po, dispatch, ack := smbBuy003Fixture(t, false)

	markup, err := ui.RenderToString(SMBBuyWorkbenchCard(po, dispatch, 1, nil, ack, "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Purchase order " + po.PONumber, "Status: OPEN", "Revision: 1",
		"Acme Supply", "300.00 USD", "site-1",
		"Widget 12-pack", "Ordered", "Received", "Invoiced",
		"Dispatch: ACCEPTED_BY_CHANNEL", "contact-acme-9",
		"Pending amendments: 1.", "No cancellation requested.",
		"Supplier acknowledgement: ACKNOWLEDGED",
		"data-po-id=\"" + po.POID + "\"", "tabIndex=\"-1\"",
		"link back instead of rewriting",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("workbench card missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Delivery is unknown") {
		t.Errorf("accepted dispatch shows the unknown banner: %s", markup)
	}

	german, err := ui.RenderToString(SMBBuyWorkbenchCard(po, dispatch, 0, nil, ack, "de-DE"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Bestellung " + po.PONumber, "Lieferantenbestätigung: ACKNOWLEDGED", "Keine offenen Änderungen."} {
		if !strings.Contains(german, want) {
			t.Errorf("German workbench card missing %q: %s", want, german)
		}
	}

	rtl, err := ui.RenderToString(SMBBuyWorkbenchCard(po, dispatch, 0, nil, ack, "ar"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dir=\"rtl\"", "lang=\"ar\"", "أمر الشراء " + po.PONumber, "الحالة: OPEN"} {
		if !strings.Contains(rtl, want) {
			t.Errorf("Arabic workbench card missing %q: %s", want, rtl)
		}
	}

	unknownPO, unknownDispatch, _ := smbBuy003Fixture(t, true)
	unknown, err := ui.RenderToString(SMBBuyWorkbenchCard(unknownPO, unknownDispatch, 0, nil, nil, "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Dispatch: UNKNOWN", "Delivery is unknown", "Check delivery", "view=dispatch"} {
		if !strings.Contains(unknown, want) {
			t.Errorf("unknown-dispatch card missing %q: %s", want, unknown)
		}
	}
}
