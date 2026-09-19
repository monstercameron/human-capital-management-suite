package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ALIGN-063: accessibility and localization qualify across the slice.
// Every qualified surface renders the same message keys; every key ships
// in every supported locale; every message carries a screen-reader
// announcement alongside its visible text. A missing key, a missing
// locale, an empty announcement, or an unqualified surface fails the
// slice — a product that cannot speak to every user in every locale does
// not release.

// Locale is a supported content locale. The set is closed.
type Locale string

// Supported locales.
const (
	LocaleEnglish Locale = "en"
	LocaleSpanish Locale = "es"
	LocaleFrench  Locale = "fr"
)

// Message is one localizable surface string with its announcement.
type Message struct {
	Text         string `json:"text"`
	Announcement string `json:"announcement"`
}

// Message errors.
var (
	ErrMessageInvalid = errors.New("transport conformance: slice message is invalid")
	ErrLocaleUnknown  = errors.New("transport conformance: locale is not supported")
	ErrSurfaceUnknown = errors.New("transport conformance: surface is not qualified")
)

// surfaceName spells a surface for qualification diagnostics.
func surfaceName(s Surface) string {
	if s == SurfaceEnhancedBrowser {
		return "ENHANCED_BROWSER"
	}
	return s.String()
}

// Required message keys. Every qualified surface renders all of them.
var messageKeys = []string{"title.home", "action.refresh", "error.unauthorized", "error.stale"}

// catalog holds the qualified strings: surface -> key -> locale. It is
// built once by buildCatalog and never mutated afterwards: Label and the
// digest only read it, so no caller inherits shared mutable state.
var catalog = buildCatalog()

func messageKeySet() []string {
	return append([]string(nil), messageKeys...)
}

func supportedLocales() []Locale {
	return []Locale{LocaleEnglish, LocaleSpanish, LocaleFrench}
}

// buildCatalog assembles the qualified string table. It is pure and
// deterministic: every surface carries the same keys, and iteration order
// cannot change the result because each write lands on a distinct key.
func buildCatalog() map[Surface]map[string]map[Locale]Message {
	texts := map[string]map[Locale][2]string{
		"title.home": {
			LocaleEnglish: {"Home", "Home page"},
			LocaleSpanish: {"Inicio", "Página de inicio"},
			LocaleFrench:  {"Accueil", "Page d'accueil"},
		},
		"action.refresh": {
			LocaleEnglish: {"Refresh", "Refresh the data"},
			LocaleSpanish: {"Actualizar", "Actualizar los datos"},
			LocaleFrench:  {"Actualiser", "Actualiser les données"},
		},
		"error.unauthorized": {
			LocaleEnglish: {"Access denied", "Access denied. Request access or return home."},
			LocaleSpanish: {"Acceso denegado", "Acceso denegado. Solicite acceso o vuelva al inicio."},
			LocaleFrench:  {"Accès refusé", "Accès refusé. Demandez l'accès ou revenez à l'accueil."},
		},
		"error.stale": {
			LocaleEnglish: {"Out of date", "Data is out of date. Refresh for the latest version."},
			LocaleSpanish: {"Desactualizado", "Los datos están desactualizados. Actualice para ver la versión más reciente."},
			LocaleFrench:  {"Données périmées", "Les données sont périmées. Actualisez pour la dernière version."},
		},
	}
	catalog := make(map[Surface]map[string]map[Locale]Message, 5)
	for _, surface := range []Surface{SurfaceSSR, SurfaceBrowser, SurfaceEnhancedBrowser, SurfaceRPC, SurfaceExport} {
		keys := make(map[string]map[Locale]Message, len(messageKeys))
		for _, key := range messageKeys {
			localized := make(map[Locale]Message, 3)
			for locale, pair := range texts[key] {
				localized[locale] = Message{Text: pair[0], Announcement: pair[1]}
			}
			keys[key] = localized
		}
		catalog[surface] = keys
	}
	return catalog
}

// Label returns the message for a surface, key, and locale. Unqualified
// surfaces, unknown keys, and unsupported locales are refused; no
// fallback silently substitutes another language.
func Label(surface Surface, key string, locale Locale) (Message, error) {
	keys, ok := catalog[surface]
	if !ok {
		return Message{}, fmt.Errorf("%w: %d", ErrSurfaceUnknown, surface)
	}
	localized, ok := keys[key]
	if !ok {
		return Message{}, fmt.Errorf("%w: %q", ErrMessageInvalid, key)
	}
	message, ok := localized[locale]
	if !ok {
		return Message{}, fmt.Errorf("%w: %q", ErrLocaleUnknown, locale)
	}
	if message.Text == "" || message.Announcement == "" {
		return Message{}, fmt.Errorf("%w: %q is incomplete", ErrMessageInvalid, key)
	}
	return message, nil
}

// QualifySlice proves the whole slice qualifies: every qualified surface
// renders every required key in every supported locale, each with visible
// text and a screen-reader announcement, and no message leaks a template
// placeholder.
func QualifySlice() error {
	for _, surface := range qualifiedSurfaces() {
		for _, key := range messageKeySet() {
			for _, locale := range supportedLocales() {
				message, err := Label(surface, key, locale)
				if err != nil {
					return fmt.Errorf("surface %s key %q locale %s: %w", surfaceName(surface), key, locale, err)
				}
				if strings.Contains(message.Text, "{") || strings.Contains(message.Announcement, "{") {
					return fmt.Errorf("surface %s key %q locale %s carries an unresolved placeholder", surfaceName(surface), key, locale)
				}
			}
		}
	}
	return nil
}

// CatalogDigest identifies the qualified string set.
func CatalogDigest() string {
	surfaces := qualifiedSurfaces()
	sort.Slice(surfaces, func(i, j int) bool { return surfaces[i] < surfaces[j] })
	h := sha256.New()
	for _, surface := range surfaces {
		for _, key := range messageKeySet() {
			for _, locale := range supportedLocales() {
				message, err := Label(surface, key, locale)
				if err != nil {
					return ""
				}
				fmt.Fprintf(h, "%d|%s|%s|%s|%s;", surface, key, locale, message.Text, message.Announcement)
			}
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
