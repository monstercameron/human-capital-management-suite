package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func TestUXBLIND061_CancellationUsesBusinessDismissalAndDangerConfirm(t *testing.T) {
	cancel := findAction(t, uxblindRInterventionActions(t, nil), ActionCancel)
	if cancel.Variant != "danger" {
		t.Fatalf("cancel action variant = %q, want danger", cancel.Variant)
	}
	want := productui.ResolveProductLocale("en-US").Text("journey.action_keep_request")
	if cancel.CancelLabel != want || cancel.CancelLabel == "Cancel review" {
		t.Fatalf("cancel dismiss label = %q, want localized keep-request label %q", cancel.CancelLabel, want)
	}
	if strings.Contains(strings.ToLower(cancel.Description), "safe point") {
		t.Fatalf("cancellation description exposes engine terminology: %q", cancel.Description)
	}
}

func TestUXBLIND075_ComparisonUsesMoneyAndPayUnitForBothBases(t *testing.T) {
	for _, tc := range []struct {
		name, currentBasis, proposedBasis, current, proposed, unit string
	}{
		{name: "annual", currentBasis: "ANNUAL_SALARY", proposedBasis: "ANNUAL_SALARY", current: "93000", proposed: "98000", unit: "annual"},
		{name: "hourly", currentBasis: payBasisHourly, proposedBasis: payBasisHourly, current: "34.50", proposed: "40.00", unit: "hourly_rate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := &journeyv1.Journey{CurrentBase: tc.current, ProposedBase: tc.proposed, Currency: "USD", CurrentPayBasis: tc.currentBasis, ProposedPayBasis: tc.proposedBasis}
			rows := comparisonLocale("en-US", j)
			if len(rows) == 0 {
				t.Fatal("comparison rows are empty")
			}
			pay := journey.ComparisonRow{}
			for _, row := range rows {
				if row.Label == productui.ResolveProductLocale("en-US").Text("journey.compare_base") {
					pay = row
					break
				}
			}
			copy := productui.ResolveProductLocale("en-US")
			for _, amount := range []string{tc.current, tc.proposed} {
				if !strings.Contains(pay.Current+" "+pay.Proposed, copy.FormatMoneyWithPayUnit(amount, "USD", tc.unit, 2)) {
					t.Fatalf("comparison %q omitted money/pay unit for %q: current=%q proposed=%q", tc.name, amount, pay.Current, pay.Proposed)
				}
			}
		})
	}
}
