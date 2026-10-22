package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// ReaderOthersSee is what the server tells the writer about how others read one
// of their own messages when it is reworded for them (CHATTONE-002): the
// reworded text, or that it is on its way, or that it is not available. It is
// only ever sent for the caller's own message.
type ReaderOthersSee struct {
	// State is "ready", "pending", "unavailable" or "written" (the writer chose
	// "Send as written" and others read the message as it was written).
	State string `json:"state"`
	Text  string `json:"text,omitempty"`
	// CanSendAsWritten is false where the administrator does not let members read
	// messages as written: the choice does not exist there.
	CanSendAsWritten bool `json:"can_send_as_written,omitempty"`
}

// ChatlangReportNoSuggestion is the reason a "Report this translation" carries
// when the reader gave no better translation.
const ChatlangReportNoSuggestion = "No suggestion given"

// chattoneW10Local is the page-session state of the writer's block and of the
// translation report form: which message has its report form open and what each
// press last did.
type chattoneW10Local struct {
	reporting string
	status    map[string]string
	// noticeGone hides the first-read notice for this page session once it was
	// answered; the answer is also remembered in the browser.
	noticeGone bool
}

func (l chattoneW10Local) with(key, value string) chattoneW10Local {
	next := map[string]string{}
	for k, v := range l.status {
		next[k] = v
	}
	if value == "" {
		delete(next, key)
	} else {
		next[key] = value
	}
	l.status = next
	return l
}

func (l chattoneW10Local) openReport(id string) chattoneW10Local {
	l.reporting = id
	return l.with("rp:"+id, "")
}

// chattoneW10Selection is the server's selection for this message, when it
// belongs to the revision on the page.
func chattoneW10Selection(m Model, msg Message) (ReaderSelection, bool) {
	if msg.PersonaActor != nil {
		return ReaderSelection{}, false
	}
	return chatlangSelectionFor(m, msg)
}

func chattoneW10Has(kinds []chatrender.Kind, want chatrender.Kind) bool {
	for _, kind := range kinds {
		if kind == want {
			return true
		}
	}
	return false
}

// RenderingMessageLabel is the accessible name a reworded message carries
// ("Reworded message from Dana"); it is empty for a message read as written.
func RenderingMessageLabel(m Model, msg Message) string {
	sel, ok := chattoneW10Selection(m, msg)
	if !ok || sel.Mark.State != "ready" || !chattoneW10Has(sel.Mark.Kinds, chatrender.Reword) {
		return ""
	}
	return chattoneW10Name(m.Locale, "reworded_from", chatlangAuthor(msg))
}

// chattoneW10Aria is the article's aria attributes for a message; nil leaves
// the article as it was.
func chattoneW10Aria(m Model, msg Message) map[string]string {
	if label := RenderingMessageLabel(m, msg); label != "" {
		return map[string]string{"label": label}
	}
	return nil
}

// chattoneExtras are the nodes of the reword feature that follow a message's
// text: the reader's mark and "Show as written" on a reworded message, and the
// writer's "Others see:" under their own message.
func chattoneExtras(m Model, h handlers, msg Message) []ui.Node {
	if m.EditingID == msg.ID {
		return nil
	}
	sel, ok := chattoneW10Selection(m, msg)
	if !ok {
		return nil
	}
	var nodes []ui.Node
	if msg.AuthorID != "" && msg.AuthorID == m.CurrentUser {
		if block := chattoneOthersSee(m, h, msg, sel); block != nil {
			nodes = append(nodes, block)
		}
		return nodes
	}
	// A translated and reworded message keeps the combined mark of the
	// translation view; this is the reword-only view.
	if sel.Mark.State == "ready" && chattoneW10Has(sel.Mark.Kinds, chatrender.Reword) && !chattoneW10Has(sel.Mark.Kinds, chatrender.Translate) {
		nodes = append(nodes, chattoneRewordedMark(m, h, msg, sel)...)
	}
	return nodes
}

// chattoneRewordedMark is the quiet mark in words on a reworded message and,
// where policy allows, "Show as written" on that message alone, which reveals
// the original in place, marked as such, and "Show reworded" to return.
func chattoneRewordedMark(m Model, h handlers, msg Message, sel ReaderSelection) []ui.Node {
	shown := h.local.chatlang.originalShown(m.SelectedID, msg.ID)
	parts := []ui.Node{html.Span(html.Props{Class: "chatlang-mark-text", Text: RenderingText(m.Locale, "reworded")})}
	canWritten := sel.Mark.CanShowAsWritten && msg.Body != ""
	if canWritten {
		label, aria := "show_written", "show_written_a"
		if shown {
			label, aria = "hide_written", "hide_written_a"
		}
		parts = append(parts, html.Button(html.Props{Class: "chatlang-mark-button", Type: "button",
			Data: map[string]string{"action": "chatlang-original", "id": msg.ID},
			Aria: map[string]string{"expanded": boolString(shown), "controls": "chatlang-original-" + msg.ID, "label": chattoneW10Name(m.Locale, aria, chatlangAuthor(msg))},
			Text: chattoneW10Text(m.Locale, label)}))
	}
	nodes := []ui.Node{html.Div(html.Props{Class: "chatlang-mark", Data: map[string]string{"chatlang-state": "reworded", "message-id": msg.ID}}, parts...)}
	if canWritten && shown {
		textProps := html.Props{Class: "chatlang-original-text", Dir: "auto"}
		if lang := chatlangKnownTag(sel.Mark.SourceLanguage); lang != "" {
			textProps.Lang, textProps.Dir = lang, chatlangDir(lang)
		}
		nodes = append(nodes, html.Div(html.Props{ID: "chatlang-original-" + msg.ID, Class: "chatlang-original", Data: map[string]string{"chatlang-original": msg.ID}},
			html.Span(html.Props{Class: "chatlang-original-label", Text: chattoneW10Text(m.Locale, "as_written")}),
			html.Div(textProps, markdownMessageBody(m, msg.Body)...)))
	}
	return nodes
}

