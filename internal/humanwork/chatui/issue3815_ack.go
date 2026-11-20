package chatui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3815: a post that asks the channel to acknowledge it. The sender marks
// their own post from its menu (when they own or manage the channel); every
// other member then sees a one-tap Acknowledge under it; the people who manage
// the channel see how many have and who has not. The server decides everything
// (who is asked, who may mark, what is kept); this file only draws what the
// server said (AcksState) and sends the two actions through the browser client.
// A read never acknowledges: only the Acknowledge button does.

// AckView is what the server said this reader may do with one post that
// requires acknowledgement.
type AckView struct {
	// Loaded is true once the server has answered for this post.
	Loaded         bool
	CanAcknowledge bool
	Acknowledged   bool
	AcknowledgedAt time.Time
	// CanManage is set for the sender and the people who manage the channel;
	// Total, Count and Pending are filled only then.
	CanManage bool
	Total     int
	Count     int
	// Pending holds the people who have not acknowledged; PendingMore counts
	// those beyond what the server listed.
	Pending     []string
	PendingMore int
	// Busy is set while an acknowledgement is on its way.
	Busy bool
	// Notice says why the last press was not accepted: "failed" or "denied".
	Notice string
}

// AcksState is the page's transient view of the acknowledgements on screen.
type AcksState struct {
	// Views holds each required post of the open conversation, by message id.
	Views map[string]AckView
	// CanRequire is set once the server said this reader may mark their own posts
	// here; Require sends the request for one message.
	CanRequire bool
	Require    func(postID string)
	// NoticePost and Notice name a message whose Require request was refused:
	// "failed" or "denied".
	NoticePost, Notice string
}

const (
	keyIssue3815Title        = "chat.issue3815.title"
	keyIssue3815Button       = "chat.issue3815.button"
	keyIssue3815Saving       = "chat.issue3815.saving"
	keyIssue3815Done         = "chat.issue3815.done"
	keyIssue3815Failed       = "chat.issue3815.failed"
	keyIssue3815Denied       = "chat.issue3815.denied"
	keyIssue3815Progress     = "chat.issue3815.progress"
	keyIssue3815Pending      = "chat.issue3815.pending"
	keyIssue3815More         = "chat.issue3815.more"
	keyIssue3815Everyone     = "chat.issue3815.everyone"
	keyIssue3815Menu         = "chat.issue3815.menu"
	keyIssue3815RequireFail  = "chat.issue3815.require_failed"
	keyIssue3815RequireDeny  = "chat.issue3815.require_denied"
	keyIssue3815RequireDone  = "chat.issue3815.require_done"
	keyIssue3815ProgressName = "chat.issue3815.progress_label"
)

var issue3815Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3815Title:        "Acknowledgement required",
		keyIssue3815Button:       "I have read and understood this",
		keyIssue3815Saving:       "Saving...",
		keyIssue3815Done:         "You acknowledged this on {date}",
		keyIssue3815Failed:       "Your acknowledgement could not be saved. Try again.",
		keyIssue3815Denied:       "You are not on the list for this acknowledgement.",
		keyIssue3815Progress:     "{count} of {total} acknowledged",
		keyIssue3815Pending:      "{count} pending",
		keyIssue3815More:         "and {count} more",
		keyIssue3815Everyone:     "Everyone has acknowledged this.",
		keyIssue3815Menu:         "Require acknowledgement",
		keyIssue3815RequireFail:  "Acknowledgement could not be requested. Try again.",
		keyIssue3815RequireDeny:  "Only the sender, as an owner or manager of this channel, can require acknowledgement.",
		keyIssue3815RequireDone:  "Acknowledgement is now required for this message.",
		keyIssue3815ProgressName: "Acknowledgement progress",
	},
	"de-DE": {
		keyIssue3815Title:        "Bestätigung erforderlich",
		keyIssue3815Button:       "Ich habe dies gelesen und verstanden",
		keyIssue3815Saving:       "Wird gespeichert...",
		keyIssue3815Done:         "Sie haben dies am {date} bestätigt",
		keyIssue3815Failed:       "Ihre Bestätigung konnte nicht gespeichert werden. Versuchen Sie es erneut.",
		keyIssue3815Denied:       "Sie stehen nicht auf der Liste für diese Bestätigung.",
		keyIssue3815Progress:     "{count} von {total} bestätigt",
		keyIssue3815Pending:      "{count} ausstehend",
		keyIssue3815More:         "und {count} weitere",
		keyIssue3815Everyone:     "Alle haben dies bestätigt.",
		keyIssue3815Menu:         "Bestätigung verlangen",
		keyIssue3815RequireFail:  "Die Bestätigung konnte nicht angefordert werden. Versuchen Sie es erneut.",
		keyIssue3815RequireDeny:  "Nur die Absenderin oder der Absender als Inhaber oder Verwalter dieses Kanals kann eine Bestätigung verlangen.",
		keyIssue3815RequireDone:  "Für diese Nachricht ist jetzt eine Bestätigung erforderlich.",
		keyIssue3815ProgressName: "Stand der Bestätigungen",
	},
	"ar": {
		keyIssue3815Title:        "مطلوب إقرار",
		keyIssue3815Button:       "لقد قرأت هذا وفهمته",
		keyIssue3815Saving:       "جارٍ الحفظ...",
		keyIssue3815Done:         "أقررت بهذا في {date}",
		keyIssue3815Failed:       "تعذّر حفظ إقرارك. حاول مرة أخرى.",
		keyIssue3815Denied:       "لست ضمن قائمة هذا الإقرار.",
		keyIssue3815Progress:     "أقرّ {count} من {total}",
		keyIssue3815Pending:      "{count} قيد الانتظار",
		keyIssue3815More:         "و{count} آخرين",
		keyIssue3815Everyone:     "أقرّ الجميع بهذا.",
		keyIssue3815Menu:         "طلب إقرار",
		keyIssue3815RequireFail:  "تعذّر طلب الإقرار. حاول مرة أخرى.",
		keyIssue3815RequireDeny:  "يمكن للمرسل فقط، بصفته مالك هذه القناة أو مديرها، طلب الإقرار.",
		keyIssue3815RequireDone:  "أصبح الإقرار مطلوبًا لهذه الرسالة.",
		keyIssue3815ProgressName: "تقدم الإقرارات",
	},
}

