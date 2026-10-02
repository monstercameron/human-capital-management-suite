package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-037: one loading component for every Chat surface. A surface draws its
// own frame (heading, tabs, filters, disabled) and puts ChatLoadingFrame where
// its rows will arrive: placeholder rows shaped like those rows, a status that
// is announced once and never printed as page text, and after eight seconds a
// line that says it is taking longer than usual with Try again. A failed read
// uses ChatLoadFailed in the same place; an empty answer uses ChatEmptyState.

// ChatLoadingSlowAfterSeconds is how long a load runs before the slow line shows.
// The line is in the page from the start but hidden, and a style rule with this
// delay reveals it, so the wait needs no timer in the client.
const ChatLoadingSlowAfterSeconds = 8

// LoadingShape is the shape of the rows the surface will show.
type LoadingShape string

const (
	// LoadingShapeQueue is a moderation row: avatar, two text bars, an action.
	LoadingShapeQueue LoadingShape = "queue"
	// LoadingShapeMessage is a message: avatar, a name bar and two text bars.
	LoadingShapeMessage LoadingShape = "message"
	// LoadingShapeList is a result row: a small tile and three bars.
	LoadingShapeList LoadingShape = "list"
	// LoadingShapePeople is a person row: avatar, a name bar and a short bar.
	LoadingShapePeople LoadingShape = "people"
	// LoadingShapeChannels is a channel row: a tile, a name bar and a short bar.
	LoadingShapeChannels LoadingShape = "channels"
	// LoadingShapeMenu is a menu row: a small avatar and one bar.
	LoadingShapeMenu LoadingShape = "menu"
	// LoadingShapeSection is a panel section: a heading bar and two text bars.
	LoadingShapeSection LoadingShape = "section"
	// LoadingShapeCard is a card: a title bar, two text bars and a button.
	LoadingShapeCard LoadingShape = "card"
)

// LoadingFrame describes one loading placeholder.
type LoadingFrame struct {
	Locale string
	Shape  LoadingShape
	// Rows is how many placeholder rows to draw; 0 means the shape's usual count.
	Rows int
	// Status is the accessible status ("Loading moderation items"). It is read by
	// assistive technology once and is not drawn.
	Status string
	// RetryData are the data attributes of the surface's own Try again handling;
	// OnRetry is the click handler for a surface that has one instead. With
	// neither, the slow line is drawn without a button.
	RetryData map[string]string
	OnRetry   *ui.Handler
}

var chatux037En = map[string]string{
	"slow":  "This is taking longer than usual.",
	"retry": "Try again",
}

var chatux037De = map[string]string{
	"slow":  "Das dauert länger als gewöhnlich.",
	"retry": "Erneut versuchen",
}

var chatux037Ar = map[string]string{
	"slow":  "يستغرق هذا وقتاً أطول من المعتاد.",
	"retry": "حاول مجدداً",
}

// ChatLoadingText answers from the tables above in en-US, de-DE and ar.
func ChatLoadingText(locale, key string) string {
	table := chatux037En
	switch {
	case strings.HasPrefix(strings.ToLower(locale), "de"):
		table = chatux037De
	case strings.HasPrefix(strings.ToLower(locale), "ar"):
		table = chatux037Ar
	}
	return chatbug039Text(key, table[key], chatux037En[key])
}

func chatux037Rows(shape LoadingShape) int {
	switch shape {
	case LoadingShapeMenu:
		return 3
	case LoadingShapeCard:
		return 3
	case LoadingShapeSection:
		return 3
	case LoadingShapeQueue:
		return 4
	}
	return 4
}

// chatux037Click is the handler a surface gave, or none.
func chatux037Click(h *ui.Handler) ui.Handler {
	if h == nil {
		return ui.Handler{}
	}
	return *h
}

func chatux037Bar(class string) ui.Node {
	return html.Span(html.Props{Class: "chat-skeleton chatux037-bar " + class})
}

