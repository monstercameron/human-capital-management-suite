package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ExpressionLibraryVersion pins the audited built-in function set. Changes to
// function signatures, semantics or cost schedules require a new version.
const ExpressionLibraryVersion = "1"
const MaxPinnedCalendarDates = 256

// FunctionSignature is the public typed contract published in compiled IR.
// ExpressionTypeUnknown in Arguments denotes a same-as-list-element argument.
type FunctionSignature struct {
	Arguments   []ExpressionType
	Result      ExpressionType
	ListElement int // -1 when the signature has no list argument
}

// FunctionEntry declares a pure evaluator operation and its resource charge.
// Cost is the base charge; PerElementCost applies to values scanned by it.
type FunctionEntry struct {
	Name           string
	Version        string
	Signature      FunctionSignature
	Cost           int
	PerElementCost int
	operation      string
}

// ExpressionFunctionLibrary is a deterministic immutable snapshot of the
// audited functions available to an expression. Entries are returned as
// defensive copies so a caller cannot mutate the compiler's registry.
type ExpressionFunctionLibrary struct {
	version string
	entries map[string]FunctionEntry
	digest  string
}

func builtinExpressionLibrary() ExpressionFunctionLibrary {
	entries := []FunctionEntry{
		{Name: "lower", Version: "1", Signature: FunctionSignature{Arguments: []ExpressionType{ExpressionTypeString}, Result: ExpressionTypeString, ListElement: -1}, Cost: 1, PerElementCost: 1, operation: "lower"},
		{Name: "upper", Version: "1", Signature: FunctionSignature{Arguments: []ExpressionType{ExpressionTypeString}, Result: ExpressionTypeString, ListElement: -1}, Cost: 1, PerElementCost: 1, operation: "upper"},
		{Name: "contains", Version: "1", Signature: FunctionSignature{Arguments: []ExpressionType{ExpressionTypeString, ExpressionTypeString}, Result: ExpressionTypeBool, ListElement: -1}, Cost: 1, PerElementCost: 1, operation: "contains"},
		{Name: "starts_with", Version: "1", Signature: FunctionSignature{Arguments: []ExpressionType{ExpressionTypeString, ExpressionTypeString}, Result: ExpressionTypeBool, ListElement: -1}, Cost: 1, PerElementCost: 1, operation: "starts_with"},
		{Name: "ends_with", Version: "1", Signature: FunctionSignature{Arguments: []ExpressionType{ExpressionTypeString, ExpressionTypeString}, Result: ExpressionTypeBool, ListElement: -1}, Cost: 1, PerElementCost: 1, operation: "ends_with"},
		{Name: "abs", Version: "1", Signature: FunctionSignature{Arguments: []ExpressionType{ExpressionTypeUnknown}, Result: ExpressionTypeUnknown, ListElement: -1}, Cost: 1, operation: "abs"},
		{Name: "len", Version: "1", Signature: FunctionSignature{Arguments: []ExpressionType{ExpressionTypeUnknown}, Result: ExpressionTypeInt, ListElement: -1}, Cost: 1, operation: "len"},
		{Name: "list_contains", Version: "1", Signature: FunctionSignature{Arguments: []ExpressionType{ExpressionTypeList, ExpressionTypeUnknown}, Result: ExpressionTypeBool, ListElement: 0}, Cost: 1, PerElementCost: 1, operation: "list_contains"},
		{Name: "interval_overlaps", Version: "1", Signature: FunctionSignature{Arguments: []ExpressionType{ExpressionTypeDate, ExpressionTypeDate, ExpressionTypeDate, ExpressionTypeDate}, Result: ExpressionTypeBool, ListElement: -1}, Cost: 2, operation: "interval_overlaps"},
		{Name: "business_day_diff", Version: "1", Signature: FunctionSignature{Arguments: []ExpressionType{ExpressionTypeDate, ExpressionTypeDate, ExpressionTypeString, ExpressionTypeString, ExpressionTypeString, ExpressionTypeList}, Result: ExpressionTypeInt, ListElement: 5}, Cost: 2, PerElementCost: 1, operation: "business_day_diff"},
	}
	byName := make(map[string]FunctionEntry, len(entries))
	for _, entry := range entries {
		entry.Signature.Arguments = append([]ExpressionType(nil), entry.Signature.Arguments...)
		byName[entry.Name] = entry
	}
	lib := ExpressionFunctionLibrary{version: ExpressionLibraryVersion, entries: byName}
	lib.digest, _ = lib.computeDigest()
	return lib
}

