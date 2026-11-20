package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ProjectJourneyClarify is the journey page's clarification slot (issue 3995).
// It is rendered empty and hidden: the browser host asks the server what this
// viewer may do and see, then fills it. The approver who holds the decision
// gets an Ask for clarification button; the approver and the requester get the
// questions they are part of, with where each stands. A viewer with neither
// sees nothing at all, so the slot never hints at a private conversation. It
// carries no identifiers (the host reads the journey from the address); its
// words arrive as data attributes so one copy serves the page and the dialog.
func ProjectJourneyClarify(localeCode string) ui.Node {
	locale := ResolveProductLocale(localeCode)
	data := map[string]string{"projectui-clarify": projectJourneyCurrent}
	for _, key := range journeyClarifyKeys() {
		data["text-"+key] = JourneyClarifyText(locale.Resolved, key)
	}
	return html.Section(html.Props{Class: "projectui-journey-clarify", Hidden: true, Dir: string(locale.Direction), Data: data, Aria: map[string]string{"live": "polite"}})
}

// JourneyClarifyText is one clarification sentence in the locale (en-US,
// de-DE or ar); any other locale reads English, and an unknown key reads empty.
func JourneyClarifyText(locale, key string) string {
	words, ok := journeyClarifyWords[key]
	if !ok {
		return ""
	}
	switch locale {
	case "de-DE", "de":
		return words[1]
	case "ar":
		return words[2]
	}
	return words[0]
}

func journeyClarifyKeys() []string {
	keys := make([]string, 0, len(journeyClarifyWords))
	for key := range journeyClarifyWords {
		keys = append(keys, key)
	}
	return keys
}

