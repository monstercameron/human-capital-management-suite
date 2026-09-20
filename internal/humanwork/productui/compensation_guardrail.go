package productui

// PROMOUX-006 GREEN: "after target-role selection, authorized purpose-bound
// data displays current exact Money, permitted increase percent, exact
// minimum and maximum annual Money, band position, currency and
// effective-date basis; the input constrains and explains the range without
// floating-point arithmetic; unauthorized viewers receive a typed
// unavailable action rather than inferred pay."
//
// This file is the one seam that turns
// promotion.CompensationGuardrail -- the domain's own already-computed
// verdict from promotion.EvaluateCompensationGuardrail -- into presentation,
// mirroring the pattern promotion_review.go (PROMOUX-005) and
// approval_disposition.go (PROMOUX-003) already established: a domain
// function decides the exact numbers and the typed status; this file only
// localizes and renders what it is handed. There is no money or percentage
// arithmetic anywhere below -- every amount arrives already computed in
// exact decimal, and [money]/[percentage] (page_work.go) format it through
// big.Rat-based locale services, never float64.
//
// The unauthorized and band-unresolved branches share the exact same
// rendered shape (title plus one disabled action plus one reason sentence):
// neither ever renders a Facts list, a Money value or a Percentage value, so
// an unauthorized viewer cannot infer a baseline, a range or a permitted
// percent from the markup -- see TestTodo_PROMOUX_006_Security.

import (
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
)

// CompensationGuardrailProps is the presentation-ready projection of one
// [promotion.CompensationGuardrail]. Every field is already localized text;
// [CompensationGuardrailCard] never resolves locale copy or recomputes a
// range or a percentage itself.
//
// When Available is false, Facts is always nil: the two states are mutually
// exclusive and there is no partial rendering in between, which is what
// keeps an unauthorized and an unresolved-band result rendering identically
// apart from UnavailableMessage.
type CompensationGuardrailProps struct {
	// Title is the group's accessible name, rendered whether or not the
	// guardrail is available, so assistive technology finds the same
	// landmark either way.
	Title     string
	Available bool
	// Facts are GREEN's data fields, already formatted and localized:
	// current pay, permitted increase, minimum, maximum, band position and
	// effective-date basis, in that declared order. Populated only when
	// Available is true.
	Facts []FactProps
	// UnavailableAction names the typed action that cannot be taken --
	// GREEN's "typed unavailable action rather than inferred pay" -- e.g.
	// "Enter proposed pay". Populated only when Available is false.
	UnavailableAction string
	// UnavailableMessage explains why, without naming the underlying cause
	// in implementation terms. Populated only when Available is false.
	UnavailableMessage string
	// ReviewContext renders the card for someone reviewing a submitted
	// proposal rather than entering one (REV-091-02): the unavailable state
	// is one quiet explanation with no action, because "Enter proposed pay"
	// is proposer wording and a dead control on a review page.
	ReviewContext bool
}

// compensationGuardrailBandPositionKey maps the engine's closed placement
// vocabulary onto a message key. The switch has no default case that
// returns a real key: an unrecognized placement falls through to the
// explicit "unspecified" key below rather than silently reusing another
// placement's copy, so a future placement value can never be presented as
// one of today's three by omission.
func compensationGuardrailBandPositionKey(p payband.Placement) string {
	switch p {
	case payband.PlacementBelowMinimum:
		return "compensation_guardrail.band_position.below_minimum"
	case payband.PlacementInBand:
		return "compensation_guardrail.band_position.in_band"
	case payband.PlacementAboveMaximum:
		return "compensation_guardrail.band_position.above_maximum"
	default:
		return "compensation_guardrail.band_position.unspecified"
	}
}

// compensationGuardrailUnavailableReasonKey maps the domain's closed
// unavailable-reason vocabulary onto a message key. Every reason
// [promotion.EvaluateCompensationGuardrail] can return has an explicit case;
// an unrecognized reason (there is none today, but a future one would land
// here before this file is updated) falls through to a generic message
// rather than silently reusing NOT_AUTHORIZED's or BAND_UNRESOLVED's own
// copy.
func compensationGuardrailUnavailableReasonKey(reason promotion.GuardrailUnavailableReason) string {
	switch reason {
	case promotion.GuardrailReasonNotAuthorized:
		return "compensation_guardrail.unavailable_reason.not_authorized"
	case promotion.GuardrailReasonBandUnresolved:
		return "compensation_guardrail.unavailable_reason.band_unresolved"
	default:
		return "compensation_guardrail.unavailable_reason.unspecified"
	}
}

// dateOf converts a business date to a time.Time at UTC midnight, purely for
// formatting -- the same conversion internal/domains/promotion/simcomp uses
// for its own day-count arithmetic (daysBetween), restated here because
// locale.FormatDate takes a time.Time and no clock is read in either place.
func dateOf(year int32, month time.Month, day uint8) time.Time {
	return time.Date(int(year), month, int(day), 0, 0, 0, 0, time.UTC)
}

