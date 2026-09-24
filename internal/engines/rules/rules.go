// Package rules is the pure decision-table evaluation engine: given a
// versioned table of typed rows and a set of typed inputs, it answers which
// row(s) apply and what they say, or reports honestly that nothing applies or
// that more than one row disagrees.
//
// Semantic owner: shared-engines. Phase: P1A/P1B.
//
// This package is deliberately narrower than "business rules." It has no
// expression language, no scripting runtime and no dependency on the bounded
// expression compiler described for RULE-001 - that capability is [OUT] until
// a second workflow family needs it, and a decision table's rows are typed
// structural conditions (equality, ordering, membership, range) rather than
// compiled expressions. What this package owns is the one question a table
// can answer on its own: given these typed inputs, which row(s) match under
// the table's declared hit policy, and can that be proven byte-for-byte and
// explained row by row.
//
// A table is a versioned immutable value, exactly like a pay band or a
// bitemporal coordinate elsewhere in internal/engines: republishing a table
// with different rows is a new version, never an edit in place, because an
// evaluation cites the exact version it ran against. Evaluation is a pure
// function of the table and the inputs - no clock, no map iteration over
// anything that affects the result, no I/O - so the same table and the same
// inputs always produce the same result and the same digest.
//
// Every comparison is exact. Decimal columns compare through
// internal/kernel/values.Decimal.Cmp, which is fixed-point arithmetic; there
// is no float64 anywhere in this package, and no comparison is ever
// approximate.
package rules

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// schemaID and schemaVersion tag the canonical stream of this engine's types.
const (
	valueSchema   = "hcmnext.engines.rules.Value"
	tableSchema   = "hcmnext.engines.rules.Table"
	schemaVersion = 1
)

// Version reports this engine's own package contract version: the schema
// version every canonical encoder in this package agrees on (see
// valueSchema/tableSchema above). It is part of the ARCH-GO-009 engine
// package contract, not a business-facing evaluation input.
func Version() int { return schemaVersion }

// Engine errors. All are matchable with errors.Is.
var (
	// ErrKindUnspecified is returned for a value or column with no declared kind.
	ErrKindUnspecified = errors.New("rules: kind is unspecified")
	// ErrKindMismatch is returned when two values, or a value and its declared
	// column, disagree on kind. Comparing a string to a decimal is a category
	// error, never a "no match."
	ErrKindMismatch = errors.New("rules: kind mismatch")
	// ErrOperatorUnspecified is returned for a condition with no declared operator.
	ErrOperatorUnspecified = errors.New("rules: condition operator is unspecified")
	// ErrOperatorUnsupported is returned when an operator does not apply to a
	// column's declared kind, e.g. an ordering comparison on a string column.
	ErrOperatorUnsupported = errors.New("rules: operator is not supported for this kind")
	// ErrOperandCount is returned when an operator's operand count is wrong,
	// e.g. IN with zero operands.
	ErrOperandCount = errors.New("rules: wrong operand count for this operator")
	// ErrColumnInvalid is returned for a column with an empty name or no kind.
	ErrColumnInvalid = errors.New("rules: column is invalid")
	// ErrDuplicateColumn is returned when two columns in the same list share a name.
	ErrDuplicateColumn = errors.New("rules: duplicate column name")
	// ErrHitPolicyUnspecified is returned for a table with no declared hit policy.
	// A table's conflict strategy is part of its publication contract, never a
	// silent default.
	ErrHitPolicyUnspecified = errors.New("rules: hit policy is unspecified")
	// ErrTableIdentity is returned for a table with no id or no version. An
	// unversioned table cannot be cited as the basis of a decision.
	ErrTableIdentity = errors.New("rules: table requires an id and a version")
	// ErrTableColumns is returned for a table with no input or no output columns.
	ErrTableColumns = errors.New("rules: table requires at least one input and one output column")
	// ErrTableEmpty is returned for a table with no rows.
	ErrTableEmpty = errors.New("rules: table has no rows")
	// ErrRowInvalid is returned for a row whose shape does not match the
	// table's declared columns.
	ErrRowInvalid = errors.New("rules: row is invalid")
	// ErrDuplicateRowID is returned when two rows in the same table share an id.
	ErrDuplicateRowID = errors.New("rules: duplicate row id")
	// ErrAmbiguousRows is returned at publication when two rows declare exactly
	// the same conditions under a hit policy that requires a single winner. The
	// second row can never be reached and its presence is a defect in the
	// table, not a priority choice.
	ErrAmbiguousRows = errors.New("rules: two rows declare identical conditions under a policy that requires a single winner")
	// ErrUnknownInput is returned when an evaluation input names a column the
	// table does not declare.
	ErrUnknownInput = errors.New("rules: input names a column the table does not declare")
	// ErrMissingInput is returned when an evaluation is missing a value for a
	// declared input column. A missing input is a caller defect and is never
	// treated as a wildcard match.
	ErrMissingInput = errors.New("rules: input is missing a declared column")
)