// journeyClarifyWords is the copy, as {en-US, de-DE, ar}. {name}, {revision}
// and {worker} are filled by the host.
var journeyClarifyWords = map[string][3]string{
	"heading":               {"Clarification", "Klärung", "توضيح"},
	"ask":                   {"Ask for clarification", "Klärung anfragen", "طلب توضيح"},
	"case":                  {"Case: promotion for {worker}", "Vorgang: Beförderung für {worker}", "الحالة: ترقية لـ {worker}"},
	"revision":              {"Proposal revision {revision}", "Vorschlagsfassung {revision}", "نسخة الاقتراح {revision}"},
	"audience":              {"Only you and {name} can read this. It is sent as a private direct message.", "Nur Sie und {name} können das lesen. Es wird als private Direktnachricht gesendet.", "أنت و{name} فقط يمكنكما قراءة هذا. يُرسل كرسالة مباشرة خاصة."},
	"audience_unknown":      {"Only you and the person who made this request can read this. It is sent as a private direct message.", "Nur Sie und die Person, die diesen Antrag gestellt hat, können das lesen. Es wird als private Direktnachricht gesendet.", "أنت ومن قدّم هذا الطلب فقط يمكنكما قراءة هذا. يُرسل كرسالة مباشرة خاصة."},
	"reply_note":            {"A reply in chat is a conversation. It does not change, approve or submit anything to this request.", "Eine Antwort im Chat ist ein Gespräch. Sie ändert nichts an diesem Antrag, genehmigt ihn nicht und reicht nichts dafür ein.", "الرد في الدردشة مجرد محادثة. لا يغيّر هذا الطلب ولا يوافق عليه ولا يرسل أي شيء إليه."},
	"question_label":        {"Your question", "Ihre Frage", "سؤالك"},
	"question_hint":         {"Ask one focused question. Only the words you write here are sent; no pay figures are added.", "Stellen Sie eine gezielte Frage. Es wird nur Ihr Text gesendet; Gehaltszahlen werden nicht ergänzt.", "اطرح سؤالًا واحدًا محددًا. يُرسل النص الذي تكتبه هنا فقط، ولا تُضاف أي أرقام رواتب."},
	"send":                  {"Send question", "Frage senden", "إرسال السؤال"},
	"cancel":                {"Cancel", "Abbrechen", "إلغاء"},
	"sending":               {"Sending…", "Wird gesendet …", "جارٍ الإرسال…"},
	"sent":                  {"Question sent to {name}.", "Frage an {name} gesendet.", "أُرسل السؤال إلى {name}."},
	"sent_unknown":          {"Question sent.", "Frage gesendet.", "أُرسل السؤال."},
	"err_invalid":           {"Write a question of up to 1,000 characters.", "Schreiben Sie eine Frage mit höchstens 1.000 Zeichen.", "اكتب سؤالًا لا يزيد عن 1000 حرف."},
	"err_stale":             {"The proposal changed after you opened this review. Reload the review and read it again before asking.", "Der Vorschlag wurde geändert, nachdem Sie diese Prüfung geöffnet haben. Laden Sie die Prüfung neu und lesen Sie sie erneut, bevor Sie fragen.", "تغيّر الاقتراح بعد فتحك هذه المراجعة. أعد تحميل المراجعة واقرأها مجددًا قبل السؤال."},
	"err_closed":            {"This case is closed, so questions can no longer be asked.", "Dieser Vorgang ist abgeschlossen, daher können keine Fragen mehr gestellt werden.", "هذه الحالة مغلقة، لذا لا يمكن طرح أسئلة بعد الآن."},
	"err_no_recipient":      {"The person who made this request cannot be messaged.", "Die Person, die diesen Antrag gestellt hat, kann nicht angeschrieben werden.", "لا يمكن مراسلة من قدّم هذا الطلب."},
	"err_denied":            {"Only the approver who holds this decision can ask.", "Nur die Person, die diese Entscheidung trifft, kann fragen.", "يمكن لصاحب هذا القرار فقط أن يسأل."},
	"err_generic":           {"The question could not be sent. Your text is kept. Try again.", "Die Frage konnte nicht gesendet werden. Ihr Text bleibt erhalten. Versuchen Sie es erneut.", "تعذّر إرسال السؤال. نصّك محفوظ. حاول مرة أخرى."},
	"blocked":               {"You cannot ask from here: the person who made this request cannot be messaged.", "Von hier aus kann nicht gefragt werden: Die Person, die diesen Antrag gestellt hat, kann nicht angeschrieben werden.", "لا يمكنك السؤال من هنا: لا يمكن مراسلة من قدّم هذا الطلب."},
	"status_awaiting_you":   {"Waiting for your decision.", "Wartet auf Ihre Entscheidung.", "بانتظار قرارك."},
	"status_awaiting_other": {"Waiting for another approver's decision.", "Wartet auf die Entscheidung einer anderen Person.", "بانتظار قرار موافق آخر."},
	"status_in_progress":    {"The request is in progress.", "Der Antrag ist in Bearbeitung.", "الطلب قيد المعالجة."},
	"status_closed":         {"This case is closed.", "Dieser Vorgang ist abgeschlossen.", "هذه الحالة مغلقة."},
	"asked_by_you":          {"You asked {name}", "Sie haben {name} gefragt", "سألتَ {name}"},
	"asked_by_you_unknown":  {"You asked the person who made the request", "Sie haben die Person gefragt, die den Antrag gestellt hat", "سألتَ من قدّم الطلب"},
	"asked_you":             {"{name} asked you", "{name} hat Sie gefragt", "سألك {name}"},
	"asked_you_unknown":     {"The approver asked you", "Die genehmigende Person hat Sie gefragt", "سألك الموافق"},
	"state_current":         {"About the proposal now under review.", "Zum Vorschlag, der jetzt geprüft wird.", "بشأن الاقتراح قيد المراجعة الآن."},
	"state_outdated":        {"The proposal changed after this question. Review it again before deciding.", "Der Vorschlag wurde nach dieser Frage geändert. Prüfen Sie ihn vor der Entscheidung erneut.", "تغيّر الاقتراح بعد هذا السؤال. راجعه مجددًا قبل اتخاذ القرار."},
	"state_closed":          {"The case is closed. This question is kept as a record.", "Der Vorgang ist abgeschlossen. Diese Frage bleibt als Nachweis erhalten.", "الحالة مغلقة. يُحفظ هذا السؤال كسجل."},
	"unavailable":           {"You can no longer open this conversation. The record of the question is kept.", "Sie können diese Unterhaltung nicht mehr öffnen. Der Nachweis der Frage bleibt erhalten.", "لم يعد بإمكانك فتح هذه المحادثة. يُحفظ سجل السؤال."},
	"open":                  {"Open conversation", "Unterhaltung öffnen", "فتح المحادثة"},
	"open_aria":             {"Open the conversation with {name}", "Unterhaltung mit {name} öffnen", "فتح المحادثة مع {name}"},
	"incomplete":            {"Some questions could not be checked. Reload to try again.", "Einige Fragen konnten nicht geprüft werden. Laden Sie neu und versuchen Sie es erneut.", "تعذّر التحقق من بعض الأسئلة. أعد التحميل وحاول مرة أخرى."},
}
