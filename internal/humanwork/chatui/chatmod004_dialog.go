package chatui

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// ModerationTargetView is the message a dialog is about and the person who wrote
// it, exactly as the server projected them for the person acting.
type ModerationTargetView struct {
	AuthorID, HomeTenantID, AuthorName, Body string
}

// ModerationDialogModel is the removal, restore or report dialog. The server
// renders it (ModerationDialog) and the browser client shows it over the chat.
type ModerationDialogModel struct {
	Model                    Model
	Selection                chat.RemovalSelection
	Target                   ModerationTargetView
	Preview                  *chat.RemovalPreview
	Action, ReasonCode, Note string
	ErrorCode                string
	Report, Sent             bool
	// CaseID is the queue item a dialog opened from the queue decides.
	CaseID string
	// Choices are the messages around the target that the moderator may tick to
	// remove together (chatmod004_selected.go), with the reader's time zone.
	Choices  []ModerationChoice
	TimeZone *time.Location
}

var moderationReasonCodes = []string{"harassment", "sensitive_information", "spam", "policy_violation"}

func chatmodName(locale, name string) string {
	if strings.TrimSpace(name) == "" {
		return chatremoveText(locale, "someone")
	}
	return name
}

func chatmodFill(text, name string) string { return strings.ReplaceAll(text, "{name}", name) }

// chatmodReasons is the short list of reasons, in ordinary words, as radio
// buttons: one choice is required and each is a button the person can see.
func chatmodReasons(locale, prefix, legend, chosen string) ui.Node {
	nodes := []ui.Node{html.Legend(html.Props{Text: legend})}
	for i, code := range moderationReasonCodes {
		id := prefix + "-" + strconv.Itoa(i)
		nodes = append(nodes, html.Div(html.Props{Class: "chatremove-reason"},
			html.Input(html.Props{ID: id, Type: "radio", Name: "reason", Value: code, Required: true, Checked: chosen == code}),
			html.Label(html.Props{For: id, Text: chatremoveText(locale, code)})))
	}
	return html.Fieldset(html.Props{Class: "chatremove-reasons"}, nodes...)
}

// chatmodQuote is the message the dialog is about, drawn as the conversation
// draws it (CHATBUG-085): a list is a list and bold is bold, not the dashes and
// asterisks that were typed. It is there to be read, so nothing in it can be
// pressed or take the caret.
func chatmodQuote(m Model, t ModerationTargetView) ui.Node {
	name := chatmodName(m.Locale, t.AuthorName)
	return html.Figure(html.Props{Class: "chatremove-quote"},
		html.Figcaption(html.Props{ID: "chatremove-author", Dir: "auto", Text: chatmodFill(chatremoveText(m.Locale, "message_from"), name)}),
		html.Blockquote(html.Props{}, html.Div(html.Props{Class: "message-body", Dir: "auto", Raw: map[string]any{"inert": ""}}, markdownMessageBody(m, t.Body)...)))
}

func chatmodError(locale, code string) []ui.Node {
	if code == "" {
		return nil
	}
	key := "failed"
	switch code {
	case "conflict":
		key = "conflict"
	case "permission_denied":
		key = "forbidden"
	}
	return []ui.Node{html.P(html.Props{ID: "chatremove-error", Role: "alert", Text: chatremoveText(locale, key)})}
}