// Kind is the declared type of a value, an input column or an output column.
// A table's columns are typed, and a value presented against a column must
// declare the same kind: two strings that happen to compare equal as text are
// not evidence that a decimal and an enum code mean the same thing.
type Kind uint8

// Kinds.
const (
	// KindUnspecified is the zero value and is never legal.
	KindUnspecified Kind = iota
	// KindDecimal is an exact fixed-point number, compared through
	// values.Decimal.Cmp. Never a float64.
	KindDecimal
	// KindString is a closed- or open-vocabulary text value, compared by exact
	// byte equality.
	KindString
	// KindBool is true or false.
	KindBool
	// KindInt is a signed integer, compared exactly.
	KindInt
	// KindList is an immutable, homogeneous list used by the expression evaluator.
	KindList
)

var kindWire = map[Kind]string{
	KindDecimal: "DECIMAL",
	KindString:  "STRING",
	KindBool:    "BOOL",
	KindInt:     "INT",
	KindList:    "LIST",
}

// String returns the stable wire token, or "KIND_UNSPECIFIED".
func (k Kind) String() string {
	if s, ok := kindWire[k]; ok {
		return s
	}
	return "KIND_UNSPECIFIED"
}

// Valid reports whether k is a declared kind.
func (k Kind) Valid() bool { _, ok := kindWire[k]; return ok }

// Value is one typed cell: a condition operand, a row output, or an
// evaluation input. It carries exactly one of a decimal, a string, a bool or
// an int, tagged by its declared Kind so a caller can never compare across
// kinds by accident.
//
// The zero Value is unset and fails Validate, matching every other kernel
// value type in this codebase.
type Value struct {
	kind Kind
	dec  values.Decimal
	str  string
	b    bool
	i    int64
	list []Value
}

// DecimalValue wraps a fixed-point decimal.
func DecimalValue(d values.Decimal) Value { return Value{kind: KindDecimal, dec: d} }

// StringValue wraps a text value.
func StringValue(s string) Value { return Value{kind: KindString, str: s} }

// BoolValue wraps a boolean value.
func BoolValue(b bool) Value { return Value{kind: KindBool, b: b} }

// IntValue wraps a signed integer value.
func IntValue(i int64) Value { return Value{kind: KindInt, i: i} }

// ListValue creates a defensive copy of a homogeneous scalar list. Nested
// lists are rejected during Validate so evaluation remains shallow and bounded.
func ListValue(items ...Value) Value {
	return Value{kind: KindList, list: append([]Value(nil), items...)}
}

// List returns a defensive copy of a list value.
func (v Value) List() ([]Value, bool) {
	if v.kind != KindList {
		return nil, false
	}
	return append([]Value(nil), v.list...), true
}

// Kind returns the value's declared kind.
func (v Value) Kind() Kind { return v.kind }

// Validate reports whether the value is usable.
func (v Value) Validate() error {
	if !v.kind.Valid() {
		return ErrKindUnspecified
	}
	if v.kind == KindDecimal {
		if err := v.dec.Validate(); err != nil {
			return fmt.Errorf("rules: decimal value: %w", err)
		}
	}
	if v.kind == KindList {
		var kind Kind
		for i, item := range v.list {
			if item.kind == KindList {
				return fmt.Errorf("rules: nested lists are not supported")
			}
			if err := item.Validate(); err != nil {
				return fmt.Errorf("rules: list item %d: %w", i, err)
			}
			if i == 0 {
				kind = item.kind
			} else if item.kind != kind {
				return fmt.Errorf("rules: heterogeneous list")
			}
		}
	}
	return nil
}

