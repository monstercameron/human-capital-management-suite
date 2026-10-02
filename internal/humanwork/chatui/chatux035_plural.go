package chatui

import "strings"

// CHATUX-035. Counts follow the plural rules of the language, through this one
// helper: a count, the noun it counts and the locale pick a form from a table,
// and the number inside the form is written in the locale's numerals (chatCount).
// English and German tell one from many; Arabic has six forms (none, one, two,
// 3 to 10, 11 to 99, 100 and more), and the noun after 18 is not the noun after 8.

// chatPluralCategory is the CLDR plural category of a whole number n in locale:
// "zero", "one", "two", "few", "many" or "other".
func chatPluralCategory(locale string, n int) string {
	if n < 0 {
		n = -n
	}
	switch lang := strings.ToLower(strings.SplitN(strings.ReplaceAll(locale, "_", "-"), "-", 2)[0]); lang {
	case "ar":
		switch rem := n % 100; {
		case n == 0:
			return "zero"
		case n == 1:
			return "one"
		case n == 2:
			return "two"
		case rem >= 3 && rem <= 10:
			return "few"
		case rem >= 11:
			return "many"
		}
		return "other"
	default:
		if n == 1 {
			return "one"
		}
		return "other"
	}
}

// chatPluralForms is the text of each counted noun by language and category.
// {n} is the count. A category a language does not list reads as "other".
var chatPluralForms = map[string]map[string]map[string]string{
	"members": {
		"en": {"one": "1 member", "other": "{n} members"},
		"de": {"one": "1 Mitglied", "other": "{n} Mitglieder"},
		"ar": {"zero": "لا أعضاء", "one": "عضو واحد", "two": "عضوان", "few": "{n} أعضاء", "many": "{n} عضوًا", "other": "{n} عضو"},
	},
	"replies": {
		"en": {"one": "1 reply", "other": "{n} replies"},
		"de": {"one": "1 Antwort", "other": "{n} Antworten"},
		"ar": {"zero": "لا ردود", "one": "رد واحد", "two": "ردان", "few": "{n} ردود", "many": "{n} ردًا", "other": "{n} رد"},
	},
	"agents": {
		"en": {"one": "1 agent", "other": "{n} agents"},
		"de": {"one": "1 Agent", "other": "{n} Agenten"},
		"ar": {"zero": "لا وكلاء", "one": "وكيل واحد", "two": "وكيلان", "few": "{n} وكلاء", "many": "{n} وكيلًا", "other": "{n} وكيل"},
	},
}

// chatPlural writes n of noun in the language of locale. An unknown noun or
// language falls back to English, so a count is never blank.
func chatPlural(locale, noun string, n int) string {
	forms := chatPluralForms[noun]
	lang := "en"
	switch {
	case strings.HasPrefix(strings.ToLower(locale), "de"):
		lang = "de"
	case strings.HasPrefix(strings.ToLower(locale), "ar"):
		lang = "ar"
	}
	byCategory := forms[lang]
	if byCategory == nil {
		byCategory = forms["en"]
	}
	form, ok := byCategory[chatPluralCategory(locale, n)]
	if !ok {
		form = byCategory["other"]
	}
	return strings.ReplaceAll(form, "{n}", chatCount(locale, n))
}
