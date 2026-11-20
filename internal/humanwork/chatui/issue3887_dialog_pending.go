package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue #3887: the report, remove and restore dialogs drew the loading
// placeholder straight into the dimmed backdrop, with no dialog around it and no
// way to leave. While the dialog's content is on its way, and when the read
// failed, the client now shows this frame: the same dialog the answer will
// fill, with its heading and Close, the placeholder (with the slow line and Try
// again after a few seconds), and a visible Cancel.

// moderationDialogTitleKey names the dialog by what it will do, before the
// server has said whose message it is.
func moderationDialogTitleKey(action string) string {
	switch action {
	case "report":
		return "report"
	case "restore":
		return "restore"
	case "remove":
		return "remove"
	case "dismiss":
		return "dismiss"
	}
	return "moderation"
}

// ModerationDialogPendingMarkup is the dialog while its content loads (errorKey
// empty) or after the read failed (errorKey names the failure in the moderation
// copy). The client reads data-chatremove-retry to read the dialog again and
// data-chatremove-close to leave it.
func ModerationDialogPendingMarkup(locale, action, errorKey string) string {
	t := func(key string) string { return chatremoveText(locale, key) }
	retry := map[string]string{"chatremove-retry": "true"}
	var body, leave ui.Node
	if errorKey == "" {
		body = ChatLoadingFrame(LoadingFrame{Locale: locale, Shape: LoadingShapeMessage, Rows: 2, Status: t("loading"), RetryData: retry})
		leave = html.Button(html.Props{Type: "button", Class: "button secondary", Data: map[string]string{"chatremove-close": "true"}, Text: t("cancel")})
	} else {
		what := t(errorKey)
		if what == "" {
			what = t("failed")
		}
		body = ChatLoadFailed(locale, what, retry, nil)
		leave = html.Button(html.Props{Type: "button", Class: "button secondary", Data: map[string]string{"chatremove-close": "true"}, Text: t("close")})
	}
	section := html.Section(html.Props{Class: "chatremove chat-dialog chatremove-dialog-pending", Lang: locale, Dir: direction(locale), Aria: map[string]string{"labelledby": "chatremove-title"}, Data: map[string]string{"chatremove-pending": "true"}},
		chatbug085Heading(locale, t(moderationDialogTitleKey(action))),
		body,
		html.Div(html.Props{Class: "chatremove-actions"}, leave))
	markup, err := ui.RenderToString(section)
	if err != nil {
		return ""
	}
	return markup
}
