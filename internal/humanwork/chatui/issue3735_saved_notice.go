package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3735: the notice a person reads when a message they saved is due. It
// says so and opens the Saved list, where the message, their note and their
// labels are; the notice itself holds a reference only.

const savedNoticeCopyTable = "\nsaved_reminder\u0000A message you saved is due\u0000Eine von Ihnen gespeicherte Nachricht ist fällig\u0000رسالة حفظتها حان موعدها\u0000" +
	"\nsaved_reminder_open\u0000Open saved messages\u0000Gespeicherte Nachrichten öffnen\u0000فتح الرسائل المحفوظة\u0000"

func savedNoticeText(locale, key string) string {
	return integrate1Copy(savedNoticeCopyTable, locale, key)
}

func moderationSavedReminderNotice(locale string, n chat.ModerationNotice) ui.Node {
	children := []ui.Node{html.H3(html.Props{Text: savedNoticeText(locale, "saved_reminder")})}
	if !n.At.IsZero() {
		children = append(children, html.P(html.Props{Class: "chatremove-date", Text: formatDay(locale, n.At, true)}))
	}
	children = append(children, html.A(html.Props{Class: "chatmod005-channel", Href: "#saved", Text: savedNoticeText(locale, "saved_reminder_open")}))
	return html.Aside(html.Props{Class: "chatremove-notice", Role: "status", Aria: map[string]string{"live": "polite"}}, children...)
}