// String returns a stable text rendering of the value, used in traces and
// error messages. It never panics; an invalid value renders as "".
func (v Value) String() string {
	switch v.kind {
	case KindDecimal:
		return v.dec.String()
	case KindString:
		return v.str
	case KindBool:
		if v.b {
			return "true"
		}
		return "false"
	case KindInt:
		return strconv.FormatInt(v.i, 10)
	case KindList:
		parts := make([]string, len(v.list))
		for i, item := range v.list {
			parts[i] = item.String()
		}
		return "[" + strings.Join(parts, ",") + "]"
	default:
		return ""
	}
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (v Value) Canonical() []byte {
	if err := v.Validate(); err != nil {
		return nil
	}
	w := canonicalbytes.New(valueSchema, schemaVersion).String("kind", v.kind.String())
	switch v.kind {
	case KindDecimal:
		w.Value("decimal", v.dec)
	case KindString:
		w.String("string", v.str)
	case KindBool:
		w.Bool("bool", v.b)
	case KindInt:
		w.Int("int", v.i)
	case KindList:
		w.Count("items", len(v.list))
		for _, item := range v.list {
			w.Field("item", item.Canonical())
		}
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Equal reports whether v and o carry the same kind and the same value. It
// refuses to answer across kinds rather than falling back to text equality.
func (v Value) Equal(o Value) (bool, error) {
	if v.kind != o.kind {
		return false, fmt.Errorf("%w: %s vs %s", ErrKindMismatch, v.kind, o.kind)
	}
	switch v.kind {
	case KindDecimal:
		return v.dec.Cmp(o.dec) == 0, nil
	case KindString:
		return v.str == o.str, nil
	case KindBool:
		return v.b == o.b, nil
	case KindInt:
		return v.i == o.i, nil
	case KindList:
		if len(v.list) != len(o.list) {
			return false, nil
		}
		for i := range v.list {
			eq, err := v.list[i].Equal(o.list[i])
			if err != nil || !eq {
				return eq, err
			}
		}
		return true, nil
	default:
		return false, ErrKindUnspecified
	}
}

// Cmp orders v against o for the two kinds that carry a total order: decimal
// and int. It refuses to order a string or a bool, because "ABOVE_MAXIMUM"
// less than "IN_BAND" is not a business fact anyone declared.
func (v Value) Cmp(o Value) (int, error) {
	if v.kind != o.kind {
		return 0, fmt.Errorf("%w: %s vs %s", ErrKindMismatch, v.kind, o.kind)
	}
	switch v.kind {
	case KindDecimal:
		return v.dec.Cmp(o.dec), nil
	case KindInt:
		switch {
		case v.i < o.i:
			return -1, nil
		case v.i > o.i:
			return 1, nil
		default:
			return 0, nil
		}
	default:
		return 0, fmt.Errorf("%w: %s has no declared order", ErrOperatorUnsupported, v.kind)
	}
}

// Column declares the name and type of one input or output slot in a table.
type Column struct {
	Name string
	Kind Kind
}

// Validate reports whether the column is well formed.
func (c Column) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("%w: empty column name", ErrColumnInvalid)
	}
	if !c.Kind.Valid() {
		return fmt.Errorf("%w: column %q has no declared kind", ErrColumnInvalid, c.Name)
	}
	if c.Kind == KindList {
		return fmt.Errorf("%w: LIST is expression-only and cannot be a decision-table column", ErrColumnInvalid)
	}
	return nil
}

// Operator is the comparison a condition applies to one column.
type Operator uint8

// Operators.
const (
	// OpUnspecified is the zero value and is never legal.
	OpUnspecified Operator = iota
	// OpAny is the wildcard: it matches every value of its column. It is the
	// typed equivalent of a dash in a decision-table cell.
	OpAny
	// OpEqual matches when the input equals the operand.
	OpEqual
	// OpNotEqual matches when the input does not equal the operand.
	OpNotEqual
	// OpLessThan matches when the input orders strictly below the operand.
	OpLessThan
	// OpLessOrEqual matches when the input orders at or below the operand.
	OpLessOrEqual
	// OpGreaterThan matches when the input orders strictly above the operand.
	OpGreaterThan
	// OpGreaterOrEqual matches when the input orders at or above the operand.
	OpGreaterOrEqual
	// OpIn matches when the input equals any of the operands.
	OpIn
	// OpNotIn matches when the input equals none of the operands.
	OpNotIn
	// OpBetween matches when the input orders within [Low, High] inclusive.
	OpBetween
)

var operatorWire = map[Operator]string{
	OpAny:            "ANY",
	OpEqual:          "EQUAL",
	OpNotEqual:       "NOT_EQUAL",
	OpLessThan:       "LESS_THAN",
	OpLessOrEqual:    "LESS_OR_EQUAL",
	OpGreaterThan:    "GREATER_THAN",
	OpGreaterOrEqual: "GREATER_OR_EQUAL",
	OpIn:             "IN",
	OpNotIn:          "NOT_IN",
	OpBetween:        "BETWEEN",
}

// String returns the stable wire token, or "OPERATOR_UNSPECIFIED".
func (o Operator) String() string {
	if s, ok := operatorWire[o]; ok {
		return s
	}
	return "OPERATOR_UNSPECIFIED"
}

// Valid reports whether o is a declared operator.
func (o Operator) Valid() bool { _, ok := operatorWire[o]; return ok }

// orderingOps is the set of operators that require a total order on their column.
var orderingOps = map[Operator]bool{
	OpLessThan:       true,
	OpLessOrEqual:    true,
	OpGreaterThan:    true,
	OpGreaterOrEqual: true,
	OpBetween:        true,
}

// Condition is one column's test in one row. Conditions in a Row are
// positional: Row.Conditions[i] tests Table.Inputs[i].
type Condition struct {
	Op       Operator
	Operand  Value   // used by EQUAL, NOT_EQUAL and the four ordering operators.
	Operands []Value // used by IN and NOT_IN.
	Low      Value   // used by BETWEEN, inclusive.
	High     Value   // used by BETWEEN, inclusive.
}

// Any returns the wildcard condition.
func Any() Condition { return Condition{Op: OpAny} }

// Equal returns a condition matching values equal to v.
func Equal(v Value) Condition { return Condition{Op: OpEqual, Operand: v} }

// NotEqual returns a condition matching values not equal to v.
func NotEqual(v Value) Condition { return Condition{Op: OpNotEqual, Operand: v} }

// LessThan returns a condition matching values strictly below v.
func LessThan(v Value) Condition { return Condition{Op: OpLessThan, Operand: v} }

// LessOrEqual returns a condition matching values at or below v.
func LessOrEqual(v Value) Condition { return Condition{Op: OpLessOrEqual, Operand: v} }

// GreaterThan returns a condition matching values strictly above v.
func GreaterThan(v Value) Condition { return Condition{Op: OpGreaterThan, Operand: v} }

// GreaterOrEqual returns a condition matching values at or above v.
func GreaterOrEqual(v Value) Condition { return Condition{Op: OpGreaterOrEqual, Operand: v} }

// In returns a condition matching any value equal to one of vs.
func In(vs ...Value) Condition { return Condition{Op: OpIn, Operands: vs} }

// NotIn returns a condition matching values equal to none of vs.
func NotIn(vs ...Value) Condition { return Condition{Op: OpNotIn, Operands: vs} }

// Between returns a condition matching values in [low, high] inclusive.
func Between(low, high Value) Condition { return Condition{Op: OpBetween, Low: low, High: high} }

// Validate reports whether the condition is well formed against the column it
// tests.
func (c Condition) Validate(col Column) error {
	if err := col.Validate(); err != nil {
		return err
	}
	if !c.Op.Valid() {
		return fmt.Errorf("%w: column %q", ErrOperatorUnspecified, col.Name)
	}
	if c.Op == OpAny {
		return nil
	}
	checkOperand := func(v Value) error {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("rules: column %q: %w", col.Name, err)
		}
		if v.Kind() != col.Kind {
			return fmt.Errorf("%w: column %q declares %s, condition carries %s",
				ErrKindMismatch, col.Name, col.Kind, v.Kind())
		}
		return nil
	}
	switch c.Op {
	case OpEqual, OpNotEqual, OpLessThan, OpLessOrEqual, OpGreaterThan, OpGreaterOrEqual:
		if err := checkOperand(c.Operand); err != nil {
			return err
		}
	case OpIn, OpNotIn:
		if len(c.Operands) == 0 {
			return fmt.Errorf("%w: column %q %s needs at least one operand", ErrOperandCount, col.Name, c.Op)
		}
		for _, o := range c.Operands {
			if err := checkOperand(o); err != nil {
				return err
			}
		}
	case OpBetween:
		if err := checkOperand(c.Low); err != nil {
			return err
		}
		if err := checkOperand(c.High); err != nil {
			return err
		}
	}
	if orderingOps[c.Op] && col.Kind != KindDecimal && col.Kind != KindInt {
		return fmt.Errorf("%w: column %q kind %s does not support %s", ErrOperatorUnsupported, col.Name, col.Kind, c.Op)
	}
	return nil
}

