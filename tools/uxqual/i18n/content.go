// Package i18n contains qualification contracts for localized experience copy.
package i18n

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	ErrInvalidMessage   = errors.New("i18n: invalid semantic message")
	ErrLocaleMismatch   = errors.New("i18n: locale catalog mismatch")
	ErrMissingParameter = errors.New("i18n: missing message parameter")
)

// MessageKind describes the job a message performs, rather than its widget.
type MessageKind string

const (
	Heading  MessageKind = "heading"
	Button   MessageKind = "button"
	Helper   MessageKind = "helper"
	Status   MessageKind = "status"
	Empty    MessageKind = "empty"
	Error    MessageKind = "error"
	Success  MessageKind = "success"
	Refusal  MessageKind = "refusal"
	Recovery MessageKind = "recovery"
)

// Message is a locale-owned semantic message. Params are named placeholders
// written as {name} in Text, matching the production localize.Message format.
type Message struct {
	Key, MeaningID, Text string
	Kind                 MessageKind
	Params               []Parameter
	NextAction           string
}

// Parameter gives a placeholder a display contract. Formatting of canonical
// dates, counts, and money remains the locale adapter's responsibility.
type Parameter struct {
	Name   string
	Format string // text, count, money, or date
}

// LocaleInput is the adapter boundary for production catalog snapshots. An
// adapter maps its locale registry to this shape without importing this
// qualification package into the renderer.
type LocaleInput struct {
	Locale   string
	Messages []Message
}

// CatalogFromInputs converts locale snapshots into the validator's catalog.
func CatalogFromInputs(inputs ...LocaleInput) Catalog {
	out := make(Catalog, len(inputs))
	for _, input := range inputs {
		out[input.Locale] = append([]Message(nil), input.Messages...)
	}
	return out
}

