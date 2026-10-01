package timeclockapp

import (
	"strings"
)

// Locale is one of the kiosk's supported presentation locales.
type Locale string

// The supported locales. Arabic is the right-to-left proof locale.
const (
	LocaleEnUS Locale = "en-US"
	LocaleDeDE Locale = "de-DE"
	LocaleAr   Locale = "ar"
)

// Locales lists the supported locales in switcher order.
func Locales() []Locale { return []Locale{LocaleEnUS, LocaleDeDE, LocaleAr} }

// ResolveLocale maps a BCP 47 tag (a query parameter, navigator.language)
// onto a supported locale by primary language, defaulting to en-US.
func ResolveLocale(tag string) Locale {
	primary := strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(primary, "-_"); i >= 0 {
		primary = primary[:i]
	}
	switch primary {
	case "de":
		return LocaleDeDE
	case "ar":
		return LocaleAr
	default:
		return LocaleEnUS
	}
}

// Dir is the text direction of the locale: "rtl" or "ltr".
func (l Locale) Dir() string {
	if l == LocaleAr {
		return "rtl"
	}
	return "ltr"
}

// Native is the locale's own name, used by the language switcher.
func (l Locale) Native() string {
	switch l {
	case LocaleDeDE:
		return "Deutsch"
	case LocaleAr:
		return "العربية"
	default:
		return "English"
	}
}

// Digits rewrites ASCII digits into the locale's digit glyphs: Arabic-Indic
// for ar, unchanged otherwise.
func (l Locale) Digits(s string) string {
	if l != LocaleAr {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) * 2)
	for _, r := range s {
		if r >= '0' && r <= '9' {
			r = '٠' + (r - '0')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Text returns the catalog string for key with {name} placeholders
// replaced from args (args alternate name, value). A key missing from the
// locale falls back to en-US, then to the key itself, so a gap is visible
// rather than blank; TestTodo_TCLOCK_016_I18n keeps the catalogs complete.
func (l Locale) Text(key string, args ...string) string {
	text, ok := catalog[l][key]
	if !ok {
		text, ok = catalog[LocaleEnUS][key]
		if !ok {
			return key
		}
	}
	for i := 0; i+1 < len(args); i += 2 {
		text = strings.ReplaceAll(text, "{"+args[i]+"}", args[i+1])
	}
	return text
}
