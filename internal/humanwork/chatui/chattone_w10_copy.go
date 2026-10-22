package chatui

import "strings"

// CHATTONE-002 and CHATTONE-003: the words of the writer's "Others see:" block
// and of the reader's per-message switch between the reworded and the written
// text, in en-US, de-DE and ar. Like the other copy tables it never asks the
// product catalog for a key.
var chattoneW10Copy = map[string][3]string{
	"others_see":         {"Others see:", "Andere sehen:", "يرى الآخرون:"},
	"others_group":       {"How others read your message", "Wie andere Ihre Nachricht lesen", "كيف يقرأ الآخرون رسالتك"},
	"others_pending":     {"Preparing the reworded version for others.", "Die umformulierte Fassung für andere wird vorbereitet.", "جارٍ إعداد النسخة المعاد صياغتها للآخرين."},
	"others_unavailable": {"The reworded version is not available, so others read your message as you wrote it.", "Die umformulierte Fassung ist nicht verfügbar, daher lesen andere Ihre Nachricht so, wie Sie sie geschrieben haben.", "النسخة المعاد صياغتها غير متاحة، لذا يقرأ الآخرون رسالتك كما كتبتها."},
	"others_written":     {"Others read your message as you wrote it.", "Andere lesen Ihre Nachricht so, wie Sie sie geschrieben haben.", "يقرأ الآخرون رسالتك كما كتبتها."},
	"edit_wording":       {"Edit wording", "Wortlaut bearbeiten", "عدّل الصياغة"},
	"edit_wording_a":     {"Edit the wording others see in your message", "Den Wortlaut bearbeiten, den andere in Ihrer Nachricht sehen", "عدّل الصياغة التي يراها الآخرون في رسالتك"},
	"send_written":       {"Send as written", "Wie geschrieben senden", "أرسلها كما كتبتها"},
	"send_written_a":     {"Let others read your message as you wrote it", "Andere Ihre Nachricht so lesen lassen, wie Sie sie geschrieben haben", "دع الآخرين يقرؤون رسالتك كما كتبتها"},
	"send_failed":        {"This could not be changed. Try again.", "Das konnte nicht geändert werden. Versuchen Sie es erneut.", "تعذر تغيير ذلك. حاول مرة أخرى."},
	"sending":            {"Changing…", "Wird geändert…", "جارٍ التغيير…"},
	"reworded_from":      {"Reworded message from {name}", "Umformulierte Nachricht von {name}", "رسالة معاد صياغتها من {name}"},
	"show_written":       {"Show as written", "Wie geschrieben anzeigen", "عرض النص كما كُتب"},
	"hide_written":       {"Show reworded", "Umformuliert anzeigen", "عرض النسخة المعاد صياغتها"},
	"show_written_a":     {"Show as written, message from {name}", "Wie geschrieben anzeigen, Nachricht von {name}", "عرض النص كما كُتب، رسالة من {name}"},
	"hide_written_a":     {"Show reworded, message from {name}", "Umformuliert anzeigen, Nachricht von {name}", "عرض النسخة المعاد صياغتها، رسالة من {name}"},
	"as_written":         {"As written", "Wie geschrieben", "كما كُتب"},

	// CHATLANG-006: the mark of a pair that passed only the lower quality mark, the
	// reader's report of a wrong translation, and the notice shown the first time.
	"inaccurate":      {"May be inaccurate", "Kann ungenau sein", "قد تكون غير دقيقة"},
	"inaccurate_help": {"Translations between these two languages are still being checked. If it matters, read the original.", "Übersetzungen zwischen diesen beiden Sprachen werden noch geprüft. Wenn es darauf ankommt, lesen Sie das Original.", "ما زالت الترجمات بين هاتين اللغتين قيد المراجعة. إن كان الأمر مهماً فاقرأ الأصل."},
	"report":          {"Report this translation", "Diese Übersetzung melden", "الإبلاغ عن هذه الترجمة"},
	"report_a":        {"Report this translation of the message from {name}", "Diese Übersetzung der Nachricht von {name} melden", "الإبلاغ عن ترجمة رسالة {name}"},
	"report_group":    {"Report a wrong translation", "Eine falsche Übersetzung melden", "الإبلاغ عن ترجمة خاطئة"},
	"report_prompt":   {"Suggest a better translation (optional)", "Bessere Übersetzung vorschlagen (optional)", "اقترح ترجمة أفضل (اختياري)"},
	"report_send":     {"Send report", "Meldung senden", "أرسل البلاغ"},
	"report_cancel":   {"Cancel", "Abbrechen", "إلغاء"},
	"report_close":    {"Close", "Schließen", "إغلاق"},
	"report_sending":  {"Sending…", "Wird gesendet…", "جارٍ الإرسال…"},
	"report_sent":     {"Thank you. Your report went to the people who look after translations.", "Danke. Ihre Meldung ging an die Personen, die sich um Übersetzungen kümmern.", "شكراً لك. وصل بلاغك إلى المسؤولين عن الترجمات."},
	"report_failed":   {"The report could not be sent. Try again.", "Die Meldung konnte nicht gesendet werden. Versuchen Sie es erneut.", "تعذر إرسال البلاغ. حاول مرة أخرى."},
	"notice_text":     {"Translated automatically.", "Automatisch übersetzt.", "تُرجمت تلقائياً."},
	"notice_show":     {"Show original", "Original anzeigen", "عرض الأصل"},
	"notice_ok":       {"Got it", "Verstanden", "حسناً"},
	"notice_group":    {"About translated messages", "Zu übersetzten Nachrichten", "حول الرسائل المترجمة"},
}

// chattoneW10Text is the word for a key in a locale.
func chattoneW10Text(locale, key string) string {
	row, ok := chattoneW10Copy[key]
	if !ok {
		return ""
	}
	index := 0
	switch {
	case strings.HasPrefix(strings.ToLower(locale), "de"):
		index = 1
	case strings.HasPrefix(strings.ToLower(locale), "ar"):
		index = 2
	}
	return chatbug039Text(key, row[index], row[0])
}

func chattoneW10Name(locale, key, name string) string {
	return strings.ReplaceAll(chattoneW10Text(locale, key), "{name}", name)
}

// CHATLANG-005: the search page's "written in" filter.
func init() {
	for key, row := range map[string][3]string{
		"search_written":      {"Written in", "Geschrieben auf", "مكتوبة بـ"},
		"search_any_language": {"Any language", "Jede Sprache", "أي لغة"},
	} {
		chattoneW10Copy[key] = row
	}
}
