// Package productui: HUB-033's version-compare flow. The compare dialog
// never edits: it reads two already-immutable versions side by side, the
// version currently open (already authorized and loaded by the page) and
// one more version ID the reader names, read through
// View.CompareDocumentVersions (GetDocumentVersion). A version the reader
// may not open reports only that it could not be loaded, never its bytes.
package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// docsCompareState is the compare dialog's fetch outcome for the second
// (named) version.
type docsCompareState struct {
	Loading, Failed, Loaded bool
	Version                 DocumentVersionProjection
}

type docsCompareDialogProps struct {
	Locale                  string
	DocumentID              string
	Base                    DocumentVersionProjection
	CompareDocumentVersions func(documentID, versionID string, done func(DocumentVersionProjection, error))
	Close                   func()
}

// docsCompareDialog lets the reader name any other immutable version of the
// open document and see it beside the version they are reading now. Both
// sides are read-only Markdown; there is no editing surface anywhere in
// this dialog, so a compare can never touch deployed bytes.
func docsCompareDialog(props docsCompareDialogProps) ui.Node {
	locale := props.Locale
	draft := ui.UseState("")
	state := ui.UseState(docsCompareState{})
	close := ui.UseEvent(func(ui.MouseEvent) { props.Close() })
	ui.UseEffect(func() func() { return docsListenEscape(props.Close) })
	useDocsModal(true, "docs-compare-dialog", "#docs-compare-input", docsDialogReturnFallbacks...)
	keydown := ui.UseEvent(func(ui.KeyboardEvent) {})
	input := ui.UseEvent(func(event ui.InputEvent) { draft.Set(event.GetValue()) })
	submit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		id := strings.TrimSpace(draft.Get())
		if id == "" || props.CompareDocumentVersions == nil || state.Get().Loading {
			return
		}
		state.Set(docsCompareState{Loading: true})
		props.CompareDocumentVersions(props.DocumentID, id, func(version DocumentVersionProjection, err error) {
			if err != nil || !version.Readable {
				state.Set(docsCompareState{Failed: true})
				return
			}
			state.Set(docsCompareState{Loaded: true, Version: version})
		})
	})
	text := func(key string) string { return docsText(locale, key) }
	body := []ui.Node{
		html.Form(html.Props{Class: "docs-compare-form", OnSubmit: submit},
			html.Label(html.Props{For: "docs-compare-input"}, ui.Text(text("version"))),
			html.Input(html.Props{ID: "docs-compare-input", Type: "text", Value: draft.Get(), OnInput: input, Raw: map[string]any{"dir": "ltr", "autocomplete": "off"}}),
			html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: state.Get().Loading}, ui.Text(text("compare_action"))),
		),
	}
	current := state.Get()
	switch {
	case current.Loading:
		body = append(body, html.P(html.Props{Class: "docs-compare-status", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(text("compare_loading"))))
	case current.Failed:
		body = append(body, html.P(html.Props{Class: "docs-compare-status is-alert", Raw: map[string]any{"role": "alert"}}, ui.Text(text("compare_failed"))))
	case current.Loaded:
		body = append(body, html.Div(html.Props{Class: "docs-compare", Aria: map[string]string{"label": text("compare_heading")}},
			docsCompareColumn(props.Locale, text("compare_base"), props.Base),
			docsCompareColumn(props.Locale, text("compare_other"), current.Version),
		))
	}
	return docsDialog("docs-compare-dialog", text("compare_heading"), text("compare_close"), close, keydown, body...)
}

// docsCompareColumn renders one immutable version's title and Markdown.
// Nothing here is editable: this is the reader's plain-text view
// (docsASTMarkdownNodes), the same renderer the open document uses.
func docsCompareColumn(locale, heading string, version DocumentVersionProjection) ui.Node {
	view := View{Locale: LocaleContext{Resolved: locale}}
	return html.Section(html.Props{Class: "docs-compare-side", Aria: map[string]string{"label": heading + ": " + version.Title}},
		html.H3(html.Props{Class: "docs-compare-side-head"}, ui.Text(heading), html.Span(html.Props{Class: "docs-compare-side-title"}, ui.Text(" — "+version.Title))),
		html.Div(html.Props{Class: "docs-markdown docs-compare-body", Dir: docsContentDirection(version.Markdown)}, docsASTMarkdownNodes(view, version.Markdown)...),
	)
}

// docsCompareStylesheet lays the two versions side by side above 40rem and
// stacks them below it, so the compare view stays legible at desktop and
// narrow widths alike (HUB-033).
func docsCompareStylesheet() string {
	return `
.docs-compare-form{display:flex;flex-wrap:wrap;gap:var(--hcm-space-2);align-items:end;margin-block-end:var(--hcm-space-2)}
.docs-compare-form label{font-weight:600}
.docs-compare-form input{min-width:16rem;min-height:var(--hcm-control-height);padding:var(--hcm-space-1) var(--hcm-space-2);font:inherit;color:var(--ink);background:var(--surface);border:1px solid var(--control-border);border-radius:var(--hcm-radius-control)}
.docs-compare-status{margin:var(--hcm-space-2) 0}
.docs-compare-status.is-alert{color:var(--hcm-color-danger,#7a1f1f)}
.docs-compare{display:grid;grid-template-columns:1fr 1fr;gap:var(--hcm-space-3)}
.docs-compare-side{min-width:0;padding:var(--hcm-space-2);border:1px solid var(--line);border-radius:var(--hcm-radius-control)}
.docs-compare-side-head{margin:0 0 var(--hcm-space-2);font-size:var(--hcm-font-size-body)}
.docs-compare-side-title{font-weight:400;color:var(--muted)}
.docs-compare-body{max-height:32rem;overflow:auto}
@media (max-width:40rem){.docs-compare{grid-template-columns:1fr}}
`
}
