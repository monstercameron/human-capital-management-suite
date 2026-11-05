package rules

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// Tenant input errors are stable categories callers may match with errors.Is.
var (
	ErrTenantPinInvalid      = errors.New("rules: tenant type pin is invalid")
	ErrTenantPath            = errors.New("rules: tenant path is invalid")
	ErrTenantTypeUnknown     = errors.New("rules: tenant type is not pinned")
	ErrTenantFieldUnknown    = errors.New("rules: tenant field is unknown")
	ErrTenantTypeMismatch    = errors.New("rules: tenant field type does not match the declared input type")
	ErrTenantClassification  = errors.New("rules: tenant field classification exceeds the declared maximum")
	ErrTenantVersionMismatch = errors.New("rules: tenant input version does not match the pinned version")
)

// TenantDataClassification is the closed lattice of tenant field
// sensitivity (WF-DATA-010): PUBLIC < INTERNAL < CONFIDENTIAL < RESTRICTED.
// It mirrors the workflow lattice, which this package cannot import
// (internal/workflow depends on rules); the parity test pins the two to the
// same order.
type TenantDataClassification string

// Tenant lattice members.
const (
	TenantPublic       TenantDataClassification = "PUBLIC"
	TenantInternal     TenantDataClassification = "INTERNAL"
	TenantConfidential TenantDataClassification = "CONFIDENTIAL"
	TenantRestricted   TenantDataClassification = "RESTRICTED"
)

// Valid reports whether c is a lattice member.
func (c TenantDataClassification) Valid() bool {
	switch c {
	case TenantPublic, TenantInternal, TenantConfidential, TenantRestricted:
		return true
	default:
		return false
	}
}

func (c TenantDataClassification) rank() int {
	switch c {
	case TenantPublic:
		return 0
	case TenantInternal:
		return 1
	case TenantConfidential:
		return 2
	case TenantRestricted:
		return 3
	default:
		return -1
	}
}

// Dominates reports whether c is at least as restrictive as need. Either
// side outside the lattice fails closed.
func (c TenantDataClassification) Dominates(need TenantDataClassification) bool {
	if !c.Valid() || !need.Valid() {
		return false
	}
	return c.rank() >= need.rank()
}

// ParseTenantDataClassification resolves one exact lattice token. Matching
// is case-sensitive; surrounding whitespace is tolerated and nothing else
// is. Unknown labels fail closed.
func ParseTenantDataClassification(raw string) (TenantDataClassification, error) {
	c := TenantDataClassification(strings.TrimSpace(raw))
	if !c.Valid() {
		return "", fmt.Errorf("rules: tenant data classification %q is not a declared lattice member", raw)
	}
	return c, nil
}

// tenantFlowAllowed reports whether a field labelled source may flow to an
// input capped at max. A blank cap is the legacy uncapped input and accepts
// everything; anything else must dominate the source label.
func tenantFlowAllowed(source string, max TenantDataClassification) error {
	parsed, err := ParseTenantDataClassification(source)
	if err != nil {
		return err
	}
	if max == "" {
		return nil
	}
	if !max.Valid() {
		return fmt.Errorf("%w: maximum %q is not a declared lattice member", ErrTenantClassification, string(max))
	}
	if !max.Dominates(parsed) {
		return fmt.Errorf("%w: field %q exceeds maximum %q", ErrTenantClassification, source, string(max))
	}
	return nil
}

// TenantField is one pinned field: its declared custom type name and its
// classification label.
type TenantField struct {
	Type           string
	Classification string
}

// TenantTypePin pins one tenant custom type version for rule compilation.
// It is a snapshot, not a live reference: the compiler checks existence,
// type and classification against exactly this version, and evaluation
// rejects inputs carrying any other version.
type TenantTypePin struct {
	Namespace string
	Kind      string
	Version   uint64
	Fields    map[string]TenantField
}