// Issue3815Text is the copy of the acknowledgement surfaces in a locale (used
// by the browser client for the sentences it announces).
func Issue3815Text(locale, key string) string {
	return chatbug039Text(key, issue3815Copy[chatEmojiLocale(locale)][key], issue3815Copy["en-US"][key])
}

// Issue3815RequireDoneKey and the other exported keys name the sentences the
// browser client announces.
const (
	Issue3815RequireDoneKey = keyIssue3815RequireDone
)

func issue3815Num(m Model, n int) string {
	if m.Number != nil {
		return m.Number(n)
	}
	return strconv.Itoa(n)
}

// ackRequire runs when the Require acknowledgement row is chosen.
func (m Model) ackRequire(postID string) {
	if m.Acks.Require == nil || postID == "" {
		return
	}
	// The dispatcher closes the menu after every action.
	m.Acks.Require(postID)
}

// ackMenuItem is the Require acknowledgement row of a message's menu. It is
// offered on the reader's own post, in a channel where the server said they may
// mark posts, until the post is marked.
func ackMenuItem(m Model, msg Message) []ui.Node {
	if !m.Acks.CanRequire || m.Acks.Require == nil || m.CurrentUser == "" || msg.ID == "" || msg.AuthorID != m.CurrentUser {
		return nil
	}
	if _, marked := m.Acks.Views[msg.ID]; marked || strings.TrimSpace(msg.Body) == "" || strings.HasPrefix(msg.Body, "⁣") {
		return nil
	}
	if host := m.selected().HostTenantID; host != "" && host != m.CurrentTenantID {
		return nil
	}
	return []ui.Node{chatux022Item("menu-item chatack-menu-item", "ack-require", msg.ID, "checklist", laneText(m, issue3815Copy, keyIssue3815Menu), false)}
}