// ModerationDialog is the one dialog behind Report, Remove and Restore. Removing
// one message is one decision: it names the author, shows the message, asks for
// a reason from the short list and an optional note, and one button does it.
// Removing several is a second, plainly separate form that shows the count
// before anything is removed.
func ModerationDialog(m ModerationDialogModel) ui.Node {
	locale := m.Model.Locale
	t := func(key string) string { return chatremoveText(locale, key) }
	name := chatmodName(locale, m.Target.AuthorName)
	cid := m.Selection.ConversationID
	post := ""
	if len(m.Selection.PostIDs) == 1 {
		post = m.Selection.PostIDs[0]
	}
	// A dialog opened from an item of the queue decides that item: it carries the
	// item's case, and its button is the decision. One opened from a message's
	// menu removes or restores that message directly.
	title, help, kind := "remove_title", "remove_help", "quick"
	switch {
	case m.Report:
		title, help, kind = "report", "report_private", "report"
	case m.Action == "restore":
		title, help = "restore_title", "restore_help"
	case m.Action == "message":
		title, help = "message_title", "message_help"
	}
	if m.CaseID != "" {
		kind = "resolve"
	}
	children := []ui.Node{chatbug085Heading(locale, chatmodFill(t(title), name)), html.P(html.Props{ID: "chatremove-help", Dir: "auto", Text: chatmodFill(t(help), name)})}
	if m.Sent {
		children = append(children, html.P(html.Props{Role: "status", Text: t("sent")}), html.A(html.Props{Href: ModerationPageHref, Data: map[string]string{"chatremove-close": "true"}, Text: t("close")}))
		return chatmodDialogShell(locale, children)
	}
	if m.Action != "restore" || m.Report {
		children = append(children, chatmodQuote(m.Model, m.Target))
	}
	describedby := "chatremove-help"
	if m.ErrorCode != "" {
		describedby += " chatremove-error"
	}
	fields := []ui.Node{}
	needsReason := (m.Action != "restore" && m.Action != "message") || m.Report
	if needsReason {
		legend := t("why_removed")
		if m.Report {
			legend = t("why_reported")
		}
		fields = append(fields, chatmodReasons(locale, "chatremove-reason", legend, m.ReasonCode))
	}
	noteLabel := chatmodFill(t("note_for_author"), name)
	switch {
	case m.Report:
		noteLabel = t("note")
	case m.Action == "message":
		noteLabel = chatmodFill(t("message_label"), name)
	}
	fields = append(fields, html.Label(html.Props{For: "chatremove-note", Dir: "auto", Text: noteLabel}), html.Textarea(html.Props{ID: "chatremove-note", Name: "note", Rows: 3, MaxLength: 2000, Required: m.Action == "message", Data: map[string]string{"chat-value": m.Note}}))
	fields = append(fields, chatmodError(locale, m.ErrorCode)...)
	button, decision := "remove", "remove"
	switch {
	case m.Report:
		button = "submit"
	case m.Action == "restore":
		button, decision = "restore", "restore"
	case m.Action == "message":
		button, decision = "send_message", "message_author"
	}
	submit := html.Props{Type: "submit", Class: "primary", Text: t(button)}
	if needsReason {
		// CHATBUG-085: nothing is sent or removed without a reason. The button is
		// off until one is chosen; the client turns it on when a reason is.
		submit.Disabled = m.ReasonCode == ""
		submit.Data = map[string]string{chatbug085NeedsReason: "true"}
	}
	if m.CaseID != "" {
		submit.Name, submit.Value = "action", decision
	}
	fields = append(fields, html.Div(html.Props{Class: "chatremove-actions"},
		html.Button(submit),
		html.A(html.Props{Href: ModerationPageHref, Data: map[string]string{"chatremove-close": "true"}, Text: t("cancel")})))
	data := map[string]string{"chatremove": kind, "conversation-id": cid, "removal-action": m.Action}
	if post != "" {
		data["post-id"] = post
	}
	if m.CaseID != "" {
		data["case-id"] = m.CaseID
	}
	children = append(children, html.Form(html.Props{Data: data, Aria: map[string]string{"describedby": describedby}}, fields...))
	if !m.Report && m.Action != "restore" && m.Action != "message" && m.CaseID == "" {
		if picked := chatmodSelected(m); picked != nil {
			children = append(children, picked)
		}
		children = append(children, chatmodSeveral(m, name))
	}
	return chatmodDialogShell(locale, children)
}

func chatmodDialogShell(locale string, children []ui.Node) ui.Node {
	return html.Section(html.Props{Class: "chatremove chat-dialog", Role: "dialog", Lang: locale, Dir: direction(locale), Aria: map[string]string{"labelledby": "chatremove-title", "modal": "true"}}, children...)
}

