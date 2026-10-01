package punchpolicy

import "fmt"

// Locale is the closed set of locales the device is required to render
// attestation question text in.
type Locale string

const (
	LocaleEnUS Locale = "en-US"
	LocaleDeDE Locale = "de-DE"
	LocaleAr   Locale = "ar"
)

// SupportedLocales lists every locale a localization key must resolve in.
func SupportedLocales() []Locale { return []Locale{LocaleEnUS, LocaleDeDE, LocaleAr} }

// QuestionTextKeys are the localization keys for the four attestation
// question kinds this package declares. A device or transport layer looks
// the worker's answer text up by kind through these keys; the key, not the
// literal text, is what a Question carries.
const (
	KeyBreakProvided     = "punchpolicy.question.break_provided"
	KeyMissedBreakReason = "punchpolicy.question.missed_break_reason"
	KeyInjury            = "punchpolicy.question.injury"
	KeyCustom            = "punchpolicy.question.custom"
)

// questionText is the localization table for this package's own
// declared keys. It is package-local, read-only data, not a mutable
// registry: a tenant's own custom question text lives with the tenant's
// QuestionSet, not here.
var questionText = map[string]map[Locale]string{
	KeyBreakProvided: {
		LocaleEnUS: "Were you provided your meal and rest breaks?",
		LocaleDeDE: "Wurden Ihnen Ihre Mahlzeiten- und Ruhepausen gewährt?",
		LocaleAr:   "هل حصلت على فترات الراحة وتناول الطعام المستحقة لك؟",
	},
	KeyMissedBreakReason: {
		LocaleEnUS: "What was the reason the break was missed?",
		LocaleDeDE: "Was war der Grund, warum die Pause ausgefallen ist?",
		LocaleAr:   "ما هو سبب تفويت فترة الراحة؟",
	},
	KeyInjury: {
		LocaleEnUS: "Were you injured during this shift?",
		LocaleDeDE: "Wurden Sie während dieser Schicht verletzt?",
		LocaleAr:   "هل تعرضت لإصابة خلال هذه الوردية؟",
	},
	KeyCustom: {
		LocaleEnUS: "",
		LocaleDeDE: "",
		LocaleAr:   "",
	},
}

// Localize resolves key in locale. KeyCustom has no fixed text in any
// locale: a custom question's text lives in the tenant's own localized
// content, so an empty result there is not a lookup failure. Every other
// declared key must resolve to non-empty text in every SupportedLocales
// entry, and TestTodo_TCLOCK_010_I18n proves it.
func Localize(key string, locale Locale) (string, error) {
	byLocale, ok := questionText[key]
	if !ok {
		return "", fmt.Errorf("punchpolicy: localization key %q is not declared", key)
	}
	text, ok := byLocale[locale]
	if !ok {
		return "", fmt.Errorf("punchpolicy: key %q has no text for locale %q", key, locale)
	}
	return text, nil
}
