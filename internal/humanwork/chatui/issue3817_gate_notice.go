package chatui

import (
	"net/url"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3817: the notice a channel's reviewers read when a request to join is
// waiting. It says what waits and opens the join form's review queue (the same
// link the channel details use); it holds no applicant and no answer.

const gateNoticeCopyTable = "\ngate_review\u0000A request to join a channel is waiting for your decision\u0000Eine Beitrittsanfrage für einen Kanal wartet auf Ihre Entscheidung\u0000طلب انضمام إلى قناة بانتظار قرارك\u0000" +
	"\ngate_review_overdue\u0000A request to join a channel has been waiting longer than planned\u0000Eine Beitrittsanfrage für einen Kanal wartet länger als geplant\u0000طلب انضمام إلى قناة ينتظر منذ مدة أطول من المخطط\u0000" +
	"\ngate_review_escalated\u0000A request to join a channel needs a decision from an administrator\u0000Eine Beitrittsanfrage für einen Kanal braucht die Entscheidung eines Administrators\u0000طلب انضمام إلى قناة يحتاج إلى قرار من مسؤول\u0000" +
	"\ngate_review_open\u0000Review requests\u0000Anfragen prüfen\u0000مراجعة الطلبات\u0000" +
	"\ngate_admitted\u0000You were let in\u0000Sie wurden eingelassen\u0000تم قبولك\u0000" +
	"\ngate_declined\u0000You were not admitted\u0000Sie wurden nicht zugelassen\u0000لم يتم قبولك\u0000" +
	"\ngate_reason\u0000Reason\u0000Grund\u0000السبب\u0000" +
	"\ngate_admitted_open\u0000Open the channel\u0000Kanal öffnen\u0000فتح القناة\u0000" +
	"\ngate_declined_open\u0000View your request\u0000Ihre Anfrage ansehen\u0000عرض طلبك\u0000"

func gateNoticeText(locale, key string) string {
	return integrate1Copy(gateNoticeCopyTable, locale, key)
}

func moderationGateReviewNotice(locale string, n chat.ModerationNotice) ui.Node {
	children := []ui.Node{html.H3(html.Props{Text: gateNoticeText(locale, n.Outcome)})}
	if !n.At.IsZero() {
		children = append(children, html.P(html.Props{Class: "chatremove-date", Text: n.At.UTC().Format("2006-01-02")}))
	}
	if n.ConversationID != "" {
		children = append(children, html.A(html.Props{Class: "chatmod005-channel", Href: "#gate=" + url.QueryEscape(n.ConversationID), Data: map[string]string{"gate-open": n.ConversationID}, Text: gateNoticeText(locale, "gate_review_open")}))
	}
	return html.Aside(html.Props{Class: "chatremove-notice", Role: "status", Aria: map[string]string{"live": "polite"}}, children...)
}
