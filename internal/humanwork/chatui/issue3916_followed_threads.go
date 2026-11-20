package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3916: a place to find and manage followed threads.
//
// The Followed threads row sits above the conversation list, next to Saved. It
// opens a panel at the top of the list with every thread the person follows:
// the conversation, the root message, the replies and the latest activity. Each
// row opens the thread in its conversation, and Stop following turns off the
// same follow the thread header's "Notify me about replies" button controls, so
// the header and the list always agree (they read and write one stored row).
//
// The list is the person's own. It is read from the server, which lists only
// threads in conversations the person can open now, so a thread whose
// conversation access was lost is not shown, counted or previewed. The panel
// never marks anything read, never says a reply is owed and never nudges anyone.

// FollowedThreadRow is one followed thread as the panel draws it.
type FollowedThreadRow struct {
	ConversationID, RootPostID string
	// Channel is the conversation's name as the rail shows it; InChannel says
	// to write it as "#name".
	Channel   string
	InChannel bool
	Author    string
	// Body is the root message preview; Removed says the root was deleted.
	Body    string
	Removed bool
	Replies int
	// ActivityLabel is the latest activity in the reader's own words ("Today,
	// 3:04 PM"); Sequence is the root's place in the conversation.
	ActivityLabel string
	Sequence      uint64
}

const (
	issue3916Toggle   = "followed-toggle"
	issue3916Open     = "followed-open"
	issue3916Stop     = "followed-stop"
	issue3916Retry    = "followed-retry"
	issue3916ToggleID = "issue3916-toggle"
	issue3916HeadID   = "issue3916-heading"

	keyIssue3916Label     = "chat.issue3916.label"
	keyIssue3916Hint      = "chat.issue3916.hint"
	keyIssue3916Count     = "chat.issue3916.count"
	keyIssue3916Heading   = "chat.issue3916.heading"
	keyIssue3916Loading   = "chat.issue3916.loading"
	keyIssue3916Empty     = "chat.issue3916.empty"
	keyIssue3916Error     = "chat.issue3916.error"
	keyIssue3916Retry     = "chat.issue3916.retry"
	keyIssue3916Stop      = "chat.issue3916.stop"
	keyIssue3916StopAria  = "chat.issue3916.stop_aria"
	keyIssue3916OpenAria  = "chat.issue3916.open_aria"
	keyIssue3916ReplyNone = "chat.issue3916.reply_none"
	keyIssue3916ReplyOne  = "chat.issue3916.reply_one"
	keyIssue3916ReplyMany = "chat.issue3916.reply_many"
	keyIssue3916Removed   = "chat.issue3916.removed"
	keyIssue3916Someone   = "chat.issue3916.someone"
	keyIssue3916Somewhere = "chat.issue3916.somewhere"
	keyIssue3916Failed    = "chat.issue3916.failed"
	keyIssue3916Shown     = "chat.issue3916.shown"
)

