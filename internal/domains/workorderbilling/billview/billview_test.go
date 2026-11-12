package billview

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The string values are the producer vocabulary the browser renders and the
// server emits; changing one is a wire change, so they are pinned here.
func TestVocabularyValuesArePinned(t *testing.T) {
	modes := []PricingMode{PricingUnitPrice, PricingTimeMaterial, PricingFixedMilestone}
	wantModes := []string{"unit-price", "time-material", "fixed-milestone"}
	for i, m := range modes {
		if string(m) != wantModes[i] {
			t.Errorf("pricing mode %d = %q, want %q", i, m, wantModes[i])
		}
	}
	kinds := []BillSourceKind{BillKindTime, BillKindQuantity, BillKindMaterial, BillKindSubcontract, BillKindMilestone, BillKindExpense}
	want := []string{"time", "quantity", "material", "subcontract", "milestone", "expense"}
	for i, k := range kinds {
		if string(k) != want[i] {
			t.Errorf("source kind %d = %q, want %q", i, k, want[i])
		}
	}
}

// An adjustment memo, with its lines and ledger events, survives a JSON round
// trip unchanged: the view is a value the client can carry and re-read.
func TestAdjustmentRoundTripsWithLinesAndEvents(t *testing.T) {
	adj := Adjustment{
		AdjustmentID: "adj-1",
		Kind:         "CREDIT",
		Lines:        []AdjustLine{{LineID: "l1", Qty: "1", NetAmount: "10.00", TaxAmount: "0.80"}},
		TotalAmount:  "10.80",
		Events:       []AdjustEvent{{Kind: "drafted", Actor: "u1", At: "2026-11-11T00:00:00Z"}},
	}
	raw, err := json.Marshal(adj)
	if err != nil {
		t.Fatal(err)
	}
	var back Adjustment
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, adj) {
		t.Fatalf("round trip changed the memo:\n got %+v\nwant %+v", back, adj)
	}
	if len(back.Events) != 1 || back.Events[0].Kind != "drafted" {
		t.Fatalf("events lost in transit: %+v", back.Events)
	}
}

func TestPreviewCarriesPerLineTreatment(t *testing.T) {
	preview := AdjustPreview{InvoiceID: "inv-1", Kind: "CREDIT", TotalAmount: "5.40",
		Lines: []AdjustPreviewLine{{LineID: "l1", ProposedQty: "1", ProposedAmount: "5.00", ProposedTax: "0.40"}}}
	raw, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	var back AdjustPreview
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Lines) != 1 || back.Lines[0].ProposedAmount != "5.00" || back.TotalAmount != "5.40" {
		t.Fatalf("preview = %+v, want the proposed line and total", back)
	}
}
