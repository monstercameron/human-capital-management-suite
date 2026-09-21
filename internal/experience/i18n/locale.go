// Package i18n owns presentation locale selection and governed translations.
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

// LocaleContext contains presentation data only; it is not legal or
// jurisdictional authority. Values returned by the constructor are detached
// copies and should be treated as immutable.
type LocaleContext struct {
	Locale    string
	Supported []string
	Fallbacks map[string][]string
}

func NewLocaleContext(locale string, supported []string, fallbacks map[string][]string) (LocaleContext, error) {
	primary, err := parseLocale(locale)
	if err != nil {
		return LocaleContext{}, err
	}
	if len(supported) == 0 {
		return LocaleContext{}, fmt.Errorf("%w: supported locales are required", ErrInvalidLocale)
	}
	set := make(map[string]bool, len(supported))
	ordered := make([]string, 0, len(supported))
	for _, raw := range supported {
		t, e := parseLocale(raw)
		if e != nil {
			return LocaleContext{}, e
		}
		n := t.String()
		if !set[n] {
			set[n] = true
			ordered = append(ordered, n)
		}
	}
	if !set[primary.String()] {
		return LocaleContext{}, fmt.Errorf("%w: %s", ErrUnsupportedLocale, primary)
	}
	edges := make(map[string][]string, len(fallbacks))
	for raw, candidates := range fallbacks {
		from, e := parseLocale(raw)
		if e != nil {
			return LocaleContext{}, e
		}
		fn := from.String()
		if !set[fn] {
			return LocaleContext{}, fmt.Errorf("%w: fallback source %s", ErrUnsupportedLocale, fn)
		}
		seen := map[string]bool{}
		for _, rawTo := range candidates {
			to, e := parseLocale(rawTo)
			if e != nil {
				return LocaleContext{}, e
			}
			n := to.String()
			if !set[n] {
				return LocaleContext{}, fmt.Errorf("%w: fallback %s", ErrUnsupportedLocale, n)
			}
			if seen[n] {
				return LocaleContext{}, fmt.Errorf("%w: duplicate fallback %s", ErrInvalidLocale, n)
			}
			seen[n] = true
			edges[fn] = append(edges[fn], n)
		}
	}
	if hasCycle(edges) {
		return LocaleContext{}, ErrFallbackCycle
	}
	return LocaleContext{Locale: primary.String(), Supported: append([]string(nil), ordered...), Fallbacks: cloneEdges(edges)}, nil
}

func parseLocale(raw string) (values.LanguageTag, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("%w: empty locale", ErrInvalidLocale)
	}
	t, ok := values.ParseLanguageTag(strings.TrimSpace(raw))
	if !ok || t == "und" {
		return "", fmt.Errorf("%w: %q", ErrInvalidLocale, raw)
	}
	return t, nil
}
func cloneEdges(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = append([]string(nil), v...)
	}
	return out
}
func hasCycle(edges map[string][]string) bool {
	state := map[string]uint8{}
	var walk func(string) bool
	walk = func(n string) bool {
		if state[n] == 1 {
			return true
		}
		if state[n] == 2 {
			return false
		}
		state[n] = 1
		for _, next := range edges[n] {
			if walk(next) {
				return true
			}
		}
		state[n] = 2
		return false
	}
	for n := range edges {
		if walk(n) {
			return true
		}
	}
	return false
}

func (c LocaleContext) FallbackPath(locale string) ([]string, error) {
	t, e := parseLocale(locale)
	if e != nil {
		return nil, e
	}
	n := t.String()
	ok := false
	for _, s := range c.Supported {
		if s == n {
			ok = true
			break
		}
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedLocale, n)
	}
	seen := map[string]bool{}
	out := []string{}
	var walk func(string)
	walk = func(x string) {
		if seen[x] {
			return
		}
		seen[x] = true
		out = append(out, x)
		for _, next := range c.Fallbacks[x] {
			walk(next)
		}
	}
	walk(n)
	return out, nil
}
