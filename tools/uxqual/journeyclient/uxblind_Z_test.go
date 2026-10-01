package journeyclient

import (
	"context"
	"testing"
)

// TestTodo_UXBLIND_078 proves that range feedback is projected when the
// controlled pay field loses focus, before a proposal submit or RPC.
func TestTodo_UXBLIND_078(t *testing.T) {
	h := staffed(t)
	h.svc.workers[2].PreferredName = "Omar"
	h.svc.workers[2].BasePay = "100.03"
	h.app.Start(context.Background(), ProposalHref("omar-reyes"))
	h.awaitPage(t, "the focused proposal", proposalFor("omar-reyes"))

	page := h.store.Page()
	page.OnFieldChange(FieldJobCode, "OPS-HRBP3")
	page.OnFieldChange(FieldBase, "100.00")
	page = h.store.Page()
	if page.OnFieldBlur == nil {
		t.Fatal("the live proposal did not expose field-blur validation")
	}
	page.OnFieldBlur(FieldBase)

	base, ok := fieldByID(h.store.Page().Proposal.Form.Fields, FieldBase)
	if !ok || base.Error != "Enter an amount from USD\u00a0105.04 to USD\u00a0115.03, inclusive." {
		t.Fatalf("blur did not project the published pay range: %+v", base)
	}
}
