package journey

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// ReviewCards are REV-091-02's two review cards for the detail page: the
// reporting-line review (PROMOUX-005) and the compensation guardrail
// (PROMOUX-006). Both are productui's own props, already localized by the
// client from the server's authorized projection; this package only lays
// them out. Either may be nil.
type ReviewCards struct {
	Promotion *productui.PromotionReviewProps
	Guardrail *productui.CompensationGuardrailProps
}

// reviewCardsSectionLocale renders the cards with productui's own components
// rather than a second drawing of the same facts. The section is absent when
// the server sent no review.
func reviewCardsSectionLocale(locale string, cards *ReviewCards) ui.Node {
	if cards == nil || (cards.Promotion == nil && cards.Guardrail == nil) {
		return nil
	}
	copy := productui.ResolveProductLocale(locale)
	items := make([]ui.Node, 0, 2)
	if cards.Promotion != nil {
		items = append(items, html.Div(html.Props{Class: "jn-review-card"},
			html.H3(html.Props{Class: "jn-subhead"}, html.Text(copy.Text("journey.reporting_line_heading"))),
			productui.PromotionReview(copy, *cards.Promotion),
		))
	}
	if cards.Guardrail != nil {
		items = append(items, html.Div(html.Props{Class: "jn-review-card"},
			productui.CompensationGuardrailCard(*cards.Guardrail),
		))
	}
	return html.Section(html.Props{Class: "jn-panel jn-review-checks", Aria: map[string]string{"labelledby": "review-checks-heading"}},
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "review-checks-heading"}, html.Text(copy.Text("journey.review_checks_heading"))),
		),
		html.Div(html.Props{Class: "jn-review-cards"}, items...),
	)
}
