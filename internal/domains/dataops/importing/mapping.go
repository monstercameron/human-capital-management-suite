package importing

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/adapters"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

// Mapping compile errors. All are matchable with errors.Is. Each is a typed
// compile-time failure: a mapping that trips one of these never returns a
// [MappingProfile], because a caller that ignores the error and uses the
// zero value must not be able to mistake it for a working mapping.
var (
	ErrMappingInvalid          = errors.New("importing: mapping specification is invalid")
	ErrDuplicateSourceColumn   = errors.New("importing: source column is mapped more than once")
	ErrDuplicateTarget         = errors.New("importing: canonical property is written by more than one mapping")
	ErrUnresolvedProperty      = errors.New("importing: target property does not resolve in the registry")
	ErrUnsupportedTargetType   = errors.New("importing: target property's Go type has no supported transform output")
	ErrUnknownTransform        = errors.New("importing: transform kind is outside the closed transform set")
	ErrTransformTypeMismatch   = errors.New("importing: transform output type does not match the target property")
	ErrTransformFieldsInvalid  = errors.New("importing: transform carries invalid or extraneous parameters for its kind")
	ErrCrosswalkUnpinned       = errors.New("importing: lookup transform has no pinned crosswalk version")
	ErrCrosswalkEmpty          = errors.New("importing: lookup transform's crosswalk has no entries")
	ErrDateLayoutNotAllowed    = errors.New("importing: date parse layout is outside the allowed closed set")
	ErrCurrencyMalformed       = errors.New("importing: currency code is not three uppercase letters")
	ErrConstantEmpty           = errors.New("importing: constant transform declares an empty value")
	ErrSourceColumnNotInBatch  = errors.New("importing: mapping reads a column the batch does not carry")
	ErrSourceColumnNameInvalid = errors.New("importing: source column name is empty or too long")
)

// TransformKind is the closed set of transforms a compiled mapping may apply.
// Every transform is pure and parameter-free of any clock, counter or random
// source, so a mapping that compiles is a mapping that can never behave
// nondeterministically at apply time - there is no transform kind left that
// could.
type TransformKind uint8

// Transform kinds.
const (
	// TransformUnspecified is the zero value and is never legal.
	TransformUnspecified TransformKind = iota
	// TransformIdentity copies the cell verbatim.
	TransformIdentity
	// TransformTrim strips leading and trailing Unicode space.
	TransformTrim
	// TransformCaseUpper trims, then upper-cases.
	TransformCaseUpper
	// TransformCaseLower trims, then lower-cases.
	TransformCaseLower
	// TransformDateParse parses the trimmed cell under an explicit,
	// allow-listed layout and emits a canonical instant.
	TransformDateParse
	// TransformMoneyParse parses the trimmed cell as a decimal amount under
	// an explicit declared currency and emits a canonical decimal.
	TransformMoneyParse
	// TransformLookup maps the trimmed cell through a pinned, explicit
	// crosswalk.
	TransformLookup
	// TransformConstant ignores the cell and always emits a fixed value.
	TransformConstant
)

var transformKindWire = map[TransformKind]string{
	TransformIdentity:   "IDENTITY",
	TransformTrim:       "TRIM",
	TransformCaseUpper:  "CASE_UPPER",
	TransformCaseLower:  "CASE_LOWER",
	TransformDateParse:  "DATE_PARSE",
	TransformMoneyParse: "MONEY_PARSE",
	TransformLookup:     "LOOKUP",
	TransformConstant:   "CONSTANT",
}

// String returns the stable wire token, or "TRANSFORM_UNSPECIFIED".
func (k TransformKind) String() string {
	if s, ok := transformKindWire[k]; ok {
		return s
	}
	return "TRANSFORM_UNSPECIFIED"
}

// Valid reports whether k is a declared transform kind.
func (k TransformKind) Valid() bool { _, ok := transformKindWire[k]; return ok }

