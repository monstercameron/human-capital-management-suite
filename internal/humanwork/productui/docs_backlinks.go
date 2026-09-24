// Package productui: HUB-035's backlinks panel. It shows every document
// that links to the one open now, read through View.LoadDocumentBacklinks
// (GetDocumentBacklinks), which already leaves out any source the viewer
// cannot also read; nothing here re-derives or widens that authorization.
package productui

import (
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type docsBacklinksPanelProps struct {
	Locale      string
	Backlinks   []DocumentBacklink
	Loading     bool
	Unavailable bool
}

// docsBacklinkStateLabel names a non-clean link state; a clean link (State
// == "") carries no extra label at all, so the common case reads plainly.
func docsBacklinkStateLabel(locale, state string) string {
	switch state {
	case "broken":
		return docsText(locale, "backlinks_state_broken")
	case "stale", "stale-anchor":
		return docsText(locale, "backlinks_state_stale")
	default:
		return ""
	}
}

// docsBacklinksPanel is the reader's rail section listing every document
// that links here. Loading and an empty, authorized result both render as
// quiet states; Unavailable (the read failed) says so without implying
// there are no backlinks.
func docsBacklinksPanel(props docsBacklinksPanelProps) ui.Node {
	locale := props.Locale
	items := make([]ui.Node, 0, len(props.Backlinks))
	for index, link := range props.Backlinks {
		href := docsDocumentHref(link.SourceDocumentID)
		label := link.SourceTitle
		children := []ui.Node{html.Span(html.Props{Class: "docs-backlink-title"}, ui.Text(label))}
		if state := docsBacklinkStateLabel(locale, link.State); state != "" {
			children = append(children, html.Span(html.Props{Class: "docs-backlink-state"}, ui.Text(" · "+state)))
		}
		items = append(items, html.Li(html.Props{Key: "backlink:" + strconv.Itoa(index) + ":" + link.SourceDocumentID},
			html.A(html.Props{Href: href, Data: map[string]string{"docs-action": "open", "docs-id": href}}, children...)))
	}
	var body ui.Node
	switch {
	case props.Loading:
		body = html.P(html.Props{Class: "docs-backlinks-empty", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(locale, "backlinks_loading")))
	case props.Unavailable:
		body = html.P(html.Props{Class: "docs-backlinks-empty", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(locale, "comments_unavailable")))
	case len(items) == 0:
		body = html.P(html.Props{Class: "docs-backlinks-empty"}, ui.Text(docsText(locale, "backlinks_empty")))
	default:
		body = html.Ul(html.Props{Class: "docs-backlinks-list"}, items...)
	}
	return html.Nav(html.Props{Class: "docs-backlinks", Aria: map[string]string{"labelledby": "docs-backlinks-heading"}},
		html.H2(html.Props{ID: "docs-backlinks-heading"}, ui.Text(docsText(locale, "backlinks_heading"))),
		body,
	)
}

func docsBacklinksStylesheet() string {
	return `
.docs-backlinks{margin-block-start:var(--hcm-space-3)}
.docs-backlinks-list{list-style:none;margin:0;padding:0;display:grid;gap:var(--hcm-space-1)}
.docs-backlinks-empty{color:var(--muted)}
.docs-backlink-state{color:var(--muted);font-size:var(--hcm-font-size-small)}
`
}