// Catalog is a set of locale catalogs that must describe the same meanings.
type Catalog map[string][]Message

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)+$`)
var placeholderPattern = regexp.MustCompile(`\{([a-z][a-z0-9_]*)\}`)
var parameterNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var implementationVocabulary = []string{
	"journeyservice", "canonical grpc", "worker projection", "authenticated cell",
	"server-enforced boundary", "tenant appearance", "credential role fallback",
	"live cell", "listworkers", "grpc", "protobuf", "rpc adapter", "catalog revision",
}

// Validate applies the product-voice contract and checks every locale against
// the first (sorted) locale's semantic key and parameter shape.
func (c Catalog) Validate() error {
	if len(c) == 0 {
		return fmt.Errorf("%w: catalog is empty", ErrLocaleMismatch)
	}
	locales := make([]string, 0, len(c))
	for locale := range c {
		if strings.TrimSpace(locale) == "" {
			return fmt.Errorf("%w: locale is empty", ErrLocaleMismatch)
		}
		locales = append(locales, locale)
	}
	sort.Strings(locales)
	var baseline map[string]Message
	for _, locale := range locales {
		messages := make(map[string]Message, len(c[locale]))
		for _, m := range c[locale] {
			if err := m.Validate(); err != nil {
				return fmt.Errorf("%s: %w", locale, err)
			}
			if _, exists := messages[m.Key]; exists {
				return fmt.Errorf("%w: duplicate key %q", ErrInvalidMessage, m.Key)
			}
			messages[m.Key] = m
		}
		if baseline == nil {
			baseline = messages
			continue
		}
		if len(messages) != len(baseline) {
			return fmt.Errorf("%w: %s has a different key set", ErrLocaleMismatch, locale)
		}
		for key, want := range baseline {
			got, ok := messages[key]
			if !ok || got.MeaningID != want.MeaningID || !sameParams(got.Params, want.Params) || got.Kind != want.Kind {
				return fmt.Errorf("%w: key %q differs in %s", ErrLocaleMismatch, key, locale)
			}
		}
	}
	return nil
}

func (m Message) Validate() error {
	if !keyPattern.MatchString(m.Key) || m.MeaningID == "" || strings.TrimSpace(m.Text) == "" || m.Kind == "" {
		return fmt.Errorf("%w: key, meaning, kind and text are required", ErrInvalidMessage)
	}
	switch m.Kind {
	case Heading, Button, Helper, Status, Empty, Error, Success, Refusal, Recovery:
	default:
		return fmt.Errorf("%w: unsupported kind %q", ErrInvalidMessage, m.Kind)
	}
	if m.Kind == Button && (strings.EqualFold(strings.TrimSpace(m.Text), "continue") || strings.TrimSpace(m.NextAction) == "") {
		return fmt.Errorf("%w: buttons must state an outcome", ErrInvalidMessage)
	}
	lowerText := strings.ToLower(m.Text)
	for _, vocabulary := range implementationVocabulary {
		if strings.Contains(lowerText, vocabulary) {
			return fmt.Errorf("%w: implementation vocabulary is user-visible", ErrInvalidMessage)
		}
	}
	if m.Kind == Button {
		switch strings.ToLower(strings.TrimSpace(m.Text)) {
		case "continue", "next", "submit", "save", "done", "confirm", "ok", "okay":
			return fmt.Errorf("%w: button must state a specific outcome", ErrInvalidMessage)
		}
	}
	if strings.HasSuffix(strings.TrimSpace(m.Text), ".") && m.Kind == Button {
		return fmt.Errorf("%w: button text is not a sentence", ErrInvalidMessage)
	}
	declared := make([]string, 0, len(m.Params))
	for _, p := range m.Params {
		if !parameterNamePattern.MatchString(p.Name) || (p.Format != "text" && p.Format != "count" && p.Format != "money" && p.Format != "date") {
			return fmt.Errorf("%w: invalid parameter %q", ErrInvalidMessage, p.Name)
		}
		declared = append(declared, p.Name)
	}
	sort.Strings(declared)
	if len(unique(declared)) != len(declared) {
		return fmt.Errorf("%w: duplicate parameter", ErrInvalidMessage)
	}
	found := placeholderPattern.FindAllStringSubmatch(m.Text, -1)
	actual := make([]string, 0, len(found))
	for _, match := range found {
		actual = append(actual, match[1])
	}
	sort.Strings(actual)
	if !sameStrings(unique(declared), unique(actual)) {
		return fmt.Errorf("%w: declared placeholders do not match text", ErrInvalidMessage)
	}
	if m.Kind == Error || m.Kind == Refusal || m.Kind == Recovery {
		if strings.TrimSpace(m.NextAction) == "" {
			return fmt.Errorf("%w: message needs a next action", ErrInvalidMessage)
		}
	}
	return nil
}

// Render substitutes named parameters after validating the semantic shape.
func (c Catalog) Render(locale, key string, params map[string]string) (string, error) {
	for _, m := range c[locale] {
		if m.Key != key {
			continue
		}
		if err := m.Validate(); err != nil {
			return "", err
		}
		if len(params) != len(m.Params) {
			return "", fmt.Errorf("%w: parameter shape for %s", ErrMissingParameter, key)
		}
		for _, p := range m.Params {
			if _, ok := params[p.Name]; !ok {
				return "", fmt.Errorf("%w: %s", ErrMissingParameter, p.Name)
			}
		}
		// Replace matches in the original template only. Sequential replacement
		// would interpret placeholder-looking parameter values as new syntax.
		out := placeholderPattern.ReplaceAllStringFunc(m.Text, func(placeholder string) string {
			name := strings.TrimSuffix(strings.TrimPrefix(placeholder, "{"), "}")
			return params[name]
		})
		return out, nil
	}
	return "", fmt.Errorf("%w: key %q", ErrMissingParameter, key)
}

func unique(in []string) []string {
	out := in[:0]
	for _, v := range in {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameParams(a, b []Parameter) bool {
	if len(a) != len(b) {
		return false
	}
	left := append([]Parameter(nil), a...)
	right := append([]Parameter(nil), b...)
	sort.Slice(left, func(i, j int) bool { return left[i].Name < left[j].Name })
	sort.Slice(right, func(i, j int) bool { return right[i].Name < right[j].Name })
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
