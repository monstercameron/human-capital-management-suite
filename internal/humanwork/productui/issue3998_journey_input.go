package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ProjectJourneyInput is the journey page's "Ask for input" slot (issue 3998).
// It is rendered empty and hidden: the browser host asks the server what this
// viewer may do, then fills it. The reviewer who holds the decision gets an Ask
// for input button, and everyone who asked gets their questions with whether
// each is still current. A viewer with neither sees nothing at all. It carries
// no identifiers (the host reads the journey from the address); its words
// arrive as data attributes so one copy serves the page and the dialog.
func ProjectJourneyInput(localeCode string) ui.Node {
	locale := ResolveProductLocale(localeCode)
	data := map[string]string{"projectui-input": projectJourneyCurrent}
	for _, key := range journeyInputKeys() {
		data["text-"+key] = JourneyInputText(locale.Resolved, key)
	}
	return html.Section(html.Props{Class: "projectui-journey-clarify projectui-journey-input", Hidden: true, Dir: string(locale.Direction), Data: data, Aria: map[string]string{"live": "polite"}})
}

// JourneyInputText is one sentence of the feature in the locale (en-US, de-DE
// or ar); any other locale reads English, and an unknown key reads empty.
func JourneyInputText(locale, key string) string {
	words, ok := journeyInputWords[key]
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

func journeyInputKeys() []string {
	keys := make([]string, 0, len(journeyInputWords))
	for key := range journeyInputWords {
		keys = append(keys, key)
	}
	return keys
}

// journeyInputWords is the copy, as {en-US, de-DE, ar}. {name}, {revision},
// {worker} and {size} are filled by the host.
var journeyInputWords = map[string][3]string{
	"heading":               {"Input from others", "Einschätzung anderer", "آراء الآخرين"},
	"ask":                   {"Ask for input", "Um Einschätzung bitten", "طلب رأي"},
	"informal_note":         {"This is informal advice. It is not an approval, and a reply or reaction in chat never approves or changes this request. If another approver is needed, use this request's own approval routing.", "Das ist eine informelle Einschätzung, keine Genehmigung. Eine Antwort oder Reaktion im Chat genehmigt oder ändert diesen Antrag nie. Wenn eine weitere genehmigende Person nötig ist, nutzen Sie die Genehmigungsweiterleitung dieses Antrags.", "هذا رأي غير رسمي وليس موافقة. لا يوافق أي رد أو تفاعل في الدردشة على هذا الطلب ولا يغيّره. إذا لزم موافق آخر، فاستخدم مسار الموافقة الخاص بهذا الطلب."},
	"label_informal":        {"Informal input, not an approval", "Informelle Einschätzung, keine Genehmigung", "رأي غير رسمي وليس موافقة"},
	"revision":              {"Proposal revision {revision}", "Vorschlagsfassung {revision}", "نسخة الاقتراح {revision}"},
	"conversation_label":    {"Where to ask", "Wo gefragt wird", "أين تسأل"},
	"conversation_none":     {"Choose a conversation", "Unterhaltung wählen", "اختر محادثة"},
	"conversation_empty":    {"You have no conversation to ask in yet. Start one in Chat first.", "Sie haben noch keine Unterhaltung, in der Sie fragen können. Starten Sie zuerst eine im Chat.", "ليس لديك محادثة للسؤال فيها بعد. ابدأ واحدة في الدردشة أولًا."},
	"direct_message":        {"Direct message", "Direktnachricht", "رسالة مباشرة"},
	"question_label":        {"Your question", "Ihre Frage", "سؤالك"},
	"question_hint":         {"Ask one focused question. Pay figures, dates and reasons are never added.", "Stellen Sie eine gezielte Frage. Gehaltszahlen, Termine und Begründungen werden nie ergänzt.", "اطرح سؤالًا واحدًا محددًا. لا تُضاف أرقام الرواتب أو التواريخ أو الأسباب أبدًا."},
	"include_name":          {"Include the person's name ({worker}) in the message", "Namen der Person ({worker}) in die Nachricht aufnehmen", "تضمين اسم الشخص ({worker}) في الرسالة"},
	"include_name_hint":     {"Off by default. It is never sent to a public channel.", "Standardmäßig aus. Er wird nie in einen öffentlichen Kanal gesendet.", "معطّل افتراضيًا. لا يُرسل أبدًا إلى قناة عامة."},
	"preview":               {"Preview", "Vorschau", "معاينة"},
	"previewing":            {"Preparing the preview…", "Vorschau wird erstellt …", "جارٍ تجهيز المعاينة…"},
	"preview_heading":       {"This is exactly what will be sent", "Genau das wird gesendet", "هذا بالضبط ما سيُرسل"},
	"audience_direct":       {"Who can read it: only you and the other person in this direct message.", "Wer es lesen kann: nur Sie und die andere Person in dieser Direktnachricht.", "من يمكنه قراءته: أنت والشخص الآخر في هذه الرسالة المباشرة فقط."},
	"audience_group":        {"Who can read it: everyone in this group conversation.", "Wer es lesen kann: alle in dieser Gruppenunterhaltung.", "من يمكنه قراءته: كل من في هذه المحادثة الجماعية."},
	"audience_private":      {"Who can read it: everyone in the private channel {name}.", "Wer es lesen kann: alle im privaten Kanal {name}.", "من يمكنه قراءته: كل من في القناة الخاصة {name}."},
	"audience_public":       {"Who can read it: everyone in the public channel {name}, which anyone in your workspace can join.", "Wer es lesen kann: alle im öffentlichen Kanal {name}, dem alle in Ihrem Arbeitsbereich beitreten können.", "من يمكنه قراءته: كل من في القناة العامة {name} التي يمكن لأي شخص في مساحة عملك الانضمام إليها."},
	"audience_size":         {"{size} people.", "{size} Personen.", "{size} أشخاص."},
	"audience_case":         {"People in the conversation who cannot open this case will see the question but not the case.", "Personen in der Unterhaltung, die diesen Vorgang nicht öffnen dürfen, sehen die Frage, aber nicht den Vorgang.", "من في المحادثة ممن لا يستطيعون فتح هذه الحالة سيرون السؤال دون الحالة."},
	"edit":                  {"Edit", "Bearbeiten", "تعديل"},
	"send":                  {"Send question", "Frage senden", "إرسال السؤال"},
	"cancel":                {"Cancel", "Abbrechen", "إلغاء"},
	"sending":               {"Sending…", "Wird gesendet …", "جارٍ الإرسال…"},
	"sent":                  {"Question sent. It is informal advice, not an approval.", "Frage gesendet. Es ist eine informelle Einschätzung, keine Genehmigung.", "أُرسل السؤال. إنه رأي غير رسمي وليس موافقة."},
	"err_invalid":           {"Write a question of up to 1,000 characters and choose a conversation.", "Schreiben Sie eine Frage mit höchstens 1.000 Zeichen und wählen Sie eine Unterhaltung.", "اكتب سؤالًا لا يزيد عن 1000 حرف واختر محادثة."},
	"err_stale":             {"The proposal changed after you opened this review. Reload the review and read it again before asking.", "Der Vorschlag wurde geändert, nachdem Sie diese Prüfung geöffnet haben. Laden Sie die Prüfung neu und lesen Sie sie erneut, bevor Sie fragen.", "تغيّر الاقتراح بعد فتحك هذه المراجعة. أعد تحميل المراجعة واقرأها مجددًا قبل السؤال."},
	"err_closed":            {"This case is closed, so questions can no longer be asked.", "Dieser Vorgang ist abgeschlossen, daher können keine Fragen mehr gestellt werden.", "هذه الحالة مغلقة، لذا لا يمكن طرح أسئلة بعد الآن."},
	"err_no_conversation":   {"You cannot post in that conversation. Choose another.", "Sie können in dieser Unterhaltung nichts posten. Wählen Sie eine andere.", "لا يمكنك النشر في تلك المحادثة. اختر محادثة أخرى."},
	"err_name_public":       {"The person's name is never sent to a public channel. Turn it off or choose another conversation.", "Der Name der Person wird nie in einen öffentlichen Kanal gesendet. Schalten Sie ihn aus oder wählen Sie eine andere Unterhaltung.", "لا يُرسل اسم الشخص أبدًا إلى قناة عامة. أوقف تضمينه أو اختر محادثة أخرى."},
	"err_denied":            {"Only the approver who holds this decision can ask.", "Nur die Person, die diese Entscheidung trifft, kann fragen.", "يمكن لصاحب هذا القرار فقط أن يسأل."},
	"err_generic":           {"The question could not be sent. Your text is kept. Try again.", "Die Frage konnte nicht gesendet werden. Ihr Text bleibt erhalten. Versuchen Sie es erneut.", "تعذّر إرسال السؤال. نصّك محفوظ. حاول مرة أخرى."},
	"status_awaiting_you":   {"Waiting for your decision.", "Wartet auf Ihre Entscheidung.", "بانتظار قرارك."},
	"status_awaiting_other": {"Waiting for another approver's decision.", "Wartet auf die Entscheidung einer anderen Person.", "بانتظار قرار موافق آخر."},
	"status_in_progress":    {"The request is in progress.", "Der Antrag ist in Bearbeitung.", "الطلب قيد المعالجة."},
	"status_closed":         {"This case is closed.", "Dieser Vorgang ist abgeschlossen.", "هذه الحالة مغلقة."},
	"item_in":               {"You asked in {name}", "Sie haben in {name} gefragt", "سألتَ في {name}"},
	"item_dm":               {"You asked in a direct message", "Sie haben in einer Direktnachricht gefragt", "سألتَ في رسالة مباشرة"},
	"item_unknown":          {"You asked for informal input", "Sie haben um eine informelle Einschätzung gebeten", "طلبتَ رأيًا غير رسمي"},
	"item_revision":         {"About proposal revision {revision}", "Zur Vorschlagsfassung {revision}", "بشأن نسخة الاقتراح {revision}"},
	"state_current":         {"About the proposal now under review. Informal advice, not an approval.", "Zum Vorschlag, der jetzt geprüft wird. Informelle Einschätzung, keine Genehmigung.", "بشأن الاقتراح قيد المراجعة الآن. رأي غير رسمي وليس موافقة."},
	"state_outdated":        {"The proposal changed after this question, so any advice may no longer apply. Review it again before deciding.", "Der Vorschlag wurde nach dieser Frage geändert, daher gilt eine Antwort womöglich nicht mehr. Prüfen Sie ihn vor der Entscheidung erneut.", "تغيّر الاقتراح بعد هذا السؤال، لذا قد لا ينطبق أي رأي. راجعه مجددًا قبل اتخاذ القرار."},
	"state_closed":          {"The case is closed. This question is kept as a record.", "Der Vorgang ist abgeschlossen. Diese Frage bleibt als Nachweis erhalten.", "الحالة مغلقة. يُحفظ هذا السؤال كسجل."},
	"state_unavailable":     {"You can no longer open this conversation, so its advice is not current. Ask again to get current advice.", "Sie können diese Unterhaltung nicht mehr öffnen, daher ist ihre Einschätzung nicht aktuell. Fragen Sie erneut, um eine aktuelle zu erhalten.", "لم يعد بإمكانك فتح هذه المحادثة، لذا فرأيها ليس حاليًا. اسأل مجددًا للحصول على رأي حالي."},
	"open":                  {"Open conversation", "Unterhaltung öffnen", "فتح المحادثة"},
	"open_aria":             {"Open the conversation {name}", "Unterhaltung {name} öffnen", "فتح المحادثة {name}"},
	"incomplete":            {"Some questions could not be checked. Reload to try again.", "Einige Fragen konnten nicht geprüft werden. Laden Sie neu und versuchen Sie es erneut.", "تعذّر التحقق من بعض الأسئلة. أعد التحميل وحاول مرة أخرى."},
}
