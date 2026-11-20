package productui

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue 3733: chat items in the shell bell. A chat notification is one message
// that needs the signed-in person: a mention of them, a reply in a thread they
// follow, or a direct message. The server computes the list from messages the
// person can read right now and keeps their read state, so the same items are
// read or unread in every session. The bell lists them beside the workflow
// notifications, counts them in its badge, and each item opens the message.

// Kinds of chat notification, as the server names them.
const (
	ChatNotificationMention = "mention"
	ChatNotificationReply   = "reply"
	ChatNotificationDirect  = "direct"
	ChatNotificationCover   = "cover"
)

// ChatNotification is one chat notification as the view shows it. AuthorID is
// the author's identity, used only to look a name up; AuthorName is
// empty when the author's name is not known to this page; the item then says
// "Someone" and never an identifier.
type ChatNotification struct {
	_                  incomparable
	ID                 string
	Kind               string
	ConversationID     string
	ConversationName   string
	PostID             string
	Sequence           uint64
	ThreadRootID       string
	ThreadRootSequence uint64
	AuthorID           string
	AuthorName         string
	// CoveringID and CoveringName are set on a cover notice (issue 3789): the
	// person who is away, whose mention this is. A cover notice never has a Body.
	CoveringID   string
	CoveringName string
	Body         string
	CreatedAt    time.Time
	Read         bool
}

// ChatNotificationHref is the address that opens the message in Chat. A reply
// opens its thread with the message the thread hangs on in view; anything else
// opens the conversation at the message. Without a message position the link
// opens the conversation.
func ChatNotificationHref(item ChatNotification) string {
	href := chatui.ChannelReferenceURL(item.ConversationID)
	extra := url.Values{}
	switch {
	case item.ThreadRootID != "" && item.ThreadRootSequence > 0:
		extra.Set("message", item.ThreadRootID)
		extra.Set("at", strconv.FormatUint(item.ThreadRootSequence, 10))
		extra.Set("thread", "1")
	case item.PostID != "" && item.Sequence > 0:
		extra.Set("message", item.PostID)
		extra.Set("at", strconv.FormatUint(item.Sequence, 10))
	}
	if len(extra) == 0 {
		return href
	}
	return href + "&" + extra.Encode()
}

func unreadChatNotificationsWithOverrides(items []ChatNotification, overrides map[string]bool) int {
	count := 0
	for _, item := range items {
		if !item.Read && !overrides[item.ID] {
			count++
		}
	}
	return count
}

// ChatNotificationListProps composes the chat part of the bell.
type ChatNotificationListProps struct {
	I18nProps
	Items         []ChatNotification
	Unavailable   bool
	ReadOverrides map[string]bool
	OnRead        func(string)
	Navigate      func(string)
}

