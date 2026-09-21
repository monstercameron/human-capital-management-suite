package journey

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// journeyCardIdentity fills the shared object identity's typed slots for
// one request card (UXLIVE-032): a human task label as the primary
// heading, the short request reference as separate metadata, and the
// request's status once.
func journeyCardIdentity(locale string, j JourneyCard) productui.ObjectIdentity {
	copy := productui.ResolveProductLocale(locale)
	primary := copy.Text("journey.card_title_unnamed")
	if name := strings.TrimSpace(j.WorkerName); name != "" {
		primary = copy.Text("journey.card_title", map[string]string{"name": name})
	}
	return productui.ObjectIdentity{
		Primary:        primary,
		ReferenceLabel: copy.Text("journey.request_reference_label"),
		Reference:      JourneyReference(j.IntentID),
		Status:         j.StageLabel,
		StatusTone:     j.StageTone,
	}
}

// journeyCardHead renders the card's identity header. The heading link's
// visible text is the task label alone; the reference and the status each
// sit in their own element, so neither can be run into the person's name
// or split by a line break inside the heading. The link still carries the
// whole identity -- person, request and state -- as its accessible name,
// which is why the visible status chip is hidden from assistive technology:
// the heading already says it, and saying it twice is the repetition this
// header exists to remove.
func journeyCardHead(locale string, j JourneyCard) ui.Node {
	copy := productui.ResolveProductLocale(locale)
	identity := journeyCardIdentity(locale, j)
	accessibleName := identity.AccessibleName(copy) + " — " + copy.Text("journey.open_request")
	return html.Div(html.Props{Class: "jn-journey-top"},
		html.H3(html.Props{},
			html.A(html.Props{Href: j.Href, OnClick: activate(j.OnOpen), Aria: map[string]string{"label": accessibleName}},
				html.Text(identity.Primary),
			),
		),
		htmlIf(identity.Status != "", func() ui.Node {
			return html.Span(html.Props{Class: "jn-journey-status", Aria: map[string]string{"hidden": "true"}},
				chip(identity.StatusTone, identity.Status))
		}),
	)
}

// journeyCardReference renders "Request 8CF888" as quiet, copyable
// metadata below the heading. The token is its own isolated, left-to-right,
// untranslated run that never wraps, so a narrow card, 200% zoom, a longer
// German label or a right-to-left Arabic line can move it as a whole but
// cannot break it. It renders nothing for a card with no intent id.
func journeyCardReference(locale string, j JourneyCard) ui.Node {
	identity := journeyCardIdentity(locale, j)
	if identity.Reference == "" {
		return nil
	}
	copy := productui.ResolveProductLocale(locale)
	reference := identity.Reference
	return html.P(html.Props{Class: "jn-journey-refline"},
		html.Span(html.Props{Class: "jn-journey-reflabel"}, html.Text(identity.ReferenceLabel)),
		html.Span(html.Props{Class: "jn-journey-ref", Raw: map[string]any{"dir": "ltr", "translate": "no"}}, html.Text(reference)),
		html.Button(html.Props{
			Type:    "button",
			Class:   "jn-copy-btn",
			Aria:    map[string]string{"label": copy.Text("journey.copy_reference", map[string]string{"reference": reference})},
			OnClick: activate(func() { copyToClipboard(reference) }),
		}, html.Text(copy.Text("journey.copy"))),
	)
}
