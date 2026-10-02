package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"strings"
	"time"
)

func renderAnnouncementAttempts(locale LocaleContext, attempts []AgentAnnouncementAttempt) ui.Node {
	if len(attempts) == 0 {
		return nil
	}
	labels := [3]string{"View posting history", "Veröffentlichungsverlauf anzeigen", "عرض سجل النشر"}
	entries := []ui.Node{}
	for _, attempt := range attempts {
		label := attempt.TimeLabel
		if label == "" {
			if at, err := time.Parse(time.RFC3339, attempt.At); err == nil {
				label = at.Format("2006-01-02 15:04 MST")
			}
		}
		result := AgentAnnouncementResultText(locale, attempt.ResultCode, attempt.Reason)
		outcome := ui.Node(html.Span(html.Props{Dir: "auto", Text: result}))
		if attempt.MessageHref != "" {
			outcome = html.A(html.Props{Href: attempt.MessageHref, Dir: "auto", Text: result})
		}
		entries = append(entries, html.Li(html.Props{}, html.Time(html.Props{Raw: map[string]any{"datetime": attempt.At}, Text: label}), ui.Text(" · "), outcome))
	}
	return html.Details(html.Props{Class: "agent-announcement-history"}, html.Summary(html.Props{Text: labels[agentRPLocaleIndex(locale)]}), html.Ul(html.Props{}, entries...))
}

func localizeAnnouncementFailure(locale LocaleContext, reason string) string {
	sentences := [][3]string{
		{"The preview expired or its documents or authority changed. Preview again before posting.", "Die Vorschau ist abgelaufen oder ihre Dokumente oder Berechtigungen haben sich geändert. Erstellen Sie vor dem Veröffentlichen eine neue Vorschau.", "انتهت صلاحية المعاينة أو تغيرت المستندات أو الصلاحيات. أنشئ معاينة جديدة قبل النشر."},
		{"The channel is locked or your posting access changed. Ask a channel manager to allow posting, then try again.", "Der Kanal ist gesperrt oder Ihre Veröffentlichungsberechtigung hat sich geändert. Bitten Sie die Kanalverwaltung, das Veröffentlichen zu erlauben, und versuchen Sie es erneut.", "القناة مقفلة أو تغيرت صلاحية النشر. اطلب من مدير القناة السماح بالنشر ثم حاول مجددًا."},
		{"The agent is paused or its access changed. Resume the agent and check its channel and document access, then preview again.", "Der Agent ist pausiert oder seine Berechtigungen haben sich geändert. Setzen Sie ihn fort, prüfen Sie den Kanal- und Dokumentzugriff und erstellen Sie eine neue Vorschau.", "الوكيل متوقف مؤقتًا أو تغيرت صلاحياته. استأنف تشغيله وتحقق من وصوله إلى القناة والمستندات ثم أعد المعاينة."},
		{"The documents or posting authority changed. Preview again before posting.", "Die Dokumente oder Veröffentlichungsberechtigungen haben sich geändert. Erstellen Sie vor dem Veröffentlichen eine neue Vorschau.", "تغيرت المستندات أو صلاحية النشر. أعد المعاينة قبل النشر."},
		{"The service is busy or unavailable. Try again in a few minutes.", "Der Dienst ist ausgelastet oder nicht verfügbar. Versuchen Sie es in einigen Minuten erneut.", "الخدمة مشغولة أو غير متاحة. حاول مجددًا بعد بضع دقائق."},
		{"Posting is temporarily unavailable. Try again in a few minutes; contact a workspace administrator if it continues.", "Das Veröffentlichen ist vorübergehend nicht verfügbar. Versuchen Sie es in einigen Minuten erneut. Wenden Sie sich bei weiteren Problemen an die Arbeitsbereichsverwaltung.", "النشر غير متاح مؤقتًا. حاول مجددًا بعد بضع دقائق وتواصل مع مسؤول مساحة العمل إذا استمرت المشكلة."},
	}
	for _, sentence := range sentences {
		if reason == sentence[0] {
			return sentence[agentRPLocaleIndex(locale)]
		}
	}
	if strings.HasPrefix(reason, "The agent's corrected reply still broke this rule: ") {
		rule := strings.TrimSuffix(strings.TrimPrefix(reason, "The agent's corrected reply still broke this rule: "), ". Preview again to try a new reply.")
		translated := rule
		for _, item := range [][3]string{
			{"the announcement must contain text", "die Ankündigung muss Text enthalten", "يجب أن يحتوي الإعلان على نص"},
			{"the announcement must be at most 16 KB", "die Ankündigung darf höchstens 16 KB lang sein", "يجب ألا يتجاوز حجم الإعلان 16 كيلوبايت"},
			{"use plain text without web addresses, square brackets or Source lines; the product adds sources", "verwenden Sie Klartext ohne Webadressen, eckige Klammern oder Quellenzeilen; das Produkt ergänzt die Quellen", "استخدم نصًا عاديًا دون عناوين ويب أو أقواس مربعة أو أسطر مصادر؛ يضيف المنتج المصادر"},
			{"use text without control characters", "verwenden Sie Text ohne Steuerzeichen", "استخدم نصًا دون محارف تحكم"},
		} {
			if rule == item[0] {
				translated = item[agentRPLocaleIndex(locale)]
			}
		}
		if strings.HasPrefix(rule, "upcoming items must have dates on or after ") {
			date := strings.TrimSuffix(strings.TrimPrefix(rule, "upcoming items must have dates on or after "), "; leave past dates out")
			translated = [3]string{rule, "bevorstehende Termine müssen am oder nach dem " + date + " liegen; lassen Sie vergangene Termine weg", "يجب أن تكون تواريخ العناصر القادمة في " + date + " أو بعده؛ استبعد التواريخ الماضية"}[agentRPLocaleIndex(locale)]
		}
		switch agentRPLocaleIndex(locale) {
		case 1:
			return "Auch die korrigierte Antwort verletzte diese Regel: " + translated + ". Erstellen Sie eine neue Vorschau."
		case 2:
			return "لم يتبع الرد المصحح هذه القاعدة: " + translated + ". أنشئ معاينة جديدة للمحاولة مجددًا."
		default:
			return "The corrected reply still broke this rule: " + rule + ". Preview again to try a new reply."
		}
	}
	return reason
}
