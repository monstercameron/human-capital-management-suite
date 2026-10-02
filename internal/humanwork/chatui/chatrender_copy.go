package chatui

import "strings"

// RenderingText is shared by the SSR components and the wasm client. The copy
// table is constructed as a value, with no mutable package registration.
func RenderingText(locale, key string) string {
	copy := map[string][3]string{
		"title":             {"Reading languages", "Lesesprachen", "لغات القراءة"},
		"reading":           {"Read messages in", "Nachrichten lesen auf", "قراءة الرسائل باللغة"},
		"translate":         {"Translate messages into my language", "Nachrichten in meine Sprache übersetzen", "ترجمة الرسائل إلى لغتي"},
		"further":           {"Also read without translation", "Auch ohne Übersetzung lesen", "القراءة أيضًا دون ترجمة"},
		"source":            {"Never translate from", "Nie übersetzen aus", "عدم الترجمة من"},
		"scope":             {"Apply to this conversation", "Auf dieses Gespräch anwenden", "تطبيق على هذه المحادثة"},
		"save":              {"Save language settings", "Spracheinstellungen speichern", "حفظ إعدادات اللغة"},
		"saved":             {"Language settings saved", "Spracheinstellungen gespeichert", "تم حفظ إعدادات اللغة"},
		"loading":           {"Loading language settings…", "Spracheinstellungen werden geladen…", "جارٍ تحميل إعدادات اللغة…"},
		"error":             {"Settings could not be saved. Try again.", "Einstellungen konnten nicht gespeichert werden. Erneut versuchen.", "تعذر حفظ الإعدادات. حاول مرة أخرى."},
		"error_signed_out":  {"You were signed out, so this was not saved. Reload the page to sign in again; your choice is kept until then.", "Sie wurden abgemeldet, daher wurde nichts gespeichert. Laden Sie die Seite neu und melden Sie sich erneut an; Ihre Auswahl bleibt bis dahin erhalten.", "تم تسجيل خروجك لذا لم يُحفظ ذلك. أعد تحميل الصفحة لتسجيل الدخول مجددًا؛ يبقى اختيارك محفوظًا هنا حتى ذلك الحين."},
		"error_denied":      {"You are not allowed to change this setting.", "Sie dürfen diese Einstellung nicht ändern.", "غير مسموح لك بتغيير هذا الإعداد."},
		"error_unreachable": {"The server did not answer, so this was not saved. Your choice is kept here; press Try again.", "Der Server hat nicht geantwortet, daher wurde nichts gespeichert. Ihre Auswahl bleibt hier erhalten; wählen Sie Erneut versuchen.", "لم يستجب الخادم لذا لم يُحفظ ذلك. يبقى اختيارك هنا؛ اضغط حاول مرة أخرى."},
		"retry":             {"Try again", "Erneut versuchen", "حاول مرة أخرى"},
		"help":              {"Choose a reading language. Your interface language stays the same.", "Wählen Sie eine Lesesprache. Ihre Oberflächensprache bleibt gleich.", "اختر لغة القراءة. تبقى لغة الواجهة كما هي."},
		"translated":        {"Translated from {language}", "Übersetzt aus {language}", "مترجم من {language}"},
		"reworded":          {"Reworded", "Umformuliert", "أعيدت الصياغة"},
		"mask":              {"Sensitive wording hidden", "Sensible Formulierungen ausgeblendet", "تم إخفاء العبارات الحساسة"},
		"original":          {"Show original", "Original anzeigen", "عرض الأصل"},
		"written":           {"Show as written", "Wie geschrieben anzeigen", "عرض النص كما كُتب"},
		"pending":           {"Preparing this view. The original will appear shortly if allowed.", "Diese Ansicht wird vorbereitet. Das Original erscheint in Kürze, sofern erlaubt.", "جارٍ إعداد هذا العرض. سيظهر الأصل قريبًا إذا كان مسموحًا."},
		"fallback":          {"This view is not ready. Showing the original.", "Diese Ansicht ist noch nicht verfügbar. Das Original wird angezeigt.", "هذا العرض غير جاهز. يتم عرض الأصل."},
		"unavailable":       {"This view is unavailable. Try again later.", "Diese Ansicht ist nicht verfügbar. Später erneut versuchen.", "هذا العرض غير متاح. حاول لاحقًا."},
		"languages":         {"Languages in this conversation", "Sprachen in diesem Gespräch", "اللغات في هذه المحادثة"},
		"languages_loading": {"Loading conversation languages…", "Gesprächssprachen werden geladen…", "جارٍ تحميل لغات المحادثة…"},
		"languages_error":   {"Languages could not be loaded. Reload Chat to retry.", "Sprachen konnten nicht geladen werden. Laden Sie Chat erneut.", "تعذر تحميل اللغات. أعد تحميل المحادثة للمحاولة مجددًا."},
		"empty":             {"No reading languages shared yet. Set your reading language.", "Noch keine Lesesprachen geteilt. Legen Sie Ihre Lesesprache fest.", "لم تُشارك لغات القراءة بعد. عيّن لغة القراءة."},
		"en":                {"English", "Englisch", "الإنجليزية"}, "de": {"German", "Deutsch", "الألمانية"},
		"fr": {"French", "Französisch", "الفرنسية"}, "es": {"Spanish", "Spanisch", "الإسبانية"},
		"pt": {"Portuguese", "Portugiesisch", "البرتغالية"}, "ar": {"Arabic", "Arabisch", "العربية"},
		"ja": {"Japanese", "Japanisch", "اليابانية"}, "hi": {"Hindi", "Hindi", "الهندية"},
		"und": {"Not specified", "Nicht angegeben", "غير محددة"}, "mul": {"Multiple languages", "Mehrere Sprachen", "لغات متعددة"},
	}
	i := 0
	if strings.HasPrefix(strings.ToLower(locale), "de") {
		i = 1
	}
	if strings.HasPrefix(strings.ToLower(locale), "ar") {
		i = 2
	}
	if value, ok := copy[key]; ok {
		return chatbug039Text(key, value[i], value[0])
	}
	return chatbug039Text(key, copy["und"][i], copy["und"][0])
}
