package chatui

import "strings"

// Copy for the emoji picker in the three product languages. Reviewed feature
// tables resolve through the shared guard with an English fallback.
const (
	emojiKeySearch      = "chat.emoji.search"
	emojiKeyTrigger     = "chat.emoji.trigger"
	emojiKeyTitle       = "chat.emoji.title"
	emojiKeyReactTitle  = "chat.emoji.react_title"
	emojiKeyItem        = "chat.emoji.item"
	emojiKeyFlag        = "chat.emoji.flag"
	emojiKeyFlagPlain   = "chat.emoji.flag_plain"
	emojiKeyTabs        = "chat.emoji.tabs"
	emojiKeyFrequent    = "chat.emoji.frequent"
	emojiKeyGrid        = "chat.emoji.grid"
	emojiKeyNoMatch     = "chat.emoji.no_match"
	emojiKeyLoading     = "chat.emoji.loading"
	emojiKeyLoadFailed  = "chat.emoji.load_failed"
	emojiKeyTone        = "chat.emoji.tone"
	emojiKeyToneNow     = "chat.emoji.tone_now"
	emojiKeyToneDefault = "chat.emoji.tone_default"
	emojiKeyToneChoose  = "chat.emoji.tone_choose"
	emojiKeyHintInsert  = "chat.emoji.hint_insert"
	emojiKeyHintReact   = "chat.emoji.hint_react"
	emojiKeyGroupPrefix = "chat.emoji.group."
	emojiKeyTonePrefix  = "chat.emoji.tone."
)

