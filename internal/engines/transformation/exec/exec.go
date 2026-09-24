// Package exec is a deterministic interpreter for a compiled
// transformation/ir.Program. It never reads the wall clock, never consumes
// randomness, and never executes anything outside the closed IR instruction
// set: a Program's own Validate is the only gate for what this package will
// run, and every declared resource limit (row count, per-instruction
// fan-out, total output size, and a total step budget across the whole
// dataset) is enforced before it can be exceeded. A breach never returns a
// partial result -- it returns a typed Refusal naming the offending step and
// nothing else.
package exec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/ir"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrRefused is the sentinel every Refusal unwraps to.
var ErrRefused = errors.New("XFORM_003_REFUSED")

// ErrExecution covers a program-shape problem discovered while building an
// Interpreter (never during a row's execution: those become a Refusal).
var ErrExecution = errors.New("transformation/exec: invalid program")

// Version reports the transformation executor contract version required by
// ARCH-GO-009. It changes only when this package's exported execution
// contract changes incompatibly.
func Version() int { return 1 }

// Explain describes the executor without exposing implementation details such
// as the decimal backend.
func Explain() string { return "transformation executor: validated deterministic IR execution" }

var decimalRE = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?$`)

// Value is one typed, presence-tracked property. A non-VALUE state
// deliberately carries no Data: there is no representation in this package
// for "ABSENT but also holds a zero value," which is exactly the class of
// defect (ABSENT silently becoming NULL, or a zero value) the RED criterion
// for XFORM-003 names.
type Value struct {
	Type   transformation.Type
	State  values.PresenceState
	Data   any
	Reason string
}

// Present builds a VALUE-state property.
func Present(t transformation.Type, data any) Value {
	return Value{Type: t, State: values.PresenceValue, Data: data}
}

// NonValue builds any non-VALUE property. Passing values.PresenceValue here
// panics: use Present for that state so a value is never separated from its
// declared state by construction.
func NonValue(t transformation.Type, state values.PresenceState, reason string) Value {
	if state == values.PresenceValue {
		panic("exec: NonValue called with PresenceValue")
	}
	return Value{Type: t, State: state, Reason: reason}
}

func (v Value) validate() error {
	if !v.State.Valid() {
		return errors.New("invalid presence state")
	}
	if v.State != values.PresenceValue && v.Data != nil {
		return errors.New("non-VALUE property carries data")
	}
	return nil
}

// Record is one row: every property keyed by "Schema.Field", matching how
// transformation.Path and the IR's own dependency keys are spelled. Keying by
// schema qualifies a program whose instructions read from more than one
// named schema (join_by_key's two sources, or a later instruction reading an
// earlier instruction's destination) without ambiguity.
type Record map[string]Value

func pathKey(p transformation.Path) string { return p.Schema + "." + p.Field }

// Limits are the resource bounds Execute enforces beyond the Program's own
// declared IR limits (step count per program, fan-out per instruction):
// MaxRows bounds the dataset itself, MaxSteps bounds the total number of
// instruction executions across the whole dataset (rows x instructions is
// not, by itself, bounded by the IR's own per-program MaxSteps), and
// MaxOutputBytes bounds the canonically-encoded result. A non-positive field
// means "no additional bound" for that dimension.
type Limits struct {
	MaxRows        int
	MaxSteps       int
	MaxOutputBytes int64
}

// Refusal is stable, machine-readable refusal metadata naming the exact step
// (a row/instruction coordinate, "dataset", or "output") that breached a
// declared limit or hit an unresolved/mistyped value.
type Refusal struct {
	Code   string `json:"code"`
	Step   string `json:"step"`
	Reason string `json:"reason"`
}

func (r Refusal) Error() string {
	return fmt.Sprintf("%s step=%q: %s", r.Code, r.Step, r.Reason)
}
func (r Refusal) Unwrap() error { return ErrRefused }

func refuse(step string, err error) error {
	return Refusal{Code: "XFORM_003_REFUSED", Step: step, Reason: err.Error()}
}
func refuseString(step, reason string) error {
	return Refusal{Code: "XFORM_003_REFUSED", Step: step, Reason: reason}
}

// Interpreter is a validated Program plus its precomputed, deterministic
// execution order (dependencies before dependents). Building it once and
// reusing it across many Execute calls avoids recomputing that order per
// row; it holds no mutable state, so one Interpreter is safe to share and
// call from many goroutines at once.
type Interpreter struct {
	program ir.Program
	order   []int
}

// New validates p and precomputes its execution order. It fails exactly
// when p.Validate() would fail, plus the structurally-impossible-after-
// Validate case of a dependency cycle (kept as a defensive, never-taken
// path rather than a panic).
func New(p ir.Program) (*Interpreter, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	destOf := make(map[string]int, len(p.Instructions))
	for i, instr := range p.Instructions {
		destOf[pathKey(instr.Destination)] = i
	}
	order, err := topoOrder(p.Instructions, destOf)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrExecution, err)
	}
	return &Interpreter{program: p, order: order}, nil
}

// Program returns the compiled program this Interpreter executes.
func (in *Interpreter) Program() ir.Program { return in.program }

func topoOrder(instructions []ir.Instruction, destOf map[string]int) ([]int, error) {
	n := len(instructions)
	adj := make([][]int, n)
	for i, instr := range instructions {
		for _, s := range instr.Sources {
			if j, ok := destOf[pathKey(s)]; ok && j != i {
				adj[i] = append(adj[i], j)
			}
		}
		sort.Ints(adj[i])
	}
	const (
		white = iota
		gray
		black
	)
	color := make([]int, n)
	order := make([]int, 0, n)
	var visit func(i int) error
	visit = func(i int) error {
		color[i] = gray
		for _, j := range adj[i] {
			switch color[j] {
			case gray:
				return fmt.Errorf("dependency cycle at instruction %d", i)
			case white:
				if err := visit(j); err != nil {
					return err
				}
			}
		}
		color[i] = black
		order = append(order, i)
		return nil
	}
	for i := 0; i < n; i++ {
		if color[i] == white {
			if err := visit(i); err != nil {
				return nil, err
			}
		}
	}
	return order, nil
}

// Execute is the package-level convenience form of New(p).Execute(dataset,
// limits) for a Program used only once.
func Execute(p ir.Program, dataset []Record, limits Limits) ([]Record, error) {
	in, err := New(p)
	if err != nil {
		return nil, err
	}
	return in.Execute(dataset, limits)
}

// Execute runs every row through the interpreter's precomputed instruction
// order. A refusal on any row (or on the dataset/output bounds themselves)
// returns nil and that Refusal -- never a partial result.
func (in *Interpreter) Execute(dataset []Record, limits Limits) ([]Record, error) {
	if limits.MaxRows > 0 && len(dataset) > limits.MaxRows {
		return nil, refuseString("dataset", fmt.Sprintf("row count %d exceeds declared max_rows %d", len(dataset), limits.MaxRows))
	}
	out := make([]Record, len(dataset))
	steps := 0
	for i, row := range dataset {
		r, err := in.executeRow(row, fmt.Sprintf("row %d", i), &steps, limits)
		if err != nil {
			return nil, err
		}
		out[i] = r
	}
	b, err := canonicalDataset(out)
	if err != nil {
		return nil, refuse("output", err)
	}
	if limits.MaxOutputBytes > 0 && int64(len(b)) > limits.MaxOutputBytes {
		return nil, refuseString("output", fmt.Sprintf("output size %d exceeds declared max_output_bytes %d", len(b), limits.MaxOutputBytes))
	}
	return out, nil
}

func (in *Interpreter) executeRow(input Record, rowLabel string, steps *int, limits Limits) (Record, error) {
	for k, v := range input {
		if err := v.validate(); err != nil {
			return nil, refuseString(rowLabel+" input "+k, err.Error())
		}
	}
	work := make(Record, len(input)+len(in.program.Instructions))
	for k, v := range input {
		work[k] = v
	}
	for _, idx := range in.order {
		instr := in.program.Instructions[idx]
		step := fmt.Sprintf("%s instruction %d (%s %s)", rowLabel, idx, instr.Op, pathKey(instr.Destination))
		if len(instr.Sources) > in.program.Limits.MaxFanOut {
			return nil, refuseString(step, fmt.Sprintf("fan_out %d exceeds declared max_fan_out %d", len(instr.Sources), in.program.Limits.MaxFanOut))
		}
		*steps++
		if limits.MaxSteps > 0 && *steps > limits.MaxSteps {
			return nil, refuseString(step, fmt.Sprintf("step budget exceeded: step %d > declared max_steps %d", *steps, limits.MaxSteps))
		}
		v, err := execInstruction(instr, work)
		if err != nil {
			return nil, refuse(step, err)
		}
		work[pathKey(instr.Destination)] = v
	}
	result := make(Record, len(in.program.Instructions))
	for _, instr := range in.program.Instructions {
		result[pathKey(instr.Destination)] = work[pathKey(instr.Destination)]
	}
	return result, nil
}

func lookup(work Record, p transformation.Path) (Value, error) {
	v, ok := work[pathKey(p)]
	if !ok {
		return NonValue(p.Type, values.PresenceAbsent, ""), nil
	}
	if v.Type != p.Type {
		return Value{}, fmt.Errorf("type mismatch at %s: have %s want %s", pathKey(p), v.Type, p.Type)
	}
	if err := v.validate(); err != nil {
		return Value{}, fmt.Errorf("%s: %w", pathKey(p), err)
	}
	return v, nil
}

func passthrough(v Value, destType transformation.Type) Value {
	if v.State == values.PresenceValue {
		return Present(destType, v.Data)
	}
	return NonValue(destType, v.State, v.Reason)
}

func execInstruction(instr ir.Instruction, work Record) (Value, error) {
	switch instr.Op {
	case ir.OpProject:
		v, err := lookup(work, instr.Sources[0])
		if err != nil {
			return Value{}, err
		}
		return passthrough(v, instr.Destination.Type), nil
	case ir.OpCoerce:
		v, err := lookup(work, instr.Sources[0])
		if err != nil {
			return Value{}, err
		}
		if v.State != values.PresenceValue {
			return passthrough(v, instr.Destination.Type), nil
		}
		data, err := coerce(v.Data, v.Type, instr.TargetType)
		if err != nil {
			return Value{}, err
		}
		return Present(instr.Destination.Type, data), nil
	case ir.OpMap:
		return execMap(instr, work)
	case ir.OpFilter:
		return execFilter(instr, work)
	case ir.OpAggregate:
		return execAggregate(instr, work)
	case ir.OpJoinByKey:
		return execJoinByKey(instr, work)
	default:
		return Value{}, fmt.Errorf("opcode %q is not executable", instr.Op)
	}
}

func execMap(instr ir.Instruction, work Record) (Value, error) {
	if len(instr.Sources) == 0 {
		if instr.Function != ir.FuncDefault {
			return Value{}, fmt.Errorf("function %q requires a source", instr.Function)
		}
		data, err := coerce(instr.Literal, transformation.TypeString, instr.Destination.Type)
		if err != nil {
			return Value{}, err
		}
		return Present(instr.Destination.Type, data), nil
	}
	v, err := lookup(work, instr.Sources[0])
	if err != nil {
		return Value{}, err
	}
	if v.State == values.PresenceValue {
		if instr.Function != ir.FuncDefault {
			text, ok := v.Data.(string)
			if !ok {
				return Value{}, fmt.Errorf("function %q source is not string", instr.Function)
			}
			mapped, err := applySharedMap(instr, text)
			if err != nil {
				return Value{}, err
			}
			data, err := coerce(mapped, transformation.TypeString, instr.Destination.Type)
			if err != nil {
				return Value{}, err
			}
			return Present(instr.Destination.Type, data), nil
		}
		return passthrough(v, instr.Destination.Type), nil
	}
	if instr.Function != ir.FuncDefault {
		return passthrough(v, instr.Destination.Type), nil
	}
	if instr.Literal == "" {
		return passthrough(v, instr.Destination.Type), nil
	}
	data, err := coerce(instr.Literal, transformation.TypeString, instr.Destination.Type)
	if err != nil {
		return Value{}, err
	}
	return Present(instr.Destination.Type, data), nil
}

var sharedMoneyPattern = regexp.MustCompile(`^(?:(?P<pre>[A-Z]{3})\s)?\$?(?P<sign>-)?(?P<int>[0-9]{1,3}(?:,[0-9]{3})*|[0-9]+)(?:\.(?P<frac>[0-9]+))?(?:\s(?P<post>[A-Z]{3}))?$`)
var connectivityMoneyPattern = regexp.MustCompile(`^-?[0-9]+(\.[0-9]{1,2})?$`)

func applySharedMap(instr ir.Instruction, text string) (string, error) {
	switch instr.Function {
	case ir.FuncTrim:
		return strings.TrimSpace(text), nil
	case ir.FuncUpper:
		return strings.ToUpper(strings.TrimSpace(text)), nil
	case ir.FuncLower:
		return strings.ToLower(strings.TrimSpace(text)), nil
	case ir.FuncLookup:
		v, ok := instr.Lookup[strings.TrimSpace(text)]
		if !ok {
			return "", errors.New("lookup unresolved")
		}
		return v, nil
	case ir.FuncCompose:
		return strings.ReplaceAll(instr.Literal, "${value}", text), nil
	case ir.FuncDateParse:
		t, err := time.Parse(instr.Literal, strings.TrimSpace(text))
		if err != nil {
			return "", err
		}
		return t.UTC().Format(time.RFC3339Nano), nil
	case ir.FuncMoneyParse:
		text = strings.TrimSpace(text)
		parts := strings.SplitN(instr.Literal, ":", 2)
		if len(parts) != 2 {
			return "", errors.New("invalid money mode")
		}
		switch parts[0] {
		case "DATAOPS":
			m := sharedMoneyPattern.FindStringSubmatch(text)
			if m == nil {
				return "", errors.New("invalid money")
			}
			names := sharedMoneyPattern.SubexpNames()
			pre, post, sign, whole, frac := "", "", "", "", ""
			for i, n := range names {
				switch n {
				case "pre":
					pre = m[i]
				case "post":
					post = m[i]
				case "sign":
					sign = m[i]
				case "int":
					whole = m[i]
				case "frac":
					frac = m[i]
				}
			}
			if (pre != "" && post != "") || (pre != "" && pre != parts[1]) || (post != "" && post != parts[1]) {
				return "", errors.New("currency mismatch")
			}
			whole = strings.ReplaceAll(whole, ",", "")
			if frac == "" {
				return sign + whole, nil
			}
			return sign + whole + "." + frac, nil
		case "CONNECTIVITY":
			s := strings.ReplaceAll(text, ",", "")
			if strings.HasPrefix(s, parts[1]) {
				s = strings.TrimSpace(strings.TrimPrefix(s, parts[1]))
			}
			if strings.ContainsAny(s, "$€£") || !connectivityMoneyPattern.MatchString(s) {
				return "", errors.New("invalid money")
			}
			return s, nil
		case "PROFILE":
			s := strings.ReplaceAll(strings.TrimSpace(text), ",", "")
			if !connectivityMoneyPattern.MatchString(s) {
				return "", errors.New("invalid money")
			}
			return s, nil
		}
	}
	return "", fmt.Errorf("function %q is not defined for map", instr.Function)
}

func execFilter(instr ir.Instruction, work Record) (Value, error) {
	v, err := lookup(work, instr.Sources[0])
	if err != nil {
		return Value{}, err
	}
	switch instr.Function {
	case ir.FuncNotNull:
		if v.State == values.PresenceValue {
			return passthrough(v, instr.Destination.Type), nil
		}
		return NonValue(instr.Destination.Type, values.PresenceAbsent, "filtered: not_null"), nil
	case ir.FuncEquals:
		if v.State != values.PresenceValue {
			return NonValue(instr.Destination.Type, v.State, v.Reason), nil
		}
		if fmt.Sprint(v.Data) != instr.Literal {
			return NonValue(instr.Destination.Type, values.PresenceAbsent, "filtered: not equal"), nil
		}
		return passthrough(v, instr.Destination.Type), nil
	default:
		return Value{}, fmt.Errorf("function %q is not defined for filter", instr.Function)
	}
}

// execJoinByKey is this package's chosen semantics for an opcode that the
// current XFORM-001 operation vocabulary never asks Compile to emit (see
// ir.TestValidateAcceptsFilterAndJoinByKey): with no compiled definition to
// observe, "combine two source fields on a declared join key" is
// implemented as an equality confirmation -- both sources must resolve to
// the same declared type and the same VALUE, and the JoinKey is carried
// only as descriptive metadata in the refusal/explain text, never as a
// comparison operand. Two sources that disagree produce an honest UNKNOWN
// rather than an invented tie-break, matching this codebase's convention of
// never silently picking a winner between disagreeing sources.
func execJoinByKey(instr ir.Instruction, work Record) (Value, error) {
	a, err := lookup(work, instr.Sources[0])
	if err != nil {
		return Value{}, err
	}
	b, err := lookup(work, instr.Sources[1])
	if err != nil {
		return Value{}, err
	}
	if a.State != values.PresenceValue {
		return NonValue(instr.Destination.Type, a.State, a.Reason), nil
	}
	if b.State != values.PresenceValue {
		return NonValue(instr.Destination.Type, b.State, b.Reason), nil
	}
	if a.Type != b.Type {
		return Value{}, fmt.Errorf("join_by_key %q sources have mixed types %s and %s", instr.JoinKey, a.Type, b.Type)
	}
	if instr.Destination.Type != a.Type {
		return Value{}, fmt.Errorf("join_by_key %q destination type %s does not match source type %s", instr.JoinKey, instr.Destination.Type, a.Type)
	}
	if fmt.Sprint(a.Data) != fmt.Sprint(b.Data) {
		return NonValue(instr.Destination.Type, values.PresenceUnknown, fmt.Sprintf("join_by_key %q: sources disagree", instr.JoinKey)), nil
	}
	return Present(instr.Destination.Type, a.Data), nil
}

func execAggregate(instr ir.Instruction, work Record) (Value, error) {
	vals := make([]Value, len(instr.Sources))
	for i, s := range instr.Sources {
		v, err := lookup(work, s)
		if err != nil {
			return Value{}, err
		}
		vals[i] = v
	}
	switch instr.Function {
	case ir.FuncCount:
		if instr.Destination.Type != transformation.TypeInt {
			return Value{}, fmt.Errorf("count destination must be int, got %s", instr.Destination.Type)
		}
		var n int64
		for _, v := range vals {
			if v.State == values.PresenceValue {
				n++
			}
		}
		return Present(instr.Destination.Type, n), nil
	case ir.FuncConcat:
		if instr.Destination.Type != transformation.TypeString {
			return Value{}, fmt.Errorf("concat destination must be string, got %s", instr.Destination.Type)
		}
		var b strings.Builder
		for _, v := range vals {
			if v.State != values.PresenceValue {
				return NonValue(instr.Destination.Type, v.State, v.Reason), nil
			}
			s, ok := v.Data.(string)
			if !ok {
				return Value{}, errors.New("concat source is not a string")
			}
			b.WriteString(s)
		}
		return Present(instr.Destination.Type, b.String()), nil
	case ir.FuncSum, ir.FuncMin, ir.FuncMax:
		return aggregateNumeric(instr, vals)
	default:
		return Value{}, fmt.Errorf("function %q is not defined for aggregate", instr.Function)
	}
}

func aggregateNumeric(instr ir.Instruction, vals []Value) (Value, error) {
	for _, v := range vals {
		if v.State != values.PresenceValue {
			return NonValue(instr.Destination.Type, v.State, v.Reason), nil
		}
	}
	t := vals[0].Type
	for _, v := range vals[1:] {
		if v.Type != t {
			return Value{}, fmt.Errorf("aggregate sources have mixed types %s and %s", t, v.Type)
		}
	}
	if instr.Destination.Type != t {
		return Value{}, fmt.Errorf("aggregate destination type %s does not match source type %s", instr.Destination.Type, t)
	}
	switch instr.Function {
	case ir.FuncSum:
		switch t {
		case transformation.TypeInt:
			sum := big.NewInt(0)
			for _, v := range vals {
				n, ok := v.Data.(int64)
				if !ok {
					return Value{}, errors.New("sum source is not int64")
				}
				sum.Add(sum, big.NewInt(n))
			}
			if !sum.IsInt64() {
				return Value{}, fmt.Errorf("sum %s overflows int64", sum.String())
			}
			return Present(t, sum.Int64()), nil
		case transformation.TypeDecimal:
			decimals := make([]values.Decimal, len(vals))
			maxScale := int32(0)
			for i, v := range vals {
				s, ok := v.Data.(string)
				if !ok || !decimalRE.MatchString(s) {
					return Value{}, fmt.Errorf("sum source is not decimal text: %v", v.Data)
				}
				d, err := executionDecimal(s)
				if err != nil {
					return Value{}, err
				}
				decimals[i] = d
				if d.Scale() > maxScale {
					maxScale = d.Scale()
				}
			}
			var total values.Decimal
			for i, d := range decimals {
				aligned, err := d.Quantize(maxScale, values.RoundingExactRequired)
				if err != nil {
					return Value{}, err
				}
				if i == 0 {
					total = aligned
					continue
				}
				total, err = total.Add(aligned)
				if err != nil {
					return Value{}, err
				}
			}
			return Present(t, total.String()), nil
		default:
			return Value{}, fmt.Errorf("sum is not defined for type %s", t)
		}
	case ir.FuncMin, ir.FuncMax:
		best := vals[0]
		for _, v := range vals[1:] {
			cmp, err := compareValues(t, best.Data, v.Data)
			if err != nil {
				return Value{}, err
			}
			if (instr.Function == ir.FuncMin && cmp > 0) || (instr.Function == ir.FuncMax && cmp < 0) {
				best = v
			}
		}
		return Present(t, best.Data), nil
	default:
		return Value{}, fmt.Errorf("unreachable aggregate function %q", instr.Function)
	}
}

func compareValues(t transformation.Type, a, b any) (int, error) {
	switch t {
	case transformation.TypeInt:
		x, ok1 := a.(int64)
		y, ok2 := b.(int64)
		if !ok1 || !ok2 {
			return 0, errors.New("min/max source is not int64")
		}
		switch {
		case x < y:
			return -1, nil
		case x > y:
			return 1, nil
		default:
			return 0, nil
		}
	case transformation.TypeDecimal:
		xs, ok1 := a.(string)
		ys, ok2 := b.(string)
		if !ok1 || !ok2 || !decimalRE.MatchString(xs) || !decimalRE.MatchString(ys) {
			return 0, errors.New("min/max source is not decimal text")
		}
		x, err := executionDecimal(xs)
		if err != nil {
			return 0, err
		}
		y, err := executionDecimal(ys)
		if err != nil {
			return 0, err
		}
		return x.Cmp(y), nil
	case transformation.TypeString, transformation.TypeDate, transformation.TypeTimestamp:
		xs, ok1 := a.(string)
		ys, ok2 := b.(string)
		if !ok1 || !ok2 {
			return 0, errors.New("min/max source is not string-comparable")
		}
		return strings.Compare(xs, ys), nil
	default:
		return 0, fmt.Errorf("min/max is not defined for type %s", t)
	}
}

func executionDecimal(text string) (values.Decimal, error) {
	scale := int32(0)
	if dot := strings.IndexByte(text, '.'); dot >= 0 {
		scale = int32(len(text) - dot - 1)
	}
	return values.NewDecimal(text, scale, values.RoundingExactRequired)
}

// coerce is a self-contained type conversion, deliberately independent of
// any sibling package's own converter: exec owns exactly this behavior for
// programs it executes, so a change made under a different file root can
// never silently change what this package's replay-stable digest means.
func coerce(data any, from, to transformation.Type) (any, error) {
	if from == to {
		return data, nil
	}
	s := fmt.Sprint(data)
	switch to {
	case transformation.TypeString:
		return s, nil
	case transformation.TypeInt:
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			return nil, fmt.Errorf("invalid int %q", s)
		}
		if !n.IsInt64() {
			return nil, fmt.Errorf("int out of range %q", s)
		}
		return n.Int64(), nil
	case transformation.TypeDecimal:
		if !decimalRE.MatchString(s) {
			return nil, fmt.Errorf("invalid decimal %q", s)
		}
		return s, nil
	case transformation.TypeDate:
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return nil, err
		}
		return s, nil
	case transformation.TypeTimestamp:
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, err
		}
		return t.UTC().Format(time.RFC3339Nano), nil
	case transformation.TypeBool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return nil, err
		}
		return b, nil
	default:
		return nil, fmt.Errorf("coerce target type %q is not declared", to)
	}
}

type wireValue struct {
	Type   transformation.Type `json:"type"`
	State  string              `json:"state"`
	Data   any                 `json:"data,omitempty"`
	Reason string              `json:"reason,omitempty"`
}

func canonicalRecord(r Record) ([]byte, error) {
	keys := make([]string, 0, len(r))
	for k := range r {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	m := make(map[string]wireValue, len(r))
	for _, k := range keys {
		v := r[k]
		if err := v.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		m[k] = wireValue{v.Type, v.State.String(), v.Data, v.Reason}
	}
	return json.Marshal(m)
}

func canonicalDataset(rows []Record) ([]byte, error) {
	encoded := make([]json.RawMessage, len(rows))
	for i, r := range rows {
		b, err := canonicalRecord(r)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", i, err)
		}
		encoded[i] = b
	}
	return json.Marshal(encoded)
}

// Digest is the canonical, content-addressed identity of a dataset: two
// datasets that are field-for-field identical (in any Go map iteration
// order) hash the same, and any difference in a single field's type, state,
// data, or reason changes it.
func Digest(rows []Record) (string, error) {
	b, err := canonicalDataset(rows)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:]), nil
}
