package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-009: while a conversation's messages are being read the timeline is a
// skeleton of message rows, not a sentence. A read that takes longer than five
// seconds adds one line saying it is still loading: the line is in the page from
// the start but hidden, and the client sets ChatSlowLoadAttribute on the document
// element once five seconds have passed, which the style rule below reveals.

// ChatSlowLoadAttribute is the attribute the client puts on the document element
// when a load has taken longer than five seconds.
const ChatSlowLoadAttribute = "data-chat-slow-load"

// ChatSlowLoadAfter is how long a load runs before the line appears.
const ChatSlowLoadAfter = 5 * time.Second

// chatux009SkeletonRows is how many message rows the skeleton draws. Five rows
// of the usual height fill a laptop screen without implying a conversation of
// that length.
const chatux009SkeletonRows = 5

// chatux009StillLoadingKey is the copy key of the line shown after five seconds.
const chatux009StillLoadingKey = "chat.ux009.still_loading"

var chatux009En = map[string]string{
	chatux009StillLoadingKey: "Still loading this conversation…",
}

var chatux009De = map[string]string{
	chatux009StillLoadingKey: "Diese Unterhaltung wird noch geladen …",
}

var chatux009Ar = map[string]string{
	chatux009StillLoadingKey: "ما زال تحميل هذه المحادثة جارياً…",
}

// chatux009Text answers from the tables above in en-US, de-DE and ar, and from
// nowhere else, so a key the product catalogue does not hold never reaches a page.
func chatux009Text(m Model, key string) string {
	table := chatux009En
	switch {
	case strings.HasPrefix(strings.ToLower(m.Locale), "de"):
		table = chatux009De
	case strings.HasPrefix(strings.ToLower(m.Locale), "ar"):
		table = chatux009Ar
	}
	return chatbug039Text(key, table[key], chatux009En[key])
}

// chatux009LoadingBody is the timeline while the open conversation is read.
func chatux009LoadingBody(m Model) []ui.Node {
	rows := make([]ui.Node, 0, chatux009SkeletonRows)
	for i := 0; i < chatux009SkeletonRows; i++ {
		lines := []ui.Node{
			html.Span(html.Props{Class: "chat-skeleton chatux009-skel-name"}),
			html.Span(html.Props{Class: "chat-skeleton chatux009-skel-line"}),
		}
		if i%2 == 0 {
			lines = append(lines, html.Span(html.Props{Class: "chat-skeleton chatux009-skel-line short"}))
		}
		rows = append(rows, html.Div(html.Props{Class: "chatux009-skel-row"},
			html.Span(html.Props{Class: "chat-skeleton chatux009-skel-avatar"}),
			html.Div(html.Props{Class: "chatux009-skel-lines"}, lines...)))
	}
	return []ui.Node{html.Div(html.Props{Class: "chatux009-skeleton", Role: "status", Aria: map[string]string{"busy": "true"}},
		html.Span(html.Props{Class: "sr-only", Text: m.t(KeyLoading)}),
		html.Div(html.Props{Class: "chatux009-skel-rows", Aria: map[string]string{"hidden": "true"}}, rows...),
		html.P(html.Props{Class: "chatux009-slow", Text: chatux009Text(m, chatux009StillLoadingKey)}))}
}

// ChatUX009Styles holds the skeleton and the five-second line. The shimmer and
// the colours come from the existing .chat-skeleton rule and the --hcm tokens.
const ChatUX009Styles = `.chatux009-skeleton{display:grid;align-content:start;gap:14px;box-sizing:border-box;inline-size:100%;padding:18px 16px;margin-block-end:auto}` +
	`.message-list:has(>.chatux009-skeleton)::before{flex:0}` +
	`.chatux009-skel-rows{display:grid;gap:20px}` +
	`.chatux009-skel-row{display:grid;grid-template-columns:36px minmax(0,1fr);gap:12px;align-items:start}` +
	`.chatux009-skel-avatar{inline-size:36px;block-size:36px;border-radius:50%}` +
	`.chatux009-skel-lines{display:grid;gap:8px;padding-block-start:3px}` +
	`.chatux009-skel-lines .chat-skeleton{block-size:10px;border-radius:999px}` +
	`.chatux009-skel-name{inline-size:min(32%,140px)}` +
	`.chatux009-skel-line{inline-size:min(88%,520px)}` +
	`.chatux009-skel-line.short{inline-size:min(54%,320px)}` +
	`.chatux009-slow{margin:0;color:var(--muted);font-size:.875rem;visibility:hidden}` +
	`:root[` + ChatSlowLoadAttribute + `] .chat-workspace .chatux009-slow{visibility:visible}`
