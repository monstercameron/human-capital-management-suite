package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue #3948: Escape (or Cancel, Close, a press on the backdrop) threw away a
// report that had a reason chosen or details typed, with no word. A report with
// something in it now asks first. The question says plainly that nothing is
// kept: a report is held only in the open dialog until it is sent, and leaving
// forgets it, so a note about a workplace matter is never left behind in a
// draft. An empty report still closes at once.

const issue3948CopyTable = "\ndiscard_title\u0000Discard this report?\u0000Diesen Bericht verwerfen?\u0000تجاهل هذا البلاغ؟\u0000" +
	"\ndiscard_body\u0000It has not been sent. Nothing you wrote is saved or shared; if you discard it, it is gone.\u0000Er wurde noch nicht gesendet. Nichts von dem, was Sie geschrieben haben, wird gespeichert oder geteilt; wenn Sie ihn verwerfen, ist er weg.\u0000لم يُرسل بعد. لا يُحفظ شيء مما كتبته ولا يُشارك؛ وإذا تجاهلته فسيُحذف.\u0000" +
	"\ndiscard_keep\u0000Keep writing\u0000Weiterschreiben\u0000متابعة الكتابة\u0000" +
	"\ndiscard_yes\u0000Discard report\u0000Bericht verwerfen\u0000تجاهل البلاغ\u0000"

// ReportDiscardText answers the discard question's copy in en-US, de-DE and ar.
func ReportDiscardText(locale, key string) string {
	return integrate1Copy(issue3948CopyTable, locale, key)
}

// ReportDiscardMarkup is the question, as the dialog the client puts in place of
// the report form while it waits for the answer. Keep writing is first and is
// where focus starts, so a stray Enter or a second Escape loses nothing.
func ReportDiscardMarkup(locale string) string {
	t := func(key string) string { return ReportDiscardText(locale, key) }
	section := html.Section(html.Props{Class: "chatremove chat-dialog chatremove-discard", Lang: locale, Dir: direction(locale), Role: "alertdialog", Aria: map[string]string{"labelledby": "chatremove-discard-title", "describedby": "chatremove-discard-body"}, Data: map[string]string{"chatremove-discard-panel": "true"}},
		html.Div(html.Props{Class: "side-heading"}, html.H2(html.Props{ID: "chatremove-discard-title", Dir: "auto", Text: t("discard_title")})),
		html.P(html.Props{ID: "chatremove-discard-body", Dir: "auto", Text: t("discard_body")}),
		html.Div(html.Props{Class: "chatremove-actions"},
			html.Button(html.Props{Type: "button", Class: "primary", Data: map[string]string{"chatremove-discard": "keep"}, Text: t("discard_keep")}),
			html.Button(html.Props{Type: "button", Class: "button secondary danger", Data: map[string]string{"chatremove-discard": "yes"}, Text: t("discard_yes")})))
	markup, err := ui.RenderToString(section)
	if err != nil {
		return ""
	}
	return markup
}