var issue3916Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3916Label:     "Followed threads",
		keyIssue3916Hint:      "Find the threads you follow, open one or stop following it",
		keyIssue3916Count:     "{n} followed",
		keyIssue3916Heading:   "Followed threads",
		keyIssue3916Loading:   "Loading the threads you follow",
		keyIssue3916Empty:     "You are not following any threads yet. To follow one, open a thread and choose Notify me about replies.",
		keyIssue3916Error:     "Your followed threads could not be loaded.",
		keyIssue3916Retry:     "Try again",
		keyIssue3916Stop:      "Stop following",
		keyIssue3916StopAria:  "Stop following the thread by {author} in {channel}",
		keyIssue3916OpenAria:  "Open the thread by {author} in {channel}: {preview}. {replies}. Latest activity {when}",
		keyIssue3916ReplyNone: "No replies yet",
		keyIssue3916ReplyOne:  "1 reply",
		keyIssue3916ReplyMany: "{n} replies",
		keyIssue3916Removed:   "The original message was deleted",
		keyIssue3916Someone:   "Someone",
		keyIssue3916Somewhere: "a conversation",
		keyIssue3916Failed:    "That thread could not be unfollowed. Try again.",
		keyIssue3916Shown:     "Followed threads: {n}",
	},
	"de-DE": {
		keyIssue3916Label:     "Verfolgte Threads",
		keyIssue3916Hint:      "Verfolgte Threads finden, einen öffnen oder nicht mehr verfolgen",
		keyIssue3916Count:     "{n} verfolgt",
		keyIssue3916Heading:   "Verfolgte Threads",
		keyIssue3916Loading:   "Die von Ihnen verfolgten Threads werden geladen",
		keyIssue3916Empty:     "Sie verfolgen noch keine Threads. Öffnen Sie dazu einen Thread und wählen Sie „Über Antworten benachrichtigen“.",
		keyIssue3916Error:     "Ihre verfolgten Threads konnten nicht geladen werden.",
		keyIssue3916Retry:     "Erneut versuchen",
		keyIssue3916Stop:      "Nicht mehr verfolgen",
		keyIssue3916StopAria:  "Den Thread von {author} in {channel} nicht mehr verfolgen",
		keyIssue3916OpenAria:  "Den Thread von {author} in {channel} öffnen: {preview}. {replies}. Letzte Aktivität {when}",
		keyIssue3916ReplyNone: "Noch keine Antworten",
		keyIssue3916ReplyOne:  "1 Antwort",
		keyIssue3916ReplyMany: "{n} Antworten",
		keyIssue3916Removed:   "Die ursprüngliche Nachricht wurde gelöscht",
		keyIssue3916Someone:   "Jemand",
		keyIssue3916Somewhere: "einer Unterhaltung",
		keyIssue3916Failed:    "Dieser Thread konnte nicht entfolgt werden. Versuchen Sie es erneut.",
		keyIssue3916Shown:     "Verfolgte Threads: {n}",
	},
	"ar": {
		keyIssue3916Label:     "السلاسل المتابَعة",
		keyIssue3916Hint:      "اعثر على السلاسل التي تتابعها، وافتح إحداها أو أوقف متابعتها",
		keyIssue3916Count:     "{n} متابَعة",
		keyIssue3916Heading:   "السلاسل المتابَعة",
		keyIssue3916Loading:   "جارٍ تحميل السلاسل التي تتابعها",
		keyIssue3916Empty:     "لا تتابع أي سلاسل بعد. لمتابعة سلسلة، افتحها واختر «أبلغني بالردود».",
		keyIssue3916Error:     "تعذر تحميل السلاسل التي تتابعها.",
		keyIssue3916Retry:     "حاول مرة أخرى",
		keyIssue3916Stop:      "إيقاف المتابعة",
		keyIssue3916StopAria:  "إيقاف متابعة سلسلة {author} في {channel}",
		keyIssue3916OpenAria:  "فتح سلسلة {author} في {channel}: {preview}. {replies}. آخر نشاط {when}",
		keyIssue3916ReplyNone: "لا توجد ردود بعد",
		keyIssue3916ReplyOne:  "رد واحد",
		keyIssue3916ReplyMany: "{n} ردود",
		keyIssue3916Removed:   "حُذفت الرسالة الأصلية",
		keyIssue3916Someone:   "شخص ما",
		keyIssue3916Somewhere: "محادثة",
		keyIssue3916Failed:    "تعذر إيقاف متابعة تلك السلسلة. حاول مرة أخرى.",
		keyIssue3916Shown:     "السلاسل المتابَعة: {n}",
	},
}

func issue3916Replies(m Model, n int) string {
	switch {
	case n <= 0:
		return laneText(m, issue3916Copy, keyIssue3916ReplyNone)
	case n == 1:
		return laneText(m, issue3916Copy, keyIssue3916ReplyOne)
	}
	return laneTextf(m, issue3916Copy, keyIssue3916ReplyMany, map[string]string{"n": m.n(n)})
}

func issue3916Channel(m Model, row FollowedThreadRow) string {
	name := strings.TrimSpace(row.Channel)
	switch {
	case name == "":
		return laneText(m, issue3916Copy, keyIssue3916Somewhere)
	case row.InChannel:
		return "#" + name
	}
	return name
}

func issue3916Author(m Model, row FollowedThreadRow) string {
	if a := strings.TrimSpace(row.Author); a != "" {
		return a
	}
	return laneText(m, issue3916Copy, keyIssue3916Someone)
}

func issue3916Preview(m Model, row FollowedThreadRow) string {
	if row.Removed {
		return laneText(m, issue3916Copy, keyIssue3916Removed)
	}
	return strings.Join(strings.Fields(row.Body), " ")
}