// allowedDateLayouts is the closed set of layouts TransformDateParse accepts.
// A mapping declares one explicitly; this package never infers one at compile
// time.
var allowedDateLayouts = map[string]bool{
	"2006-01-02": true,
	time.RFC3339: true,
	"2006/01/02": true,
	"01/02/2006": true,
	"20060102":   true,
}

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// TransformSpec is one transform, closed over its own kind's parameters. A
// field not meaningful for the declared Kind must be left zero; Validate
// rejects both a missing required parameter and an extraneous one, so a
// spec's Kind alone always determines which fields exist.
type TransformSpec struct {
	Kind TransformKind

	// Layout is required for TransformDateParse: an explicit Go reference
	// layout from allowedDateLayouts.
	Layout string

	// Currency is required for TransformMoneyParse: an explicit ISO-4217-shaped
	// three-letter uppercase code. A cell that itself carries a currency token
	// disagreeing with this one fails to apply.
	Currency string

	// Crosswalk and CrosswalkVersion are required for TransformLookup. The
	// crosswalk is pinned at compile time: this package never re-reads it, and
	// two compiles of the same spec always see the same map.
	Crosswalk        map[string]string
	CrosswalkVersion string

	// Constant is required for TransformConstant: the fixed value every row
	// emits, regardless of the source cell.
	Constant string
}

// Validate rejects a transform whose parameters do not match its declared
// kind exactly.
func (t TransformSpec) Validate() error {
	if !t.Kind.Valid() {
		return fmt.Errorf("%w: kind %d", ErrUnknownTransform, uint8(t.Kind))
	}
	hasLayout := t.Layout != ""
	hasCurrency := t.Currency != ""
	hasCrosswalk := len(t.Crosswalk) != 0 || t.CrosswalkVersion != ""
	hasConstant := t.Constant != ""

	switch t.Kind {
	case TransformDateParse:
		if !hasLayout {
			return fmt.Errorf("%w: date parse declares no layout", ErrTransformFieldsInvalid)
		}
		if !allowedDateLayouts[t.Layout] {
			return fmt.Errorf("%w: %q", ErrDateLayoutNotAllowed, t.Layout)
		}
		if hasCurrency || hasCrosswalk || hasConstant {
			return fmt.Errorf("%w: date parse carries unrelated parameters", ErrTransformFieldsInvalid)
		}
	case TransformMoneyParse:
		if !hasCurrency {
			return fmt.Errorf("%w: money parse declares no currency", ErrTransformFieldsInvalid)
		}
		if !currencyPattern.MatchString(t.Currency) {
			return fmt.Errorf("%w: %q", ErrCurrencyMalformed, t.Currency)
		}
		if hasLayout || hasCrosswalk || hasConstant {
			return fmt.Errorf("%w: money parse carries unrelated parameters", ErrTransformFieldsInvalid)
		}
	case TransformLookup:
		if t.CrosswalkVersion == "" {
			return ErrCrosswalkUnpinned
		}
		if len(t.Crosswalk) == 0 {
			return ErrCrosswalkEmpty
		}
		for k, v := range t.Crosswalk {
			if k == "" || v == "" {
				return fmt.Errorf("%w: crosswalk has an empty key or value", ErrTransformFieldsInvalid)
			}
		}
		if hasLayout || hasCurrency || hasConstant {
			return fmt.Errorf("%w: lookup carries unrelated parameters", ErrTransformFieldsInvalid)
		}
	case TransformConstant:
		if t.Constant == "" {
			return ErrConstantEmpty
		}
		if hasLayout || hasCurrency || hasCrosswalk {
			return fmt.Errorf("%w: constant carries unrelated parameters", ErrTransformFieldsInvalid)
		}
	default: // Identity, Trim, CaseUpper, CaseLower.
		if hasLayout || hasCurrency || hasCrosswalk || hasConstant {
			return fmt.Errorf("%w: %s carries parameters it does not use", ErrTransformFieldsInvalid, t.Kind)
		}
	}
	return nil
}

// TargetClass is the closed set of canonical output shapes this package's
// transforms can produce. A mapping compiles only when its target property's
// declared Go type resolves to one of these.
type TargetClass uint8

// Target classes.
const (
	// ClassUnsupported means no transform in the closed set can faithfully
	// populate the target property's declared Go type.
	ClassUnsupported TargetClass = iota
	// ClassString is a plain canonical string value.
	ClassString
	// ClassDecimal is a canonical fixed-point decimal, encoded as plain
	// signed decimal text.
	ClassDecimal
	// ClassInstant is a canonical UTC instant, encoded as RFC 3339 text.
	ClassInstant
)

