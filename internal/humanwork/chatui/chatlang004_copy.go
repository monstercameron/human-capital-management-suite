package chatui

import (
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// CHATLANG-004: every word of the reading and writing across languages comes
// from this table, in en-US, de-DE and ar. It never asks the product catalog
// for a key, so the served page cannot print one (⟦key⟧).

func chatlangLocaleIndex(locale string) int {
	switch {
	case strings.HasPrefix(strings.ToLower(locale), "de"):
		return 1
	case strings.HasPrefix(strings.ToLower(locale), "ar"):
		return 2
	}
	return 0
}

func chatlangText(locale, key string) string {
	copy := map[string][3]string{
		"translated":      {"Translated from {language}", "Übersetzt aus {language}", "مترجم من {language}"},
		"translating":     {"Translating…", "Wird übersetzt…", "جارٍ الترجمة…"},
		"not_translated":  {"Not translated", "Nicht übersetzt", "لم تتم الترجمة"},
		"show_original":   {"Show original", "Original anzeigen", "عرض الأصل"},
		"hide_original":   {"Hide original", "Original ausblenden", "إخفاء الأصل"},
		"show_original_a": {"Show original of the message from {name}", "Original der Nachricht von {name} anzeigen", "عرض أصل رسالة {name}"},
		"hide_original_a": {"Hide original of the message from {name}", "Original der Nachricht von {name} ausblenden", "إخفاء أصل رسالة {name}"},
		"original_in":     {"Original ({language})", "Original ({language})", "الأصل ({language})"},
		"original":        {"Original", "Original", "الأصل"},
		"bar":             {"Messages in other languages are translated for you.", "Nachrichten in anderen Sprachen werden für Sie übersetzt.", "تُترجم الرسائل المكتوبة بلغات أخرى من أجلك."},
		"bar_originals":   {"Show originals", "Originale anzeigen", "عرض النصوص الأصلية"},
		"bar_settings":    {"Language settings", "Spracheinstellungen", "إعدادات اللغة"},
		"bar_dismiss":     {"Dismiss", "Schließen", "إغلاق"},
		"bar_label":       {"Translation", "Übersetzung", "الترجمة"},
		"fix_menu":        {"Message language…", "Sprache der Nachricht…", "لغة الرسالة…"},
		"fix_prompt":      {"Language of this message", "Sprache dieser Nachricht", "لغة هذه الرسالة"},
		"fix_none":        {"No language", "Keine Sprache", "بلا لغة"},
		"fix_cancel":      {"Cancel", "Abbrechen", "إلغاء"},
		"fix_failed":      {"The language could not be changed. Try again.", "Die Sprache konnte nicht geändert werden. Erneut versuchen.", "تعذر تغيير اللغة. حاول مرة أخرى."},
		"fix_saving":      {"Changing the language…", "Sprache wird geändert…", "جارٍ تغيير اللغة…"},
		"fix_group":       {"Message language", "Sprache der Nachricht", "لغة الرسالة"},
		"off_note":        {"Translation is not turned on in this workspace. Your choices are saved for when it is.", "Die Übersetzung ist in diesem Arbeitsbereich nicht eingeschaltet. Ihre Auswahl bleibt für später gespeichert.", "الترجمة غير مفعّلة في مساحة العمل هذه. تُحفظ اختياراتك لحين تفعيلها."},
		"audience_one":    {"{n} person will read this in {languages}", "{n} Person liest dies auf {languages}", "سيقرأ شخص واحد هذه الرسالة {languages}"},
		"audience_many":   {"{n} people will read this in {languages}", "{n} Personen lesen dies auf {languages}", "سيقرأ {n} {persons} هذه الرسالة {languages}"},
		"audience_two":    {"{n} people will read this in {languages}", "{n} Personen lesen dies auf {languages}", "سيقرأ شخصان هذه الرسالة {languages}"},
		"and":             {" and ", " und ", " و"},
		"comma":           {", ", ", ", "، "},
	}
	if value, ok := copy[key]; ok {
		return chatbug039Text(key, value[chatlangLocaleIndex(locale)], value[0])
	}
	return ""
}

// chatlangLanguageName is a language's name in the reader's language. A tag
// the product has no name for is shown as its upper-case tag, never a key.
func chatlangLanguageName(locale, tag string) string {
	tag = chatrender.Language(tag)
	switch tag {
	case "en", "de", "fr", "es", "pt", "ar", "ja", "hi":
		return RenderingText(locale, tag)
	}
	return strings.ToUpper(tag)
}

// chatlangArabicIn puts "ب" (in) before a language name: بالألمانية, بالعربية.
func chatlangArabicIn(name string) string {
	if strings.HasPrefix(name, "ال") {
		return "بال" + strings.TrimPrefix(name, "ال")
	}
	return "ب" + name
}

// chatlangJoin lists names the way the reader's language does: "A and B",
// "A, B and C".
func chatlangJoin(locale string, names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	and, comma := chatlangText(locale, "and"), chatlangText(locale, "comma")
	return strings.Join(names[:len(names)-1], comma) + and + names[len(names)-1]
}

// chatlangArabicPersons is "أشخاص" with the number agreement Arabic needs.
func chatlangArabicPersons(n int) string {
	if n >= 3 && n <= 10 {
		return "أشخاص"
	}
	return "شخصًا"
}

// ChatlangAudienceLine is the one muted line under the composer when the
// conversation has readers of other languages: "3 people will read this in
// German". It is empty when nobody reads a translation, so a conversation in
// one language never sees it. Counts come from the server; names are sorted so
// the line does not change between renders.
func ChatlangAudienceLine(m Model, a ChatlangAudience) string {
	if !a.Offered || len(a.Readers) == 0 {
		return ""
	}
	total := 0
	tags := make([]string, 0, len(a.Readers))
	for tag, n := range a.Readers {
		if n > 0 {
			total += n
			tags = append(tags, tag)
		}
	}
	if total == 0 {
		return ""
	}
	// Most readers first, then by tag, so the line does not change between renders.
	sort.Slice(tags, func(i, j int) bool {
		if a.Readers[tags[i]] != a.Readers[tags[j]] {
			return a.Readers[tags[i]] > a.Readers[tags[j]]
		}
		return tags[i] < tags[j]
	})
	index := chatlangLocaleIndex(m.Locale)
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		name := chatlangLanguageName(m.Locale, tag)
		if index == 2 {
			name = chatlangArabicIn(name)
		}
		names = append(names, name)
	}
	key := "audience_many"
	switch total {
	case 1:
		key = "audience_one"
	case 2:
		key = "audience_two"
	}
	line := chatlangText(m.Locale, key)
	line = strings.ReplaceAll(line, "{n}", m.n(total))
	line = strings.ReplaceAll(line, "{persons}", chatlangArabicPersons(total))
	return strings.ReplaceAll(line, "{languages}", chatlangJoin(m.Locale, names))
}
