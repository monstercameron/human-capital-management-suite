// Package localize provides deterministic, revisioned localization primitives.
// It deliberately accepts decimal values as strings: converting money through
// binary floating point is not a presentation detail, it is data loss.
package localize

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidContext = errors.New("localize: invalid locale context")
	ErrMissingMessage = errors.New("localize: missing translation")
	ErrInvalidDecimal = errors.New("localize: invalid decimal")
)

type Direction string

const (
	LTR Direction = "ltr"
	RTL Direction = "rtl"
)

// Context is immutable caller configuration. Version identifies the exact
// catalog and format profile used for a result.
type Context struct{ Locale, TimeZone, Calendar, CatalogVersion string }
type LocaleContext = Context

type Message struct {
	Text   string
	Plural map[string]string
	Gender map[string]string
}
type Catalog struct {
	Locale, Version string
	Messages        map[string]Message
}
type CatalogRevision = Catalog

type Registry struct {
	mu       sync.RWMutex
	catalogs map[string]Catalog
}

func NewRegistry() *Registry { return &Registry{catalogs: make(map[string]Catalog)} }
func (r *Registry) Register(c Catalog) error {
	if r == nil || strings.TrimSpace(c.Locale) == "" || strings.TrimSpace(c.Version) == "" {
		return ErrInvalidContext
	}
	m := make(map[string]Message, len(c.Messages))
	for k, v := range c.Messages {
		v.Plural = clone(v.Plural)
		v.Gender = clone(v.Gender)
		m[k] = v
	}
	c.Locale = canonicalLocale(c.Locale)
	c.Messages = m
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.catalogs == nil {
		r.catalogs = make(map[string]Catalog)
	}
	r.catalogs[c.Locale+"\x00"+c.Version] = c
	return nil
}
func (r *Registry) Catalog(locale, version string) (Catalog, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.catalogs[canonicalLocale(locale)+"\x00"+version]
	if !ok {
		return Catalog{}, false
	}
	c.Messages = cloneMessages(c.Messages)
	return c, true
}

type ResolveOptions struct {
	Plural   string
	Count    *big.Rat
	Gender   string
	Vars     map[string]string
	Fallback []string
}
type Result struct {
	Text, Key, Locale, CatalogVersion, FallbackPath, PluralCase, GenderCase, FormatProfile string
	Direction                                                                              Direction
}

func (r *Registry) Resolve(ctx Context, key string, o ResolveOptions) (Result, error) {
	ctx.Locale = canonicalLocale(ctx.Locale)
	if ctx.Calendar == "" {
		ctx.Calendar = "gregorian"
	}
	if ctx.TimeZone == "" {
		ctx.TimeZone = "UTC"
	}
	if ctx.Locale == "" || ctx.CatalogVersion == "" {
		return Result{}, ErrInvalidContext
	}
	locales := append([]string{ctx.Locale}, o.Fallback...)
	var msg Message
	var used, version string
	r.mu.RLock()
	for _, loc := range locales {
		// Resolve is read-only and can safely inspect the registry's immutable
		// registered catalog. Catalog() clones the complete message map for
		// callers that may mutate it; doing that for every rendered label made a
		// five-dimension status presentation allocate the whole product catalog
		// eleven times per render.
		if c, ok := r.catalogs[canonicalLocale(loc)+"\x00"+ctx.CatalogVersion]; ok {
			if x, yes := c.Messages[key]; yes {
				msg = x
				used = c.Locale
				version = c.Version
				break
			}
		}
	}
	r.mu.RUnlock()
	if used == "" {
		return Result{}, fmt.Errorf("%w: %s", ErrMissingMessage, key)
	}
	plural := o.Plural
	if plural == "" && o.Count != nil {
		plural = pluralCase(ctx.Locale, o.Count)
	}
	gender := o.Gender
	text := msg.Text
	if gender != "" && msg.Gender[gender] != "" {
		text = msg.Gender[gender]
	} else if plural != "" && msg.Plural[plural] != "" {
		text = msg.Plural[plural]
	}
	if text == "" {
		return Result{}, fmt.Errorf("%w: %s", ErrMissingMessage, key)
	}
	for k, v := range o.Vars {
		text = strings.ReplaceAll(text, "{"+k+"}", v)
	}
	path := used
	if used != ctx.Locale {
		path = ctx.Locale + " -> " + used
	}
	return Result{Text: text, Key: key, Locale: used, CatalogVersion: version, FallbackPath: path, PluralCase: plural, GenderCase: gender, FormatProfile: ctx.Locale + "/" + ctx.Calendar + "/" + ctx.TimeZone, Direction: direction(ctx.Locale)}, nil
}

func DirectionFor(locale string) Direction { return direction(canonicalLocale(locale)) }
func direction(l string) Direction {
	if strings.HasPrefix(l, "ar") || strings.HasPrefix(l, "he") || strings.HasPrefix(l, "fa") || strings.HasPrefix(l, "ur") {
		return RTL
	}
	return LTR
}
func pluralCase(locale string, n *big.Rat) string {
	if strings.HasPrefix(canonicalLocale(locale), "ar") && n.IsInt() && n.Sign() >= 0 {
		switch n.Cmp(big.NewRat(0, 1)) {
		case 0:
			return "zero"
		}
		if n.Cmp(big.NewRat(1, 1)) == 0 {
			return "one"
		}
		if n.Cmp(big.NewRat(2, 1)) == 0 {
			return "two"
		}
		remainder := new(big.Int).Mod(n.Num(), big.NewInt(100)).Int64()
		if remainder >= 3 && remainder <= 10 {
			return "few"
		}
		if remainder >= 11 && remainder <= 99 {
			return "many"
		}
		return "other"
	}
	if n.IsInt() && n.Cmp(big.NewRat(1, 1)) == 0 {
		return "one"
	}
	return "other"
}