var targetClassWire = map[TargetClass]string{
	ClassString:  "STRING",
	ClassDecimal: "DECIMAL",
	ClassInstant: "INSTANT",
}

// String returns the stable wire token, or "CLASS_UNSUPPORTED".
func (c TargetClass) String() string {
	if s, ok := targetClassWire[c]; ok {
		return s
	}
	return "CLASS_UNSUPPORTED"
}

// classifyGoType maps a [model.PropertyDefinition.GoType] string to the
// output class this package's closed transform set can produce for it. Only
// exactly these three concrete types are supported; every other declared Go
// type is ErrUnsupportedTargetType at compile time.
func classifyGoType(goType string) TargetClass {
	switch goType {
	case "string":
		return ClassString
	case "values.Decimal":
		return ClassDecimal
	case "values.Instant":
		return ClassInstant
	default:
		return ClassUnsupported
	}
}

// transformOutputClass reports the class a transform kind produces, once its
// own Validate has passed.
func transformOutputClass(kind TransformKind) TargetClass {
	switch kind {
	case TransformDateParse:
		return ClassInstant
	case TransformMoneyParse:
		return ClassDecimal
	case TransformIdentity, TransformTrim, TransformCaseUpper, TransformCaseLower,
		TransformLookup, TransformConstant:
		return ClassString
	default:
		return ClassUnsupported
	}
}

// FieldMapping maps one source column to one canonical property through
// exactly one transform.
type FieldMapping struct {
	SourceColumn string
	Target       model.PropertyRef
	Transform    TransformSpec

	// IsIdentity marks this field as (part of) the row's business identity.
	// The validator groups rows by the concatenation of every IsIdentity
	// field's mapped value to detect duplicates. Zero, one or many fields may
	// be marked.
	IsIdentity bool
}