var chatEmojiCopy = map[string]map[string]string{
	"en-US": {
		emojiKeySearch:                          "Search emoji",
		emojiKeyTrigger:                         "Insert emoji",
		emojiKeyTitle:                           "Choose an emoji",
		emojiKeyReactTitle:                      "Choose a reaction",
		emojiKeyItem:                            "Emoji {emoji}",
		emojiKeyFlag:                            "Flag: {code}",
		emojiKeyFlagPlain:                       "Flag",
		emojiKeyTabs:                            "Emoji categories",
		emojiKeyFrequent:                        "Frequently used",
		emojiKeyGrid:                            "Emoji",
		emojiKeyNoMatch:                         "No emoji match “{query}”.",
		emojiKeyLoading:                         "Loading the emoji list…",
		emojiKeyLoadFailed:                      "The full emoji list could not be loaded.",
		emojiKeyTone:                            "Skin tone",
		emojiKeyToneNow:                         "Skin tone: {tone}. Change",
		emojiKeyToneDefault:                     "Default skin tone",
		emojiKeyToneChoose:                      "Choose a skin tone",
		emojiKeyHintInsert:                      "Type to search. Enter inserts, Shift+Enter keeps this open.",
		emojiKeyHintReact:                       "Type to search. Enter adds the reaction.",
		emojiKeyGroupPrefix + "smileys-emotion": "Smileys & Emotion",
		emojiKeyGroupPrefix + "people-body":     "People & Body",
		emojiKeyGroupPrefix + "animals-nature":  "Animals & Nature",
		emojiKeyGroupPrefix + "food-drink":      "Food & Drink",
		emojiKeyGroupPrefix + "travel-places":   "Travel & Places",
		emojiKeyGroupPrefix + "activities":      "Activities",
		emojiKeyGroupPrefix + "objects":         "Objects",
		emojiKeyGroupPrefix + "symbols":         "Symbols",
		emojiKeyGroupPrefix + "flags":           "Flags",
		emojiKeyTonePrefix + "1":                "Light skin tone",
		emojiKeyTonePrefix + "2":                "Medium-light skin tone",
		emojiKeyTonePrefix + "3":                "Medium skin tone",
		emojiKeyTonePrefix + "4":                "Medium-dark skin tone",
		emojiKeyTonePrefix + "5":                "Dark skin tone",
	},
	"de-DE": {
		emojiKeySearch:                          "Emoji suchen",
		emojiKeyTrigger:                         "Emoji einfügen",
		emojiKeyTitle:                           "Emoji auswählen",
		emojiKeyReactTitle:                      "Reaktion auswählen",
		emojiKeyItem:                            "Emoji {emoji}",
		emojiKeyFlag:                            "Flagge: {code}",
		emojiKeyFlagPlain:                       "Flagge",
		emojiKeyTabs:                            "Emoji-Kategorien",
		emojiKeyFrequent:                        "Häufig verwendet",
		emojiKeyGrid:                            "Emoji",
		emojiKeyNoMatch:                         "Keine Emoji passen zu „{query}“.",
		emojiKeyLoading:                         "Die Emoji-Liste wird geladen …",
		emojiKeyLoadFailed:                      "Die vollständige Emoji-Liste konnte nicht geladen werden.",
		emojiKeyTone:                            "Hautfarbe",
		emojiKeyToneNow:                         "Hautfarbe: {tone}. Ändern",
		emojiKeyToneDefault:                     "Standard-Hautfarbe",
		emojiKeyToneChoose:                      "Hautfarbe wählen",
		emojiKeyHintInsert:                      "Zum Suchen tippen. Eingabe fügt ein, Umschalt+Eingabe lässt das Fenster offen.",
		emojiKeyHintReact:                       "Zum Suchen tippen. Eingabe fügt die Reaktion hinzu.",
		emojiKeyGroupPrefix + "smileys-emotion": "Smileys & Emotionen",
		emojiKeyGroupPrefix + "people-body":     "Personen & Körper",
		emojiKeyGroupPrefix + "animals-nature":  "Tiere & Natur",
		emojiKeyGroupPrefix + "food-drink":      "Essen & Trinken",
		emojiKeyGroupPrefix + "travel-places":   "Reisen & Orte",
		emojiKeyGroupPrefix + "activities":      "Aktivitäten",
		emojiKeyGroupPrefix + "objects":         "Objekte",
		emojiKeyGroupPrefix + "symbols":         "Symbole",
		emojiKeyGroupPrefix + "flags":           "Flaggen",
		emojiKeyTonePrefix + "1":                "Helle Hautfarbe",
		emojiKeyTonePrefix + "2":                "Mittelhelle Hautfarbe",
		emojiKeyTonePrefix + "3":                "Mittlere Hautfarbe",
		emojiKeyTonePrefix + "4":                "Mitteldunkle Hautfarbe",
		emojiKeyTonePrefix + "5":                "Dunkle Hautfarbe",
	},
	"ar": {
		emojiKeySearch:                          "البحث عن رمز تعبيري",
		emojiKeyTrigger:                         "إدراج رمز تعبيري",
		emojiKeyTitle:                           "اختر رمزًا تعبيريًا",
		emojiKeyReactTitle:                      "اختر تفاعلًا",
		emojiKeyItem:                            "رمز تعبيري {emoji}",
		emojiKeyFlag:                            "علم: {code}",
		emojiKeyFlagPlain:                       "علم",
		emojiKeyTabs:                            "فئات الرموز التعبيرية",
		emojiKeyFrequent:                        "الأكثر استخدامًا",
		emojiKeyGrid:                            "الرموز التعبيرية",
		emojiKeyNoMatch:                         "لا توجد رموز تعبيرية تطابق «{query}».",
		emojiKeyLoading:                         "جارٍ تحميل قائمة الرموز التعبيرية…",
		emojiKeyLoadFailed:                      "تعذّر تحميل قائمة الرموز التعبيرية الكاملة.",
		emojiKeyTone:                            "لون البشرة",
		emojiKeyToneNow:                         "لون البشرة: {tone}. تغيير",
		emojiKeyToneDefault:                     "لون البشرة الافتراضي",
		emojiKeyToneChoose:                      "اختر لون البشرة",
		emojiKeyHintInsert:                      "اكتب للبحث. Enter للإدراج، وShift+Enter لإبقاء النافذة مفتوحة.",
		emojiKeyHintReact:                       "اكتب للبحث. Enter لإضافة التفاعل.",
		emojiKeyGroupPrefix + "smileys-emotion": "الوجوه والمشاعر",
		emojiKeyGroupPrefix + "people-body":     "الأشخاص والجسم",
		emojiKeyGroupPrefix + "animals-nature":  "الحيوانات والطبيعة",
		emojiKeyGroupPrefix + "food-drink":      "الطعام والشراب",
		emojiKeyGroupPrefix + "travel-places":   "السفر والأماكن",
		emojiKeyGroupPrefix + "activities":      "الأنشطة",
		emojiKeyGroupPrefix + "objects":         "الأشياء",
		emojiKeyGroupPrefix + "symbols":         "الرموز",
		emojiKeyGroupPrefix + "flags":           "الأعلام",
		emojiKeyTonePrefix + "1":                "بشرة فاتحة",
		emojiKeyTonePrefix + "2":                "بشرة فاتحة متوسطة",
		emojiKeyTonePrefix + "3":                "بشرة متوسطة",
		emojiKeyTonePrefix + "4":                "بشرة داكنة متوسطة",
		emojiKeyTonePrefix + "5":                "بشرة داكنة",
	},
}

func chatEmojiLocale(locale string) string {
	switch {
	case strings.HasPrefix(locale, "de"):
		return "de-DE"
	case strings.HasPrefix(locale, "ar"):
		return "ar"
	}
	return "en-US"
}

// chatEmojiLang is the language code of the emoji data for a locale.
func chatEmojiLang(locale string) string {
	switch chatEmojiLocale(locale) {
	case "de-DE":
		return "de"
	case "ar":
		return "ar"
	}
	return "en"
}

// chatEmojiText resolves the picker table through the shared copy guard.
func chatEmojiText(m Model, key string) string {
	return chatbug039Text(key, chatEmojiCopy[chatEmojiLocale(m.Locale)][key], chatEmojiCopy["en-US"][key])
}

// chatEmojiLegacyKeys are the earlier copy keys of the same words, which a
// catalog may still carry a translation for.
var chatEmojiLegacyKeys = map[string]string{
	emojiKeyTrigger: KeyEmojiPicker, emojiKeyTitle: KeyEmojiPickerTitle, emojiKeyReactTitle: KeyPickReaction, emojiKeyItem: KeyEmojiItem,
}

func chatEmojiFormat(m Model, key string, args map[string]string) string {
	value := chatEmojiText(m, key)
	for name, arg := range args {
		value = strings.ReplaceAll(value, "{"+name+"}", arg)
	}
	return value
}