// issue3916Control is the Followed threads row above the list. It is a
// disclosure button: it names the panel it controls and whether it is open, and
// carries the count once the list has loaded.
func issue3916Control(m Model, h handlers) ui.Node {
	on := h.local.followedOpen
	label := laneText(m, issue3916Copy, keyIssue3916Label)
	name := label
	children := []ui.Node{icon("bell"), html.Span(html.Props{Class: "chat-row-name", Text: label})}
	if m.FollowedLoaded && !m.FollowedError {
		count := laneTextf(m, issue3916Copy, keyIssue3916Count, map[string]string{"n": m.n(len(m.FollowedThreads))})
		name += ", " + count
		if len(m.FollowedThreads) > 0 {
			children = append(children, html.Span(html.Props{Class: "chat-count issue3916-count", Aria: map[string]string{"hidden": "true"}, Text: m.n(len(m.FollowedThreads))}))
		}
	}
	class := "chat-row issue3916-row"
	if on {
		class += " on"
	}
	return html.Div(html.Props{Class: "issue3916-control"},
		html.Button(html.Props{ID: issue3916ToggleID, Class: class, Type: "button", Title: laneText(m, issue3916Copy, keyIssue3916Hint),
			Data: map[string]string{"action": issue3916Toggle},
			Aria: map[string]string{"expanded": boolString(on), "controls": "issue3916-panel", "label": name}}, children...))
}

// issue3916Panel is the open list: a labelled region with the heading, a polite
// status line, and the loading, empty, unavailable or populated body.
func issue3916Panel(m Model) ui.Node {
	heading := html.H2(html.Props{ID: issue3916HeadID, Class: "issue3916-heading", Text: laneText(m, issue3916Copy, keyIssue3916Heading), Raw: map[string]any{"tabindex": "-1"}})
	status := ""
	var body ui.Node
	switch {
	case m.FollowedLoading && !m.FollowedLoaded:
		body = html.P(html.Props{Class: "rail-empty issue3916-note", Role: "status", Text: laneText(m, issue3916Copy, keyIssue3916Loading)})
	case m.FollowedError && !m.FollowedLoaded:
		body = html.Div(html.Props{Class: "issue3916-note", Role: "alert"},
			html.P(html.Props{Class: "rail-empty", Text: laneText(m, issue3916Copy, keyIssue3916Error)}),
			html.Button(html.Props{Class: "button secondary small issue3916-retry", Type: "button", Data: map[string]string{"action": issue3916Retry}, Text: laneText(m, issue3916Copy, keyIssue3916Retry)}))
	case len(m.FollowedThreads) == 0:
		body = html.P(html.Props{Class: "rail-empty issue3916-note", Role: "status", Text: laneText(m, issue3916Copy, keyIssue3916Empty)})
	default:
		status = laneTextf(m, issue3916Copy, keyIssue3916Shown, map[string]string{"n": m.n(len(m.FollowedThreads))})
		items := make([]ui.Node, 0, len(m.FollowedThreads))
		for _, row := range m.FollowedThreads {
			items = append(items, html.WithKey(issue3916Item(m, row), "followed:"+row.ConversationID+"\x00"+row.RootPostID))
		}
		body = html.Ul(html.Props{Class: "issue3916-list"}, items...)
	}
	children := []ui.Node{heading,
		html.Span(html.Props{Class: "sr-only", Role: "status", Aria: map[string]string{"live": "polite"}, Text: status}), body}
	// A refused Stop following keeps the row and says so, in an alert that is
	// read at once; the list itself loaded.
	if m.FollowedNote != "" {
		children = append(children, html.P(html.Props{Class: "issue3916-failed", Role: "alert", Text: laneText(m, issue3916Copy, keyIssue3916Failed)}))
	}
	return html.Section(html.Props{ID: "issue3916-panel", Class: "issue3916-panel", Aria: map[string]string{"labelledby": issue3916HeadID}}, children...)
}

