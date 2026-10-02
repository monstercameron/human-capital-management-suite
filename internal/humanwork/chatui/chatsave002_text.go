package chatui

import (
	"strconv"
	"strings"
)

// CHATSAVE-002. The Saved panel's own words, in the three product languages.
// Every string is answered from this table and never from the product catalog,
// so a catalog that does not hold a key can never print a bracketed key on the
// page. A placeholder is written {name}.
var chatsave002Table = map[string][3]string{
	"todo_count":    {"{n} to do", "{n} zu erledigen", "{n} للتنفيذ"},
	"todo_none":     {"Nothing left to do", "Nichts mehr zu erledigen", "لا شيء متبقٍ للتنفيذ"},
	"segments":      {"Show saved messages", "Gespeicherte Nachrichten anzeigen", "عرض الرسائل المحفوظة"},
	"in":            {"in {where}", "in {where}", "في {where}"},
	"show_more":     {"Show more", "Mehr anzeigen", "عرض المزيد"},
	"show_less":     {"Show less", "Weniger anzeigen", "عرض أقل"},
	"item_open":     {"Open in the conversation", "In der Unterhaltung öffnen", "فتح في المحادثة"},
	"actions":       {"Actions for this saved message", "Aktionen für diese gespeicherte Nachricht", "إجراءات هذه الرسالة المحفوظة"},
	"act_done":      {"Mark done", "Als erledigt markieren", "وضع علامة تم"},
	"act_remind":    {"Remind me", "Erinnern", "ذكّرني"},
	"act_note":      {"Add a note", "Notiz hinzufügen", "إضافة ملاحظة"},
	"act_note_edit": {"Edit note", "Notiz bearbeiten", "تعديل الملاحظة"},
	"done_on":       {"Done {when}", "Erledigt {when}", "تم {when}"},
	"reminder":      {"Reminder: {when}", "Erinnerung: {when}", "تذكير: {when}"},
	"overdue":       {"Overdue", "Überfällig", "متأخر"},
	"menu_label":    {"Remind me", "Erinnern", "ذكّرني"},
	"p_hour":        {"In 1 hour", "In 1 Stunde", "بعد ساعة"},
	"p_afternoon":   {"This afternoon", "Heute Nachmittag", "بعد الظهر"},
	"p_tomorrow":    {"Tomorrow at {time}", "Morgen um {time}", "غدًا في {time}"},
	"p_monday":      {"Next Monday at {time}", "Nächsten Montag um {time}", "الإثنين القادم في {time}"},
	"p_pick":        {"Pick a date and time", "Datum und Uhrzeit wählen", "اختر التاريخ والوقت"},
	"p_set":         {"Set reminder", "Erinnerung setzen", "ضبط التذكير"},
	"note_hint":     {"Enter to save, Esc to cancel", "Enter zum Speichern, Esc zum Abbrechen", "Enter للحفظ وEsc للإلغاء"},
	"note_ph":       {"Add a note for yourself", "Notiz für sich selbst hinzufügen", "أضف ملاحظة لنفسك"},
	"undo":          {"Undo", "Rückgängig", "تراجع"},
	"u_done":        {"Marked done", "Als erledigt markiert", "تم وضع علامة تم"},
	"u_remove":      {"Removed from Saved", "Aus Gespeichert entfernt", "أُزيلت من المحفوظات"},
	"no_match":      {"No saved messages match your search.", "Keine gespeicherten Nachrichten passen zu Ihrer Suche.", "لا توجد رسائل محفوظة تطابق بحثك."},
	"back":          {"Back", "Zurück", "رجوع"},
	"today":         {"Today", "Heute", "اليوم"},
	"yesterday":     {"Yesterday", "Gestern", "أمس"},
	"tomorrow":      {"Tomorrow", "Morgen", "غدًا"},
}

// chatsave002Text answers one of this panel's strings; placeholders are
// replaced from pairs of name, value. An unknown key answers empty, never the key.
func chatsave002Text(locale, key string, pairs ...string) string {
	values, ok := chatsave002Table[key]
	if !ok {
		return ""
	}
	text := values[0]
	switch dateLocale(locale) {
	case "de":
		text = values[1]
	case "ar":
		text = values[2]
	}
	text = chatbug039Text(key, text, values[0])
	for i := 0; i+1 < len(pairs); i += 2 {
		text = strings.ReplaceAll(text, "{"+pairs[i]+"}", pairs[i+1])
	}
	return text
}

// chatsave002Num writes a count in the locale's own digits.
func chatsave002Num(locale string, n int) string {
	text := strconv.Itoa(n)
	if dateLocale(locale) == "ar" {
		return arabicDigits(text)
	}
	return text
}

// SavedTodoCountText is the line under the panel's heading: how many saved
// messages are still to do.
func SavedTodoCountText(locale string, todo int) string {
	if todo <= 0 {
		return chatsave002Text(locale, "todo_none")
	}
	return chatsave002Text(locale, "todo_count", "n", chatsave002Num(locale, todo))
}
