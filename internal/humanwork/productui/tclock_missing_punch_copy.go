package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// missingPunchCopy is deliberately local to this surface. The shell catalog
// may override these strings later, but the workflow remains usable while a
// locale catalog is being composed.
type missingPunchCopy struct {
	title, intro, unavailable, unavailableHelp, original, session, worker, event, proposed, proposedHelp, reason, reasonHelp                string
	request, record, submit, sending, pending, emptyPending, review, approve, reject, decisionNote, decisionHelp, reopen, trace             string
	periodClosed, required, receipt, submitted, reference, afterSubmit, approved, rejected, error, noteRequired, reopenRequired             string
	requestedBy, asks, sentTo, newRequest, reasonGiven, reviewTitle, reviewIntro, traceReview, rejectTitle, rejectBody, rejectYes, rejectNo string
}

func missingPunchText(locale LocaleContext) missingPunchCopy {
	switch locale.normalized().Resolved {
	case "de-DE":
		return missingPunchCopy{
			title: "Fehlenden Zeiteintrag korrigieren", intro: "Ausstempeln vergessen oder zur falschen Zeit gestempelt? Schildern Sie, was passiert ist. Ihre Führungskraft prüft die Anfrage, bevor sich Ihr Stundenzettel ändert.",
			unavailable: "Die Korrektur von Zeiteinträgen ist für diesen Arbeitsbereich noch nicht verfügbar.", unavailableHelp: "Versuchen Sie es gleich noch einmal. Wenn es sich weiterhin nicht öffnet, bitten Sie Ihre Führungskraft, den Eintrag für Sie zu korrigieren.",
			original: "Ursprünglicher Eintrag", session: "Schicht", worker: "Mitarbeiter", event: "Ursprünglicher Eintrag",
			proposed: "Wann haben Sie tatsächlich ausgestempelt?", proposedHelp: "Angaben in Ihrer Ortszeit.",
			reason: "Was ist passiert?", reasonHelp: "Zum Beispiel: Mein Handy war leer, deshalb konnte ich nicht ausstempeln. Ich habe die Baustelle um 15:30 Uhr verlassen.",
			request: "Was soll korrigiert werden?", record: "Das ist aktuell erfasst", submit: "An meine Führungskraft senden", sending: "Wird gesendet…",
			pending: "Korrekturen zur Prüfung", emptyPending: "Keine Korrekturen warten auf Prüfung. Neue Anfragen Ihres Teams erscheinen hier.",
			review: "Prüfung", approve: "Korrektur genehmigen", reject: "Anfrage ablehnen", decisionNote: "Notiz an den Mitarbeiter (erforderlich)", decisionHelp: "Erforderlich beim Genehmigen und beim Ablehnen. Der Mitarbeiter sieht diese Notiz.",
			reopen: "Referenz für die Wiedereröffnung des Abrechnungszeitraums", trace: "Anfrage verfolgen",
			periodClosed: "Dieser Abrechnungszeitraum ist geschlossen. Tragen Sie vor der Genehmigung die Wiedereröffnungsreferenz aus der Lohnbuchhaltung ein.",
			required:     "Füllen Sie beide Felder aus, bevor Sie senden.", receipt: "Anfrage gesendet", submitted: "An Ihre Führungskraft zur Prüfung gesendet.", reference: "Referenz",
			afterSubmit: "Sobald sie genehmigt ist, zeigt Ihr Stundenzettel die neue Ausstempelzeit.",
			approved:    "Korrektur genehmigt. Der Stundenzettel zeigt jetzt die neue Ausstempelzeit.", rejected: "Anfrage abgelehnt. Der Mitarbeiter sieht Ihre Notiz.", error: "Das konnte nicht gespeichert werden. Versuchen Sie es erneut.",
			noteRequired: "Begründen Sie Ihre Entscheidung in einer Notiz.", reopenRequired: "Tragen Sie die Wiedereröffnungsreferenz ein.",
			requestedBy: "Angefragt von", asks: "{worker} möchte das Ausstempeln ändern auf {time}.", sentTo: "Zur Prüfung an Ihre Führungskraft", newRequest: "Weitere Korrektur anfragen", reasonGiven: "Angegebener Grund", reviewTitle: "Korrekturen prüfen", reviewIntro: "Genehmigen oder lehnen Sie Korrekturen ab, die Ihr Team angefragt hat. Genehmigte Korrekturen ändern deren Stundenzettel.", traceReview: "Bearbeitungsverlauf ansehen", rejectTitle: "Diese Anfrage ablehnen?", rejectBody: "Der Stundenzettel bleibt unverändert. Der Mitarbeiter sieht Ihre Notiz.", rejectYes: "Ja, Anfrage ablehnen", rejectNo: "Zurück",
		}
	case "ar", "ar-SA":
		return missingPunchCopy{
			title: "تصحيح تسجيل وقت مفقود", intro: "نسيت تسجيل الانصراف أو سجّلت في وقت خاطئ؟ أخبرنا بما حدث فعلًا. يراجع مشرفك الطلب قبل أن تتغير بطاقة وقتك.",
			unavailable: "تصحيح تسجيلات الوقت غير متاح لمساحة العمل هذه بعد.", unavailableHelp: "حاول مرة أخرى بعد قليل. إن لم تُفتح الصفحة فاطلب من مشرفك تصحيح التسجيل نيابةً عنك.",
			original: "التسجيل الأصلي", session: "الوردية", worker: "الموظف", event: "التسجيل الأصلي",
			proposed: "متى سجّلت انصرافك فعلًا؟", proposedHelp: "الأوقات بتوقيتك المحلي.",
			reason: "ماذا حدث؟", reasonHelp: "مثال: نفدت بطارية هاتفي فلم أستطع تسجيل الانصراف. غادرت الموقع عند 3:30 مساءً.",
			request: "ما الذي تريد تصحيحه؟", record: "المسجَّل حاليًا", submit: "إرسال إلى مشرفي", sending: "جارٍ الإرسال…",
			pending: "تصحيحات بانتظار مراجعتك", emptyPending: "لا توجد تصحيحات بانتظار المراجعة. ستظهر هنا الطلبات الجديدة من فريقك.",
			review: "مراجعة", approve: "الموافقة على التصحيح", reject: "رفض الطلب", decisionNote: "ملاحظة للموظف (مطلوبة)", decisionHelp: "مطلوبة عند الموافقة وعند الرفض. سيرى الموظف هذه الملاحظة.",
			reopen: "مرجع إعادة فتح فترة الرواتب", trace: "تتبّع الطلب",
			periodClosed: "فترة الرواتب هذه مغلقة. أدخل مرجع إعادة الفتح من قسم الرواتب قبل الموافقة.",
			required:     "املأ الحقلين قبل الإرسال.", receipt: "تم إرسال الطلب", submitted: "أُرسل إلى مشرفك للمراجعة.", reference: "المرجع",
			afterSubmit: "بعد الموافقة ستعرض بطاقة وقتك وقت الانصراف الجديد.",
			approved:    "تمت الموافقة على التصحيح. تعرض بطاقة الوقت الآن وقت الانصراف الجديد.", rejected: "تم رفض الطلب. سيرى الموظف ملاحظتك.", error: "تعذّر الحفظ. حاول مرة أخرى.",
			noteRequired: "اكتب ملاحظة تشرح قرارك.", reopenRequired: "أدخل مرجع إعادة الفتح.",
			requestedBy: "مقدَّم من", asks: "يطلب {worker} تغيير وقت الانصراف إلى {time}.", sentTo: "بانتظار مراجعة مشرفك", newRequest: "طلب تصحيح آخر", reasonGiven: "السبب المذكور", reviewTitle: "مراجعة التصحيحات", reviewIntro: "وافق على التصحيحات التي طلبها فريقك أو ارفضها. التصحيحات المعتمدة تغيّر بطاقات وقتهم.", traceReview: "عرض سجل معالجة الطلب", rejectTitle: "هل تريد رفض هذا الطلب؟", rejectBody: "تبقى بطاقة الوقت كما هي. سيرى الموظف ملاحظتك.", rejectYes: "نعم، ارفض الطلب", rejectNo: "رجوع",
		}
	default:
		return missingPunchCopy{
			title: "Fix a missing punch", intro: "Forgot to clock out, or clocked at the wrong time? Tell us what really happened. Your supervisor reviews it before your timecard changes.",
			unavailable: "Time punch corrections are not available for this workspace yet.", unavailableHelp: "Try again in a moment. If it still does not open, ask your supervisor to fix the punch for you.",
			original: "Original punch", session: "Shift", worker: "Worker", event: "Original punch",
			proposed: "When did you actually clock out?", proposedHelp: "Times are in your local time zone.",
			reason: "What happened?", reasonHelp: "For example: My phone died, so I could not clock out. I left the site at 3:30 PM.",
			request: "What should we fix?", record: "What is on record now", submit: "Send to my supervisor", sending: "Sending…",
			pending: "Corrections waiting for your review", emptyPending: "No corrections are waiting for review. New requests from your crew appear here.",
			review: "Review", approve: "Approve correction", reject: "Reject request", decisionNote: "Note to the worker (required)", decisionHelp: "Required whether you approve or reject. The worker can see this note.",
			reopen: "Pay period reopen reference", trace: "Track this request",
			periodClosed: "This pay period is closed. Enter the reopen reference from payroll before approving.",
			required:     "Fill in both fields before sending.", receipt: "Request sent", submitted: "Sent to your supervisor for review.", reference: "Reference",
			afterSubmit: "Once it is approved, your timecard shows the new clock-out.",
			approved:    "Correction approved. The timecard now shows the new clock-out.", rejected: "Request rejected. The worker can see your note.", error: "That could not be saved. Try again.",
			noteRequired: "Add a note explaining your decision.", reopenRequired: "Enter the reopen reference.",
			requestedBy: "Requested by", asks: "{worker} asks to change the clock-out to {time}.", sentTo: "Waiting for your supervisor", newRequest: "Request another correction", reasonGiven: "Reason given", reviewTitle: "Review punch corrections", reviewIntro: "Approve or reject corrections your crew asked for. Approved corrections change their timecards.", traceReview: "See how this request was handled", rejectTitle: "Reject this request?", rejectBody: "The timecard stays as it is. The worker sees your note.", rejectYes: "Yes, reject request", rejectNo: "Go back",
		}
	}
}

// fillBidi expands {name} placeholders into isolated inline runs so a Latin
// name or a numeric time keeps its own direction inside an Arabic sentence.
// The template order stays with the translator.
func fillBidi(template string, values map[string]string) []ui.Node {
	var nodes []ui.Node
	for {
		open := strings.IndexByte(template, '{')
		if open < 0 {
			break
		}
		close := strings.IndexByte(template[open:], '}')
		if close < 0 {
			break
		}
		name := template[open+1 : open+close]
		value, ok := values[name]
		if !ok {
			break
		}
		if open > 0 {
			nodes = append(nodes, ui.Text(template[:open]))
		}
		nodes = append(nodes, html.Tag("bdi", html.Props{}, html.Strong(html.Props{}, ui.Text(value))))
		template = template[open+close+1:]
	}
	if template != "" {
		nodes = append(nodes, ui.Text(template))
	}
	return nodes
}