// CompensationGuardrailPropsFrom adapts an already-evaluated
// [promotion.CompensationGuardrail] into presentation. It is a pure field
// copy plus locale formatting: there is no branch here that recomputes a
// range, a percentage or a band position -- every number rendered came from
// the guardrail exactly as computed, which is the property
// TestTodo_PROMOUX_006_Browser proves.
func CompensationGuardrailPropsFrom(locale LocaleContext, g promotion.CompensationGuardrail) CompensationGuardrailProps {
	title := locale.Text("compensation_guardrail.title")
	if !g.Available() {
		return CompensationGuardrailProps{
			Title:              title,
			Available:          false,
			UnavailableAction:  locale.Text("compensation_guardrail.unavailable_action"),
			UnavailableMessage: locale.Text(compensationGuardrailUnavailableReasonKey(g.Reason)),
		}
	}
	basis := dateOf(g.EffectiveDateBasis.Year(), g.EffectiveDateBasis.Month(), g.EffectiveDateBasis.Day())
	return CompensationGuardrailProps{
		Title:     title,
		Available: true,
		Facts: []FactProps{
			{Label: locale.Text("compensation_guardrail.current_pay"), Value: money(locale, g.CurrentAnnualized)},
			{Label: locale.Text("compensation_guardrail.permitted_increase"), Value: percentage(locale, g.PermittedIncreasePercent.Fraction().String())},
			{Label: locale.Text("compensation_guardrail.minimum"), Value: money(locale, g.MinimumAnnualized)},
			{Label: locale.Text("compensation_guardrail.maximum"), Value: money(locale, g.MaximumAnnualized)},
			{Label: locale.Text("compensation_guardrail.band_position"), Value: locale.Text(compensationGuardrailBandPositionKey(g.BandPosition))},
			{Label: locale.Text("compensation_guardrail.effective_date_basis"), Value: locale.FormatDate(basis)},
		},
	}
}

// CompensationGuardrailReviewPropsFrom is [CompensationGuardrailPropsFrom] for
// the proposal review page: identical facts when available, and when not, no
// unavailable action and a reviewer-facing explanation of why pay is absent.
func CompensationGuardrailReviewPropsFrom(locale LocaleContext, g promotion.CompensationGuardrail) CompensationGuardrailProps {
	props := CompensationGuardrailPropsFrom(locale, g)
	props.ReviewContext = true
	if props.Available {
		return props
	}
	props.UnavailableAction = ""
	switch g.Reason {
	case promotion.GuardrailReasonNotAuthorized:
		props.UnavailableMessage = locale.Text("compensation_guardrail.review_unavailable.not_authorized")
	case promotion.GuardrailReasonBandUnresolved:
		props.UnavailableMessage = locale.Text("compensation_guardrail.review_unavailable.band_unresolved")
	default:
		props.UnavailableMessage = locale.Text("compensation_guardrail.unavailable_reason.unspecified")
	}
	return props
}

// CompensationGuardrailCard renders GREEN's guardrail exactly as
// CompensationGuardrailPropsFrom already localized it.
//
// The unavailable branch renders GREEN's "typed unavailable action rather
// than inferred pay": a disabled action naming what cannot be done, plus one
// reason sentence -- never a Facts list, never a Money value, never a
// Percentage value. Showing a permitted range at all would let a reader
// solve for the baseline (a maximum of base x 1.18 discloses base), so the
// two branches are structurally different subtrees, not the same subtree
// with blanked-out numbers.
func CompensationGuardrailCard(props CompensationGuardrailProps) ui.Node {
	if !props.Available && props.ReviewContext {
		return html.Div(html.Props{Class: "compensation-guardrail compensation-guardrail-unavailable", Raw: map[string]any{"role": "group"}, Aria: map[string]string{"label": props.Title}},
			html.H3(html.Props{}, ui.Text(props.Title)),
			html.P(html.Props{Class: "compensation-guardrail-reason"}, ui.Text(props.UnavailableMessage)),
		)
	}
	if !props.Available {
		return html.Div(html.Props{Class: "compensation-guardrail compensation-guardrail-unavailable", Raw: map[string]any{"role": "group"}, Aria: map[string]string{"label": props.Title}},
			html.H3(html.Props{}, ui.Text(props.Title)),
			html.Button(html.Props{Class: "button compensation-guardrail-action", Disabled: true, Type: "button"}, ui.Text(props.UnavailableAction)),
			html.P(html.Props{Class: "compensation-guardrail-reason", Raw: map[string]any{"role": "status"}}, ui.Text(props.UnavailableMessage)),
		)
	}
	// The label/value rows sit in a <dl>: a <dt>/<dd> pair outside a
	// description list is not a pair to assistive technology.
	return html.Div(html.Props{Class: "compensation-guardrail compensation-guardrail-available", Raw: map[string]any{"role": "group"}, Aria: map[string]string{"label": props.Title}},
		html.H3(html.Props{}, ui.Text(props.Title)),
		html.Tag("dl", html.Props{Class: "compensation-guardrail-facts"}, factRows(props.Facts)...),
	)
}