// chattoneOthersSee is the writer's side of rewording: under their own message,
// "Others see:" with the wording and two actions. Nothing is drawn until the
// server has said a rewording applies to this message.
func chattoneOthersSee(m Model, h handlers, msg Message, sel ReaderSelection) ui.Node {
	see := sel.OthersSee
	if see == nil || see.State == "" {
		return nil
	}
	local := h.local.chatlang.w10
	status := local.status["rw:"+msg.ID]
	t := func(key string) string { return chattoneW10Text(m.Locale, key) }
	group := func(children ...ui.Node) ui.Node {
		return html.Div(html.Props{Class: "chattone-others", Role: "group", Data: map[string]string{"chattone-others": see.State, "message-id": msg.ID}, Aria: map[string]string{"label": t("others_group")}}, children...)
	}
	switch see.State {
	case "pending":
		return group(html.Span(html.Props{Class: "chattone-others-note", Role: "status", Text: t("others_pending")}))
	case "unavailable":
		return group(html.Span(html.Props{Class: "chattone-others-note", Role: "status", Text: t("others_unavailable")}))
	case "written":
		return group(html.Span(html.Props{Class: "chattone-others-note", Text: t("others_written")}))
	}
	if see.Text == "" {
		return nil
	}
	busy := status == "busy"
	canEdit := m.Callbacks.BeginEdit != nil && m.Callbacks.SetEditDraft != nil
	actions := []ui.Node{html.Button(html.Props{Class: "chatlang-mark-button", Type: "button", Disabled: busy || !canEdit, Title: t("edit_wording_a"),
		Data: map[string]string{"action": "chatlang-reword-edit", "id": msg.ID}, Aria: map[string]string{"label": t("edit_wording_a")}, Text: t("edit_wording")})}
	if see.CanSendAsWritten {
		actions = append(actions, html.Button(html.Props{Class: "chatlang-mark-button", Type: "button", Disabled: busy, Title: t("send_written_a"),
			Data: map[string]string{"action": "chatlang-reword-send", "id": msg.ID}, Aria: map[string]string{"label": t("send_written_a")}, Text: t("send_written")}))
	}
	children := []ui.Node{
		html.Span(html.Props{Class: "chattone-others-label", Text: t("others_see")}),
		html.Div(html.Props{Class: "chattone-others-text", Dir: "auto", Data: map[string]string{"chattone-others-text": msg.ID}}, markdownMessageBody(m, see.Text)...),
		html.Div(html.Props{Class: "chattone-others-actions"}, actions...),
	}
	switch status {
	case "busy":
		children = append(children, html.Span(html.Props{Class: "chattone-others-note", Role: "status", Text: t("sending")}))
	case "failed":
		children = append(children, html.Span(html.Props{Class: "chattone-others-note chattone-others-error", Role: "alert", Text: t("send_failed")}))
	}
	return group(children...)
}

// chattoneRewordClick handles the writer's two actions. It reports whether the
// action was one of them.
func chattoneRewordClick(m Model, local localStore, action, id string) bool {
	switch action {
	case "chatlang-reword-edit":
		sel, ok := m.ReaderSelections[id]
		if !ok || sel.OthersSee == nil || sel.OthersSee.Text == "" || m.Callbacks.BeginEdit == nil || m.Callbacks.SetEditDraft == nil {
			return true
		}
		// The reworded wording becomes the draft of an ordinary edit: the writer
		// changes it and saves, and the message then says what they chose.
		m.Callbacks.SetEditDraft(id, sel.OthersSee.Text)
		m.Callbacks.BeginEdit(id)
		return true
	case "chatlang-reword-send":
		if id == "" || m.SelectedID == "" {
			return true
		}
		local.update(func(u *localUI) { u.chatlang.w10 = u.chatlang.w10.with("rw:"+id, "busy") })
		chattoneSendAsWritten(m.SelectedID, id, func(err error) {
			ui.PostAsync(func() {
				local.update(func(u *localUI) {
					if err != nil {
						u.chatlang.w10 = u.chatlang.w10.with("rw:"+id, "failed")
					} else {
						u.chatlang.w10 = u.chatlang.w10.with("rw:"+id, "")
					}
				})
			})
		})
		return true
	}
	return false
}