// Validate reports whether the pin is complete: identified, versioned at or
// above 1, carrying at least one named, typed, classified field.
func (p TenantTypePin) Validate() error {
	if strings.TrimSpace(p.Namespace) == "" || strings.TrimSpace(p.Kind) == "" {
		return fmt.Errorf("%w: namespace and kind are required", ErrTenantPinInvalid)
	}
	if p.Version == 0 {
		return fmt.Errorf("%w: version must be >= 1", ErrTenantPinInvalid)
	}
	if len(p.Fields) == 0 {
		return fmt.Errorf("%w: at least one field required", ErrTenantPinInvalid)
	}
	for name, field := range p.Fields {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%w: field name is empty", ErrTenantPinInvalid)
		}
		if strings.TrimSpace(field.Type) == "" {
			return fmt.Errorf("%w: field %q has no type", ErrTenantPinInvalid, name)
		}
		if _, err := ParseTenantDataClassification(field.Classification); err != nil {
			return fmt.Errorf("%w: field %q: %v", ErrTenantPinInvalid, name, err)
		}
	}
	return nil
}

// identity is the dependency name the pin publishes under.
func (p TenantTypePin) identity() string {
	return "tenant." + p.Namespace + "." + p.Kind
}

// Canonical returns the deterministic byte encoding of the pin, or nil when
// the pin fails Validate.
func (p TenantTypePin) Canonical() []byte {
	if err := p.Validate(); err != nil {
		return nil
	}
	names := make([]string, 0, len(p.Fields))
	for name := range p.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	w := canonicalbytes.New("hcmnext.engines.rules.TenantTypePin", 1).
		String("namespace", p.Namespace).
		String("kind", p.Kind).
		Int("version", int64(p.Version)).
		Count("fields", len(names))
	for _, name := range names {
		field := p.Fields[name]
		w.String("field.name", name).String("field.type", field.Type).String("field.classification", field.Classification)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest cites the exact pinned version for compiled artifacts.
func (p TenantTypePin) Digest() (string, error) {
	raw := p.Canonical()
	if raw == nil {
		return "", fmt.Errorf("rules: tenant pin %s is invalid, refusing to digest it", p.identity())
	}
	return canonicalbytes.Digest(raw), nil
}

// TenantPath is a parsed tenant field path.
type TenantPath struct {
	Namespace string
	Kind      string
	Field     string
}

// ParseTenantPath resolves tenant.<namespace>.<kind>.<field>. Anything else
// is refused: paths are exact, never prefix-matched, and relationships stay
// out of scope.
func ParseTenantPath(path string) (TenantPath, error) {
	parts := strings.Split(path, ".")
	if len(parts) != 4 || parts[0] != "tenant" {
		return TenantPath{}, fmt.Errorf("%w: %q wants tenant.<namespace>.<kind>.<field>", ErrTenantPath, path)
	}
	for _, part := range parts[1:] {
		if strings.TrimSpace(part) == "" {
			return TenantPath{}, fmt.Errorf("%w: %q has a blank segment", ErrTenantPath, path)
		}
	}
	return TenantPath{Namespace: parts[1], Kind: parts[2], Field: parts[3]}, nil
}

// IsTenantPath reports whether name claims the reserved tenant namespace.
// Claiming it without the full four segments is a compile error, never a
// platform input.
func IsTenantPath(name string) bool {
	return name == "tenant" || strings.HasPrefix(name, "tenant.")
}

// tenantTableKind maps a custom field type name to a decision-table kind,
// mirroring the derivation mapping. Anything unlisted is refused: the
// compiler never guesses a coercion, and dates have no table kind.
func tenantTableKind(fieldType string) (Kind, error) {
	switch strings.ToLower(strings.TrimSpace(fieldType)) {
	case "string":
		return KindString, nil
	case "bool", "boolean":
		return KindBool, nil
	case "integer", "int", "int64":
		return KindInt, nil
	case "number", "decimal", "money":
		return KindDecimal, nil
	}
	return KindUnspecified, fmt.Errorf("%w: field type %q has no decision-table kind", ErrTenantTypeMismatch, fieldType)
}

// tenantExpressionType maps a custom field type name to an expression type,
// mirroring the derivation mapping date included.
func tenantExpressionType(fieldType string) (ExpressionType, error) {
	switch strings.ToLower(strings.TrimSpace(fieldType)) {
	case "string":
		return ExpressionTypeString, nil
	case "bool", "boolean":
		return ExpressionTypeBool, nil
	case "integer", "int", "int64":
		return ExpressionTypeInt, nil
	case "number", "decimal", "money":
		return ExpressionTypeDecimal, nil
	case "date":
		return ExpressionTypeDate, nil
	}
	return ExpressionTypeUnknown, fmt.Errorf("%w: field type %q has no expression type", ErrTenantTypeMismatch, fieldType)
}

// validateTenantPins checks every pin and refuses duplicate identities: one
// namespace and kind pins exactly one version per compilation.
func validateTenantPins(pins []TenantTypePin) error {
	seen := make(map[string]bool, len(pins))
	for _, pin := range pins {
		if err := pin.Validate(); err != nil {
			return err
		}
		id := pin.identity()
		if seen[id] {
			return fmt.Errorf("%w: duplicate pin %s", ErrTenantPinInvalid, id)
		}
		seen[id] = true
	}
	return nil
}

// resolveTenantField binds one parsed path to its pin and field.
func resolveTenantField(pins []TenantTypePin, path TenantPath) (TenantTypePin, TenantField, error) {
	for _, pin := range pins {
		if pin.Namespace == path.Namespace && pin.Kind == path.Kind {
			field, ok := pin.Fields[path.Field]
			if !ok {
				return TenantTypePin{}, TenantField{}, fmt.Errorf("%w: %s in %s", ErrTenantFieldUnknown, path.Field, pin.identity())
			}
			return pin, field, nil
		}
	}
	return TenantTypePin{}, TenantField{}, fmt.Errorf("%w: tenant.%s.%s", ErrTenantTypeUnknown, path.Namespace, path.Kind)
}

// TenantTable is a decision table compiled against pinned tenant types. Its
// inputs may name tenant paths; the pins they were checked against travel
// with the table so evaluation can reject inputs from another version.
type TenantTable struct {
	Table Table
	Pins  []TenantTypePin
}

// Validate reports whether the table is internally consistent, every pin is
// complete and unique, and every tenant-pathed input resolves against its
// pin in existence, type and classification.
func (t TenantTable) Validate() error {
	if err := t.Table.Validate(); err != nil {
		return err
	}
	if err := validateTenantPins(t.Pins); err != nil {
		return err
	}
	return checkTenantColumns(t.Table, t.Pins)
}

// checkTenantColumns resolves every tenant-pathed input against its pin:
// the path must exist, the field type must match the declared column kind,
// and the field classification must fit the declared maximum.
func checkTenantColumns(table Table, pins []TenantTypePin) error {
	for _, col := range table.Inputs {
		if !IsTenantPath(col.Name) {
			continue
		}
		path, err := ParseTenantPath(col.Name)
		if err != nil {
			return err
		}
		_, field, err := resolveTenantField(pins, path)
		if err != nil {
			return err
		}
		want, err := tenantTableKind(field.Type)
		if err != nil {
			return fmt.Errorf("rules: input %q: %w", col.Name, err)
		}
		if want != col.Kind {
			return fmt.Errorf("%w: input %q is %s, column declares %s", ErrTenantTypeMismatch, col.Name, want, col.Kind)
		}
		if err := tenantFlowAllowed(field.Classification, col.MaxClassification); err != nil {
			return fmt.Errorf("rules: input %q: %w", col.Name, err)
		}
	}
	for _, col := range table.Outputs {
		if IsTenantPath(col.Name) {
			return fmt.Errorf("%w: output %q claims the reserved tenant namespace", ErrTenantPath, col.Name)
		}
	}
	return nil
}

// CompileTenantTable validates the table and binds every tenant-pathed
// input to its pin. Pins are sorted into the compiled artifact so the
// encoding never depends on caller order.
func CompileTenantTable(table Table, pins []TenantTypePin) (TenantTable, error) {
	out := TenantTable{Table: table, Pins: append([]TenantTypePin(nil), pins...)}
	sort.SliceStable(out.Pins, func(i, j int) bool { return out.Pins[i].identity() < out.Pins[j].identity() })
	if err := out.Validate(); err != nil {
		return TenantTable{}, err
	}
	return out, nil
}

// Canonical returns the deterministic byte encoding of the compiled table,
// or nil when the table fails Validate.
func (t TenantTable) Canonical() []byte {
	if err := t.Validate(); err != nil {
		return nil
	}
	pins := append([]TenantTypePin(nil), t.Pins...)
	sort.SliceStable(pins, func(i, j int) bool { return pins[i].identity() < pins[j].identity() })
	w := canonicalbytes.New("hcmnext.engines.rules.TenantTable", 1).Value("table", t.Table).Count("pins", len(pins))
	for _, pin := range pins {
		w.Value("pin", pin)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest cites the exact table and pins the inputs were checked against.
func (t TenantTable) Digest() (string, error) {
	raw := t.Canonical()
	if raw == nil {
		return "", fmt.Errorf("rules: tenant table %s@%s is invalid, refusing to digest it", t.Table.ID, t.Table.Version)
	}
	return canonicalbytes.Digest(raw), nil
}

// EvaluateTenantTable rejects any tenant input whose version differs from
// the pin the table compiled against, then evaluates the table. Platform
// inputs carry no version and are unaffected.
func EvaluateTenantTable(t TenantTable, inputs map[string]Value, versions map[string]uint64) (Result, error) {
	if err := t.Validate(); err != nil {
		return Result{}, err
	}
	for _, col := range t.Table.Inputs {
		if !IsTenantPath(col.Name) {
			continue
		}
		path, err := ParseTenantPath(col.Name)
		if err != nil {
			return Result{}, err
		}
		pin, _, err := resolveTenantField(t.Pins, path)
		if err != nil {
			return Result{}, err
		}
		got, ok := versions[col.Name]
		if !ok || got != pin.Version {
			return Result{}, fmt.Errorf("%w: input %q pinned at %d, input carries %d", ErrTenantVersionMismatch, col.Name, pin.Version, got)
		}
	}
	return Evaluate(t.Table, inputs)
}

// EvaluateTenantExpression rejects any tenant input whose version differs
// from the pin recorded in the compiled dependencies, then evaluates.
// Platform inputs carry no version and are unaffected.
func EvaluateTenantExpression(c CompiledExpression, inputs map[string]Value, versions map[string]uint64) (ExpressionResult, error) {
	for _, input := range c.Inputs {
		if !IsTenantPath(input.Name) {
			continue
		}
		path, err := ParseTenantPath(input.Name)
		if err != nil {
			return ExpressionResult{}, err
		}
		want := "tenant." + path.Namespace + "." + path.Kind
		var pinned uint64
		found := false
		for _, dep := range c.Dependencies {
			if dep.Name == want {
				found = true
				pinned, err = strconv.ParseUint(dep.Version, 10, 64)
				if err != nil {
					return ExpressionResult{}, fmt.Errorf("%w: dependency %q version %q is not a pin", ErrTenantVersionMismatch, dep.Name, dep.Version)
				}
			}
		}
		if !found {
			return ExpressionResult{}, fmt.Errorf("%w: input %q was compiled without its pin", ErrTenantVersionMismatch, input.Name)
		}
		got, ok := versions[input.Name]
		if !ok || got != pinned {
			return ExpressionResult{}, fmt.Errorf("%w: input %q pinned at %d, input carries %d", ErrTenantVersionMismatch, input.Name, pinned, got)
		}
	}
	return c.Evaluate(inputs)
}
