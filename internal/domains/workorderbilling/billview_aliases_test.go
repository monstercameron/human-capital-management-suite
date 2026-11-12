package workorderbilling

import (
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderbilling/billview"
)

// Every alias must be the leaf's type, so a value built through either name is
// interchangeable and the browser client sees the same shape the server does.
func TestViewAliasesAreTheLeafTypes(t *testing.T) {
	pairs := []struct {
		name       string
		root, leaf reflect.Type
	}{
		{"Adjustment", reflect.TypeOf(Adjustment{}), reflect.TypeOf(billview.Adjustment{})},
		{"AdjustInvoice", reflect.TypeOf(AdjustInvoice{}), reflect.TypeOf(billview.AdjustInvoice{})},
		{"AdjustPreview", reflect.TypeOf(AdjustPreview{}), reflect.TypeOf(billview.AdjustPreview{})},
		{"BillPreview", reflect.TypeOf(BillPreview{}), reflect.TypeOf(billview.BillPreview{})},
		{"BillSourceView", reflect.TypeOf(BillSourceView{}), reflect.TypeOf(billview.BillSourceView{})},
		{"PricingMode", reflect.TypeOf(PricingMode("")), reflect.TypeOf(billview.PricingMode(""))},
		{"BillSourceKind", reflect.TypeOf(BillSourceKind("")), reflect.TypeOf(billview.BillSourceKind(""))},
	}
	for _, p := range pairs {
		if p.root != p.leaf {
			t.Errorf("%s: root type %v is not the leaf type %v", p.name, p.root, p.leaf)
		}
	}
}

func TestViewConstantsKeepTheirWireValues(t *testing.T) {
	if PricingTimeMaterial != billview.PricingTimeMaterial || string(PricingTimeMaterial) != "time-material" {
		t.Fatalf("PricingTimeMaterial = %q, want the leaf's time-material", PricingTimeMaterial)
	}
	if BillKindMilestone != billview.BillKindMilestone || string(BillKindMilestone) != "milestone" {
		t.Fatalf("BillKindMilestone = %q, want the leaf's milestone", BillKindMilestone)
	}
}
