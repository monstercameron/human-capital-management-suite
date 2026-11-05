// WF-DATA-021: derived fields computed from bounded expressions.
//
// A derived field declares an expression over the record's own fields and
// named linked inputs; it is computed and stored on write and maintained
// by the propagation engine as linked records change. Expressions parse
// and compile against the rules engine within default cost limits, every
// variable resolves to a declared field or a declared link, and the
// field's classification must dominate every input or publication is
// refused. Callers never supply derived values: a provided value that
// differs from the computed one fails the write.
package custom

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrInvalidDerived reports an unusable derived declaration: a bad
	// expression, an undeclared variable, a derivation cycle or an
	// under-classified field.
	ErrInvalidDerived = errors.New("custom: invalid derived field")
	// ErrDerivedMismatch reports a caller-supplied derived value that
	// differs from the computed one.
	ErrDerivedMismatch = errors.New("custom: derived value differs from computed")
)

// DerivedSpec declares one derived field: a bounded expression plus the
// named linked inputs it may reference. Linked inputs always arrive as
// lists, even over one-to-one relationships, so expressions aggregate
// uniformly.
type DerivedSpec struct {
	Expression string
	Links      []DerivedLink
}

// DerivedLink declares one linked input: the expression variable, the
// relationship traversed from the source record, the join field read on
// both ends under the same name, the target field collected as the list,
// and the maximum targets evaluated. The bound keeps every evaluation
// finite however the data grows.
type DerivedLink struct {
	Input        string
	Relationship string
	Field        string
	Value        string
	MaxTargets   int
}

// expressionTypes maps custom field types to expression input types.
// Anything unlisted is refused: derivation never guesses a coercion.
func expressionTypeFor(fieldType string) (rules.ExpressionType, error) {
	switch strings.ToLower(strings.TrimSpace(fieldType)) {
	case "string":
		return rules.ExpressionTypeString, nil
	case "bool", "boolean":
		return rules.ExpressionTypeBool, nil
	case "integer", "int", "int64":
		return rules.ExpressionTypeInt, nil
	case "number", "decimal", "money":
		return rules.ExpressionTypeDecimal, nil
	case "date":
		return rules.ExpressionTypeDate, nil
	}
	return rules.ExpressionTypeUnknown, fmt.Errorf("%w: field type %q has no expression type", ErrInvalidDerived, fieldType)
}

