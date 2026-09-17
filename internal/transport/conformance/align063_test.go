package conformance_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/conformance"
)

// TestTodo_ALIGN_063 proves accessibility and localization qualify across
// the slice: every surface renders every key in every locale, each with
// visible text and a screen-reader announcement.
func TestTodo_ALIGN_063(t *testing.T) {
	if err := conformance.QualifySlice(); err != nil {
		t.Fatalf("QualifySlice: %v", err)
	}
	message, err := conformance.Label(conformance.SurfaceSSR, "title.home", conformance.LocaleEnglish)
	if err != nil {
		t.Fatalf("Label: %v", err)
	}
	if message.Text != "Home" || message.Announcement == "" {
		t.Fatalf("message = %+v", message)
	}
}

func TestTodo_ALIGN_063_Property(t *testing.T) {
	if conformance.CatalogDigest() != conformance.CatalogDigest() {
		t.Fatal("catalog digest is not deterministic")
	}
	for _, locale := range []conformance.Locale{conformance.LocaleEnglish, conformance.LocaleSpanish, conformance.LocaleFrench} {
		first, err := conformance.Label(conformance.SurfaceEnhancedBrowser, "action.refresh", locale)
		if err != nil {
			t.Fatalf("Label(enhanced, refresh, %s): %v", locale, err)
		}
		second, err := conformance.Label(conformance.SurfaceEnhancedBrowser, "action.refresh", locale)
		if err != nil || first != second {
			t.Fatalf("label is not stable for locale %s", locale)
		}
	}
}

func TestTodo_ALIGN_063_Golden(t *testing.T) {
	const wantDigest = "sha256:75269adc7e6a37d0c0da641fed4593a83d212ea1dbbc75160fe977e86437cf19"
	if got := conformance.CatalogDigest(); got != wantDigest {
		t.Fatalf("catalog digest=%q want=%q", got, wantDigest)
	}
}

func TestTodo_ALIGN_063_Security(t *testing.T) {
	// Unqualified surfaces, unknown keys, and unsupported locales are
	// refused — no fallback silently substitutes another language.
	if _, err := conformance.Label(99, "title.home", conformance.LocaleEnglish); !errors.Is(err, conformance.ErrSurfaceUnknown) {
		t.Fatalf("Label(surface 99) = %v, want ErrSurfaceUnknown", err)
	}
	if _, err := conformance.Label(conformance.SurfaceSSR, "ghost.key", conformance.LocaleEnglish); !errors.Is(err, conformance.ErrMessageInvalid) {
		t.Fatalf("Label(ghost) = %v, want ErrMessageInvalid", err)
	}
	if _, err := conformance.Label(conformance.SurfaceSSR, "title.home", "de"); !errors.Is(err, conformance.ErrLocaleUnknown) {
		t.Fatalf("Label(de) = %v, want ErrLocaleUnknown", err)
	}
}

func TestTodo_ALIGN_063_Integration(t *testing.T) {
	// Localized strings are real translations, not copies of English: no
	// locale silently falls back.
	english, err := conformance.Label(conformance.SurfaceSSR, "error.unauthorized", conformance.LocaleEnglish)
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []conformance.Locale{conformance.LocaleSpanish, conformance.LocaleFrench} {
		localized, err := conformance.Label(conformance.SurfaceSSR, "error.unauthorized", locale)
		if err != nil {
			t.Fatalf("Label(%s): %v", locale, err)
		}
		if localized.Text == english.Text || localized.Announcement == english.Announcement {
			t.Fatalf("locale %s fell back to English: %+v", locale, localized)
		}
	}
}

func TestTodo_ALIGN_063_Fault(t *testing.T) {
	// Empty keys and locales are refused before any lookup.
	if _, err := conformance.Label(conformance.SurfaceSSR, "", conformance.LocaleEnglish); !errors.Is(err, conformance.ErrMessageInvalid) {
		t.Fatalf("Label(empty key) = %v, want ErrMessageInvalid", err)
	}
	if _, err := conformance.Label(conformance.SurfaceSSR, "title.home", ""); !errors.Is(err, conformance.ErrLocaleUnknown) {
		t.Fatalf("Label(empty locale) = %v, want ErrLocaleUnknown", err)
	}
}

func TestTodo_ALIGN_063_Conformance(t *testing.T) {
	// Every qualified surface — including the enhanced browser — renders
	// every key with text and announcement.
	surfaces := []conformance.Surface{
		conformance.SurfaceSSR, conformance.SurfaceBrowser,
		conformance.SurfaceEnhancedBrowser, conformance.SurfaceRPC,
		conformance.SurfaceExport,
	}
	keys := []string{"title.home", "action.refresh", "error.unauthorized", "error.stale"}
	locales := []conformance.Locale{conformance.LocaleEnglish, conformance.LocaleSpanish, conformance.LocaleFrench}
	for _, surface := range surfaces {
		for _, key := range keys {
			for _, locale := range locales {
				message, err := conformance.Label(surface, key, locale)
				if err != nil {
					t.Fatalf("Label(%s, %q, %s): %v", surface, key, locale, err)
				}
				if message.Text == "" || message.Announcement == "" {
					t.Fatalf("Label(%s, %q, %s) is incomplete: %+v", surface, key, locale, message)
				}
			}
		}
	}
}

func FuzzTodo_ALIGN_063_Fuzz(f *testing.F) {
	f.Add(uint8(5), uint8(0), uint8(0))
	f.Fuzz(func(t *testing.T, surfaceByte, keyByte, localeByte uint8) {
		surfaces := []conformance.Surface{
			conformance.SurfaceSSR, conformance.SurfaceBrowser,
			conformance.SurfaceEnhancedBrowser, conformance.SurfaceRPC,
			conformance.SurfaceExport, 99,
		}
		keys := []string{"title.home", "action.refresh", "error.unauthorized", "error.stale", "ghost.key"}
		locales := []conformance.Locale{
			conformance.LocaleEnglish, conformance.LocaleSpanish,
			conformance.LocaleFrench, "de",
		}
		surface := surfaces[surfaceByte%uint8(len(surfaces))]
		first, firstErr := conformance.Label(surface, keys[keyByte%uint8(len(keys))], locales[localeByte%uint8(len(locales))])
		second, secondErr := conformance.Label(surface, keys[keyByte%uint8(len(keys))], locales[localeByte%uint8(len(locales))])
		if (firstErr == nil) != (secondErr == nil) || first != second {
			t.Fatalf("label lookup is not deterministic")
		}
	})
}
