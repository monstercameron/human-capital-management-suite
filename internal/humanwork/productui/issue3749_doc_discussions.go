// Package productui: issue 3749's "Discussed in" rail section. It lists the
// conversations that mention the open document, read through
// View.LoadDocumentDiscussions. The server has already left out every
// conversation, and every message, the viewer may not open, so nothing here
// re-derives or widens that decision, and nothing here says how many were left
// out. An empty answer reads the same whether nothing mentions the document or
// everything that does is locked.
package productui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// DocumentDiscussion is one conversation that mentions the open document, as
// this viewer may see it. Name is empty for a direct message; Href is the
// address of the latest message the viewer may read.
type DocumentDiscussion struct {
	_              incomparable
	ConversationID string
	Name           string
	Direct         bool
	Messages       int
	LatestAt       string
	Href           string
}

// DocumentDiscussions is the answer to one read. Incomplete says a conversation
// could not be checked, so a short list is not taken as the whole list.
type DocumentDiscussions struct {
	Items      []DocumentDiscussion
	Incomplete bool
}

// docsDiscussionsTimeout bounds how long the rail says "Loading…" before it
// offers a retry instead.
const docsDiscussionsTimeout = 20 * time.Second

func init() {
	add := func(locale string, entries map[string]string) {
		if docsLibraryCopy[locale] == nil {
			docsLibraryCopy[locale] = map[string]string{}
		}
		for key, value := range entries {
			docsLibraryCopy[locale][key] = value
		}
	}
	add("en-US", map[string]string{
		"discussions_heading": "Discussed in", "discussions_loading": "Loading conversations…",
		"discussions_unavailable": "Conversations that mention this document could not be loaded.",
		"discussions_empty":       "No conversations mention this document yet.",
		"discussions_incomplete":  "Some conversations could not be checked. Try again to see them.",
		"discussions_direct":      "Direct message", "discussions_open": "Open conversation {name}, {count}",
		"discussions_messages": "{n} messages", "discussions_messages_one": "1 message",
	})
	add("de-DE", map[string]string{
		"discussions_heading": "Besprochen in", "discussions_loading": "Unterhaltungen werden geladen…",
		"discussions_unavailable": "Unterhaltungen, die dieses Dokument erwähnen, konnten nicht geladen werden.",
		"discussions_empty":       "Noch keine Unterhaltung erwähnt dieses Dokument.",
		"discussions_incomplete":  "Einige Unterhaltungen konnten nicht geprüft werden. Versuchen Sie es erneut, um sie zu sehen.",
		"discussions_direct":      "Direktnachricht", "discussions_open": "Unterhaltung {name} öffnen, {count}",
		"discussions_messages": "{n} Nachrichten", "discussions_messages_one": "1 Nachricht",
	})
	add("ar", map[string]string{
		"discussions_heading": "نوقش في", "discussions_loading": "جارٍ تحميل المحادثات…",
		"discussions_unavailable": "تعذر تحميل المحادثات التي تشير إلى هذا المستند.",
		"discussions_empty":       "لا توجد محادثات تشير إلى هذا المستند بعد.",
		"discussions_incomplete":  "تعذر فحص بعض المحادثات. حاول مرة أخرى لعرضها.",
		"discussions_direct":      "رسالة مباشرة", "discussions_open": "فتح المحادثة {name}، {count}",
		"discussions_messages": "{n} رسائل", "discussions_messages_one": "رسالة واحدة",
	})
}

type docsDiscussionsPanelProps struct {
	Locale      string
	Discussions []DocumentDiscussion
	Loading     bool
	Failed      bool
	Incomplete  bool
	Retry       func()
}

// docsDiscussionsLinkable keeps a link inside Chat on this origin.
func docsDiscussionsLinkable(href string) bool {
	return strings.HasPrefix(href, "/workspace/app/chat") && !strings.HasPrefix(href, "//")
}