// ackBlock is what is drawn under a post that requires acknowledgement, or
// under a post whose request to require it was refused.
func ackBlock(m Model, msg Message) []ui.Node {
	view, marked := m.Acks.Views[msg.ID]
	if !marked || !view.Loaded {
		if m.Acks.NoticePost == msg.ID && m.Acks.Notice != "" {
			return []ui.Node{html.P(html.Props{Class: "chatack-notice", Role: "alert", Text: ackRequireNotice(m)})}
		}
		return nil
	}
	children := []ui.Node{html.Strong(html.Props{Class: "chatack-title", Text: laneText(m, issue3815Copy, keyIssue3815Title)})}
	if view.CanManage {
		children = append(children, ackRoll(m, view)...)
	}
	switch {
	case view.Acknowledged:
		text := laneTextf(m, issue3815Copy, keyIssue3815Done, map[string]string{"date": ackDate(m, view.AcknowledgedAt)})
		children = append(children, html.P(html.Props{ID: "ack-done-" + msg.ID, Class: "chatack-status chatack-done", Role: "status", TabIndex: -1, Text: text}))
	case view.CanAcknowledge:
		label := laneText(m, issue3815Copy, keyIssue3815Button)
		if view.Busy {
			label = laneText(m, issue3815Copy, keyIssue3815Saving)
		}
		form := []ui.Node{html.Button(html.Props{Type: "submit", Class: "button primary chatack-button", Disabled: view.Busy, Aria: map[string]string{"busy": boolString(view.Busy)}, Text: label})}
		children = append(children, html.Form(html.Props{Class: "chatack-form", Data: map[string]string{"ack-form": msg.ID}}, form...))
	}
	if view.Notice != "" {
		text := laneText(m, issue3815Copy, keyIssue3815Failed)
		if view.Notice == "denied" {
			text = laneText(m, issue3815Copy, keyIssue3815Denied)
		}
		children = append(children, html.P(html.Props{Class: "chatack-notice", Role: "alert", Text: text}))
	}
	return []ui.Node{html.Div(html.Props{Class: "chatack", Role: "group", Aria: map[string]string{"label": laneText(m, issue3815Copy, keyIssue3815Title)}, Data: map[string]string{"ack-post": msg.ID}}, children...)}
}

func ackDate(m Model, at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return formatDay(m.Locale, at, true)
}

func ackRequireNotice(m Model) string {
	if m.Acks.Notice == "denied" {
		return laneText(m, issue3815Copy, keyIssue3815RequireDeny)
	}
	return laneText(m, issue3815Copy, keyIssue3815RequireFail)
}

// ackRoll is the manager's line: how many have acknowledged, and, folded away
// until asked for, who has not. A person the page has no name for is counted,
// never shown by an internal id.
func ackRoll(m Model, view AckView) []ui.Node {
	progress := laneTextf(m, issue3815Copy, keyIssue3815Progress, map[string]string{"count": issue3815Num(m, view.Count), "total": issue3815Num(m, view.Total)})
	nodes := []ui.Node{html.P(html.Props{Class: "chatack-progress", Text: progress})}
	pending := len(view.Pending) + view.PendingMore
	if pending == 0 {
		return append(nodes, html.P(html.Props{Class: "chatack-status", Text: laneText(m, issue3815Copy, keyIssue3815Everyone)}))
	}
	var names []ui.Node
	unnamed := view.PendingMore
	for _, id := range view.Pending {
		if name := chatKnownName(m, id); name != "" {
			names = append(names, html.Li(html.Props{Dir: "auto", Text: name}))
		} else {
			unnamed++
		}
	}
	if unnamed > 0 {
		names = append(names, html.Li(html.Props{Text: laneTextf(m, issue3815Copy, keyIssue3815More, map[string]string{"count": issue3815Num(m, unnamed)})}))
	}
	summary := laneTextf(m, issue3815Copy, keyIssue3815Pending, map[string]string{"count": issue3815Num(m, pending)})
	return append(nodes, html.Details(html.Props{Class: "chatack-roll"}, html.Summary(html.Props{Class: "chatack-summary", Text: summary}), html.Ul(html.Props{Class: "chatack-list"}, names...)))
}

// Issue3815Styles lays the block out for a phone first: it wraps rather than
// scrolls at 320px, the Acknowledge button is a 44px target, and logical
// properties keep it right in a right-to-left language.
const Issue3815Styles = `
.chatack{display:flex;flex-direction:column;align-items:flex-start;gap:6px;box-sizing:border-box;inline-size:min(100%,32rem);min-inline-size:0;margin-block-start:6px;padding:8px 12px;border:1px solid var(--hcm-color-border);border-inline-start:3px solid var(--hcm-color-brand-primary);border-radius:var(--hcm-radius-control);background:var(--soft);overflow-wrap:anywhere}
.chatack-title{font-size:.8125rem}
.chatack-progress,.chatack-status{margin:0;font-size:.8125rem;color:var(--hcm-color-text-muted)}
.chatack-done{color:var(--hcm-color-text)}
.chatack-form{margin:0}
.chatack-button{min-block-size:44px;min-inline-size:44px;max-inline-size:100%;white-space:normal}
.chatack-roll{font-size:.8125rem;max-inline-size:100%}
.chatack-summary{min-block-size:44px;display:flex;align-items:center;cursor:pointer}
.chatack-list{margin:0;padding-inline-start:20px}
.chatack-notice{margin:6px 0 0;font-size:.8125rem;color:var(--hcm-color-danger,#b3261e)}
@media (prefers-reduced-motion:reduce){.chatack,.chatack *{transition:none;animation:none}}
`
