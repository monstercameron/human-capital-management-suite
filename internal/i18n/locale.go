// Package i18n owns locale selection and governed translation catalogs.
package i18n

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidLocale     = errors.New("i18n: invalid locale")
	ErrUnsupportedLocale = errors.New("i18n: unsupported locale")
	ErrFallbackCycle     = errors.New("i18n: fallback cycle")
)

// LocaleContext is an immutable locale policy. Fallbacks are directed edges
// from a locale to its ordered fallback candidates.
type LocaleContext struct {
	Locale    string
	Supported []string
	Fallbacks map[string][]string
}

// NewLocaleContext validates and copies a locale policy. Locale identifiers
// are canonicalized using BCP-47 language tags.
func NewLocaleContext(locale string, supported []string, fallbacks map[string][]string) (LocaleContext, error) {
	primary, err := parseLocale(locale)
	if err != nil {
		return LocaleContext{}, err
	}
	if len(supported) == 0 {
		return LocaleContext{}, fmt.Errorf("%w: supported locales are required", ErrInvalidLocale)
	}
	set := make(map[string]struct{}, len(supported))
	ordered := make([]string, 0, len(supported))
	for _, raw := range supported {
		tag, e := parseLocale(raw)
		if e != nil {
			return LocaleContext{}, e
		}
		name := tag.String()
		if _, ok := set[name]; !ok {
			set[name] = struct{}{}
			ordered = append(ordered, name)
		}
	}
	if _, ok := set[primary.String()]; !ok {
		return LocaleContext{}, fmt.Errorf("%w: %s", ErrUnsupportedLocale, primary)
	}
	copyFallbacks := make(map[string][]string, len(fallbacks))
	for raw, candidates := range fallbacks {
		from, e := parseLocale(raw)
		if e != nil {
			return LocaleContext{}, e
		}
		fromName := from.String()
		if _, ok := set[fromName]; !ok {
			return LocaleContext{}, fmt.Errorf("%w: fallback source %s", ErrUnsupportedLocale, fromName)
		}
		seen := map[string]struct{}{}
		for _, candidate := range candidates {
			to, e := parseLocale(candidate)
			if e != nil {
				return LocaleContext{}, e
			}
			toName := to.String()
			if _, ok := set[toName]; !ok {
				return LocaleContext{}, fmt.Errorf("%w: fallback %s", ErrUnsupportedLocale, toName)
			}
			if _, duplicate := seen[toName]; duplicate {
				return LocaleContext{}, fmt.Errorf("%w: duplicate fallback %s", ErrInvalidLocale, toName)
			}
			seen[toName] = struct{}{}
			copyFallbacks[fromName] = append(copyFallbacks[fromName], toName)
		}
	}
	if hasCycle(copyFallbacks) {
		return LocaleContext{}, ErrFallbackCycle
	}
	return LocaleContext{Locale: primary.String(), Supported: ordered, Fallbacks: copyFallbacks}, nil
}

func parseLocale(raw string) (values.LanguageTag, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("%w: empty locale", ErrInvalidLocale)
	}
	tag, ok := values.ParseLanguageTag(strings.TrimSpace(raw))
	if !ok || tag == "und" {
		return "", fmt.Errorf("%w: %q", ErrInvalidLocale, raw)
	}
	return tag, nil
}

func hasCycle(edges map[string][]string) bool {
	state := map[string]uint8{}
	var visit func(string) bool
	visit = func(node string) bool {
		if state[node] == 1 {
			return true
		}
		if state[node] == 2 {
			return false
		}
		state[node] = 1
		for _, next := range edges[node] {
			if visit(next) {
				return true
			}
		}
		state[node] = 2
		return false
	}
	for node := range edges {
		if visit(node) {
			return true
		}
	}
	return false
}

// FallbackPath returns a deterministic depth-first path beginning at the
// requested locale and ending at each reachable fallback once.
func (c LocaleContext) FallbackPath(locale string) ([]string, error) {
	tag, err := parseLocale(locale)
	if err != nil {
		return nil, err
	}
	name := tag.String()
	found := false
	for _, s := range c.Supported {
		if s == name {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedLocale, name)
	}
	seen := map[string]bool{}
	path := []string{}
	var walk func(string)
	walk = func(n string) {
		if seen[n] {
			return
		}
		seen[n] = true
		path = append(path, n)
		for _, next := range c.Fallbacks[n] {
			walk(next)
		}
	}
	walk(name)
	return path, nil
}