// docsDiscussionsPanel is the rail section. Loading and an empty, authorized
// result are quiet states; Failed says so without implying nothing mentions
// the document, and offers a retry.
func docsDiscussionsPanel(props docsDiscussionsPanelProps) ui.Node {
	locale := props.Locale
	retry := ui.UseEvent(func(ui.MouseEvent) {
		if props.Retry != nil {
			props.Retry()
		}
	})
	items := make([]ui.Node, 0, len(props.Discussions))
	for _, item := range props.Discussions {
		if !docsDiscussionsLinkable(item.Href) {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if item.Direct || name == "" {
			name = docsText(locale, "discussions_direct")
		}
		count := docsCount(locale, "discussions_messages", item.Messages)
		label := strings.NewReplacer("{name}", name, "{count}", count).Replace(docsText(locale, "discussions_open"))
		items = append(items, html.Li(html.Props{Key: "discussion:" + item.ConversationID},
			html.A(html.Props{Class: "docs-discussion-link", Href: item.Href, Aria: map[string]string{"label": label}},
				html.Span(html.Props{Class: "docs-discussion-name"}, ui.Text(name)),
				html.Span(html.Props{Class: "docs-discussion-count"}, ui.Text(count)),
			)))
	}
	var body ui.Node
	switch {
	case props.Loading:
		body = html.P(html.Props{Class: "docs-discussions-empty", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(locale, "discussions_loading")))
	case props.Failed:
		body = html.Div(html.Props{Class: "docs-discussions-retry"},
			html.P(html.Props{Class: "docs-discussions-empty", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(locale, "discussions_unavailable"))),
			html.Button(html.Props{Class: "button secondary", Type: "button", OnClick: retry}, ui.Text(docsText(locale, "compare_versions_retry"))),
		)
	case len(items) == 0 && !props.Incomplete:
		body = html.P(html.Props{Class: "docs-discussions-empty"}, ui.Text(docsText(locale, "discussions_empty")))
	default:
		children := []ui.Node{}
		if len(items) > 0 {
			children = append(children, html.Ul(html.Props{Class: "docs-discussions-list", Role: "list"}, items...))
		}
		if props.Incomplete {
			children = append(children,
				html.P(html.Props{Class: "docs-discussions-empty", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(locale, "discussions_incomplete"))),
				html.Button(html.Props{Class: "button secondary", Type: "button", OnClick: retry}, ui.Text(docsText(locale, "compare_versions_retry"))))
		}
		body = html.Div(html.Props{Class: "docs-discussions-body"}, children...)
	}
	return html.Nav(html.Props{Class: "docs-discussions", Aria: map[string]string{"labelledby": "docs-discussions-heading"}},
		html.H2(html.Props{ID: "docs-discussions-heading"}, ui.Text(docsText(locale, "discussions_heading"))),
		body,
	)
}

type docsDiscussionsRead struct {
	Key        docsFetchKey
	Items      []DocumentDiscussion
	Incomplete bool
	Failed     bool
	Done       bool
}

type docsDiscussionsSectionProps struct {
	Locale     string
	DocumentID string
	Load       func(documentID string, done func(DocumentDiscussions, error))
}

// docsDiscussionsSection reads the list for one document and renders the
// panel. It owns its read, keyed by document and attempt, so a reply for an
// earlier document is dropped and the rail shows loading until the current
// key lands. Replies arrive off the frame loop and apply through ui.PostAsync.
func docsDiscussionsSection(props docsDiscussionsSectionProps) ui.Node {
	attempt := ui.UseState(0)
	result := ui.UseState(docsDiscussionsRead{})
	wanted := ui.UseRef(docsFetchKey{})
	key := docsFetchKey{props.DocumentID, attempt.Get()}
	ui.UseEffectOf(func() func() {
		if props.Load == nil {
			return nil
		}
		wanted.Set(key)
		settle := func(read docsDiscussionsRead) {
			ui.PostAsync(func() {
				if wanted.Get() != key {
					return
				}
				if current := result.Get(); current.Key == key && current.Done {
					return
				}
				read.Key, read.Done = key, true
				result.Set(read)
			})
		}
		timeout := time.AfterFunc(docsDiscussionsTimeout, func() { settle(docsDiscussionsRead{Failed: true}) })
		props.Load(key.ID, func(answer DocumentDiscussions, err error) {
			timeout.Stop()
			if err != nil {
				settle(docsDiscussionsRead{Failed: true})
				return
			}
			settle(docsDiscussionsRead{Items: answer.Items, Incomplete: answer.Incomplete})
		})
		return func() { timeout.Stop() }
	}, key)
	shown := result.Get()
	done := shown.Key == key && shown.Done
	if !done {
		shown = docsDiscussionsRead{}
	}
	return ui.CreateElement(docsDiscussionsPanel, docsDiscussionsPanelProps{
		Locale: props.Locale, Discussions: shown.Items, Loading: !done, Failed: done && shown.Failed, Incomplete: done && shown.Incomplete,
		Retry: func() { attempt.Set(attempt.Get() + 1) },
	})
}

func docsDiscussionsStylesheet() string {
	return `
.docs-discussions{margin-block-start:var(--hcm-space-3);padding:var(--hcm-space-2);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);background:var(--surface)}
.docs-discussions h2{margin:0 0 var(--hcm-space-1);font-size:var(--hcm-font-size-small);font-weight:650;color:var(--muted)}
.docs-discussions-list{list-style:none;margin:0;padding:0;display:grid;gap:var(--hcm-space-1)}
.docs-discussions-body{display:grid;justify-items:start;gap:var(--hcm-space-1)}
.docs-discussions-list{inline-size:100%}
.docs-discussion-link{display:flex;flex-wrap:wrap;align-items:baseline;justify-content:space-between;gap:var(--hcm-space-1);min-block-size:44px;padding-block:var(--hcm-space-1);color:var(--ink);text-decoration:none;overflow-wrap:anywhere}
.docs-discussion-link:hover .docs-discussion-name,.docs-discussion-link:focus-visible .docs-discussion-name{text-decoration:underline}
.docs-discussion-count{color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-discussions-empty{margin:0;color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-discussions-retry{display:grid;justify-items:start;gap:var(--hcm-space-1)}
@media (prefers-reduced-motion:no-preference){.docs-discussion-link{transition:background-color .12s}}
`
}
