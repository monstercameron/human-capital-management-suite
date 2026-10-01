package productui

import (
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// productCatalogRegistry is the product message registry with its built-in
// catalogs registered on first use, one locale at a time.
//
// Building a registered catalog clones and post-processes every message of
// that locale. Doing it for every supported locale at package init was the
// single largest cost of starting the browser client (UXBLIND-089: about 70 ms
// of a cold start, on the main thread, before the first byte of the page was
// hydrated), and a session only ever reads its own locale plus the English
// fallback. The registered result is identical; only its timing moves.
type productCatalogRegistry struct {
	registry *localize.Registry
	// builtin holds one registration per supported locale. The map is built
	// once and never written again; each entry is its own sync.OnceFunc, so
	// concurrent first reads of one locale register it exactly once.
	builtin map[string]func()
}

func newProductCatalogRegistry() *productCatalogRegistry {
	registry := localize.NewRegistry()
	builtin := make(map[string]func(), len(productMessages))
	for locale, messages := range productMessages {
		builtin[locale] = sync.OnceFunc(func() {
			if err := registry.Register(localize.Catalog{Locale: locale, Version: productCatalogVersion, Messages: productCatalogMessages(locale, messages)}); err != nil {
				panic(err)
			}
		})
	}
	return &productCatalogRegistry{registry: registry, builtin: builtin}
}

// ensure registers the built-in catalog of each named locale that has one.
func (r *productCatalogRegistry) ensure(locales ...string) {
	for _, locale := range locales {
		if register, ok := r.builtin[locale]; ok {
			register()
		}
	}
}

// Register adds a catalog revision (an activated tenant catalog) alongside
// the built-in ones.
func (r *productCatalogRegistry) Register(catalog localize.Catalog) error {
	return r.registry.Register(catalog)
}

// Resolve registers the requested locale and its fallbacks, then resolves.
func (r *productCatalogRegistry) Resolve(ctx localize.Context, key string, options localize.ResolveOptions) (localize.Result, error) {
	r.ensure(ctx.Locale)
	r.ensure(options.Fallback...)
	return r.registry.Resolve(ctx, key, options)
}

// Catalog returns a registered catalog revision, registering the locale's
// built-in catalog first.
func (r *productCatalogRegistry) Catalog(locale, version string) (localize.Catalog, bool) {
	r.ensure(locale)
	return r.registry.Catalog(locale, version)
}

// registered reports whether a locale's catalog is in the registry without
// registering it. It is how a test sees that a locale was never paid for.
func (r *productCatalogRegistry) registered(locale, version string) bool {
	_, ok := r.registry.Catalog(locale, version)
	return ok
}