func ChatNotificationList(props ChatNotificationListProps) ui.Node {
	if props.Unavailable {
		return html.P(html.Props{Class: "muted", Role: "status"}, ui.Text(props.Text("chat_notifications.unavailable")))
	}
	if len(props.Items) == 0 {
		return html.P(html.Props{Class: "muted"}, ui.Text(props.Text("chat_notifications.empty")))
	}
	rows := make([]ui.Node, 0, len(props.Items))
	for _, item := range props.Items {
		actor := strings.TrimSpace(item.AuthorName)
		if actor == "" {
			actor = props.Text("chat_notifications.someone")
		}
		place := strings.TrimSpace(item.ConversationName)
		if place == "" {
			place = props.Text("chat_notifications.unnamed_place")
		} else {
			place = "#" + place
		}
		covered := strings.TrimSpace(item.CoveringName)
		if covered == "" {
			covered = props.Text("chat_notifications.someone_covered")
		}
		key := "chat_notifications.mention"
		switch {
		case item.Kind == ChatNotificationCover:
			key = "chat_notifications.cover"
		case item.Kind == ChatNotificationReply:
			key = "chat_notifications.reply"
		case item.Kind == ChatNotificationDirect:
			key = "chat_notifications.direct"
		}
		title := props.Text(key, map[string]string{"actor": actor, "place": place, "covered": covered})
		when := props.Text("notifications.time_unavailable")
		whenProps := html.Props{Class: "muted workflow-notification-time"}
		if !item.CreatedAt.IsZero() {
			when = props.Locale.FormatTimestamp(item.CreatedAt)
			whenProps.Raw = map[string]any{"datetime": item.CreatedAt.UTC().Format(time.RFC3339)}
		}
		read := item.Read || props.ReadOverrides[item.ID]
		stateText := props.Text("notifications.unread")
		if read {
			stateText = props.Text("notifications.read")
		}
		rowClass := "workflow-notification-row chat-notification-row"
		if !read {
			rowClass += " is-unread"
		}
		children := []ui.Node{html.Strong(html.Props{}, ui.Text(title))}
		if body := strings.TrimSpace(item.Body); body != "" && item.Kind != ChatNotificationCover {
			children = append(children, html.Span(html.Props{Class: "chat-notification-body", Dir: "auto"}, ui.Text(body)))
		}
		children = append(children,
			html.Tag("time", whenProps, ui.Text(when)),
			html.Small(html.Props{Class: "muted workflow-notification-read-state"}, ui.Text(stateText)),
		)
		linkProps := html.Props{Class: "workflow-notification chat-notification", Aria: map[string]string{
			"label": props.Text("notifications.item_aria", map[string]string{"title": title, "actor": actor, "time": when, "state": stateText}),
		}}
		row := ui.CreateElement(workflowNotificationRow, workflowNotificationRowProps{
			ID: item.ID, Class: rowClass, OnRead: props.OnRead,
			Content: softwareLink(props.Navigate, linkProps, ChatNotificationHref(item), children...),
		})
		row.Props["key"] = item.ID
		rows = append(rows, row)
	}
	return html.Ul(html.Props{Class: "workflow-notifications chat-notifications", Aria: map[string]string{"label": props.Text("chat_notifications.heading")}}, rows...)
}

func chatNotificationTranslations(locale string) map[string]string {
	switch locale {
	case "de-DE":
		return map[string]string{
			"chat_notifications.heading":         "Chat-Benachrichtigungen",
			"chat_notifications.empty":           "Noch keine Chat-Benachrichtigungen.",
			"chat_notifications.unavailable":     "Chat-Benachrichtigungen sind vorübergehend nicht verfügbar. Öffnen Sie den Chat, um Ihre Nachrichten zu lesen.",
			"chat_notifications.mention":         "{actor} hat Sie in {place} erwähnt",
			"chat_notifications.reply":           "{actor} hat in einem Thread geantwortet, dem Sie folgen ({place})",
			"chat_notifications.direct":          "{actor} hat Ihnen eine Direktnachricht gesendet",
			"chat_notifications.someone":         "Jemand",
			"chat_notifications.someone_covered": "jemand, den Sie vertreten",
			"chat_notifications.cover":           "{actor} hat {covered} in {place} erwähnt. Sie sind die Vertretung.",
			"chat_notifications.unnamed_place":   "einer Unterhaltung",
			"chat_notifications.mark_all":        "Alle als gelesen markieren",
			"chat_notifications.open_chat":       "Chat öffnen",
		}
	case "ar":
		return map[string]string{
			"chat_notifications.heading":         "إشعارات الدردشة",
			"chat_notifications.empty":           "لا توجد إشعارات دردشة حتى الآن.",
			"chat_notifications.unavailable":     "إشعارات الدردشة غير متاحة مؤقتًا. افتح الدردشة لقراءة رسائلك.",
			"chat_notifications.mention":         "أشار إليك {actor} في {place}",
			"chat_notifications.reply":           "ردّ {actor} في سلسلة رسائل تتابعها ({place})",
			"chat_notifications.direct":          "أرسل إليك {actor} رسالة مباشرة",
			"chat_notifications.someone":         "شخص ما",
			"chat_notifications.someone_covered": "شخص تنوب عنه",
			"chat_notifications.cover":           "أشار {actor} إلى {covered} في {place}. أنت من ينوب عنه.",
			"chat_notifications.unnamed_place":   "محادثة",
			"chat_notifications.mark_all":        "وضع علامة مقروء على الكل",
			"chat_notifications.open_chat":       "فتح الدردشة",
		}
	default:
		return map[string]string{
			"chat_notifications.heading":         "Chat notifications",
			"chat_notifications.empty":           "No chat notifications yet.",
			"chat_notifications.unavailable":     "Chat notifications are temporarily unavailable. Open Chat to read your messages.",
			"chat_notifications.mention":         "{actor} mentioned you in {place}",
			"chat_notifications.reply":           "{actor} replied in a thread you follow in {place}",
			"chat_notifications.direct":          "{actor} sent you a direct message",
			"chat_notifications.someone":         "Someone",
			"chat_notifications.someone_covered": "someone you cover",
			"chat_notifications.cover":           "{actor} mentioned {covered} in {place}. You are their cover.",
			"chat_notifications.unnamed_place":   "a conversation",
			"chat_notifications.mark_all":        "Mark all as read",
			"chat_notifications.open_chat":       "Open Chat",
		}
	}
}

