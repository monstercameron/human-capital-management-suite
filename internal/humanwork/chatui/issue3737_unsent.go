package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3737: a message that has not been delivered stays on screen.
//
// Pressing Send used to empty the composer and, when the network was down or
// the server refused, leave nothing in the conversation. The text now becomes a
// row at the end of the open conversation: "Sending" while it goes out, then
// "Not sent" with the text kept and three controls, Retry, Edit and Discard.
// Retry sends the same message again under the same idempotency key, so a send
// that actually reached the server before the connection dropped is never
// posted twice. Edit puts the text back in the composer; Discard drops it.
//
// The rows are the sender's own, in memory for this session only. They are not
// a draft, are never written to storage, and are cleared on sign-out.

// UnsentReason says why a row is not a delivered message.
type UnsentReason string

const (
	// UnsentSending: the request is in flight (no reason, no controls).
	UnsentSending UnsentReason = ""
	// UnsentOffline: the connection was down or the server could not be
	// reached. The client sends it again by itself when the connection returns.
	UnsentOffline UnsentReason = "offline"
	// UnsentFailed: the server answered with a refusal or an error. Only the
	// sender decides what happens next.
	UnsentFailed UnsentReason = "failed"
)

// UnsentMessage is one message the sender has not seen delivered.
type UnsentMessage struct {
	ID, ConversationID, Body string
	Reason                   UnsentReason
}

const (
	issue3737Retry   = "unsent-retry"
	issue3737Edit    = "unsent-edit"
	issue3737Discard = "unsent-discard"

	keyIssue3737Heading  = "chat.issue3737.heading"
	keyIssue3737Sending  = "chat.issue3737.sending"
	keyIssue3737Sending2 = "chat.issue3737.sending_aria"
	keyIssue3737Offline  = "chat.issue3737.offline"
	keyIssue3737Failed   = "chat.issue3737.failed"
	keyIssue3737Retry    = "chat.issue3737.retry"
	keyIssue3737Edit     = "chat.issue3737.edit"
	keyIssue3737Discard  = "chat.issue3737.discard"
	keyIssue3737RetryA   = "chat.issue3737.retry_aria"
	keyIssue3737EditA    = "chat.issue3737.edit_aria"
	keyIssue3737DropA    = "chat.issue3737.discard_aria"
)

var issue3737Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3737Heading:  "Not sent",
		keyIssue3737Sending:  "Sending…",
		keyIssue3737Sending2: "Sending your message: {preview}",
		keyIssue3737Offline:  "You are offline. This sends when you reconnect.",
		keyIssue3737Failed:   "Something went wrong, so this was not sent.",
		keyIssue3737Retry:    "Retry",
		keyIssue3737Edit:     "Edit",
		keyIssue3737Discard:  "Discard",
		keyIssue3737RetryA:   "Retry sending: {preview}",
		keyIssue3737EditA:    "Edit unsent message: {preview}",
		keyIssue3737DropA:    "Discard unsent message: {preview}",
	},
	"de-DE": {
		keyIssue3737Heading:  "Nicht gesendet",
		keyIssue3737Sending:  "Wird gesendet …",
		keyIssue3737Sending2: "Ihre Nachricht wird gesendet: {preview}",
		keyIssue3737Offline:  "Sie sind offline. Die Nachricht wird gesendet, sobald die Verbindung wieder besteht.",
		keyIssue3737Failed:   "Etwas ist schiefgelaufen, die Nachricht wurde nicht gesendet.",
		keyIssue3737Retry:    "Erneut senden",
		keyIssue3737Edit:     "Bearbeiten",
		keyIssue3737Discard:  "Verwerfen",
		keyIssue3737RetryA:   "Erneut senden: {preview}",
		keyIssue3737EditA:    "Nicht gesendete Nachricht bearbeiten: {preview}",
		keyIssue3737DropA:    "Nicht gesendete Nachricht verwerfen: {preview}",
	},
	"ar": {
		keyIssue3737Heading:  "لم تُرسل",
		keyIssue3737Sending:  "جارٍ الإرسال…",
		keyIssue3737Sending2: "جارٍ إرسال رسالتك: {preview}",
		keyIssue3737Offline:  "أنت غير متصل. ستُرسل عند عودة الاتصال.",
		keyIssue3737Failed:   "حدث خطأ، لذلك لم تُرسل هذه الرسالة.",
		keyIssue3737Retry:    "إعادة المحاولة",
		keyIssue3737Edit:     "تعديل",
		keyIssue3737Discard:  "تجاهل",
		keyIssue3737RetryA:   "إعادة محاولة إرسال: {preview}",
		keyIssue3737EditA:    "تعديل الرسالة غير المرسلة: {preview}",
		keyIssue3737DropA:    "تجاهل الرسالة غير المرسلة: {preview}",
	},
}

// issue3737Preview is the short form of a body that names a control.
func issue3737Preview(body string) string {
	text := strings.Join(strings.Fields(body), " ")
	runes := []rune(text)
	if len(runes) <= 60 {
		return text
	}
	return strings.TrimSpace(string(runes[:60])) + "…"
}