// expressionVariables collects every variable the expression references.
func expressionVariables(expr rules.Expression) []string {
	seen := map[string]bool{}
	var walk func(node rules.Expr)
	walk = func(node rules.Expr) {
		if node.Kind == rules.ExprKindVariable && node.Name != "" {
			seen[node.Name] = true
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(expr.Root)
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// parseDerivedVars parses one derived expression without types and
// returns its sorted referenced variables. Cycle detection and the
// definition-local declared-input check need names only, so they run
// before the object set is known.
func parseDerivedVars(spec *DerivedSpec) ([]string, error) {
	expr, err := rules.Parse(spec.Expression)
	if err != nil {
		return nil, err
	}
	return expressionVariables(expr), nil
}

// compileDerived parses, types and compiles one derived expression
// within default cost limits. Only referenced variables become inputs:
// linked inputs type as lists over the given element types, everything
// else types from the record's own fields, and an unindexed referenced
// type fails closed. The compiled root must be a storable scalar.
func compileDerived(name string, fields map[string]FieldDefinition, spec *DerivedSpec, links map[string]rules.ExpressionType) (rules.CompiledExpression, []string, error) {
	fail := func(err error) (rules.CompiledExpression, []string, error) {
		return rules.CompiledExpression{}, nil, fmt.Errorf("%w: field %q: %v", ErrInvalidDerived, name, err)
	}
	vars, err := parseDerivedVars(spec)
	if err != nil {
		return fail(err)
	}
	declared := map[string]bool{}
	for _, link := range spec.Links {
		declared[link.Input] = true
	}
	inputs := make([]rules.Input, 0, len(vars))
	for _, v := range vars {
		if declared[v] {
			element, ok := links[v]
			if !ok {
				return fail(fmt.Errorf("link %q has no resolved element type", v))
			}
			inputs = append(inputs, rules.Input{Name: v, Type: rules.ExpressionTypeList, ElementType: element})
			continue
		}
		fieldDef, ok := fields[v]
		if !ok {
			return fail(fmt.Errorf("references undeclared %q", v))
		}
		typ, err := expressionTypeFor(fieldDef.Type)
		if err != nil {
			return fail(err)
		}
		inputs = append(inputs, rules.Input{Name: v, Type: typ})
	}
	expr, err := rules.ParseWithInputs(spec.Expression, inputs)
	if err != nil {
		return fail(err)
	}
	// Reject is the only honest choice for stored derivations: a missing
	// input fails the write instead of silently defaulting.
	expr.UnknownSemantics = rules.UnknownSemanticsReject
	compiled, err := rules.CompileExpression(expr, rules.DefaultCostLimit)
	if err != nil {
		return fail(err)
	}
	if compiled.IR.Root < 0 || compiled.IR.Root >= len(compiled.IR.Nodes) {
		return fail(fmt.Errorf("compiled root %d is outside %d nodes", compiled.IR.Root, len(compiled.IR.Nodes)))
	}
	switch root := compiled.IR.Nodes[compiled.IR.Root].Type; root {
	case rules.ExpressionTypeBool, rules.ExpressionTypeInt, rules.ExpressionTypeDecimal,
		rules.ExpressionTypeString, rules.ExpressionTypeDate:
	default:
		return fail(fmt.Errorf("result type %s is not a storable scalar", root))
	}
	return compiled, vars, nil
}

// validateDerivedLocal checks one derived declaration without external
// context: plain kind, a parsing expression with declared inputs, and
// well-formed links. Typed compilation needs the object set for link
// element types, so it runs at publication in ValidateDerived.
func validateDerivedLocal(name string, fields map[string]FieldDefinition, fieldDef FieldDefinition) error {
	spec := fieldDef.Derived
	if spec == nil {
		return nil
	}
	if fieldDef.Kind != FieldKindPlain {
		return fmt.Errorf("%w: field %q is governed and derived", ErrInvalidDerived, name)
	}
	if strings.TrimSpace(spec.Expression) == "" {
		return fmt.Errorf("%w: field %q has no expression", ErrInvalidDerived, name)
	}
	seen := map[string]bool{}
	for _, link := range spec.Links {
		if link.Input == "" || link.Relationship == "" || link.Field == "" || link.Value == "" {
			return fmt.Errorf("%w: field %q has an incomplete link", ErrInvalidDerived, name)
		}
		if link.MaxTargets < 1 {
			return fmt.Errorf("%w: field %q link %q needs an explicit target bound", ErrInvalidDerived, name, link.Input)
		}
		if _, ok := fields[link.Input]; ok {
			return fmt.Errorf("%w: field %q link %q shadows a record field", ErrInvalidDerived, name, link.Input)
		}
		if seen[link.Input] {
			return fmt.Errorf("%w: field %q declares link %q twice", ErrInvalidDerived, name, link.Input)
		}
		seen[link.Input] = true
	}
	vars, err := parseDerivedVars(spec)
	if err != nil {
		return fmt.Errorf("%w: field %q: %v", ErrInvalidDerived, name, err)
	}
	for _, v := range vars {
		if _, ok := fields[v]; ok {
			continue
		}
		if seen[v] {
			continue
		}
		return fmt.Errorf("%w: field %q references undeclared %q", ErrInvalidDerived, name, v)
	}
	return nil
}

// validateDerivedGraph refuses derivation cycles, including
// self-reference, by ordering the derived fields topologically.
func validateDerivedGraph(fields map[string]FieldDefinition) error {
	_, err := derivedOrder(fields)
	return err
}

// encodeDerived writes one field's derived declaration into the canonical
// encoding. A nil declaration writes nothing, so existing digests hold.
func encodeDerived(w *canonicalbytes.Writer, name string, fieldDef FieldDefinition) {
	spec := fieldDef.Derived
	if spec == nil {
		return
	}
	w.String(fmt.Sprintf("fields.%s.derived.expression", name), spec.Expression)
	links := append([]DerivedLink(nil), spec.Links...)
	sort.Slice(links, func(i, j int) bool { return links[i].Input < links[j].Input })
	for i, link := range links {
		prefix := fmt.Sprintf("fields.%s.derived.links.%d", name, i)
		w.String(prefix+".input", link.Input).
			String(prefix+".relationship", link.Relationship).
			String(prefix+".field", link.Field).
			String(prefix+".value", link.Value).
			Int(prefix+".max_targets", int64(link.MaxTargets))
	}
}

// toRulesValue converts one stored field value to an expression value.
// Conversions are strict but JSON-tolerant: stored integers may arrive as
// floating point after a store round-trip, and only integral ones convert.
func toRulesValue(fieldType string, value any) (rules.Value, error) {
	lowered := strings.ToLower(strings.TrimSpace(fieldType))
	switch lowered {
	case "string", "date":
		text, ok := value.(string)
		if !ok {
			return rules.Value{}, fmt.Errorf("%w: %s value is not text", ErrInvalidDerived, fieldType)
		}
		if lowered == "date" {
			if _, err := time.Parse("2006-01-02", text); err != nil {
				return rules.Value{}, fmt.Errorf("%w: date value %q is not a calendar day", ErrInvalidDerived, text)
			}
		}
		return rules.StringValue(text), nil
	case "bool", "boolean":
		b, ok := value.(bool)
		if !ok {
			return rules.Value{}, fmt.Errorf("%w: %s value is not boolean", ErrInvalidDerived, fieldType)
		}
		return rules.BoolValue(b), nil
	case "integer", "int", "int64":
		switch n := value.(type) {
		case int:
			return rules.IntValue(int64(n)), nil
		case int8:
			return rules.IntValue(int64(n)), nil
		case int16:
			return rules.IntValue(int64(n)), nil
		case int32:
			return rules.IntValue(int64(n)), nil
		case int64:
			return rules.IntValue(n), nil
		case float64:
			if n != float64(int64(n)) {
				return rules.Value{}, fmt.Errorf("%w: integer value %v is fractional", ErrInvalidDerived, n)
			}
			return rules.IntValue(int64(n)), nil
		}
		return rules.Value{}, fmt.Errorf("%w: integer value has no numeric form", ErrInvalidDerived)
	case "number", "decimal", "money":
		text, scale, err := decimalText(value)
		if err != nil {
			return rules.Value{}, err
		}
		dec, err := values.NewDecimal(text, scale, values.RoundingHalfEven)
		if err != nil {
			return rules.Value{}, fmt.Errorf("%w: decimal %q: %v", ErrInvalidDerived, text, err)
		}
		return rules.DecimalValue(dec), nil
	}
	return rules.Value{}, fmt.Errorf("%w: field type %q has no expression type", ErrInvalidDerived, fieldType)
}

// decimalText renders one stored number in the canonical decimal text and
// counts its fractional digits. No rounding ever applies: the text is the
// value.
func decimalText(value any) (string, int32, error) {
	switch n := value.(type) {
	case string:
		text := strings.TrimSpace(n)
		if text == "" {
			return "", 0, fmt.Errorf("%w: decimal value is blank", ErrInvalidDerived)
		}
		digits := int32(0)
		if dot := strings.IndexByte(text, '.'); dot >= 0 {
			digits = int32(len(text) - dot - 1)
		}
		return text, digits, nil
	case float64:
		text := strconv.FormatFloat(n, 'f', -1, 64)
		digits := int32(0)
		if dot := strings.IndexByte(text, '.'); dot >= 0 {
			digits = int32(len(text) - dot - 1)
		}
		return text, digits, nil
	case int64:
		return strconv.FormatInt(n, 10), 0, nil
	case int:
		return strconv.Itoa(n), 0, nil
	}
	return "", 0, fmt.Errorf("%w: decimal value has no numeric form", ErrInvalidDerived)
}

// fromRulesValue converts one evaluated result to the stored native for a
// field type. Decimals store as canonical text so no precision is lost to
// floating point; lists are refused because derived fields are scalar.
func fromRulesValue(fieldType string, value rules.Value) (any, error) {
	switch value.Kind() {
	case rules.KindBool:
		b, ok := value.Bool()
		if !ok {
			return nil, fmt.Errorf("%w: boolean result is not boolean", ErrInvalidDerived)
		}
		return b, nil
	case rules.KindInt:
		n, ok := value.Int()
		if !ok {
			return nil, fmt.Errorf("%w: integer result is not an integer", ErrInvalidDerived)
		}
		return n, nil
	case rules.KindDecimal:
		dec, err := value.Decimal()
		if err != nil {
			return nil, fmt.Errorf("%w: decimal result: %v", ErrInvalidDerived, err)
		}
		return dec.String(), nil
	case rules.KindString:
		return value.String(), nil
	}
	return nil, fmt.Errorf("%w: result kind %s is not storable", ErrInvalidDerived, value.Kind())
}

// maxClassification folds one more input label into the running join: the
// most restrictive label seen so far. Unknown labels fail the join
// instead of vanishing from it.
func maxClassification(current FieldClassification, next FieldClassification) (FieldClassification, bool) {
	joined, ok := joinLabel(current.Classification, next.Classification)
	if !ok {
		return FieldClassification{}, false
	}
	out := current
	out.Classification = joined
	return out, true
}

// joinLabel returns the more restrictive of two lattice labels.
func joinLabel(a, b string) (string, bool) {
	ra, oka := labelRank(a)
	rb, okb := labelRank(b)
	if !oka || !okb {
		return "", false
	}
	if ra >= rb {
		return a, true
	}
	return b, true
}

// labelRank orders the lattice from public to restricted. Unknown labels
// have no rank and fail the join closed.
func labelRank(label string) (int, bool) {
	switch strings.ToUpper(strings.TrimSpace(label)) {
	case "PUBLIC":
		return 0, true
	case "INTERNAL":
		return 1, true
	case "CONFIDENTIAL":
		return 2, true
	case "RESTRICTED":
		return 3, true
	}
	return 0, false
}