// Match reports whether input satisfies the condition. The caller must have
// already validated the condition against input's column; Match assumes the
// kinds already agree and surfaces a kind-mismatch error only as a defensive
// fallback.
func (c Condition) Match(input Value) (bool, error) {
	switch c.Op {
	case OpAny:
		return true, nil
	case OpEqual:
		return input.Equal(c.Operand)
	case OpNotEqual:
		eq, err := input.Equal(c.Operand)
		if err != nil {
			return false, err
		}
		return !eq, nil
	case OpLessThan:
		cmp, err := input.Cmp(c.Operand)
		if err != nil {
			return false, err
		}
		return cmp < 0, nil
	case OpLessOrEqual:
		cmp, err := input.Cmp(c.Operand)
		if err != nil {
			return false, err
		}
		return cmp <= 0, nil
	case OpGreaterThan:
		cmp, err := input.Cmp(c.Operand)
		if err != nil {
			return false, err
		}
		return cmp > 0, nil
	case OpGreaterOrEqual:
		cmp, err := input.Cmp(c.Operand)
		if err != nil {
			return false, err
		}
		return cmp >= 0, nil
	case OpIn:
		for _, o := range c.Operands {
			eq, err := input.Equal(o)
			if err != nil {
				return false, err
			}
			if eq {
				return true, nil
			}
		}
		return false, nil
	case OpNotIn:
		for _, o := range c.Operands {
			eq, err := input.Equal(o)
			if err != nil {
				return false, err
			}
			if eq {
				return false, nil
			}
		}
		return true, nil
	case OpBetween:
		low, err := input.Cmp(c.Low)
		if err != nil {
			return false, err
		}
		high, err := input.Cmp(c.High)
		if err != nil {
			return false, err
		}
		return low >= 0 && high <= 0, nil
	default:
		return false, fmt.Errorf("%w: %s", ErrOperatorUnspecified, c.Op)
	}
}

