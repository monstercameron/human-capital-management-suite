package chatui

import "strings"

// CHATBUG-059. A failed action said what happened in English whatever language
// the page was in ("We couldn't load the member list. The service did not
// answer. Try again."), and the creator line of a conversation you made read
// "Erstellt von du". The notice's words are a lead sentence per action and a
// reason clause per failure; each exists in en-US, de-DE and ar. Every lead
// below is the whole sentence, because the object and the verb change places
// from one language to the next.

// chatbug059Leads: the action phrase, then the sentence in English, German and
// Arabic.
var chatbug059Leads = map[string][3]string{
	"download attachment":                       {"We couldn't download attachment.", "Der Anhang konnte nicht heruntergeladen werden.", "تعذّر تنزيل المرفق."},
	"copy the link":                             {"We couldn't copy the link.", "Der Link konnte nicht kopiert werden.", "تعذّر نسخ الرابط."},
	"leave this channel":                        {"We couldn't leave this channel.", "Der Kanal konnte nicht verlassen werden.", "تعذّرت مغادرة هذه القناة."},
	"find an existing direct message":           {"We couldn't find an existing direct message.", "Eine vorhandene Direktnachricht konnte nicht gefunden werden.", "تعذّر العثور على رسالة مباشرة موجودة."},
	"unpin this message":                        {"We couldn't unpin this message.", "Die Nachricht konnte nicht gelöst werden.", "تعذّر إلغاء تثبيت هذه الرسالة."},
	"pin this message":                          {"We couldn't pin this message.", "Die Nachricht konnte nicht angeheftet werden.", "تعذّر تثبيت هذه الرسالة."},
	"start this direct message":                 {"We couldn't start this direct message.", "Die Direktnachricht konnte nicht begonnen werden.", "تعذّر بدء هذه الرسالة المباشرة."},
	"send this agent mention":                   {"We couldn't send this agent mention.", "Die Agentenerwähnung konnte nicht gesendet werden.", "تعذّر إرسال إشارة الوكيل."},
	"save your notification settings":           {"We couldn't save your notification settings.", "Ihre Benachrichtigungseinstellungen konnten nicht gespeichert werden.", "تعذّر حفظ إعدادات الإشعارات."},
	"read your notification settings":           {"We couldn't read your notification settings.", "Ihre Benachrichtigungseinstellungen konnten nicht gelesen werden.", "تعذّرت قراءة إعدادات الإشعارات."},
	"retry this agent":                          {"We couldn't retry this agent.", "Der Agent konnte nicht erneut gestartet werden.", "تعذّرت إعادة تشغيل هذا الوكيل."},
	"remove your reaction":                      {"We couldn't remove your reaction.", "Ihre Reaktion konnte nicht entfernt werden.", "تعذّرت إزالة تفاعلك."},
	"add your reaction":                         {"We couldn't add your reaction.", "Ihre Reaktion konnte nicht hinzugefügt werden.", "تعذّرت إضافة تفاعلك."},
	"read this person's business details":       {"We couldn't read this person's business details.", "Die Geschäftsdaten dieser Person konnten nicht gelesen werden.", "تعذّرت قراءة بيانات العمل لهذا الشخص."},
	"read the pinned messages":                  {"We couldn't read the pinned messages.", "Die angehefteten Nachrichten konnten nicht gelesen werden.", "تعذّرت قراءة الرسائل المثبتة."},
	"open this channel in browse":               {"We couldn't open this channel in browse.", "Der Kanal konnte nicht in der Kanalübersicht geöffnet werden.", "تعذّر فتح هذه القناة في التصفح."},
	"mark this conversation as read":            {"We couldn't mark this conversation as read.", "Die Unterhaltung konnte nicht als gelesen markiert werden.", "تعذّر تحديد هذه المحادثة كمقروءة."},
	"mark this conversation as unread":          {"We couldn't mark this conversation as unread.", "Die Unterhaltung konnte nicht als ungelesen markiert werden.", "تعذّر تحديد هذه المحادثة كغير مقروءة."},
	"load thread messages":                      {"We couldn't load thread messages.", "Die Thread-Nachrichten konnten nicht geladen werden.", "تعذّر تحميل رسائل السلسلة."},
	"load this thread":                          {"We couldn't load this thread.", "Der Thread konnte nicht geladen werden.", "تعذّر تحميل هذه السلسلة."},
	"load the member list":                      {"We couldn't load the member list.", "Die Mitgliederliste konnte nicht geladen werden.", "تعذّر تحميل قائمة الأعضاء."},
	"load this conversation":                    {"We couldn't load this conversation.", "Die Unterhaltung konnte nicht geladen werden.", "تعذّر تحميل هذه المحادثة."},
	"load your conversations":                   {"We couldn't load your conversations.", "Ihre Unterhaltungen konnten nicht geladen werden.", "تعذّر تحميل محادثاتك."},
	"add everyone you picked":                   {"We couldn't add everyone you picked.", "Nicht alle ausgewählten Personen konnten hinzugefügt werden.", "تعذّرت إضافة جميع من اخترتهم."},
	"add everyone you picked (some were added)": {"We couldn't add everyone you picked (some were added).", "Einige Personen wurden hinzugefügt, aber nicht alle ausgewählten.", "أُضيف بعضهم، لكن تعذّرت إضافة الجميع."},
	"load newest messages":                      {"We couldn't load newest messages.", "Die neuesten Nachrichten konnten nicht geladen werden.", "تعذّر تحميل أحدث الرسائل."},
	"load newer messages":                       {"We couldn't load newer messages.", "Neuere Nachrichten konnten nicht geladen werden.", "تعذّر تحميل الرسائل الأحدث."},
	"load earlier messages":                     {"We couldn't load earlier messages.", "Frühere Nachrichten konnten nicht geladen werden.", "تعذّر تحميل الرسائل الأقدم."},
	"list channels you can join":                {"We couldn't list channels you can join.", "Die Kanäle, denen Sie beitreten können, konnten nicht aufgelistet werden.", "تعذّر عرض القنوات التي يمكنك الانضمام إليها."},
	"join this channel":                         {"We couldn't join this channel.", "Dem Kanal konnte nicht beigetreten werden.", "تعذّر الانضمام إلى هذه القناة."},
	"delete this message":                       {"We couldn't delete this message.", "Die Nachricht konnte nicht gelöscht werden.", "تعذّر حذف هذه الرسالة."},
	"save this edit":                            {"We couldn't save this edit.", "Die Änderung konnte nicht gespeichert werden.", "تعذّر حفظ هذا التعديل."},
	"save your sidebar":                         {"We couldn't save your sidebar.", "Ihre Seitenleiste konnte nicht gespeichert werden.", "تعذّر حفظ شريطك الجانبي."},
	"send this message":                         {"We couldn't send this message.", "Die Nachricht konnte nicht gesendet werden.", "تعذّر إرسال هذه الرسالة."},
	"post this reply":                           {"We couldn't post this reply.", "Die Antwort konnte nicht gesendet werden.", "تعذّر إرسال هذا الرد."},
}

