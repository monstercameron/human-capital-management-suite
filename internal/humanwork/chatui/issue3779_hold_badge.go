package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3779: the open conversation's legal hold, in its details. The server
// answers only a workspace administrator who can open the conversation, so
// everyone else gets no state at all and sees nothing: a member is never told
// that a hold exists. The page shows a badge and what the hold means; placing
// and releasing is on the Chat settings page.

// HoldState is what the server said about the open conversation's legal hold.
// Transient client state, never message content.
type HoldState struct {
	// ConversationID is the conversation the answer is about; an answer for
	// another conversation is not shown.
	ConversationID string
	// Loaded is true once the server has answered.
	Loaded bool
	Held   bool
	// Count is how many holds are in force on the conversation.
	Count int
}

// holdHere returns the state when it describes the open conversation.
func (m Model) holdHere() (HoldState, bool) {
	s := m.Hold
	return s, s.Loaded && s.ConversationID != "" && s.ConversationID == m.SelectedID
}

const (
	keyIssue3779Title = "chat.issue3779.title"
	keyIssue3779Badge = "chat.issue3779.badge"
	keyIssue3779Help  = "chat.issue3779.help"
	keyIssue3779Many  = "chat.issue3779.many"
	keyIssue3779Where = "chat.issue3779.where"
)

var issue3779Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3779Title: "Legal hold",
		keyIssue3779Badge: "On legal hold",
		keyIssue3779Help:  "Retention never removes anything in this conversation, and a message deleted here keeps a protected copy. People can still write, edit and delete as usual.",
		keyIssue3779Many:  "{count} holds are in force on this conversation.",
		keyIssue3779Where: "Holds are placed and released in Chat settings. Only workspace administrators see this.",
	},
	"de-DE": {
		keyIssue3779Title: "Rechtliche Aufbewahrungspflicht",
		keyIssue3779Badge: "Unter Aufbewahrungspflicht",
		keyIssue3779Help:  "Die Aufbewahrung entfernt in dieser Unterhaltung nie etwas, und eine hier gelöschte Nachricht behält eine geschützte Kopie. Personen können weiterhin wie gewohnt schreiben, bearbeiten und löschen.",
		keyIssue3779Many:  "Für diese Unterhaltung gelten {count} Pflichten.",
		keyIssue3779Where: "Pflichten werden in den Chat-Einstellungen festgelegt und aufgehoben. Das sehen nur Administratoren des Arbeitsbereichs.",
	},
	"ar": {
		keyIssue3779Title: "الحجز القانوني",
		keyIssue3779Badge: "تحت حجز قانوني",
		keyIssue3779Help:  "لا تزيل سياسة الاحتفاظ أي شيء في هذه المحادثة، وتحتفظ الرسالة المحذوفة هنا بنسخة محمية. يمكن للأشخاص الكتابة والتعديل والحذف كالمعتاد.",
		keyIssue3779Many:  "يوجد {count} حجوزات سارية على هذه المحادثة.",
		keyIssue3779Where: "تُفرض الحجوزات وتُرفع من إعدادات الدردشة. لا يراها إلا مسؤولو مساحة العمل.",
	},
}

// issue3779Section is the Legal hold section of the details. It is absent
// unless the server said the open conversation is held.
func issue3779Section(m Model) ui.Node {
	s, ok := m.holdHere()
	if !ok || !s.Held {
		return nil
	}
	title := laneText(m, issue3779Copy, keyIssue3779Title)
	badge := laneText(m, issue3779Copy, keyIssue3779Badge)
	children := []ui.Node{
		html.H4(html.Props{Class: "chathold-badge-title", ID: "chathold-details-title", Text: title}),
		html.Span(html.Props{Class: "chathold-badge", Role: "note", Data: map[string]string{"hold-badge": "true"}, Aria: map[string]string{"label": badge}},
			icon("lock"), html.Span(html.Props{Class: "chathold-badge-text", Aria: map[string]string{"hidden": "true"}, Text: badge})),
		html.P(html.Props{Class: "chathold-line", Text: laneText(m, issue3779Copy, keyIssue3779Help)}),
	}
	if s.Count > 1 {
		children = append(children, html.P(html.Props{Class: "chathold-line", Text: laneTextf(m, issue3779Copy, keyIssue3779Many, map[string]string{"count": chatRetentionPreviewNumber(m.Locale, s.Count)})}))
	}
	children = append(children, html.P(html.Props{Class: "chathold-line chathold-muted", Text: laneText(m, issue3779Copy, keyIssue3779Where)}))
	return html.Section(html.Props{Class: "details-section chathold-details", Aria: map[string]string{"labelledby": "chathold-details-title"}, Data: map[string]string{"hold-section": "true"}}, children...)
}

// Issue3779Styles lays the badge, the details section and the settings rows out
// for a phone first: text wraps instead of clipping, and logical properties keep
// it right in a right-to-left language.
const Issue3779Styles = `
.chathold-badge{display:inline-flex;align-items:center;gap:4px;align-self:flex-start;min-inline-size:0;max-inline-size:100%;padding:2px 8px;border:1px solid var(--hcm-color-border);border-radius:999px;background:var(--soft);color:var(--hcm-color-text);font-size:.75rem;font-weight:600;line-height:1.3;white-space:normal;overflow-wrap:anywhere}
.chathold-badge .chat-icon{inline-size:14px;block-size:14px;flex:none}
.chathold-details{display:flex;flex-direction:column;gap:6px;padding:12px 16px;border-block-start:1px solid var(--hcm-color-border);overflow-wrap:anywhere}
.chathold-badge-title{margin:0;font-size:.875rem}
.chathold-details .chathold-line{margin:0;font-size:.8125rem}
.chathold-muted{color:var(--hcm-color-text-muted)}
.chathold-item{align-items:flex-start}
.chathold-item .chathold-line{display:block}
.chathold-release{flex:1 1 220px}
@media (prefers-reduced-motion:reduce){.chathold-details,.chathold-details *{transition:none;animation:none}}
`