func chatux037Row(shape LoadingShape, i int) ui.Node {
	switch shape {
	case LoadingShapeQueue:
		return html.Div(html.Props{Class: "chatux037-row chatux037-queue"},
			chatux037Bar("chatux037-avatar"),
			html.Div(html.Props{Class: "chatux037-lines"}, chatux037Bar("w40"), chatux037Bar("w90"), chatux037Bar("chatux037-action")))
	case LoadingShapeList:
		return html.Div(html.Props{Class: "chatux037-row chatux037-list"},
			chatux037Bar("chatux037-tile"),
			html.Div(html.Props{Class: "chatux037-lines"}, chatux037Bar("w40"), chatux037Bar("w90"), chatux037Bar("w60")))
	case LoadingShapePeople:
		return html.Div(html.Props{Class: "chatux037-row chatux037-people"},
			chatux037Bar("chatux037-avatar small"),
			html.Div(html.Props{Class: "chatux037-lines"}, chatux037Bar("w40"), chatux037Bar("w60")))
	case LoadingShapeChannels:
		return html.Div(html.Props{Class: "chatux037-row chatux037-channels"},
			chatux037Bar("chatux037-tile small"),
			html.Div(html.Props{Class: "chatux037-lines"}, chatux037Bar("w40"), chatux037Bar("w75")))
	case LoadingShapeMenu:
		return html.Div(html.Props{Class: "chatux037-row chatux037-menu"},
			chatux037Bar("chatux037-avatar tiny"), chatux037Bar("w60"))
	case LoadingShapeSection:
		return html.Div(html.Props{Class: "chatux037-section"},
			chatux037Bar("w30 tall"), chatux037Bar("w90"), chatux037Bar("w60"))
	case LoadingShapeCard:
		return html.Div(html.Props{Class: "chatux037-card"},
			chatux037Bar("w40 tall"), chatux037Bar("w90"), chatux037Bar("w75"), chatux037Bar("chatux037-action"))
	}
	lines := []ui.Node{chatux037Bar("w30"), chatux037Bar("w90")}
	if i%2 == 0 {
		lines = append(lines, chatux037Bar("w60"))
	}
	return html.Div(html.Props{Class: "chatux037-row chatux037-message"},
		chatux037Bar("chatux037-avatar"), html.Div(html.Props{Class: "chatux037-lines"}, lines...))
}

// ChatLoadingFrame is the placeholder every Chat surface draws while it reads.
func ChatLoadingFrame(f LoadingFrame) ui.Node {
	if f.Shape == "" {
		f.Shape = LoadingShapeMessage
	}
	count := f.Rows
	if count <= 0 {
		count = chatux037Rows(f.Shape)
	}
	rows := make([]ui.Node, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, chatux037Row(f.Shape, i))
	}
	slow := []ui.Node{html.Span(html.Props{Text: ChatLoadingText(f.Locale, "slow")})}
	if f.RetryData != nil || f.OnRetry != nil {
		slow = append(slow, html.Button(html.Props{Type: "button", Class: "chatux037-retry", Text: ChatLoadingText(f.Locale, "retry"), Data: f.RetryData, OnClick: chatux037Click(f.OnRetry)}))
	}
	children := []ui.Node{}
	if f.Status != "" {
		children = append(children, html.Span(html.Props{Class: "sr-only", Role: "status", Text: f.Status}))
	}
	children = append(children,
		html.Div(html.Props{Class: "chatux037-rows", Aria: map[string]string{"hidden": "true"}}, rows...),
		html.Div(html.Props{Class: "chatux037-slow", Role: "status"}, slow...))
	return html.Div(html.Props{Class: "chatux037-loading", Dir: direction(f.Locale), Data: map[string]string{"chat-loading": string(f.Shape)}, Aria: map[string]string{"busy": "true"}}, children...)
}

// ChatLoadFailed is what a surface shows in place of its rows when the read
// failed: what failed, and Try again.
func ChatLoadFailed(locale, what string, retryData map[string]string, onRetry *ui.Handler) ui.Node {
	children := []ui.Node{html.P(html.Props{Class: "chatux037-failed-text", Dir: "auto", Text: what})}
	if retryData != nil || onRetry != nil {
		children = append(children, html.Button(html.Props{Type: "button", Class: "chatux037-retry", Text: ChatLoadingText(locale, "retry"), Data: retryData, OnClick: chatux037Click(onRetry)}))
	}
	return html.Div(html.Props{Class: "chatux037-failed", Role: "alert", Dir: direction(locale), Data: map[string]string{"chat-load-failed": "true"}}, children...)
}

// ChatEmptyState is the empty answer of a surface: a centred icon, what there
// is nothing of, and one line about it. It is the empty state Saved draws.
func ChatEmptyState(glyph, title, hint string) ui.Node {
	children := []ui.Node{chatsave002Icon(glyph, false), html.P(html.Props{Class: "chatux037-empty-title", Text: title})}
	if hint != "" {
		children = append(children, html.P(html.Props{Class: "chatux037-empty-hint", Text: hint}))
	}
	return html.Div(html.Props{Class: "chatsave-empty chatux037-empty", Role: "status"}, children...)
}

