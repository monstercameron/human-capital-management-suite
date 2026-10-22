package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATLANG-006 on the reader's side: the "may be inaccurate" mark for a pair
// that passed only the lower quality mark, "Report this translation" with the
// reader's optional better wording, and the notice a person sees the first time
// they read a translated message. The server decides what is translated and
// whether the pair is marked; this only says so.

// chatlangFirstTranslated is the first translated message of the open
// conversation, the one that carries the first-read notice.
func chatlangFirstTranslated(m Model) string {
	for _, msg := range m.Messages {
		if chatlangMessageOf(m, msg).Translated {
			return msg.ID
		}
	}
	return ""
}

// chatlangTranslationAddons returns what joins the mark of a translated message
// (the "may be inaccurate" words and the report button) and what follows it (the
// report form and, once per person, the first-read notice).
func chatlangTranslationAddons(m Model, h handlers, msg Message, canOriginal bool) (words, buttons, below []ui.Node) {
	sel, ok := chattoneW10Selection(m, msg)
	if !ok {
		return nil, nil, nil
	}
	t := func(key string) string { return chattoneW10Text(m.Locale, key) }
	local := h.local.chatlang.w10
	if sel.Inaccurate {
		words = append(words, html.Span(html.Props{Class: "chatlang-mark-text chatlang-inaccurate", Title: t("inaccurate_help"), Data: map[string]string{"chatlang-inaccurate": "true"}, Text: t("inaccurate")}))
	}
	if msg.AuthorID != m.CurrentUser {
		buttons = append(buttons, html.Button(html.Props{Class: "chatlang-mark-button", Type: "button",
			Data: map[string]string{"action": "chatlang-report-open", "id": msg.ID},
			Aria: map[string]string{"expanded": boolString(local.reporting == msg.ID), "label": chattoneW10Name(m.Locale, "report_a", chatlangAuthor(msg))},
			Text: t("report")}))
	}
	if local.reporting == msg.ID {
		below = append(below, chatlangReportForm(m, msg, local))
	}
	if msg.ID == chatlangFirstTranslated(m) && !local.noticeGone && !chatlangNoticeStoredSeen() {
		shown := h.local.chatlang.originalShown(m.SelectedID, msg.ID)
		notice := []ui.Node{html.Span(html.Props{Class: "chatlang-notice-text", Text: t("notice_text")})}
		// "Show original" is offered only where the reader may see the original.
		if canOriginal {
			notice = append(notice, html.Button(html.Props{Class: "chatlang-mark-button", Type: "button", Disabled: shown, Data: map[string]string{"action": "chatlang-notice-show", "id": msg.ID}, Text: t("notice_show")}))
		}
		notice = append(notice, html.Button(html.Props{Class: "chatlang-mark-button", Type: "button", Data: map[string]string{"action": "chatlang-notice-ok"}, Text: t("notice_ok")}))
		below = append(below, html.Div(html.Props{Class: "chatlang-notice", Role: "group", Data: map[string]string{"chatlang-notice": "first-read"}, Aria: map[string]string{"label": t("notice_group")}}, notice...))
	}
	return words, buttons, below
}

func chatlangReportForm(m Model, msg Message, local chattoneW10Local) ui.Node {
	t := func(key string) string { return chattoneW10Text(m.Locale, key) }
	status := local.status["rp:"+msg.ID]
	field := "chatlang-report-text-" + msg.ID
	if status == "sent" {
		return html.Div(html.Props{Class: "chatlang-report", Role: "group", Aria: map[string]string{"label": t("report_group")}},
			html.P(html.Props{Class: "chatlang-report-status", Role: "status", Text: t("report_sent")}),
			html.Button(html.Props{Class: "chatlang-mark-button", Type: "button", Data: map[string]string{"action": "chatlang-report-cancel", "id": msg.ID}, Text: t("report_close")}))
	}
	busy := status == "busy"
	children := []ui.Node{
		html.Label(html.Props{For: field, Class: "chatlang-fix-label", Text: t("report_prompt")}),
		html.Textarea(html.Props{ID: field, Class: "chatlang-report-text", Dir: "auto", Disabled: busy, Rows: 2, MaxLength: 1000}),
		html.Div(html.Props{Class: "chatlang-fix-options"},
			html.Button(html.Props{Class: "chatlang-fix-option", Type: "button", Disabled: busy, Data: map[string]string{"action": "chatlang-report-send", "id": msg.ID}, Text: t("report_send")}),
			html.Button(html.Props{Class: "chatlang-fix-option", Type: "button", Disabled: busy, Data: map[string]string{"action": "chatlang-report-cancel", "id": msg.ID}, Text: t("report_cancel")})),
	}
	switch status {
	case "busy":
		children = append(children, html.P(html.Props{Class: "chatlang-fix-status", Role: "status", Text: t("report_sending")}))
	case "failed":
		children = append(children, html.P(html.Props{Class: "chatlang-fix-status", Role: "alert", Text: t("report_failed")}))
	}
	return html.Div(html.Props{Class: "chatlang-fix chatlang-report", Role: "group", Aria: map[string]string{"label": t("report_group")}}, children...)
}

// chatlangW10Click handles the report form and the first-read notice. It
// reports whether the action was one of them.
func chatlangW10Click(m Model, local localStore, action, id string) bool {
	switch action {
	case "chatlang-report-open":
		local.update(func(u *localUI) { u.chatlang.w10 = u.chatlang.w10.openReport(id) })
	case "chatlang-report-cancel":
		local.update(func(u *localUI) { u.chatlang.w10 = u.chatlang.w10.openReport("") })
	case "chatlang-report-send":
		sel, ok := m.ReaderSelections[id]
		if !ok || sel.Rendering.Message != id || m.SelectedID == "" {
			return true
		}
		reason := strings.TrimSpace(domValue("chatlang-report-text-" + id))
		if reason == "" {
			reason = ChatlangReportNoSuggestion
		}
		local.update(func(u *localUI) { u.chatlang.w10 = u.chatlang.w10.with("rp:"+id, "busy") })
		chatlangSendReport(m.SelectedID, sel.Rendering, reason, func(err error) {
			ui.PostAsync(func() {
				local.update(func(u *localUI) {
					if err != nil {
						u.chatlang.w10 = u.chatlang.w10.with("rp:"+id, "failed")
					} else {
						u.chatlang.w10 = u.chatlang.w10.with("rp:"+id, "sent")
					}
				})
			})
		})
	case "chatlang-notice-show":
		chatlangRememberNoticeSeen()
		local.update(func(u *localUI) {
			u.chatlang.w10.noticeGone = true
			u.chatlang = u.chatlang.toggleOriginal(id)
		})
	case "chatlang-notice-ok":
		chatlangRememberNoticeSeen()
		local.update(func(u *localUI) { u.chatlang.w10.noticeGone = true })
	default:
		return false
	}
	return true
}

// chatsearchWrittenControl is the search page's "Written in" choice: it narrows
// the results to messages written in one language, whatever language the reader
// reads them in.
func chatsearchWrittenControl(locale string) ui.Node {
	options := []ui.Node{html.Option(html.Props{Value: "", Text: chattoneW10Text(locale, "search_any_language")})}
	for _, tag := range []string{"en", "de", "fr", "es", "pt", "ar", "ja", "hi"} {
		options = append(options, html.Option(html.Props{Value: tag, Text: chatlangLanguageName(locale, tag)}))
	}
	return html.Label(html.Props{Text: chattoneW10Text(locale, "search_written"), For: "chatsearch-written"}, html.Select(html.Props{ID: "chatsearch-written", Name: "written"}, options...))
}
