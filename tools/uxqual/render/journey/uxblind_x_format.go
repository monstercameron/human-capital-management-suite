package journey

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// journeyPayLineLocale is the renderer-side consumer of the raw compensation
// projection. Older fixtures carry only PayLine and continue to render that
// already-localized sentence; live cards carry Edit's raw values, so the
// header and card use the same shared money/unit formatter as the client.
func journeyPayLineLocale(locale string, j JourneyCard) string {
	if strings.TrimSpace(j.Edit.CurrentBase) == "" || strings.TrimSpace(j.Edit.Base) == "" {
		return j.PayLine
	}
	copy := productui.ResolveProductLocale(locale)
	from := copy.FormatMoneyWithPayUnit(j.Edit.CurrentBase, j.Edit.Currency, journeyPayUnit(j.Edit.CurrentPayBasis), 2)
	proposedBasis := j.Edit.ProposedPayBasis
	if strings.TrimSpace(proposedBasis) == "" {
		proposedBasis = j.Edit.CurrentPayBasis
	}
	to := copy.FormatMoneyWithPayUnit(j.Edit.Base, j.Edit.Currency, journeyPayUnit(proposedBasis), 2)
	if from == "" || to == "" {
		return j.PayLine
	}
	line := from + " → " + to
	if marker := strings.Index(j.PayLine, " ("); marker >= 0 {
		line += j.PayLine[marker:]
	}
	return line
}

func journeyPayUnit(basis string) string {
	if strings.EqualFold(strings.TrimSpace(basis), "HOURLY_RATE") || strings.EqualFold(strings.TrimSpace(basis), "hourly") {
		return "hourly_rate"
	}
	return "annual"
}