// ChatLoadingMarkup is the placeholder as markup, for the browser client to put
// in a region it fills from the server.
func ChatLoadingMarkup(f LoadingFrame) string {
	markup, err := ui.RenderToString(ChatLoadingFrame(f))
	if err != nil {
		return ""
	}
	return markup
}

// ChatUX037Styles holds the placeholder rows, the slow line and the failed and
// empty states. The shimmer is the existing .chat-skeleton rule, which uses the
// motion tokens and stops under reduced motion; the slow line is a visibility
// step, not motion, so it is kept when animation is turned off.
const ChatUX037Styles = `.chatux037-loading{display:grid;align-content:start;gap:14px;box-sizing:border-box;inline-size:100%;padding:16px}` +
	`.chatux037-rows{display:grid;gap:18px}` +
	`.chatux037-row{display:grid;gap:12px;align-items:start;min-inline-size:0}` +
	`.chatux037-queue,.chatux037-message{grid-template-columns:36px minmax(0,1fr)}.chatux037-list{grid-template-columns:28px minmax(0,1fr)}` +
	`.chatux037-people{grid-template-columns:32px minmax(0,1fr);align-items:center}.chatux037-channels{grid-template-columns:20px minmax(0,1fr);align-items:center}` +
	`.chatux037-menu{grid-template-columns:24px minmax(0,1fr);align-items:center;padding-block:4px}` +
	`.chatux037-lines{display:grid;gap:8px;min-inline-size:0;padding-block-start:3px}` +
	`.chatux037-bar{block-size:10px;border-radius:999px}` +
	`.chatux037-bar.tall{block-size:14px}` +
	`.chatux037-bar.w30{inline-size:30%}.chatux037-bar.w40{inline-size:40%}.chatux037-bar.w60{inline-size:60%}.chatux037-bar.w75{inline-size:75%}.chatux037-bar.w90{inline-size:90%}` +
	`.chatux037-bar.chatux037-avatar{inline-size:36px;block-size:36px;border-radius:50%}.chatux037-bar.chatux037-avatar.small{inline-size:32px;block-size:32px}.chatux037-bar.chatux037-avatar.tiny{inline-size:24px;block-size:24px}` +
	`.chatux037-bar.chatux037-tile{inline-size:28px;block-size:28px;border-radius:var(--hcm-radius-control,6px)}.chatux037-bar.chatux037-tile.small{inline-size:20px;block-size:20px}` +
	`.chatux037-bar.chatux037-action{inline-size:72px;block-size:28px;border-radius:var(--hcm-radius-control,6px);margin-block-start:4px}` +
	`.chatux037-section{display:grid;gap:8px;padding-block:8px}` +
	`.chatux037-card{display:grid;gap:10px;padding:16px;border:1px solid var(--line,#ddd);border-radius:var(--hcm-radius-surface,8px)}` +
	`.chatux037-slow{display:flex;flex-wrap:wrap;align-items:center;gap:12px;margin:0;color:var(--muted);font-size:.875rem;visibility:hidden;opacity:0;animation:chatux037-reveal 1ms linear 8s forwards}` +
	`:root:root .chatux037-slow.chatux037-slow{animation:chatux037-reveal 1ms linear 8s forwards!important}` +
	`@keyframes chatux037-reveal{to{visibility:visible;opacity:1}}` +
	`.chatux037-retry{min-block-size:32px;padding:4px 12px;border:1px solid var(--line,#ccc);border-radius:var(--hcm-radius-control,6px);background:var(--surface,transparent);color:inherit;font:inherit;font-size:.875rem;cursor:pointer}` +
	`.chatux037-failed{display:grid;justify-items:start;gap:10px;padding:16px}.chatux037-failed-text{margin:0}` +
	`.chatux037-empty{padding-block:48px}.chatux037-empty-title{font-weight:650;color:var(--ink,inherit)}.chatux037-empty .chatux037-empty-hint{margin:0;color:var(--muted)}` +
	`.mention-agent-state .chatux037-loading{padding:4px 8px;gap:6px}.mention-agent-state .chatux037-rows{gap:6px}.chatux037-loading .chatux037-slow{margin:0}` +
	`.chatmod005-tab:disabled{opacity:.55;cursor:default}`