// chatmodSeveral is the bulk form: every message of this author in this channel
// between two times. The count comes first; nothing is removed until it is
// confirmed.
func chatmodSeveral(m ModerationDialogModel, name string) ui.Node {
	locale := m.Model.Locale
	t := func(key string) string { return chatremoveText(locale, key) }
	fields := []ui.Node{
		html.P(html.Props{Dir: "auto", Text: chatmodFill(t("several_help"), name)}),
		chatmodReasons(locale, "chatremove-bulk-reason", t("why_removed"), m.ReasonCode),
		html.Label(html.Props{For: "chatremove-from", Text: t("from")}), html.Input(html.Props{ID: "chatremove-from", Type: "datetime-local", Name: "from", Required: true}),
		html.Label(html.Props{For: "chatremove-until", Text: t("until")}), html.Input(html.Props{ID: "chatremove-until", Type: "datetime-local", Name: "until", Required: true}),
		html.Input(html.Props{Type: "hidden", Name: "author", Value: m.Target.AuthorID, Data: map[string]string{"home-tenant": m.Target.HomeTenantID}}),
	}
	if m.Preview != nil {
		fields = append(fields, html.P(html.Props{Role: "status", Data: map[string]string{"chatremove-count": ""}, Text: ModerationCountText(locale, m.Preview.Count)}))
	}
	label, action := "preview", "preview"
	if m.Preview != nil {
		label, action = "confirm", "apply"
	}
	fields = append(fields, html.Button(html.Props{Type: "submit", Class: "primary", Disabled: m.Preview != nil && m.Preview.Count == 0, Text: t(label)}))
	data := map[string]string{"chatremove": action, "conversation-id": m.Selection.ConversationID, "removal-action": "remove"}
	if m.Preview != nil {
		data["confirmation"] = m.Preview.Confirmation
		data["confirmed-count"] = strconv.Itoa(m.Preview.Count)
	}
	return html.Details(html.Props{Class: "chatremove-several"}, html.Summary(html.Props{Text: t("several")}), html.Form(html.Props{Data: data}, fields...))
}

// chatremoveTombstone is what a reader sees where an administrator removed a
// message.
func chatremoveTombstone(m Model, msg Message) ui.Node {
	return ModerationTombstone(m, msg, true)
}

// ModerationTombstone shares the retained-thread presentation for both commands.
// The caller supplies authoritative removal provenance, never authored text.
// The thread stays. The author of a removed message also sees why and how to ask
// for a review; a person who may restore it sees Restore.
func ModerationTombstone(m Model, msg Message, byAdministrator bool) ui.Node {
	key := "deleted"
	if byAdministrator {
		key = "removed"
	}
	children := []ui.Node{html.P(html.Props{Class: "message-body", Text: chatremoveText(m.Locale, key)})}
	if byAdministrator {
		children = append(children, chatmodTombstoneActions(m, msg)...)
	}
	if msg.Replies > 0 {
		children = append(children, html.Button(html.Props{Type: "button", Class: "thread-link", Disabled: m.Callbacks.OpenThread == nil, Data: map[string]string{"action": "thread", "id": msg.ID}, Text: m.t(KeyThread)}))
	}
	return html.Div(html.Props{Class: "message-row chatremove-tombstone", Data: map[string]string{"message-id": msg.ID}}, children...)
}

func chatmodTombstoneActions(m Model, msg Message) []ui.Node {
	var out []ui.Node
	if notice, ok := m.Moderation.Notices[msg.ID]; ok && msg.AuthorID != "" && msg.AuthorID == m.CurrentUser {
		out = append(out, html.P(html.Props{Class: "chatremove-own-reason", Dir: "auto", Text: strings.ReplaceAll(chatremoveText(m.Locale, "own_reason"), "{reason}", chatremoveReason(m.Locale, notice.Reason))}))
		if notice.CanAppeal {
			out = append(out, html.Form(html.Props{Class: "chatremove-appeal", Data: map[string]string{"chatremove": "appeal", "conversation-id": notice.ConversationID, "post-id": notice.PostID, "host-tenant": notice.HostTenantID}},
				html.Button(html.Props{Type: "submit", Class: "chatremove-link", Text: chatremoveText(m.Locale, "appeal")})))
		}
	}
	if cid := m.SelectedID; cid != "" && m.Moderation.CanRestoreIn(cid) {
		out = append(out, html.Button(html.Props{Type: "button", Class: "chatremove-link", Data: map[string]string{"chatremove-open": ModerationPageHref + "?" + chatmodQuery(m.Locale, cid, msg.ID, "restore")}, Text: chatremoveText(m.Locale, "restore")}))
	}
	return out
}

// chatmodQuery is the query of a dialog link: which message, which action, and
// the language the server must answer in.
func chatmodQuery(locale, cid, id, action string) string {
	return url.Values{"conversation": {cid}, "post": {id}, "action": {action}, "locale": {locale}}.Encode()
}