func (fm FieldMapping) canonical(w *canonWriter) {
	w.str(fm.SourceColumn).str(string(fm.Target)).boolField(fm.IsIdentity)
	t := fm.Transform
	w.str(t.Kind.String()).str(t.Layout).str(t.Currency).str(t.CrosswalkVersion).str(t.Constant)
	keys := make([]string, 0, len(t.Crosswalk))
	for k := range t.Crosswalk {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	w.u64(uint64(len(keys)))
	for _, k := range keys {
		w.str(k).str(t.Crosswalk[k])
	}
}

// MappingSpecInput is the typed, caller-supplied mapping request that
// [Compile] resolves against the canonical property registry.
type MappingSpecInput struct {
	Version string
	Fields  []FieldMapping
}

// DiagnosticSeverity is the closed set of severities a compiled mapping's
// diagnostics carry.
type DiagnosticSeverity uint8

// Diagnostic severities.
const (
	// SeverityUnspecified is the zero value and is never legal.
	SeverityUnspecified DiagnosticSeverity = iota
	// SeverityInfo is a non-blocking, informational note.
	SeverityInfo
	// SeverityWarning flags a mapping that compiled but may need attention.
	SeverityWarning
)

var diagnosticSeverityWire = map[DiagnosticSeverity]string{
	SeverityInfo:    "INFO",
	SeverityWarning: "WARNING",
}

// String returns the stable wire token, or "SEVERITY_UNSPECIFIED".
func (s DiagnosticSeverity) String() string {
	if w, ok := diagnosticSeverityWire[s]; ok {
		return w
	}
	return "SEVERITY_UNSPECIFIED"
}

// Diagnostic is one typed note attached to a compiled mapping.
type Diagnostic struct {
	Severity     DiagnosticSeverity
	Code         string
	SourceColumn string
	Target       model.PropertyRef
	Message      string
}

// MappingProfile is the immutable, digested result of compiling a
// [MappingSpecInput] against the canonical property registry.
type MappingProfile struct {
	Version     string
	Fields      []FieldMapping      // sorted by SourceColumn.
	Reads       []string            // sorted, deduplicated source columns.
	Writes      []model.PropertyRef // sorted, deduplicated target properties.
	Diagnostics []Diagnostic
	Digest      string
	engine      []adapters.Lowered
}

// FieldFor returns the compiled mapping for a source column.
func (m MappingProfile) FieldFor(sourceColumn string) (FieldMapping, bool) {
	for _, f := range m.Fields {
		if f.SourceColumn == sourceColumn {
			return f, true
		}
	}
	return FieldMapping{}, false
}

// Compile resolves spec against reg and returns an immutable, digested
// mapping, or a typed compile error. Compile never returns a partially usable
// MappingProfile: any RED condition (an unresolved property, a target type no
// closed transform can produce, a transform whose own parameters are
// invalid, or a source column/target mapped twice) fails the whole compile.
func Compile(reg *model.Registry, spec MappingSpecInput) (MappingProfile, error) {
	if reg == nil {
		return MappingProfile{}, fmt.Errorf("%w: registry is nil", ErrMappingInvalid)
	}
	if spec.Version == "" {
		return MappingProfile{}, fmt.Errorf("%w: mapping declares no version", ErrMappingInvalid)
	}
	if len(spec.Fields) == 0 {
		return MappingProfile{}, fmt.Errorf("%w: mapping declares no fields", ErrMappingInvalid)
	}

	seenColumn := make(map[string]struct{}, len(spec.Fields))
	seenTarget := make(map[model.PropertyRef]struct{}, len(spec.Fields))
	fields := make([]FieldMapping, 0, len(spec.Fields))
	diagnostics := make([]Diagnostic, 0, len(spec.Fields))

	for _, fm := range spec.Fields {
		if fm.SourceColumn == "" || len(fm.SourceColumn) > MaxColumnNameLength {
			return MappingProfile{}, fmt.Errorf("%w: %q", ErrSourceColumnNameInvalid, fm.SourceColumn)
		}
		if _, dup := seenColumn[fm.SourceColumn]; dup {
			return MappingProfile{}, fmt.Errorf("%w: %q", ErrDuplicateSourceColumn, fm.SourceColumn)
		}
		seenColumn[fm.SourceColumn] = struct{}{}

		if err := fm.Target.Validate(); err != nil {
			return MappingProfile{}, fmt.Errorf("%w: %q: %w", ErrUnresolvedProperty, fm.Target, err)
		}
		if _, dup := seenTarget[fm.Target]; dup {
			return MappingProfile{}, fmt.Errorf("%w: %q", ErrDuplicateTarget, fm.Target)
		}
		seenTarget[fm.Target] = struct{}{}

		resolution, err := reg.ResolveProperty(fm.Target)
		if err != nil {
			return MappingProfile{}, fmt.Errorf("%w: %q: %w", ErrUnresolvedProperty, fm.Target, err)
		}

		class := classifyGoType(resolution.Property.GoType)
		if class == ClassUnsupported {
			return MappingProfile{}, fmt.Errorf("%w: %q has go type %q",
				ErrUnsupportedTargetType, fm.Target, resolution.Property.GoType)
		}

		if err := fm.Transform.Validate(); err != nil {
			return MappingProfile{}, fmt.Errorf("importing: field %q: %w", fm.SourceColumn, err)
		}
		outClass := transformOutputClass(fm.Transform.Kind)
		if outClass != class {
			return MappingProfile{}, fmt.Errorf("%w: %q emits %s but %q expects %s",
				ErrTransformTypeMismatch, fm.Transform.Kind, outClass, fm.Target, class)
		}

		fields = append(fields, fm)
		diagnostics = append(diagnostics, Diagnostic{
			Severity:     SeverityInfo,
			Code:         "mapping.field_compiled",
			SourceColumn: fm.SourceColumn,
			Target:       fm.Target,
			Message:      fmt.Sprintf("%s -> %s via %s", fm.SourceColumn, fm.Target, fm.Transform.Kind),
		})
	}

	sort.Slice(fields, func(i, j int) bool { return fields[i].SourceColumn < fields[j].SourceColumn })
	sort.Slice(diagnostics, func(i, j int) bool { return diagnostics[i].SourceColumn < diagnostics[j].SourceColumn })

	reads := make([]string, 0, len(fields))
	writes := make([]model.PropertyRef, 0, len(fields))
	for _, f := range fields {
		reads = append(reads, f.SourceColumn)
		writes = append(writes, f.Target)
	}
	sort.Strings(reads)
	sort.Slice(writes, func(i, j int) bool { return writes[i] < writes[j] })

	profile := MappingProfile{
		Version:     spec.Version,
		Fields:      fields,
		Reads:       reads,
		Writes:      writes,
		Diagnostics: diagnostics,
	}
	profile.engine = make([]adapters.Lowered, len(fields))
	for i, f := range fields {
		lowered, err := adapters.LowerDataOpsImport(adapters.DataOpsImportMapping{
			Version: profile.Version,
			Fields: []adapters.DataOpsFieldMapping{{SourceColumn: f.SourceColumn, Target: string(f.Target), Transform: adapters.DataOpsTransform{
				Kind: adapters.DataOpsTransformKind(f.Transform.Kind.String()), Layout: f.Transform.Layout,
				Currency: f.Transform.Currency, Crosswalk: f.Transform.Crosswalk,
				CrosswalkVersion: f.Transform.CrosswalkVersion, Constant: f.Transform.Constant,
			}, IsIdentity: f.IsIdentity}},
			Limits: adapters.DataOpsLimits{MaxRows: MaxRows, MaxCellBytes: MaxCellBytes},
		})
		if err != nil {
			return MappingProfile{}, fmt.Errorf("importing: shared transformation lowering: %w", err)
		}
		profile.engine[i] = lowered
	}

	w := newCanonWriter("hcmnext.dataops.importing.MappingProfile", 1)
	w.str(profile.Version).u64(uint64(len(profile.Fields)))
	for _, f := range profile.Fields {
		f.canonical(w)
	}
	profile.Digest = w.digestHex()
	return profile, nil
}

// MappedValue is one field of one row after a mapping has been applied.
type MappedValue struct {
	SourceColumn string
	Target       model.PropertyRef
	Class        TargetClass

	// Value is the canonical text form of the transform's output: plain
	// signed decimal text for ClassDecimal, RFC 3339 UTC for ClassInstant, or
	// the string itself for ClassString. It is meaningful only when OK.
	Value string

	// OK reports whether the transform applied successfully. When false,
	// ErrorCode is a stable rule identity the validator turns into a
	// [ValidationError].
	OK        bool
	ErrorCode string
}

// Transform rule identities, surfaced to the validator as ErrorCode on a
// failed [MappedValue].
const (
	RuleDateParseFailed  = "transform.date_parse_failed"
	RuleMoneyParseFailed = "transform.money_parse_failed"
	RuleLookupUnresolved = "transform.lookup_unresolved"
)

// Apply runs the compiled mapping over one row, given the batch header the
// row was staged under. It returns one MappedValue per compiled field, in the
// mapping's own field order, or a structural error when the batch does not
// carry a column the mapping reads - a setup problem the caller should catch
// once for the whole batch, not per row.
func (m MappingProfile) Apply(header []string, row Row) ([]MappedValue, error) {
	index := make(map[string]int, len(header))
	for i, name := range header {
		index[name] = i
	}
	cells := row.Cells()

	out := make([]MappedValue, 0, len(m.Fields))
	for fieldIndex, f := range m.Fields {
		i, ok := index[f.SourceColumn]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrSourceColumnNotInBatch, f.SourceColumn)
		}
		mv := MappedValue{SourceColumn: f.SourceColumn, Target: f.Target, Class: transformOutputClass(f.Transform.Kind)}
		engine := m.engine[fieldIndex]
		row := make(map[string]string, 1)
		for _, binding := range engine.Bindings {
			if binding.SourceKey != "" {
				row[binding.SourceKey] = cells[i]
			}
		}
		results, err := engine.Run([]map[string]string{row})
		if err != nil {
			mv.OK = false
			switch f.Transform.Kind {
			case TransformDateParse:
				mv.ErrorCode = RuleDateParseFailed
			case TransformMoneyParse:
				mv.ErrorCode = RuleMoneyParseFailed
			case TransformLookup:
				mv.ErrorCode = RuleLookupUnresolved
			default:
				mv.ErrorCode = "transform.failed"
			}
		} else {
			texts, textErr := engine.Texts(results[0])
			if textErr != nil {
				return nil, textErr
			}
			mv.Value, mv.OK = texts[string(f.Target)]
			if !mv.OK {
				mv.ErrorCode = "transform.failed"
			}
		}
		out = append(out, mv)
	}
	return out, nil
}
