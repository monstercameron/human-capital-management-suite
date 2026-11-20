package chat

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Issue 3932. Private labels on saved items. A label is a short name the owner
// gives to their own saved messages; it lives on the saved item, so it is
// scoped, erased and hidden exactly like the item's note. It is never posted,
// never in a notification and never visible to anyone else.

var ErrSavedLabelLimit = errors.New("chat: saved label limit reached")

const (
	SavedLabelMaxRunes   = 40
	SavedLabelsPerItem   = 10
	SavedLabelsPerPerson = 50
)

// NormalizeSavedLabel trims a label, folds runs of white space into one space
// and says whether the result is a usable label: 1 to 40 characters, no
// control characters and no comma (a comma separates labels when typed).
func NormalizeSavedLabel(label string) (string, bool) {
	if !utf8.ValidString(label) {
		return "", false
	}
	label = strings.Join(strings.Fields(label), " ")
	n := utf8.RuneCountInString(label)
	if n == 0 || n > SavedLabelMaxRunes {
		return "", false
	}
	for _, r := range label {
		if unicode.IsControl(r) || r == ',' {
			return "", false
		}
	}
	return label, true
}

// SavedLabelKey is the case-insensitive identity of a label.
func SavedLabelKey(label string) string { return strings.ToLower(label) }

func normalizeSavedLabels(labels []string) ([]string, error) {
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		normal, ok := NormalizeSavedLabel(label)
		if !ok {
			return nil, ErrInvalidArgument
		}
		out = append(out, normal)
	}
	return out, nil
}

// ApplySavedLabels returns the labels an item holds after removals and then
// additions, keeping the first spelling and the order of first appearance. Two
// labels that differ only in letter case are one label.
func ApplySavedLabels(current, add, remove []string) ([]string, error) {
	add, err := normalizeSavedLabels(add)
	if err != nil {
		return nil, err
	}
	remove, err = normalizeSavedLabels(remove)
	if err != nil {
		return nil, err
	}
	dropped := map[string]bool{}
	for _, label := range remove {
		dropped[SavedLabelKey(label)] = true
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(current)+len(add))
	for _, label := range append(append([]string{}, current...), add...) {
		key := SavedLabelKey(label)
		if seen[key] || (dropped[key] && !containsLabel(add, label)) {
			continue
		}
		seen[key] = true
		out = append(out, label)
	}
	if len(out) > SavedLabelsPerItem {
		return nil, ErrSavedLabelLimit
	}
	return out, nil
}

func containsLabel(labels []string, label string) bool {
	for _, l := range labels {
		if SavedLabelKey(l) == SavedLabelKey(label) {
			return true
		}
	}
	return false
}

// SavedLabelStore is the optional label-wide part of a SavedStore: renaming or
// removing one label across all of the caller's own saved items. Neither ever
// deletes a saved item.
type SavedLabelStore interface {
	RenameSavedLabel(ctx context.Context, p Principal, from, to string) (int, error)
	RemoveSavedLabel(ctx context.Context, p Principal, label string) (int, error)
}

func (s *Service) savedLabelStore(ctx context.Context, p Principal, tenant string) (SavedLabelStore, error) {
	if _, err := s.savedStore(ctx, p, tenant); err != nil {
		return nil, err
	}
	store, ok := s.store.(SavedLabelStore)
	if !ok {
		return nil, ErrUnavailable
	}
	return store, nil
}

// RenameSavedLabel renames one of the caller's labels on every saved item that
// carries it, and returns how many items changed. Renaming to a label an item
// already has merges the two.
func (s *Service) RenameSavedLabel(ctx context.Context, p Principal, tenant, from, to string) (int, error) {
	store, err := s.savedLabelStore(ctx, p, tenant)
	if err != nil {
		return 0, err
	}
	from, okFrom := NormalizeSavedLabel(from)
	to, okTo := NormalizeSavedLabel(to)
	if !okFrom || !okTo {
		return 0, ErrInvalidArgument
	}
	return store.RenameSavedLabel(ctx, p, from, to)
}

// RemoveSavedLabel takes one of the caller's labels off every saved item that
// carries it. The saved messages stay saved.
func (s *Service) RemoveSavedLabel(ctx context.Context, p Principal, tenant, label string) (int, error) {
	store, err := s.savedLabelStore(ctx, p, tenant)
	if err != nil {
		return 0, err
	}
	label, ok := NormalizeSavedLabel(label)
	if !ok {
		return 0, ErrInvalidArgument
	}
	return store.RemoveSavedLabel(ctx, p, label)
}