// signature is a stable text rendering of the condition used to detect
// literally-duplicate rows and to render an explanation trace. It is defined
// only for a condition that has already passed Validate.
func (c Condition) signature() string {
	switch c.Op {
	case OpAny:
		return "ANY"
	case OpIn, OpNotIn:
		parts := make([]string, len(c.Operands))
		for i, o := range c.Operands {
			parts[i] = o.String()
		}
		return c.Op.String() + "(" + strings.Join(parts, ",") + ")"
	case OpBetween:
		return c.Op.String() + "(" + c.Low.String() + "," + c.High.String() + ")"
	default:
		return c.Op.String() + "(" + c.Operand.String() + ")"
	}
}

// HitPolicy is a table's declared conflict strategy: how many matching rows
// are legal and which output(s) a match produces. It has no default; a table
// with no declared hit policy fails Validate rather than silently behaving
// like FIRST.
type HitPolicy uint8

// Hit policies.
const (
	// HitPolicyUnspecified is the zero value and is never legal.
	HitPolicyUnspecified HitPolicy = iota
	// HitPolicyFirst returns the first row, in declared order, whose
	// conditions all match. Rows after the winner are not evaluated: order is
	// the priority.
	HitPolicyFirst
	// HitPolicyUnique requires that at most one row match. Two or more matches
	// is StatusConflict, never a silently chosen row.
	HitPolicyUnique
	// HitPolicyCollect returns every matching row's outputs, in declared
	// order. It is the explicit strategy for a table whose rows may
	// legitimately overlap.
	HitPolicyCollect
)

var hitPolicyWire = map[HitPolicy]string{
	HitPolicyFirst:   "FIRST",
	HitPolicyUnique:  "UNIQUE",
	HitPolicyCollect: "COLLECT",
}

// String returns the stable wire token, or "HIT_POLICY_UNSPECIFIED".
func (h HitPolicy) String() string {
	if s, ok := hitPolicyWire[h]; ok {
		return s
	}
	return "HIT_POLICY_UNSPECIFIED"
}

// Valid reports whether h is a declared hit policy.
func (h HitPolicy) Valid() bool { _, ok := hitPolicyWire[h]; return ok }

// Row is one line of a decision table: a typed condition per input column and
// a typed output per output column. Conditions and Outputs are positional
// against the owning Table's Inputs and Outputs.
type Row struct {
	// ID is the row's stable identity. It is cited in every result and trace
	// that the row produces, so a decision can be audited back to the exact
	// row that made it without re-running the table.
	ID         string
	Conditions []Condition
	Outputs    []Value
}

// Table is one versioned decision table. Identity is (ID, Version):
// republishing a table with different rows, columns or hit policy is a new
// version, never an edit, because an evaluation cites the exact version it
// ran against.
//
// Rows are evaluated in declared order, and that order is never resorted by
// this package. Order is itself part of the table's published contract - it
// is what HitPolicyFirst's priority means - so a table that is evaluated
// twice, or evaluated on a different machine, always considers rows in the
// same sequence.
type Table struct {
	ID        string
	Version   string
	Inputs    []Column
	Outputs   []Column
	HitPolicy HitPolicy
	Rows      []Row
}

