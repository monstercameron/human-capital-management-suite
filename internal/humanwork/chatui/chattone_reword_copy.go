package chatui

// chattoneRewordCopy holds the words of the reword settings: English, German
// and Arabic for every key. A key missing from the table is a bug the copy test
// catches, never a raw key on the page.
var chattoneRewordCopy = map[string][3]string{
	"title":        {"Heated messages", "Aufgeheizte Nachrichten", "الرسائل المتوترة"},
	"loading":      {"Loading…", "Wird geladen…", "جارٍ التحميل…"},
	"failed":       {"This could not be saved. Try again.", "Das konnte nicht gespeichert werden. Versuchen Sie es erneut.", "تعذّر الحفظ. حاول مرة أخرى."},
	"saved":        {"Saved.", "Gespeichert.", "تم الحفظ."},
	"retry":        {"Try again", "Erneut versuchen", "حاول مرة أخرى"},
	"forced":       {"In this conversation heated messages are shown reworded. The original is kept as the record and its writer can always read it.", "In diesem Gespräch werden aufgeheizte Nachrichten umformuliert angezeigt. Das Original bleibt als Aufzeichnung erhalten, und seine Verfasserin oder sein Verfasser kann es immer lesen.", "في هذه المحادثة تُعرض الرسائل المتوترة بصياغة معدّلة. يُحتفظ بالأصل كسجل ويمكن لكاتبه قراءته دائمًا."},
	"show":         {"Show heated messages", "Aufgeheizte Nachrichten anzeigen", "عرض الرسائل المتوترة"},
	"show_help":    {"Choose how you read them. The original is always kept.", "Wählen Sie, wie Sie sie lesen. Das Original bleibt immer erhalten.", "اختر طريقة قراءتها. يُحتفظ بالأصل دائمًا."},
	"reworded":     {"Reworded", "Umformuliert", "بصياغة معدّلة"},
	"written":      {"As written", "Wie geschrieben", "كما كُتبت"},
	"everywhere":   {"In every conversation", "In jedem Gespräch", "في كل المحادثات"},
	"here":         {"In this conversation", "In diesem Gespräch", "في هذه المحادثة"},
	"general":      {"Use my choice for every conversation", "Meine Wahl für alle Gespräche verwenden", "استخدم اختياري لكل المحادثات"},
	"admin":        {"Administrator settings", "Einstellungen der Administration", "إعدادات المسؤول"},
	"mode":         {"Reword heated messages", "Aufgeheizte Nachrichten umformulieren", "إعادة صياغة الرسائل المتوترة"},
	"off":          {"Off", "Aus", "إيقاف"},
	"offered":      {"Offered to writers", "Schreibenden angeboten", "معروض على الكتّاب"},
	"on":           {"On", "An", "تشغيل"},
	"off_help":     {"Members read every message as it was written.", "Mitglieder lesen jede Nachricht, wie sie geschrieben wurde.", "يقرأ الأعضاء كل رسالة كما كُتبت."},
	"offered_help": {"A reworded version is made for heated messages; each member chooses which to read.", "Für aufgeheizte Nachrichten wird eine umformulierte Fassung erstellt; jedes Mitglied wählt, welche es liest.", "تُنشأ نسخة معدّلة للرسائل المتوترة، ويختار كل عضو ما يقرؤه."},
	"on_help":      {"Heated messages are shown reworded by default; members who may read the original can switch.", "Aufgeheizte Nachrichten werden standardmäßig umformuliert angezeigt; Mitglieder mit Zugriff auf das Original können wechseln.", "تُعرض الرسائل المتوترة معدّلة افتراضيًا، ويمكن للأعضاء المسموح لهم بقراءة الأصل التبديل."},
	"view":         {"Members may view messages as written", "Mitglieder dürfen Nachrichten wie geschrieben ansehen", "يمكن للأعضاء عرض الرسائل كما كُتبت"},
	"yes":          {"Yes", "Ja", "نعم"},
	"no":           {"No", "Nein", "لا"},
	"yes_help":     {"Members can show the original of a reworded message.", "Mitglieder können das Original einer umformulierten Nachricht anzeigen.", "يمكن للأعضاء عرض الأصل لرسالة معدّلة."},
	"no_help":      {"Only the writer can read the original; everyone else always reads the reworded message. Originals are kept for records and moderators.", "Nur die Verfasserin oder der Verfasser kann das Original lesen; alle anderen lesen immer die umformulierte Nachricht. Originale bleiben für Aufzeichnungen und Moderation erhalten.", "يقرأ الأصل كاتبه فقط، ويقرأ الجميع الرسالة المعدّلة دائمًا. تُحفظ الأصول للسجلات والمشرفين."},
	"workspace":    {"Whole workspace", "Gesamter Arbeitsbereich", "مساحة العمل كلها"},
	"channel":      {"This channel only", "Nur dieser Kanal", "هذه القناة فقط"},
	"inherit":      {"Use the workspace's choice here", "Wahl des Arbeitsbereichs hier verwenden", "استخدم اختيار مساحة العمل هنا"},
	"override":     {"This channel has its own choice.", "Dieser Kanal hat eine eigene Wahl.", "لهذه القناة اختيارها الخاص."},
}

// ChattoneRewordText is the reword settings' word for a key in a locale, through
// the shared copy resolution (a missing or marker text is never shown).
func ChattoneRewordText(locale, key string) string {
	row, ok := chattoneRewordCopy[key]
	if !ok {
		return ""
	}
	return chatbug039Text(key, row[chatbug039LocaleIndex(locale)], row[0])
}