func issue3916Item(m Model, row FollowedThreadRow) ui.Node {
	channel, author, preview := issue3916Channel(m, row), issue3916Author(m, row), issue3916Preview(m, row)
	replies := issue3916Replies(m, row.Replies)
	openName := laneTextf(m, issue3916Copy, keyIssue3916OpenAria, map[string]string{"author": author, "channel": channel, "preview": preview, "replies": replies, "when": row.ActivityLabel})
	stopName := laneTextf(m, issue3916Copy, keyIssue3916StopAria, map[string]string{"author": author, "channel": channel})
	meta := []string{author, replies}
	if row.ActivityLabel != "" {
		meta = append(meta, row.ActivityLabel)
	}
	previewClass := "issue3916-preview"
	if row.Removed {
		previewClass += " removed"
	}
	return html.Li(html.Props{Class: "issue3916-item", Data: map[string]string{"followed-root": row.RootPostID}},
		html.Button(html.Props{Class: "issue3916-open", Type: "button", Data: map[string]string{"action": issue3916Open, "id": row.RootPostID, "extra": row.ConversationID},
			Aria: map[string]string{"label": openName}},
			html.Span(html.Props{Class: "issue3916-channel", Dir: "auto", Text: channel}),
			html.Span(html.Props{Class: previewClass, Dir: "auto", Text: preview}),
			html.Span(html.Props{Class: "issue3916-meta", Dir: "auto", Text: strings.Join(meta, " · ")})),
		html.Button(html.Props{Class: "button secondary small issue3916-stop", Type: "button", Data: map[string]string{"action": issue3916Stop, "id": row.RootPostID, "extra": row.ConversationID},
			Aria: map[string]string{"label": stopName}, Text: laneText(m, issue3916Copy, keyIssue3916Stop)}))
}

// issue3916Click handles the panel's controls. It reports whether it consumed
// the click.
func issue3916Click(e ui.MouseEvent, m Model, local localStore) bool {
	action, id, conversation := eventAction(e)
	switch action {
	case issue3916Toggle:
		if local.get().followedOpen {
			local.update(func(u *localUI) { u.followedOpen = false })
			return true
		}
		local.update(func(u *localUI) { u.followedOpen = true })
		// Opening always asks again, so the list is never older than the click.
		if m.Callbacks.LoadFollowedThreads != nil {
			m.Callbacks.LoadFollowedThreads()
		}
		focusField(issue3916HeadID)
		return true
	case issue3916Retry:
		if m.Callbacks.LoadFollowedThreads != nil {
			m.Callbacks.LoadFollowedThreads()
		}
		return true
	case issue3916Open:
		if m.Callbacks.OpenFollowedThread == nil {
			return true
		}
		for _, row := range m.FollowedThreads {
			if row.RootPostID == id && row.ConversationID == conversation {
				m.Callbacks.OpenFollowedThread(conversation, id, row.Sequence)
				break
			}
		}
		return true
	case issue3916Stop:
		if m.Callbacks.UnfollowThread != nil {
			m.Callbacks.UnfollowThread(conversation, id)
			// The row goes away; focus moves to the heading rather than falling to the page.
			focusField(issue3916HeadID)
		}
		return true
	}
	return false
}

// Issue3916Styles lays the panel out as a bounded, scrolling list at the top
// of the conversation list; rows are 44px targets on phones and logical
// properties keep the layout right in right-to-left.
const Issue3916Styles = `.issue3916-control{padding:0 .75rem}` +
	`.issue3916-row{width:100%}.issue3916-row.on{font-weight:600}` +
	`.issue3916-panel{margin:.25rem .75rem .5rem;padding:.5rem;border:1px solid var(--line,rgba(0,0,0,.15));border-radius:.5rem;max-height:50vh;overflow-y:auto}` +
	`.issue3916-heading{margin:0 0 .25rem;font-size:.875rem;font-weight:600}.issue3916-heading:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.issue3916-note{margin:.25rem 0;display:grid;gap:.5rem;justify-items:start}` +
	`.issue3916-list{list-style:none;margin:0;padding:0;display:grid;gap:.25rem}` +
	`.issue3916-item{display:flex;align-items:flex-start;gap:.5rem;justify-content:space-between}` +
	`.issue3916-open{flex:1 1 auto;min-width:0;display:grid;gap:.125rem;padding:.375rem .5rem;border:0;border-radius:.375rem;background:none;color:inherit;font:inherit;text-align:start;cursor:pointer}` +
	`.issue3916-open:hover{background:var(--soft)}` +
	`.issue3916-open:focus-visible,.issue3916-stop:focus-visible,.issue3916-row:focus-visible,.issue3916-retry:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.issue3916-channel{font-weight:600;font-size:.8125rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.issue3916-preview{overflow:hidden;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow-wrap:anywhere}.issue3916-preview.removed{font-style:italic}` +
	`.issue3916-meta{font-size:.75rem;opacity:.8}` +
	`.issue3916-stop{flex:none;white-space:nowrap}` +
	`.issue3916-failed{margin:.5rem 0 0;font-size:.8125rem}` +
	`@media (max-width:640px){.issue3916-row,.issue3916-open,.issue3916-stop,.issue3916-retry{min-height:44px}}` +
	`@media (prefers-reduced-motion:reduce){.issue3916-panel{scroll-behavior:auto}}`
