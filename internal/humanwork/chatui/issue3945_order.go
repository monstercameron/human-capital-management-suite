package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// ISSUE-3945: the result order. Newest first stays the default; "Oldest first"
// is asked for with a sort:oldest word in the query, which is why it survives
// every page of the answer, a recent-search replay, opening a result and coming
// back, and shows as a removable chip like any other applied choice. The
// always-visible switch writes that word. Refine search has no second control for it (#900005).

// issue3945Oldest reports whether the parsed query asks for oldest first.
func issue3945Oldest(f chatsearch.Filters) bool { return f.Order == chatsearch.OrderOldest }

// issue3945OrderSwitch is the two-option switch above the results: it names the
// order in use, applies the other one at once and keeps the query and filters.
// It stays enabled while a search runs so keyboard focus is never lost to it.
func issue3945OrderSwitch(locale string, oldest bool) ui.Node {
	t := func(key string) string { return chatsearchText(locale, key) }
	option := func(value string, pressed bool) ui.Node {
		class := "chatsearch-order-option"
		if pressed {
			class += " is-selected"
		}
		return html.Button(html.Props{Type: "button", Class: class, Text: t(value),
			Aria: map[string]string{"pressed": boolString(pressed)},
			Data: map[string]string{"chatsearch-action": "order", "extra": value}})
	}
	return html.Div(html.Props{Class: "chatsearch-order", Role: "group", Aria: map[string]string{"label": t("order"), "describedby": "chatsearch-order-note"}},
		html.Span(html.Props{Class: "chatsearch-order-label", Aria: map[string]string{"hidden": "true"}, Text: t("order")}),
		option("newest", !oldest), option("oldest", oldest),
		html.Span(html.Props{ID: "chatsearch-order-note", Class: "chatsearch-order-note", Text: t("ordernote")}))
}

// issue3945ChipLabel is the removable chip's words for an applied order.
func issue3945ChipLabel(locale, value string) string {
	if value == chatsearch.OrderOldest {
		return chatsearchText(locale, "oldest")
	}
	return chatsearchText(locale, "newest")
}

const issue3945Styles = `
.chatsearch-order{display:flex;flex-wrap:wrap;align-items:center;gap:.5rem}
.chatsearch-order-label{font-weight:600}
.chatsearch-view .chatsearch-order-option{padding-inline:.875rem;border-radius:999px}
.chatsearch-view .chatsearch-order-option.is-selected{background:var(--accent,var(--hcm-color-brand-primary));border-color:var(--accent,var(--hcm-color-brand-primary));color:var(--on-brand,#fff)}
.chatsearch-order-note{flex:1 1 100%;margin:0;font-size:.8125rem;color:var(--hcm-color-text-muted,var(--hcm-color-text))}
`

// issue3945Restricted counts the applied chips that narrow a search. The order
// only arranges the answer, so it never makes a search "restricted" and
// widening keeps it.
func issue3945Restricted(chips []string) bool {
	for _, chip := range chips {
		if !strings.HasPrefix(strings.ToLower(chip), "sort:") {
			return true
		}
	}
	return false
}