// chatbug059Fallback is the lead for an action no table names: not the
// English sentence, which would leave a German page with an English line.
var chatbug059Fallback = [3]string{"", "Das hat nicht geklappt.", "لم تنجح العملية."}

// chatbug059Clauses: the reason clause in English, German and Arabic. The
// English is the key; it is what the client's error mapping produces.
var chatbug059Clauses = map[string][3]string{
	"You do not have access to it.":                        {"", "Sie haben darauf keinen Zugriff.", "ليس لديك صلاحية الوصول إليه."},
	"Your sign-in has expired. Sign in again.":             {"", "Ihre Anmeldung ist abgelaufen. Melden Sie sich erneut an.", "انتهت صلاحية تسجيل دخولك. سجّل الدخول مرة أخرى."},
	"It is no longer there.":                               {"", "Es ist nicht mehr vorhanden.", "لم يعد موجودًا."},
	"Someone else changed it first. Try again.":            {"", "Jemand anderes hat es zuerst geändert. Versuchen Sie es erneut.", "غيّره شخص آخر أولًا. حاول مرة أخرى."},
	"The details were not accepted.":                       {"", "Die Angaben wurden nicht akzeptiert.", "لم يتم قبول التفاصيل."},
	"There were too many requests. Try again in a moment.": {"", "Es gab zu viele Anfragen. Versuchen Sie es gleich noch einmal.", "كانت هناك طلبات كثيرة جدًا. حاول مرة أخرى بعد قليل."},
	"This server does not offer it yet.":                   {"", "Dieser Server bietet das noch nicht an.", "لا يوفّر هذا الخادم ذلك بعد."},
	"The service did not answer. Try again.":               {"", "Der Dienst hat nicht geantwortet. Versuchen Sie es erneut.", "لم تستجب الخدمة. حاول مرة أخرى."},
	"The service reported an error. Try again.":            {"", "Der Dienst hat einen Fehler gemeldet. Versuchen Sie es erneut.", "أبلغت الخدمة عن خطأ. حاول مرة أخرى."},
	"Something went wrong. Try again.":                     {"", "Etwas ist schiefgelaufen. Versuchen Sie es erneut.", "حدث خطأ ما. حاول مرة أخرى."},
}

