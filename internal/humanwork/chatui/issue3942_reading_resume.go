package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3942: return to the reading position when switching conversations.
//
// The browser remembers, for this person only and only for this session, the
// message at the top of the timeline and how far down the view it sat when they
// left a conversation. Coming back loads the messages around it and this slim
// bar, in flow above the timeline, says so and offers Jump to newest. An
// unread message, a search result or a link to a message always wins over the
// remembered place, and the bar never names a message the person cannot open.

// The outcomes of a return, as the browser reports them.
const (
	// ReadingResumed: the message the person was reading is back at the same spot.
	ReadingResumed = "resumed"
	// ReadingNearby: that message is gone or no longer visible to them; the
	// closest message they can open is shown instead.
	ReadingNearby = "nearby"
	// ReadingUnavailable: the place could not be reopened; the newest messages
	// are shown.
	ReadingUnavailable = "unavailable"
)

// ReadingResumeView is the state of the last return in one conversation.
type ReadingResumeView struct {
	// ConversationID is the conversation the state belongs to; any other
	// conversation draws nothing.
	ConversationID string
	Status         string
}

var issue3942Copy = map[string][3]string{
	"label":       {"Reading position", "Leseposition", "موضع القراءة"},
	"resumed":     {"Back where you were reading. Newer messages are below.", "Zurück an der Stelle, an der Sie gelesen haben. Neuere Nachrichten stehen weiter unten.", "عدتَ إلى موضع قراءتك. الرسائل الأحدث في الأسفل."},
	"nearby":      {"The message you were reading is no longer available. Showing the closest messages instead.", "Die Nachricht, die Sie gelesen haben, ist nicht mehr verfügbar. Stattdessen werden die nächstgelegenen Nachrichten angezeigt.", "الرسالة التي كنت تقرأها لم تعد متاحة. يتم عرض أقرب الرسائل بدلًا منها."},
	"unavailable": {"Could not return to where you were reading. Showing the newest messages.", "Die vorherige Leseposition konnte nicht geöffnet werden. Die neuesten Nachrichten werden angezeigt.", "تعذّرت العودة إلى موضع قراءتك. يتم عرض أحدث الرسائل."},
	"newest":      {"Jump to newest", "Zu den neuesten springen", "الانتقال إلى الأحدث"},
	"dismiss":     {"Dismiss", "Ausblenden", "إخفاء"},
}

func issue3942Text(m Model, key string) string {
	copy := issue3942Copy[key]
	return chatbug039Text("chat.issue3942."+key, copy[chatbug039LocaleIndex(m.Locale)], copy[0])
}

// issue3942Bar is the bar above the timeline. It is nil unless the open
// conversation has just been returned to a remembered place.
func issue3942Bar(m Model) ui.Node {
	view := m.ReadingResume
	if view.Status == "" || view.ConversationID == "" || view.ConversationID != m.SelectedID {
		return nil
	}
	text := issue3942Text(m, view.Status)
	if text == "" {
		return nil
	}
	children := []ui.Node{
		html.P(html.Props{ID: "chat-reading-resume-status", Class: "reading-resume-status", Role: "status", Aria: map[string]string{"live": "polite"}, Text: text}),
	}
	if view.Status != ReadingUnavailable {
		children = append(children, html.Button(html.Props{Class: "button small reading-resume-newest", Type: "button", Disabled: m.Callbacks.JumpFromReadingResume == nil,
			Data: map[string]string{"action": "reading-resume-newest"}, Text: issue3942Text(m, "newest")}))
	}
	children = append(children, html.Button(html.Props{Class: "button secondary small reading-resume-dismiss", Type: "button", Disabled: m.Callbacks.DismissReadingResume == nil,
		Data: map[string]string{"action": "reading-resume-dismiss"}, Text: issue3942Text(m, "dismiss")}))
	return html.Div(html.Props{ID: "chat-reading-resume", Class: "reading-resume-bar", Role: "group", Dir: direction(m.Locale), Aria: map[string]string{"label": issue3942Text(m, "label")}}, children...)
}

// issue3942Click handles the bar's buttons. It reports whether the press was one
// of them.
func issue3942Click(e ui.MouseEvent, m Model) bool {
	action, _, _ := eventAction(e)
	switch action {
	case "reading-resume-newest":
		if m.Callbacks.JumpFromReadingResume != nil {
			BeginScrollToNewest()
			m.Callbacks.JumpFromReadingResume()
			focusChatTimeline()
		}
	case "reading-resume-dismiss":
		if m.Callbacks.DismissReadingResume != nil {
			m.Callbacks.DismissReadingResume()
			focusChatTimeline()
		}
	default:
		return false
	}
	return true
}

const issue3942Styles = `.chat-workspace .reading-resume-bar{display:flex;flex-wrap:wrap;align-items:center;gap:6px 12px;margin:0;padding:8px 16px;border-block-end:1px solid var(--line);background:var(--soft);min-width:0}` +
	`.chat-workspace .reading-resume-status{flex:1 1 14rem;margin:0;min-width:0;color:var(--muted);font-size:.8125rem;overflow-wrap:anywhere}` +
	`.chat-workspace .reading-resume-bar .button{min-height:44px;min-width:44px}` +
	`@media(max-width:560px){.chat-workspace .reading-resume-bar{padding-inline:12px}.chat-workspace .reading-resume-bar .button{flex:1 1 auto}}`
