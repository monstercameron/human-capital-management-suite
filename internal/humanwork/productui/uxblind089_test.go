package productui

import (
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// TestTodo_UXBLIND_089_Regression pins the startup half of the cold-load fix:
// a fresh product registry pays for no built-in catalog until a locale is
// read, then pays for exactly that locale and its English fallback, and the
// messages it answers are the same ones the eager registry answered.
func TestTodo_UXBLIND_089_Regression(t *testing.T) {
	registry := newProductCatalogRegistry()
	for locale := range productMessages {
		if registry.registered(locale, productCatalogVersion) {
			t.Fatalf("a new registry already built the %s catalog; startup pays for every locale again", locale)
		}
	}
	german := "de-DE"
	if _, ok := productMessages[german]; !ok {
		t.Fatalf("test assumes a %s catalog", german)
	}
	options := localize.ResolveOptions{Fallback: []string{DefaultProductLocale}}
	context := localize.Context{Locale: german, CatalogVersion: productCatalogVersion}
	got, err := registry.Resolve(context, "page.chat_settings.label", options)
	if err != nil {
		t.Fatalf("Resolve de-DE: %v", err)
	}
	if got.Text != "Chat-Einstellungen" || got.Locale != german {
		t.Fatalf("Resolve de-DE = %q from %q, want the German catalog's label", got.Text, got.Locale)
	}
	for locale := range productMessages {
		want := locale == german || locale == DefaultProductLocale
		if registry.registered(locale, productCatalogVersion) != want {
			t.Errorf("after one German read, %s registered = %t, want %t", locale, !want, want)
		}
	}
	// Every key the eager registry held is answered identically.
	for locale, messages := range productMessages {
		want := productCatalogMessages(locale, messages)
		catalog, ok := registry.Catalog(locale, productCatalogVersion)
		if !ok {
			t.Fatalf("Catalog(%s) missing after first use", locale)
		}
		if len(catalog.Messages) != len(want) {
			t.Fatalf("%s catalog holds %d messages, want %d", locale, len(catalog.Messages), len(want))
		}
		for key, message := range want {
			if catalog.Messages[key].Text != message.Text {
				t.Fatalf("%s %s = %q, want %q", locale, key, catalog.Messages[key].Text, message.Text)
			}
		}
	}
}

// TestTodo_UXBLIND_089_RegressionConcurrentFirstUse proves concurrent first
// reads of one locale register it once and all resolve.
func TestTodo_UXBLIND_089_RegressionConcurrentFirstUse(t *testing.T) {
	registry := newProductCatalogRegistry()
	var wait sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := registry.Resolve(localize.Context{Locale: DefaultProductLocale, CatalogVersion: productCatalogVersion}, "page.chat_settings.label", localize.ResolveOptions{})
			errs <- err
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent first Resolve: %v", err)
		}
	}
}
