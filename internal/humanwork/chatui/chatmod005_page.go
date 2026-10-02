package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// ModerationPageModel is the Moderation page: the open items a moderator can
// open, then the notices sent to the person looking. NoQueue is true for a
// person who holds no moderation permission: they see their notices alone.
type ModerationPageModel struct {
	Locale    string
	State     LoadState
	Items     []chat.ModerationItem
	Notices   []chat.ModerationNotice
	Names     map[string]string
	Query     string
	ErrorCode string
	NoQueue   bool
	// Tab is "open" (the default) or "resolved"; OpenCount is how many open
	// items the person can open, shown on the Open tab while Resolved is read.
	Tab       string
	OpenCount int
	// TimeZone is the reader's, so a time reads the way the conversation shows it.
	TimeZone *time.Location
}

func chatremoveName(m ModerationPageModel, id string) string {
	if name := m.Names[id]; name != "" {
		return name
	}
	return chatremoveText(m.Locale, "someone")
}

func moderationKindLabel(locale, kind string) string {
	switch kind {
	case "report":
		return chatremoveText(locale, "kind_report")
	case "appeal":
		return chatremoveText(locale, "appeal_item")
	}
	return chatremoveText(locale, kind)
}

// ModerationPage renders the page; every string is from this feature's own
// en-US, de-DE and ar table, never from a catalog that could answer a key.
func ModerationPage(m ModerationPageModel) ui.Node { return moderationQueuePage(m) }

// ModerationAuthorNotice is one notice sent to the person looking: their removed
// message with the reason and how to ask for a review, a moderator's message, or
// the outcome of a report they sent.
func ModerationAuthorNotice(locale string, n chat.ModerationNotice) ui.Node {
	t := func(key string) string { return chatremoveText(locale, key) }
	title := "outcome"
	switch {
	case strings.HasPrefix(n.ID, "outcome:"):
	case n.Outcome == "remove":
		title = "notice"
	case n.Outcome == "restore":
		title = "restored"
	case n.Outcome == "message_author":
		title = "from_moderator"
	}
	children := []ui.Node{html.H3(html.Props{Text: t(title)})}
	if !n.At.IsZero() {
		children = append(children, html.P(html.Props{Class: "chatremove-date", Text: n.At.UTC().Format("2006-01-02")}))
	}
	children = append(children, html.P(html.Props{Dir: "auto", Text: chatremoveReason(locale, n.Reason)}))
	if title == "outcome" {
		children = append(children, html.P(html.Props{Text: t("outcome_" + n.Outcome)}))
	}
	if n.CanAppeal {
		children = append(children, html.Form(html.Props{Data: map[string]string{"chatremove": "appeal", "conversation-id": n.ConversationID, "post-id": n.PostID, "host-tenant": n.TenantID}}, html.Button(html.Props{Class: "primary", Type: "submit", Text: t("appeal")})))
	}
	return html.Aside(html.Props{Class: "chatremove-notice", Role: "status", Aria: map[string]string{"live": "polite"}}, children...)
}
