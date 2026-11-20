package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// issue3860Scope prints the words being searched for in full, wrapped, above
// the filter chips, so the query is readable when the page heading truncates it.
func issue3860Scope(locale, text string) ui.Node {
	text = strings.TrimSpace(text)
	lead := map[string]string{"en": "Results for", "de": "Ergebnisse für", "ar": "نتائج"}
	open := map[string]string{"en": "“", "de": "„", "ar": "«"}
	shut := map[string]string{"en": "”", "de": "“", "ar": "»"}
	key := "en"
	if strings.HasPrefix(locale, "de") {
		key = "de"
	} else if strings.HasPrefix(locale, "ar") {
		key = "ar"
	}
	// #3857: a search made of filters alone still has a summary to focus.
	label := lead[key] + " " + open[key] + text + shut[key]
	if text == "" {
		label = map[string]string{"en": "Results", "de": "Ergebnisse", "ar": "النتائج"}[key]
	}
	return html.P(html.Props{ID: Issue3857SummaryID, TabIndex: -1, Class: "chatsearch-scope", Dir: "auto", Role: "status"}, html.Span(html.Props{Text: label}))
}

// issue3860Chip is one removable active filter: a short visible label with a
// close glyph, named for assistive technology by the full "Remove filter: ..."
// sentence. The visible text drops that prefix so the chips stay compact.
func issue3860Chip(prefix, label, chip string, disabled bool) ui.Node {
	return html.Button(html.Props{Type: "button", Class: "chatsearch-chip", Disabled: disabled,
		Aria: map[string]string{"label": label},
		Data: map[string]string{"chatsearch-action": "remove", "extra": chip}},
		html.Span(html.Props{Class: "chatsearch-chip-text", Text: strings.TrimPrefix(label, prefix)}),
		html.Span(html.Props{Class: "chatsearch-chip-x", Aria: map[string]string{"hidden": "true"}, Text: "×"}))
}

const issue3860Styles = `
.chatsearch-scope{margin:0;overflow-wrap:anywhere;word-break:break-word}
.chatsearch-chips{gap:.375rem}
.chatsearch-view .chatsearch-chips button{position:relative;display:inline-flex;align-items:center;gap:.375rem;min-height:32px;max-width:100%;padding:.125rem .625rem;border-radius:999px;font-size:.8125rem;line-height:1.25;text-align:start;white-space:normal;overflow-wrap:anywhere}
.chatsearch-view .chatsearch-chips button::after{content:"";position:absolute;inset-block:-6px;inset-inline:0}
.chatsearch-chip-x{flex:none;font-size:1rem;line-height:1}
`
