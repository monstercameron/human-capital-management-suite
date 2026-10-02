package productui

import (
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// CHATBUG-014: productCatalogMessages builds the finished catalog of one
// locale from its reviewed source: it copies every message and then passes
// over all of them several times. The same finished catalog was built again by
// every caller. A browser client starting in English built the English one
// three times before its first paint (once to register it and twice to count
// which keys English is missing from English), about 170 ms on the review
// machine. The finished catalog of a built-in locale never changes, so it is
// built once and shared by its readers.

// productCatalogBaseByLocale holds one finished catalog per built-in locale,
// each built on first use. The map is built once and never written again.
var productCatalogBaseByLocale = func() map[string]func() map[string]localize.Message {
	memo := make(map[string]func() map[string]localize.Message, len(productMessages))
	for locale, messages := range productMessages {
		memo[locale] = sync.OnceValue(func() map[string]localize.Message {
			return productCatalogMessages(locale, messages)
		})
	}
	return memo
}()

// productLocaleOwnLabel is the name a language is listed under in the language
// menu, which every page's header draws.
//
// The names are reviewed once, in the English catalog; no other built-in
// catalog restates them. The menu asked each language for its own name, and
// asking a language for anything registers its whole catalog (a copy of every
// message and several passes over them) only to fall back to English for the
// one label. An English page therefore built the German and the Arabic
// catalogs before its first paint, about 150 ms on the review machine. A
// language whose built-in catalog does not name itself is answered from
// English directly, which is the text the fallback arrived at.
func productLocaleOwnLabel(candidate LocaleContext, code string) string {
	key := productLocaleLabelKey(code)
	if candidate.CatalogVersion == productCatalogVersion && candidate.Resolved != DefaultProductLocale {
		if _, own := productMessages[candidate.Resolved][key]; !own {
			english := candidate
			english.Resolved = DefaultProductLocale
			return english.Text(key)
		}
	}
	return candidate.Text(key)
}

// productCatalogBase returns the finished catalog of locale. The result is
// shared: callers read it and must not write to it. A caller that needs to
// change it copies it first (see mergeProductCatalog).
func productCatalogBase(locale string) map[string]localize.Message {
	if memo, ok := productCatalogBaseByLocale[locale]; ok {
		return memo()
	}
	return productCatalogMessages(locale, productMessages[locale])
}
