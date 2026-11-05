package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// payBenefitReconPage is the route adapter for payroll and benefit
// reconciliation status. It resolves the server-authorized records through
// BEN-008 and shapes them for the reconciliation feature component; an
// empty record set or a domain read failure keeps an honest empty state.
func payBenefitReconPage(view View) ui.Node {
	projection := view.Benefits
	switch projection.State {
	case BenefitsReadNoRecord:
		return benefitsStateFallback(view, "pay_benefit_recon", "no_record")
	case BenefitsReadError:
		return benefitsStateFallback(view, "pay_benefit_recon", "read_error")
	case BenefitsReadReady:
	default:
		return benefitsUnavailable(view, "pay_benefit_recon")
	}
	records, err := ResolveBenefitReconciliation(projection.Recon)
	if err != nil {
		return benefitsStateFallback(view, "pay_benefit_recon", "read_error")
	}
	if len(records) == 0 {
		return benefitsStateFallback(view, "pay_benefit_recon", "no_record")
	}
	return benefitsReconContent(view, records)
}
