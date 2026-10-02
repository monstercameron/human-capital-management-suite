package chatui

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func chatsearchText(locale, key string) string {
	en := map[string]string{"search": "Search", "filters": "Refine search", "channel": "In a channel", "person": "From a person", "kind": "Content kind", "all": "All kinds", "before": "Before", "after": "After", "on": "On a date", "file": "With a file", "link": "With a link", "reactions": "Has reactions", "threads": "In threads", "mentions": "Mentions me", "agent": "By an agent", "mine": "Mine only", "voice": "Has a voice message", "apply": "Apply filters", "remove": "Remove filter", "loading": "Searching…", "error": "Search is unavailable. Try again.", "invalid": "Check your filters and dates, then try again.", "meaning": "Search by meaning is unavailable. Use keywords and filters.", "empty": "No matches for {query}. Remove a filter or try fewer words.", "widen": "Widen search", "shown": "shown", "open": "Open result", "only": "Only you", "more": "Show next results", "back": "Return to results", "recent": "Recent searches", "clear": "Clear recent searches", "missing": "Some content sources are unavailable: {kinds}. Try again later.", "count": "{n} results", "countOne": "1 result", "keyword": "Keyword search", "retry": "Try again", "unavailable": "Search is not available right now.", "none": "No results"}
	de := map[string]string{"search": "Suchen", "filters": "Suche eingrenzen", "channel": "In einem Kanal", "person": "Von einer Person", "kind": "Inhaltsart", "all": "Alle Arten", "before": "Vor", "after": "Nach", "on": "Am Datum", "file": "Mit Datei", "link": "Mit Link", "reactions": "Mit Reaktionen", "threads": "In Threads", "mentions": "Erwähnt mich", "agent": "Von einem Agenten", "mine": "Nur meine", "voice": "Mit Sprachnachricht", "apply": "Filter anwenden", "remove": "Filter entfernen", "loading": "Aktuelle Inhalte werden durchsucht…", "error": "Suche nicht verfügbar. Erneut versuchen.", "invalid": "Filter und Daten prüfen und erneut versuchen.", "meaning": "Bedeutungssuche ist nicht verfügbar. Stichwörter und Filter verwenden.", "empty": "Keine Treffer für {query}. Filter entfernen oder weniger Wörter versuchen.", "widen": "Suche erweitern", "shown": "angezeigt", "open": "Ergebnis öffnen", "only": "Nur für dich", "more": "Nächste Ergebnisse anzeigen", "back": "Zu Ergebnissen zurückkehren", "recent": "Letzte Suchen", "clear": "Letzte Suchen löschen", "missing": "Einige Inhaltsquellen sind nicht verfügbar: {kinds}. Später erneut versuchen.", "count": "{n} Ergebnisse", "countOne": "1 Ergebnis", "keyword": "Stichwortsuche", "retry": "Erneut versuchen", "unavailable": "Die Suche ist gerade nicht verfügbar.", "none": "Keine Ergebnisse"}
	ar := map[string]string{"search": "بحث", "filters": "تضييق البحث", "channel": "في قناة", "person": "من شخص", "kind": "نوع المحتوى", "all": "كل الأنواع", "before": "قبل", "after": "بعد", "on": "في تاريخ", "file": "مع ملف", "link": "مع رابط", "reactions": "له تفاعلات", "threads": "في سلاسل الردود", "mentions": "يذكرني", "agent": "من وكيل", "mine": "محتواي فقط", "voice": "يتضمن رسالة صوتية", "apply": "تطبيق المرشحات", "remove": "إزالة المرشح", "loading": "جارٍ البحث في المحتوى الحالي…", "error": "البحث غير متاح. حاول مرة أخرى.", "invalid": "راجع المرشحات والتواريخ ثم حاول مرة أخرى.", "meaning": "البحث بالمعنى غير متاح. استخدم الكلمات والمرشحات.", "empty": "لا توجد نتائج لـ {query}. أزل مرشحاً أو جرّب كلمات أقل.", "widen": "توسيع البحث", "shown": "معروض", "open": "فتح النتيجة", "only": "لك فقط", "more": "عرض النتائج التالية", "back": "العودة إلى النتائج", "recent": "عمليات البحث الأخيرة", "clear": "مسح عمليات البحث الأخيرة", "missing": "بعض مصادر المحتوى غير متاحة: {kinds}. حاول لاحقاً.", "count": "{n} نتيجة", "countOne": "نتيجة واحدة", "keyword": "بحث بالكلمات", "retry": "حاول مرة أخرى", "unavailable": "البحث غير متاح حالياً.", "none": "لا توجد نتائج"}
	copy := en
	if strings.HasPrefix(locale, "de") {
		copy = de
	}
	if strings.HasPrefix(locale, "ar") {
		copy = ar
	}
	return chatbug039Text(key, copy[key], en[key])
}
func chatsearchKind(locale string, kind chatsearch.Kind) string {
	labels := map[chatsearch.Kind][3]string{
		chatsearch.Message: {"Message", "Nachricht", "رسالة"}, chatsearch.Thread: {"Thread reply", "Thread-Antwort", "رد في سلسلة"}, chatsearch.Conversation: {"Channel", "Kanal", "قناة"}, chatsearch.Person: {"Person", "Person", "شخص"}, chatsearch.File: {"File", "Datei", "ملف"}, chatsearch.Pin: {"Pinned messages", "Angeheftete Nachrichten", "رسائل مثبتة"}, chatsearch.Todo: {"To-do", "Aufgabe", "مهمة"}, chatsearch.Poll: {"Poll", "Umfrage", "استطلاع"}, chatsearch.AgentAnswer: {"Agent answer", "Agentenantwort", "إجابة الوكيل"}, chatsearch.SourceTitle: {"Source", "Quelle", "مصدر"}, chatsearch.Voice: {"Voice message", "Sprachnachricht", "رسالة صوتية"}, chatsearch.VoiceCorrection: {"Transcript correction", "Transkriptkorrektur", "تصحيح النص الصوتي"}, chatsearch.Announcement: {"Announcement", "Ankündigung", "إعلان"}, chatsearch.Reminder: {"Reminder", "Erinnerung", "تذكير"}, chatsearch.PrivateTask: {"Private task", "Private Aufgabe", "مهمة خاصة"}, chatsearch.PrivateReminder: {"Private reminder", "Private Erinnerung", "تذكير خاص"}, chatsearch.GateQuestion: {"Gate question", "Zugangsfrage", "سؤال الوصول"}, chatsearch.GateAnswer: {"Your gate answer", "Deine Zugangsantwort", "إجابتك للوصول"}, chatsearch.Saved: {"Saved item", "Gespeicherter Inhalt", "عنصر محفوظ"}, chatsearch.Location: {"Location", "Ort", "موقع"}, chatsearch.GateDefinition: {"Gate", "Zugang", "بوابة"}, chatsearch.FilterDefinition: {"Filter", "Filter", "مرشح"}, chatsearch.Moderation: {"Moderation item", "Moderationseintrag", "عنصر المراجعة"},
	}
	if name, ok := chatsearch003Name(locale, kind, false); ok {
		return name
	}
	i := 0
	if strings.HasPrefix(locale, "de") {
		i = 1
	}
	if strings.HasPrefix(locale, "ar") {
		i = 2
	}
	return chatbug039Text(string(kind), labels[kind][i], labels[kind][0])
}
