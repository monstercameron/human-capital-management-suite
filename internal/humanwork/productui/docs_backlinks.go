// Package productui: HUB-035's backlinks panel. It shows every document
// that links to the one open now, read through View.LoadDocumentBacklinks
// (GetDocumentBacklinks), which already leaves out any source the viewer
// cannot also read; nothing here re-derives or widens that authorization.
package productui

import (
	"strconv"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// docsBacklinksTimeout bounds how long the rail says "Loading linked
// documents…" before it offers a retry instead.
const docsBacklinksTimeout = 20 * time.Second

// docsDedupeBacklinks keeps one entry per source document. The read returns
// one row per link, so a document that links here twice (two blocks, or a
// heading and a paragraph) was listed twice under the same title (D-11). The
// first row wins, and a clean state beats a stale or broken one.
func docsDedupeBacklinks(rows []DocumentBacklink) []DocumentBacklink {
	out := make([]DocumentBacklink, 0, len(rows))
	index := map[string]int{}
	for _, row := range rows {
		key := row.SourceDocumentID
		if key == "" {
			out = append(out, row)
			continue
		}
		if at, seen := index[key]; seen {
			if out[at].State != "" && row.State == "" {
				out[at].State = ""
			}
			continue
		}
		index[key] = len(out)
		out = append(out, row)
	}
	return out
}

// docsBacklinksResult is one finished backlinks read for Key: its rows, or
// Failed when the read errored or timed out.
type docsBacklinksResult struct {
	Key    docsFetchKey
	Rows   []DocumentBacklink
	Failed bool
	Done   bool
}

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
		body = html.Div(html.Props{Class: "docs-backlinks-retry"},
			html.P(html.Props{Class: "docs-backlinks-empty", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(locale, "backlinks_unavailable"))),
			html.Button(html.Props{Class: "button secondary", Type: "button", Data: map[string]string{"docs-action": "backlinks-retry"}}, ui.Text(docsText(locale, "compare_versions_retry"))),
		)
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
.docs-backlinks{margin-block-start:var(--hcm-space-3);padding:var(--hcm-space-2);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);background:var(--surface)}
.docs-backlinks h2{margin:0 0 var(--hcm-space-1);font-size:var(--hcm-font-size-small);font-weight:650;color:var(--muted)}
.docs-backlinks-list{list-style:none;margin:0;padding:0;display:grid;gap:var(--hcm-space-1)}
.docs-backlinks-empty{margin:0;color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-backlink-title{color:var(--ink)}
.docs-backlink-state{color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-backlinks-retry{display:grid;justify-items:start;gap:var(--hcm-space-1)}
`
}