// Validate reports whether the table is internally consistent and safe to
// evaluate: identified and versioned, at least one input and output column,
// no duplicate column names, at least one row, every row shaped exactly like
// the declared columns and typed to match them, no duplicate row ids, and -
// unless the hit policy is explicitly COLLECT - no two rows declaring exactly
// the same conditions.
func (t Table) Validate() error {
	if t.ID == "" || t.Version == "" {
		return fmt.Errorf("%w: id %q version %q", ErrTableIdentity, t.ID, t.Version)
	}
	if len(t.Inputs) == 0 || len(t.Outputs) == 0 {
		return ErrTableColumns
	}
	if !t.HitPolicy.Valid() {
		return ErrHitPolicyUnspecified
	}
	if err := validateColumns(t.Inputs); err != nil {
		return fmt.Errorf("rules: table %s@%s: inputs: %w", t.ID, t.Version, err)
	}
	if err := validateColumns(t.Outputs); err != nil {
		return fmt.Errorf("rules: table %s@%s: outputs: %w", t.ID, t.Version, err)
	}
	if len(t.Rows) == 0 {
		return fmt.Errorf("%w: %s@%s", ErrTableEmpty, t.ID, t.Version)
	}

	rowIDs := make(map[string]bool, len(t.Rows))
	signatures := make(map[string]string, len(t.Rows))
	for i, row := range t.Rows {
		if row.ID == "" {
			return fmt.Errorf("%w: table %s@%s row %d has no id", ErrRowInvalid, t.ID, t.Version, i)
		}
		if rowIDs[row.ID] {
			return fmt.Errorf("%w: %s in table %s@%s", ErrDuplicateRowID, row.ID, t.ID, t.Version)
		}
		rowIDs[row.ID] = true

		if len(row.Conditions) != len(t.Inputs) {
			return fmt.Errorf("%w: row %s has %d conditions, table declares %d input columns",
				ErrRowInvalid, row.ID, len(row.Conditions), len(t.Inputs))
		}
		sig := make([]string, len(row.Conditions))
		for ci, cond := range row.Conditions {
			if err := cond.Validate(t.Inputs[ci]); err != nil {
				return fmt.Errorf("rules: row %s: %w", row.ID, err)
			}
			sig[ci] = cond.signature()
		}

		if len(row.Outputs) != len(t.Outputs) {
			return fmt.Errorf("%w: row %s has %d outputs, table declares %d output columns",
				ErrRowInvalid, row.ID, len(row.Outputs), len(t.Outputs))
		}
		for oi, out := range row.Outputs {
			if err := out.Validate(); err != nil {
				return fmt.Errorf("rules: row %s output %q: %w", row.ID, t.Outputs[oi].Name, err)
			}
			if out.Kind() != t.Outputs[oi].Kind {
				return fmt.Errorf("%w: row %s output %q declares %s, table declares %s",
					ErrKindMismatch, row.ID, t.Outputs[oi].Name, out.Kind(), t.Outputs[oi].Kind)
			}
		}

		if t.HitPolicy != HitPolicyCollect {
			key := strings.Join(sig, "|")
			if prior, dup := signatures[key]; dup {
				return fmt.Errorf("%w: %s and %s in table %s@%s", ErrAmbiguousRows, prior, row.ID, t.ID, t.Version)
			}
			signatures[key] = row.ID
		}
	}
	return nil
}

func validateColumns(cols []Column) error {
	seen := make(map[string]bool, len(cols))
	for _, c := range cols {
		if err := c.Validate(); err != nil {
			return err
		}
		if seen[c.Name] {
			return fmt.Errorf("%w: %q", ErrDuplicateColumn, c.Name)
		}
		seen[c.Name] = true
	}
	return nil
}

