package productui

import "strings"

// The agent surfaces carry their copy in per-file trilingual tables: one map
// from copy key to the English, German and Arabic sentences. The tables below
// are package-level so a render does not rebuild them on every lookup, and
// every file resolves them through the two helpers here, so locale matching
// and missing-key handling stay the same everywhere.
//
// Matching is over canonical Resolved tags (en-US, de-DE, ar): a Resolved tag
// always comes from ResolveProductLocale, which maps language-only and
// regional tags (de, de-AT, ar-EG) to the reviewed catalogs. A base-language
// prefix still selects German or Arabic when a hand-built context carries a
// non-canonical tag, so no German or Arabic reader falls back to English.

// agentuxCopyIndex is the table column for a locale: 0 for English, 1 for
// German, 2 for Arabic.
func agentuxCopyIndex(locale LocaleContext) int {
	switch resolved := locale.Resolved; {
	case strings.HasPrefix(resolved, "de"):
		return 1
	case strings.HasPrefix(resolved, "ar"):
		return 2
	default:
		return 0
	}
}

// agentuxCopyText resolves one key from a trilingual copy table. A missing
// key reads as the empty string.
func agentuxCopyText(table map[string][3]string, locale LocaleContext, key string) string {
	text, _ := agentuxCopyLookup(table, locale, key)
	return text
}

// agentuxCopyLookup resolves one key from a trilingual copy table, reporting
// whether the table holds the key. Tables whose callers fall back to the key
// itself use this form.
func agentuxCopyLookup(table map[string][3]string, locale LocaleContext, key string) (string, bool) {
	values, ok := table[key]
	if !ok {
		return "", false
	}
	return values[agentuxCopyIndex(locale)], true
}