// ActionFailureNotice is the line shown above the conversation when an action
// failed: what could not be done, then what to do. clause is the English
// reason the client chose; a clause this table does not know (a sentence the
// message filter wrote, already in the reader's language) is kept as it is.
func ActionFailureNotice(locale, action, clause string) string {
	i := chatbug039LocaleIndex(locale)
	if i == 0 {
		return "We couldn't " + action + ". " + clause
	}
	lead := chatbug059Fallback[i]
	if known, ok := chatbug059Leads[action]; ok {
		lead = known[i]
	}
	if known, ok := chatbug059Clauses[clause]; ok {
		clause = known[i]
	}
	return strings.TrimSpace(lead + " " + clause)
}

// chatbug059Notices are the one-line confirmations the client raises as
// English sentences ("Link copied"), each with its German and Arabic form.
var chatbug059Notices = map[string][2]string{
	"Joined the channel":                            {"Sie sind dem Kanal beigetreten", "انضممت إلى القناة"},
	"That message is not pinned any more":           {"Diese Nachricht ist nicht mehr angeheftet", "لم تعد هذه الرسالة مثبّتة"},
	"Message unpinned":                              {"Anheftung der Nachricht aufgehoben", "تم إلغاء تثبيت الرسالة"},
	"Message pinned":                                {"Nachricht angeheftet", "تم تثبيت الرسالة"},
	"Message deleted":                               {"Nachricht gelöscht", "تم حذف الرسالة"},
	"Reaction removed":                              {"Reaktion entfernt", "تمت إزالة التفاعل"},
	"Message preview unavailable":                   {"Nachrichtenvorschau nicht verfügbar", "معاينة الرسالة غير متاحة"},
	"This conversation is no longer updating live.": {"Diese Unterhaltung wird nicht mehr live aktualisiert.", "لم تعد هذه المحادثة تتحدث مباشرة."},
	"Conversation created":                          {"Unterhaltung erstellt", "تم إنشاء المحادثة"},
	"Link copied":                                   {"Link kopiert", "تم نسخ الرابط"},
	"Added to the conversation":                     {"Zur Unterhaltung hinzugefügt", "تمت الإضافة إلى المحادثة"},
	"Notification settings saved":                   {"Benachrichtigungseinstellungen gespeichert", "تم حفظ إعدادات الإشعارات"},
	"You are not in that channel":                   {"Sie sind nicht in diesem Kanal", "لست في تلك القناة"},
	"You left the channel":                          {"Sie haben den Kanal verlassen", "غادرت القناة"},
}

// LocalizeChatNotice is a notice in the reader's language: the table's German or
// Arabic sentence for one of the client's English confirmations, and any other
// text (already localized, or a failure line) as it is.
func LocalizeChatNotice(locale, text string) string {
	row, ok := chatbug059Notices[text]
	if !ok {
		return text
	}
	if i := chatbug039LocaleIndex(locale); i > 0 {
		return chatbug039Text("", row[i-1], text)
	}
	return text
}

// ChatDirectLookupNotice is the line shown when the check for an existing direct
// message could not be finished (the list kept paging), in the reader's language.
func ChatDirectLookupNotice(locale string) string {
	return chatbug039Text("", [3]string{"Could not finish checking existing direct messages", "Die vorhandenen Direktnachrichten konnten nicht vollständig geprüft werden", "تعذّر إكمال التحقق من الرسائل المباشرة الموجودة"}[chatbug039LocaleIndex(locale)], "Could not finish checking existing direct messages")
}

// chatbug059CreatedBy is the line that names who made a conversation. The
// person looking is "you", in the case the sentence needs: "Erstellt von Ihnen".
func chatbug059CreatedBy(m Model, c Conversation, name string) string {
	if c.OwnerID != "" && c.OwnerID == m.CurrentUser {
		return chatbug039Text("created_by_you", [3]string{"Created by you", "Erstellt von Ihnen", "أنشأتها أنت"}[chatbug039LocaleIndex(m.Locale)], "Created by you")
	}
	return strings.ReplaceAll(chatux005Text(m, "created_by"), "{name}", name)
}