func FormatName(locale, given, family string) string {
	if strings.HasPrefix(canonicalLocale(locale), "ja") || strings.HasPrefix(canonicalLocale(locale), "zh") {
		return strings.TrimSpace(family + " " + given)
	}
	return strings.TrimSpace(given + " " + family)
}

func FormatDate(ctx Context, t time.Time) (string, error) {
	loc, err := time.LoadLocation(ctx.TimeZone)
	if err != nil {
		return "", err
	}
	t = t.In(loc)
	if strings.EqualFold(ctx.Calendar, "buddhist") {
		return fmt.Sprintf("%02d/%02d/%04d", t.Day(), int(t.Month()), t.Year()+543), nil
	}
	if strings.HasPrefix(canonicalLocale(ctx.Locale), "en-US") {
		return fmt.Sprintf("%02d/%02d/%04d", t.Month(), t.Day(), t.Year()), nil
	}
	if strings.HasPrefix(canonicalLocale(ctx.Locale), "de") {
		return fmt.Sprintf("%02d.%02d.%04d", t.Day(), t.Month(), t.Year()), nil
	}
	if strings.HasPrefix(canonicalLocale(ctx.Locale), "ar") {
		months := [...]string{"", "يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو", "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر"}
		return arabicDigits(fmt.Sprintf("%d", t.Day())) + " " + months[t.Month()] + " " + arabicDigits(fmt.Sprintf("%04d", t.Year())), nil
	}
	return fmt.Sprintf("%04d-%02d-%02d", t.Year(), t.Month(), t.Day()), nil
}

func arabicDigits(value string) string {
	return strings.Map(func(digit rune) rune {
		if digit >= '0' && digit <= '9' {
			return '٠' + digit - '0'
		}
		return digit
	}, value)
}

func FormatNumber(locale, decimal string, fraction int) (string, error) {
	return formatDecimal(locale, decimal, fraction, false)
}
func FormatMoney(locale, decimal, currency string, fraction int) (string, error) {
	s, e := formatDecimal(locale, decimal, fraction, false)
	if e != nil {
		return "", e
	}
	l := canonicalLocale(locale)
	if strings.HasPrefix(l, "de") || strings.HasPrefix(l, "fr") {
		return s + " " + currency, nil
	}
	return currency + " " + s, nil
}
func formatDecimal(locale, in string, fraction int, _ bool) (string, error) {
	if fraction < 0 {
		return "", ErrInvalidDecimal
	}
	r, ok := new(big.Rat).SetString(strings.TrimSpace(in))
	if !ok {
		return "", ErrInvalidDecimal
	}
	neg := r.Sign() < 0
	if neg {
		r.Abs(r)
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(fraction)), nil)
	n := new(big.Int).Mul(r.Num(), scale)
	q := new(big.Int).Quo(n, r.Denom())
	rem := new(big.Int).Rem(n, r.Denom())
	if new(big.Int).Mul(rem, big.NewInt(2)).Cmp(r.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	raw := q.String()
	if fraction > 0 {
		for len(raw) <= fraction {
			raw = "0" + raw
		}
		raw = raw[:len(raw)-fraction] + "." + raw[len(raw)-fraction:]
	}
	parts := strings.Split(raw, ".")
	sep := ","
	dec := "."
	l := canonicalLocale(locale)
	if strings.HasPrefix(l, "de") || strings.HasPrefix(l, "fr") {
		sep = "."
		dec = ","
	} else if strings.HasPrefix(l, "ar") {
		sep = "٬"
		dec = "٫"
	}
	for i := len(parts[0]) - 3; i > 0; i -= 3 {
		parts[0] = parts[0][:i] + sep + parts[0][i:]
	}
	out := parts[0]
	if len(parts) > 1 {
		out += dec + parts[1]
	}
	if neg {
		out = "-" + out
	}
	if strings.HasPrefix(l, "ar") {
		out = arabicDigits(out)
	}
	return out, nil
}

// canonicalLocale is on the hot path: Resolve calls it for the context and
// for every fallback candidate of every rendered label. The underscore form
// is rare, so the scan-and-return path avoids the allocation and the
// strings.Count/Replace pair that showed up in client CPU profiles.
func canonicalLocale(s string) string {
	s = strings.TrimSpace(s)
	if strings.IndexByte(s, '_') < 0 {
		return s
	}
	return strings.ReplaceAll(s, "_", "-")
}
func clone(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	o := map[string]string{}
	for k, v := range m {
		o[k] = v
	}
	return o
}
func cloneMessages(m map[string]Message) map[string]Message {
	o := map[string]Message{}
	for k, v := range m {
		v.Plural = clone(v.Plural)
		v.Gender = clone(v.Gender)
		o[k] = v
	}
	return o
}
func (r *Registry) Versions(locale string) []string {
	var out []string
	for k, c := range r.catalogs {
		if strings.HasPrefix(k, canonicalLocale(locale)+"\x00") {
			out = append(out, c.Version)
		}
	}
	sort.Strings(out)
	return out
}