// issue3737Open returns the rows that belong to the open conversation.
func issue3737Open(m Model) []UnsentMessage {
	if m.SelectedID == "" || len(m.Unsent) == 0 {
		return nil
	}
	var rows []UnsentMessage
	for _, row := range m.Unsent {
		if row.ConversationID == m.SelectedID {
			rows = append(rows, row)
		}
	}
	return rows
}

// issue3737Rows draws the open conversation's rows, oldest first.
func issue3737Rows(m Model) []ui.Node {
	rows := issue3737Open(m)
	nodes := make([]ui.Node, 0, len(rows))
	for _, row := range rows {
		nodes = append(nodes, html.WithKey(issue3737Row(m, row), "unsent:"+row.ID))
	}
	return nodes
}

func issue3737Row(m Model, row UnsentMessage) ui.Node {
	preview := issue3737Preview(row.Body)
	body := html.P(html.Props{Class: "unsent-body", Dir: "auto", Text: row.Body})
	rowID := "unsent-row-" + row.ID
	if row.Reason == UnsentSending {
		return html.Div(html.Props{ID: rowID, Class: "unsent-row", Role: "group", Data: map[string]string{"unsent": row.ID, "state": "sending"}, Raw: map[string]any{"tabindex": "-1"},
			Aria: map[string]string{"label": laneTextf(m, issue3737Copy, keyIssue3737Sending2, map[string]string{"preview": preview}), "busy": "true"}},
			body,
			html.P(html.Props{Class: "unsent-status", Role: "status", Text: laneText(m, issue3737Copy, keyIssue3737Sending)}))
	}
	reason := keyIssue3737Failed
	if row.Reason == UnsentOffline {
		reason = keyIssue3737Offline
	}
	button := func(action, label, aria string, primary bool) ui.Node {
		class := "button secondary small unsent-action"
		if primary {
			class = "button small unsent-action"
		}
		return html.Button(html.Props{Class: class, Type: "button", Data: map[string]string{"action": action, "id": row.ID},
			Aria: map[string]string{"label": laneTextf(m, issue3737Copy, aria, map[string]string{"preview": preview})}, Text: laneText(m, issue3737Copy, label)})
	}
	return html.Div(html.Props{ID: rowID, Class: "unsent-row unsent-failed", Data: map[string]string{"unsent": row.ID, "state": "failed"}, Raw: map[string]any{"tabindex": "-1"}},
		body,
		html.P(html.Props{Class: "unsent-status", Role: "alert", Aria: map[string]string{"atomic": "true"}},
			icon("warning"),
			html.Strong(html.Props{Text: laneText(m, issue3737Copy, keyIssue3737Heading)}),
			html.Span(html.Props{Text: laneText(m, issue3737Copy, reason)})),
		html.Div(html.Props{Class: "unsent-actions"},
			button(issue3737Retry, keyIssue3737Retry, keyIssue3737RetryA, true),
			button(issue3737Edit, keyIssue3737Edit, keyIssue3737EditA, false),
			button(issue3737Discard, keyIssue3737Discard, keyIssue3737DropA, false)))
}

// issue3737Dispatch routes a control to its callback and reports whether the
// action was one of the three.
func issue3737Dispatch(m Model, action, id string) bool {
	var call func(string)
	switch action {
	case issue3737Retry:
		call = m.Callbacks.RetryUnsent
	case issue3737Edit:
		call = m.Callbacks.EditUnsent
	case issue3737Discard:
		call = m.Callbacks.DiscardUnsent
	default:
		return false
	}
	if call != nil && id != "" {
		call(id)
	}
	return true
}

// issue3737Click handles the controls on a click.
func issue3737Click(e ui.MouseEvent, m Model) bool {
	action, id, _ := eventAction(e)
	if !issue3737Dispatch(m, action, id) {
		return false
	}
	switch action {
	case issue3737Retry:
		// The row stays and turns into Sending; keep the keyboard on it.
		focusField("unsent-row-" + id)
	case issue3737Edit, issue3737Discard:
		FocusComposerFor(m.SelectedID)
	}
	return true
}

// Issue3737Styles: the row reads as the sender's own message that is waiting,
// not as a delivered one; logical properties keep it right in RTL and the
// controls are 44px targets on phones.
const Issue3737Styles = `.unsent-row{margin:6px 16px;padding:8px 12px;border:1px dashed var(--hcm-color-border,var(--line));border-inline-start:3px solid var(--hcm-color-warning);border-radius:var(--hcm-radius-control);background:var(--surface);max-width:var(--chat-measure,720px);overflow-wrap:anywhere}` +
	`.unsent-row[data-state=sending]{border-inline-start-color:var(--muted);opacity:.8}.unsent-row:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.unsent-body{margin:0 0 4px;white-space:pre-wrap}.unsent-status{margin:0;display:flex;flex-wrap:wrap;align-items:center;gap:6px;font-size:.8125rem;color:var(--muted)}` +
	`.unsent-failed .unsent-status{color:inherit}.unsent-status .chat-icon{width:16px;height:16px;color:var(--hcm-color-warning)}` +
	`.unsent-actions{display:flex;flex-wrap:wrap;gap:8px;margin-top:8px}.unsent-action:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`@media (max-width:640px){.unsent-row{margin-inline:8px}.unsent-action{min-height:44px;min-width:44px}}` +
	`@media (prefers-reduced-motion:reduce){.unsent-row{transition:none;animation:none}}`