// Canonical returns the canonical byte encoding of the table, or nil when the
// table fails Validate. Two tables with the same id, version, columns, hit
// policy and rows always produce identical bytes; any difference in any of
// those changes the bytes.
func (t Table) Canonical() []byte {
	if err := t.Validate(); err != nil {
		return nil
	}
	w := canonicalbytes.New(tableSchema, schemaVersion).
		String("id", t.ID).
		String("version", t.Version).
		String("hit_policy", t.HitPolicy.String()).
		Count("inputs", len(t.Inputs))
	for _, c := range t.Inputs {
		w.String("input.name", c.Name).String("input.kind", c.Kind.String())
	}
	w.Count("outputs", len(t.Outputs))
	for _, c := range t.Outputs {
		w.String("output.name", c.Name).String("output.kind", c.Kind.String())
	}
	w.Count("rows", len(t.Rows))
	for _, row := range t.Rows {
		w.String("row.id", row.ID)
		for _, cond := range row.Conditions {
			w.String("row.condition", cond.signature())
		}
		for _, out := range row.Outputs {
			w.String("row.output", out.String())
		}
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns "sha256:<hex>" over the table's canonical bytes: the version
// identity a citing evaluation carries. It fails when the table itself fails
// Validate, because an unversioned or malformed table cannot be cited as
// evidence.
func (t Table) Digest() (string, error) {
	raw := t.Canonical()
	if raw == nil {
		return "", fmt.Errorf("rules: table %s@%s is invalid, refusing to digest it", t.ID, t.Version)
	}
	return canonicalbytes.Digest(raw), nil
}

// Compile is the table validation step: it runs the same structural checks
// as Table.Validate - identity, columns, hit policy, rows shaped and typed
// to match the declared columns, no duplicate row ids, and no two rows
// declaring the same conditions under a non-COLLECT hit policy - and
// returns table unchanged when it passes.
//
// rules has no separate compiled representation the way an
// expression-language engine would: a decision table's rows are already
// the literal, typed truth Evaluate walks, so there is nothing to lower
// them into. What Compile adds is a named, standalone validate-then-cite
// entry point a table publisher can call once at publish time, instead of
// deferring the discovery of a malformed table to the first Evaluate call
// against it - the ARCH-GO-009 counterpart to Evaluate.
func Compile(table Table) (Table, error) {
	if err := table.Validate(); err != nil {
		return Table{}, fmt.Errorf("rules: compile: %w", err)
	}
	return table, nil
}

// Status is the honest outcome of an evaluation.
type Status uint8

// Statuses.
const (
	// StatusUnspecified is the zero value and is never a legal result.
	StatusUnspecified Status = iota
	// StatusMatched means the hit policy produced exactly the result it
	// promises: one row for FIRST/UNIQUE, one or more for COLLECT.
	StatusMatched
	// StatusUnknown means no row matched. This is a gap in the table's
	// coverage for this input, reported honestly rather than defaulted.
	StatusUnknown
	// StatusConflict means a UNIQUE table had more than one row match the
	// same input. This is an ambiguity the table failed to prevent for this
	// specific input; it is reported rather than resolved by row order.
	StatusConflict
)

var statusWire = map[Status]string{
	StatusMatched:  "MATCHED",
	StatusUnknown:  "UNKNOWN",
	StatusConflict: "CONFLICT",
}

// String returns the stable wire token, or "STATUS_UNSPECIFIED".
func (s Status) String() string {
	if w, ok := statusWire[s]; ok {
		return w
	}
	return "STATUS_UNSPECIFIED"
}

// Valid reports whether s is a legal status.
func (s Status) Valid() bool { _, ok := statusWire[s]; return ok }

// Match is one matching row's cited identity and typed outputs.
type Match struct {
	RowID   string
	Outputs []Value
}

// ColumnTrace is the explanation of one column's test against one row.
type ColumnTrace struct {
	Column   string
	Operator string
	// Condition is a stable text rendering of the condition that was tested,
	// e.g. "GREATER_THAN(10.0000)" or "ANY".
	Condition string
	// Input is the text rendering of the input value that was tested against it.
	Input   string
	Matched bool
}

// RowTrace is the explanation of one row's evaluation: whether it matched and
// why, column by column.
type RowTrace struct {
	RowID   string
	Matched bool
	Columns []ColumnTrace
}

// Result is the engine's whole answer: the table version it ran against, the
// honest status, every matching row's outputs in declared order, and a full
// per-row explanation trace. A Result is self-describing: it never needs the
// table re-resolved to be audited.
type Result struct {
	TableID      string
	TableVersion string
	// TableDigest is the digest of the exact table version this result was
	// computed against.
	TableDigest string
	Status      Status
	// MatchedRowIDs is every row id that matched, in declared table order.
	// For HitPolicyFirst this has at most one entry, because evaluation stops
	// at the first match.
	MatchedRowIDs []string
	// Matches carries each matched row's outputs, in the same order as
	// MatchedRowIDs.
	Matches []Match
	// Trace is one entry per row that was actually evaluated, in declared
	// table order. HitPolicyFirst stops at the winning row, so rows after it
	// carry no trace entry; HitPolicyUnique and HitPolicyCollect always
	// evaluate every row, because every row's participation is part of the
	// conflict/collection answer.
	Trace []RowTrace
}

// Evaluate runs table against inputs and returns the honest result.
//
// inputs must carry exactly one value per declared input column, addressed by
// column name, and each value's kind must match its column's declared kind.
// A missing input or a column the table does not declare is a caller defect
// and is returned as an error; it is never treated as a wildcard or silently
// ignored, because a rule engine that guesses at a missing value is how a
// missing raise percentage becomes "no finance approval required."
//
// Evaluate is a pure function: no clock, no map iteration that affects the
// result (inputs are looked up by the table's own declared column order, not
// ranged over), and two calls with the same table and the same inputs always
// produce byte-identical Canonical() results.
func Evaluate(table Table, inputs map[string]Value) (Result, error) {
	if err := table.Validate(); err != nil {
		return Result{}, fmt.Errorf("rules: table: %w", err)
	}

	ordered := make([]Value, len(table.Inputs))
	for i, col := range table.Inputs {
		v, ok := inputs[col.Name]
		if !ok {
			return Result{}, fmt.Errorf("%w: %q", ErrMissingInput, col.Name)
		}
		if err := v.Validate(); err != nil {
			return Result{}, fmt.Errorf("rules: input %q: %w", col.Name, err)
		}
		if v.Kind() != col.Kind {
			return Result{}, fmt.Errorf("%w: column %q declares %s, input carries %s",
				ErrKindMismatch, col.Name, col.Kind, v.Kind())
		}
		ordered[i] = v
	}
	if len(inputs) != len(table.Inputs) {
		for name := range inputs {
			found := false
			for _, col := range table.Inputs {
				if col.Name == name {
					found = true
					break
				}
			}
			if !found {
				return Result{}, fmt.Errorf("%w: %q", ErrUnknownInput, name)
			}
		}
	}

	digest, err := table.Digest()
	if err != nil {
		return Result{}, fmt.Errorf("rules: %w", err)
	}

	result := Result{TableID: table.ID, TableVersion: table.Version, TableDigest: digest}

	for _, row := range table.Rows {
		rt := RowTrace{RowID: row.ID, Matched: true}
		for ci, cond := range row.Conditions {
			ok, err := cond.Match(ordered[ci])
			if err != nil {
				return Result{}, fmt.Errorf("rules: row %s: column %q: %w", row.ID, table.Inputs[ci].Name, err)
			}
			rt.Columns = append(rt.Columns, ColumnTrace{
				Column:    table.Inputs[ci].Name,
				Operator:  cond.Op.String(),
				Condition: cond.signature(),
				Input:     ordered[ci].String(),
				Matched:   ok,
			})
			if !ok {
				rt.Matched = false
			}
		}
		result.Trace = append(result.Trace, rt)
		if rt.Matched {
			result.MatchedRowIDs = append(result.MatchedRowIDs, row.ID)
			result.Matches = append(result.Matches, Match{RowID: row.ID, Outputs: append([]Value(nil), row.Outputs...)})
			if table.HitPolicy == HitPolicyFirst {
				break
			}
		}
	}

	switch table.HitPolicy {
	case HitPolicyFirst, HitPolicyCollect:
		if len(result.Matches) == 0 {
			result.Status = StatusUnknown
		} else {
			result.Status = StatusMatched
		}
	case HitPolicyUnique:
		switch len(result.Matches) {
		case 0:
			result.Status = StatusUnknown
		case 1:
			result.Status = StatusMatched
		default:
			result.Status = StatusConflict
		}
	}
	return result, nil
}

// Explain renders a human-readable narrative of the result: the table
// version it ran against, the honest status, which rows matched, and a
// column-by-column trace of why each traced row did or did not match. It is
// meant for audit logs and review screens; branch on Status and Matches for
// anything programmatic.
func (r Result) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "table %s@%s -> %s", r.TableID, r.TableVersion, r.Status)
	if len(r.MatchedRowIDs) > 0 {
		fmt.Fprintf(&b, " (rows: %s)", strings.Join(r.MatchedRowIDs, ", "))
	}
	for _, rt := range r.Trace {
		fmt.Fprintf(&b, "\n  row %s: matched=%t", rt.RowID, rt.Matched)
		for _, ct := range rt.Columns {
			fmt.Fprintf(&b, "\n    %s %s %s vs input %s -> %t", ct.Column, ct.Operator, ct.Condition, ct.Input, ct.Matched)
		}
	}
	return b.String()
}