func chatNotificationStylesheet() string {
	return `
.chat-notification-body{color:var(--muted);font-weight:400;overflow-wrap:anywhere;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}
.chat-notification-actions{display:flex;flex-wrap:wrap;gap:var(--hcm-space-2);align-items:center;margin-block:var(--hcm-space-2)}
.chat-notification-actions .button,.chat-notification-actions a{min-height:44px;box-sizing:border-box}
`
}

// chatNotificationSection is the chat part of the bell's panel: its heading, the
// list (or its empty and unavailable states), "Mark all as read" while some are
// unread, and a link to Chat. reads holds items marked read in this session
// before the page reloads; a refused save puts the item back and says so.
func chatNotificationSection(view View, reads ui.State[map[string]bool], failed ui.State[bool], markAll ui.Handler) []ui.Node {
	markOne := func(id string) {
		setChatRead(reads, id, true)
		if view.MarkChatNotificationRead == nil {
			return
		}
		view.MarkChatNotificationRead(id, func(err error) {
			if err != nil {
				setChatRead(reads, id, false)
				failed.Set(true)
			}
		})
	}
	nodes := []ui.Node{
		html.H3(html.Props{}, ui.Text(view.Locale.Text("chat_notifications.heading"))),
		ui.CreateElement(ChatNotificationList, ChatNotificationListProps{
			I18nProps: I18nProps{Locale: view.Locale}, Items: view.ChatNotifications, Unavailable: view.ChatNotificationsUnavailable,
			ReadOverrides: reads.Get(), OnRead: markOne, Navigate: view.Navigate,
		}),
	}
	actions := []ui.Node{}
	if !view.ChatNotificationsUnavailable && view.MarkAllChatNotificationsRead != nil && unreadChatNotificationsWithOverrides(view.ChatNotifications, reads.Get()) > 0 {
		actions = append(actions, html.Button(html.Props{Class: "button secondary compact", Type: "button", OnClick: markAll}, ui.Text(view.Locale.Text("chat_notifications.mark_all"))))
	}
	actions = append(actions, appLink(view, html.Props{}, statefulHref(view, PageChat), ui.Text(view.Locale.Text("chat_notifications.open_chat"))))
	return append(nodes, html.Div(html.Props{Class: "chat-notification-actions"}, actions...))
}

func setChatRead(reads ui.State[map[string]bool], id string, read bool) {
	next := make(map[string]bool, len(reads.Get())+1)
	for known, marked := range reads.Get() {
		next[known] = marked
	}
	if read {
		next[id] = true
	} else {
		delete(next, id)
	}
	reads.Set(next)
}

// markAllChatNotificationsRead marks everything shown read at once and puts the
// items back, with an error, when the save is refused.
func markAllChatNotificationsRead(view View, reads ui.State[map[string]bool], failed ui.State[bool]) {
	if view.MarkAllChatNotificationsRead == nil {
		return
	}
	before := reads.Get()
	next := make(map[string]bool, len(before)+len(view.ChatNotifications))
	for known, marked := range before {
		next[known] = marked
	}
	for _, item := range view.ChatNotifications {
		next[item.ID] = true
	}
	reads.Set(next)
	view.MarkAllChatNotificationsRead(func(err error) {
		if err != nil {
			reads.Set(before)
			failed.Set(true)
		}
	})
}