// DefaultExpressionFunctionLibrary returns the version 1 published library.
func DefaultExpressionFunctionLibrary() ExpressionFunctionLibrary { return builtinExpressionLibrary() }

// Version and Digest identify the complete registry snapshot.
func (l ExpressionFunctionLibrary) Version() string { return l.version }
func (l ExpressionFunctionLibrary) Digest() string  { return l.digest }

// Entries returns all registry declarations ordered by function name.
func (l ExpressionFunctionLibrary) Entries() []FunctionEntry {
	names := make([]string, 0, len(l.entries))
	for name := range l.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]FunctionEntry, 0, len(names))
	for _, name := range names {
		entry := l.entries[name]
		entry.Signature.Arguments = append([]ExpressionType(nil), entry.Signature.Arguments...)
		out = append(out, entry)
	}
	return out
}

func (l ExpressionFunctionLibrary) entry(name string) (FunctionEntry, bool) {
	e, ok := l.entries[strings.ToLower(name)]
	return e, ok
}

func (l ExpressionFunctionLibrary) computeDigest() (string, error) {
	w := canonicalbytes.New("hcmnext.engines.rules.ExpressionFunctionLibrary", 1).String("version", l.version)
	entries := l.Entries()
	w.Count("entries", len(entries))
	for _, e := range entries {
		w.String("name", e.Name).String("entry.version", e.Version).String("result", e.Signature.Result.String()).Int("cost", int64(e.Cost)).Int("per_element_cost", int64(e.PerElementCost)).Int("list_element", int64(e.Signature.ListElement)).Count("arguments", len(e.Signature.Arguments))
		for _, arg := range e.Signature.Arguments {
			w.String("argument", arg.String())
		}
	}
	return w.Digest()
}

func validateLibrary(l ExpressionFunctionLibrary) error {
	if l.version == "" || len(l.entries) == 0 {
		return fmt.Errorf("%w: empty function library", ErrExpressionInvalid)
	}
	digest, err := l.computeDigest()
	if err != nil {
		return err
	}
	if digest != l.digest {
		return fmt.Errorf("%w: function library digest mismatch", ErrExpressionInvalid)
	}
	return nil
}

// BusinessCalendarSnapshotDigest identifies the exact set of working dates
// consumed by business_day_diff. Dates are canonicalized and sorted, so the
// digest is independent of caller slice order. Duplicate dates are rejected.
func BusinessCalendarSnapshotDigest(ref, version string, workingDates []string) (string, error) {
	if ref == "" || version == "" || len(ref) > 256 || len(version) > 128 || strings.TrimSpace(ref) != ref || strings.TrimSpace(version) != version {
		return "", fmt.Errorf("%w: invalid calendar identity", ErrExpressionDependency)
	}
	if len(workingDates) > MaxPinnedCalendarDates {
		return "", fmt.Errorf("%w: calendar snapshot has %d dates, limit %d", ErrExpressionUnbounded, len(workingDates), MaxPinnedCalendarDates)
	}
	dates := append([]string(nil), workingDates...)
	for i, date := range dates {
		parsed, err := values.ParseLocalDate(date)
		if err != nil || parsed.String() != date {
			return "", fmt.Errorf("%w: invalid working date at index %d", ErrExpressionValue, i)
		}
	}
	sort.Strings(dates)
	for i := 1; i < len(dates); i++ {
		if dates[i] == dates[i-1] {
			return "", fmt.Errorf("%w: duplicate working date %s", ErrExpressionValue, dates[i])
		}
	}
	w := canonicalbytes.New("hcmnext.engines.rules.BusinessCalendarSnapshot", 1).String("ref", ref).String("version", version).Count("working_dates", len(dates))
	for _, date := range dates {
		w.String("working_date", date)
	}
	return w.Digest()
}
