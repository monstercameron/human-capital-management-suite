package application

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// TestTodo_RBAC_RT_005_Integration is RBAC-RT-005's INTEGRATION: the served
// cell discloses execution diagnostics to oversight readers through the one
// rule, while raw pay follows the data policy — granted to administrators,
// masked for the redacted-only auditor, and intact for the authorized
// business reviewer who receives no diagnostics.
func TestTodo_RBAC_RT_005_Integration(t *testing.T) {
	h := rbacCompose(t)
	inspect := func(user string) *journeyv1.JourneyDetail {
		t.Helper()
		resp, err := h.journey.InspectJourney(h.rpc(user), &journeyv1.InspectJourneyRequest{IntentId: h.intentID})
		if err != nil {
			t.Fatalf("InspectJourney as %s: %v", user, err)
		}
		return resp.GetDetail()
	}
	diagnosticsPresent := func(user string, detail *journeyv1.JourneyDetail) bool {
		t.Helper()
		present := len(detail.GetNodes()) > 0 || len(detail.GetTransitions()) > 0 ||
			len(detail.GetEvidenceIds()) > 0 || len(detail.GetPlannedWrites()) > 0 || detail.GetLedger() != nil
		if !present {
			t.Fatalf("%s: no diagnostics disclosed (nodes=%d transitions=%d evidence=%d planned=%d ledger=%v)",
				user, len(detail.GetNodes()), len(detail.GetTransitions()),
				len(detail.GetEvidenceIds()), len(detail.GetPlannedWrites()), detail.GetLedger() != nil)
		}
		return true
	}
	wantPay := h.workers["rbac-fay"]
	assertPay := func(user string, detail *journeyv1.JourneyDetail, want bool) {
		t.Helper()
		got := sameDecimal(detail.GetJourney().GetCurrentBase(), wantPay.base) ||
			sameDecimal(detail.GetJourney().GetProposedBase(), wantPay.bonus)
		if got != want {
			t.Fatalf("%s: pay disclosed = %v, want %v (current=%q proposed=%q)",
				user, got, want, detail.GetJourney().GetCurrentBase(), detail.GetJourney().GetProposedBase())
		}
	}

	admin := inspect("admin")
	diagnosticsPresent("admin", admin)
	assertPay("admin", admin, true)

	compAdmin := inspect("compAdmin")
	diagnosticsPresent("compAdmin", compAdmin)
	assertPay("compAdmin", compAdmin, true)

	// The auditor reviews the execution evidence but never raw pay: the
	// redacted-only compensation grant admits the inspection with the
	// amounts omitted.
	auditor := inspect("auditor")
	diagnosticsPresent("auditor", auditor)
	assertPay("auditor", auditor, false)
	if auditor.GetJourney().GetCurrentBase() != "" || auditor.GetJourney().GetProposedBase() != "" {
		t.Fatalf("auditor: pay masked incompletely (current=%q proposed=%q)",
			auditor.GetJourney().GetCurrentBase(), auditor.GetJourney().GetProposedBase())
	}

	// The authorized business reviewer keeps full business data while the
	// diagnostics projection stays withheld.
	dana := inspect("dana")
	if len(dana.GetNodes()) != 0 || len(dana.GetTransitions()) != 0 || len(dana.GetEvidenceIds()) != 0 || dana.GetLedger() != nil {
		t.Fatalf("dana: diagnostics disclosed without the diagnostics grant (nodes=%d transitions=%d evidence=%d)",
			len(dana.GetNodes()), len(dana.GetTransitions()), len(dana.GetEvidenceIds()))
	}
	assertPay("dana", dana, true)
}
