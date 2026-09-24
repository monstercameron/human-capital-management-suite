package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// EvalState is the result-presence state of an expression evaluation.
type EvalState uint8

const (
	EvalStateUnspecified EvalState = iota
	EvalStatePresent
	EvalStateUnknown
)

func (s EvalState) String() string {
	switch s {
	case EvalStatePresent:
		return "PRESENT"
	case EvalStateUnknown:
		return "UNKNOWN"
	default:
		return "EVAL_STATE_UNSPECIFIED"
	}
}

// EvaluationStep is a bounded trace entry. It carries operation shape and
// status without exposing protected input values.
type EvaluationStep struct {
	Index  int
	Opcode Opcode
	Type   ExpressionType
	State  EvalState
}

// ExpressionResult is the pure evaluator's typed result and trace.
type ExpressionResult struct {
	State  EvalState
	Type   ExpressionType
	Value  Value
	Digest string
	Steps  []EvaluationStep
}

// Evaluate is the method form for a compiled expression. Missing map entries
// are UNKNOWN, never a zero value.
func (c CompiledExpression) Evaluate(inputs map[string]Value) (ExpressionResult, error) {
	library := DefaultExpressionFunctionLibrary()
	if err := validateCompiledArtifact(c, library); err != nil {
		return ExpressionResult{}, err
	}
	inputTypes := make(map[string]Input, len(c.Inputs))
	for _, input := range c.Inputs {
		inputTypes[input.Name] = input
	}
	steps := make([]EvaluationStep, 0, len(c.IR.Nodes))
	cache := make(map[int]runtimeValue, len(c.IR.Nodes))
	inputNames := make([]string, 0, len(inputTypes))
	for name := range inputTypes {
		inputNames = append(inputNames, name)
	}
	sort.Strings(inputNames)
	runtimeCost := c.Cost.Total
	// Preflight aggregate list lengths before validating or traversing any
	// element, so several individually valid lists cannot exceed MaxCost.
	for _, name := range inputNames {
		value, present := inputs[name]
		if !present || value.Kind() != KindList {
			continue
		}
		if len(value.list) > c.Limits.MaxIterations {
			return ExpressionResult{}, fmt.Errorf("%w: input list %q has %d items, limit %d", ErrExpressionUnbounded, name, len(value.list), c.Limits.MaxIterations)
		}
		if len(value.list) > c.Limits.MaxCost-runtimeCost {
			return ExpressionResult{}, ErrExpressionUnbounded
		}
		runtimeCost += len(value.list)
	}
	for _, name := range inputNames {
		decl := inputTypes[name]
		value, present := inputs[name]
		if !present {
			continue
		}
		if err := value.Validate(); err != nil {
			return ExpressionResult{}, fmt.Errorf("%w: input %q: %v", ErrExpressionEvaluation, name, err)
		}
		if value.Kind() == KindList {
			for i, item := range value.list {
				if !runtimeValueMatches(decl.ElementType, item) {
					return ExpressionResult{}, fmt.Errorf("%w: input %q item %d does not match %s", ErrExpressionType, name, i, decl.ElementType)
				}
			}
		}
	}
	value, err := evaluateNode(c.IR, c.IR.Root, inputs, inputTypes, cache, &steps, &runtimeCost, c.Limits.MaxCost, c.Limits.MaxIterations, c.Dependencies)
	if err != nil {
		return ExpressionResult{}, err
	}
	if value.state == EvalStateUnknown && c.UnknownSemantics == UnknownSemanticsReject {
		return ExpressionResult{}, ErrExpressionUnknown
	}
	result := ExpressionResult{State: value.state, Type: c.IR.Nodes[c.IR.Root].Type, Digest: c.Digest, Steps: steps}
	if value.state == EvalStatePresent {
		result.Value = value.value
	}
	return result, nil
}

func validateCompiledArtifact(c CompiledExpression, library ExpressionFunctionLibrary) error {
	invalid := func(reason string) error {
		return fmt.Errorf("%w: compiled expression %s", ErrExpressionInvalid, reason)
	}
	if c.Digest == "" || c.IR.Root < 0 || c.IR.Root >= len(c.IR.Nodes) || !c.UnknownSemantics.Valid() || c.Version == "" {
		return invalid("is incomplete")
	}
	if c.FunctionLibraryVersion != library.Version() || c.FunctionLibraryDigest != library.Digest() {
		return fmt.Errorf("%w: compiled function library %q is unavailable", ErrExpressionFunction, c.FunctionLibraryVersion)
	}
	if c.Limits.MaxNodes <= 0 || c.Limits.MaxDepth <= 0 || c.Limits.MaxIterations <= 0 || c.Limits.MaxCost <= 0 || len(c.Inputs)+len(c.Dependencies) > c.Limits.MaxCost {
		return ErrExpressionUnbounded
	}
	inputs := append([]Input(nil), c.Inputs...)
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Name < inputs[j].Name })
	inputTypes := make(map[string]Input, len(inputs))
	for _, input := range inputs {
		if input.Name == "" || len(input.Name) > 256 || !input.Type.Valid() || (input.Type == ExpressionTypeList && !input.ElementType.Valid()) || (input.Type != ExpressionTypeList && input.ElementType != ExpressionTypeUnspecified) {
			return invalid("has an invalid input schema")
		}
		if _, ok := inputTypes[input.Name]; ok {
			return invalid("has duplicate input declarations")
		}
		inputTypes[input.Name] = input
	}
	deps := append([]Dependency(nil), c.Dependencies...)
	sort.Slice(deps, func(i, j int) bool { return dependencyLess(deps[i], deps[j]) })
	for _, dep := range deps {
		if dep.Name == "" || dep.Version == "" || dep.Digest == "" || len(dep.Name) > 256 || len(dep.Version) > 128 || len(dep.Digest) > 256 {
			return invalid("has an invalid dependency")
		}
	}
	depDigest, err := dependenciesDigest(deps)
	if err != nil {
		return err
	}
	if depDigest != c.DependencyDigest {
		return invalid("dependency digest does not match its declarations")
	}
	if len(c.IR.Nodes) > c.Limits.MaxNodes {
		return ErrExpressionUnbounded
	}
	depths := make([]int, len(c.IR.Nodes))
	iterationCost, functionCost := 0, 0
	state := compilerState{library: library, dependencies: deps}
	for i, node := range c.IR.Nodes {
		depth := 1
		for _, child := range node.Children {
			if child < 0 || child >= i {
				return invalid("has a forward or invalid child index")
			}
			if depths[child]+1 > depth {
				depth = depths[child] + 1
			}
		}
		depths[i] = depth
		if depth > c.Limits.MaxDepth {
			return ErrExpressionUnbounded
		}
		switch node.Opcode {
		case OpcodeCall:
			entry, result, callErr := state.callType(node.Function, node.Children, c.IR.Nodes)
			if callErr != nil {
				return callErr
			}
			if node.Function != entry.Name || node.FunctionVersion != entry.Version || node.FunctionCost != entry.Cost {
				return fmt.Errorf("%w: call %q does not match the published registry entry", ErrExpressionFunction, node.Function)
			}
			if node.Type != result && result != ExpressionTypeUnknown {
				return invalid("call result type does not match its registry signature")
			}
			functionCost += entry.Cost
		case OpcodeIterate:
			if len(node.Children) != 2 || node.IterationBound <= 0 || node.IterationBound > c.Limits.MaxIterations {
				return ErrExpressionUnbounded
			}
			if node.Iteration != IterationOpAll && node.Iteration != IterationOpAny {
				return invalid("has an invalid iteration opcode")
			}
			if node.Children[0] >= i || node.Children[1] >= i {
				return invalid("has invalid iteration children")
			}
			if node.IterationBound > c.Limits.MaxIterations-iterationCost {
				return ErrExpressionUnbounded
			}
			iterationCost += node.IterationBound
		case OpcodeLiteral, OpcodeVariable, OpcodeUnary, OpcodeBinary, OpcodeUnknown:
		default:
			return invalid("has an unknown opcode")
		}
		if !node.Type.Valid() {
			return invalid("has an invalid node type")
		}
	}
	depth := depths[c.IR.Root]
	total := len(c.IR.Nodes) + iterationCost + functionCost + len(inputs) + len(deps)
	if c.Cost != (Cost{Nodes: len(c.IR.Nodes), Depth: depth, Iterations: iterationCost, Total: total}) {
		return invalid("cost summary does not match its IR")
	}
	if total > c.Limits.MaxCost {
		return ErrExpressionUnbounded
	}
	digest, err := compiledDigest(c.Version, c.UnknownSemantics, inputs, deps, depDigest, c.FunctionLibraryVersion, c.FunctionLibraryDigest, c.IR, c.Cost, c.Limits)
	if err != nil {
		return err
	}
	if digest != c.Digest {
		return invalid("digest does not match its published IR, dependencies, cost or limits")
	}
	return nil
}

// EvaluateExpression compiles and evaluates an owned expression in one pure
// operation. Call CompileExpression directly when the compiled artifact must
// be persisted or reused.
func EvaluateExpression(expr Expression, inputs map[string]Value, limits ...CostLimit) (ExpressionResult, error) {
	compiled, err := CompileExpression(expr, limits...)
	if err != nil {
		return ExpressionResult{}, err
	}
	return compiled.Evaluate(inputs)
}

// Explain reports the result without printing input values.
func (r ExpressionResult) Explain() string {
	return fmt.Sprintf("expression digest=%s -> %s type=%s steps=%d", r.Digest, r.State, r.Type, len(r.Steps))
}

type runtimeValue struct {
	state EvalState
	value Value
}

func evaluateNode(ir IR, index int, inputs map[string]Value, inputTypes map[string]Input, cache map[int]runtimeValue, steps *[]EvaluationStep, runtimeCost *int, maxCost, maxList int, dependencies []Dependency) (runtimeValue, error) {
	if value, ok := cache[index]; ok {
		return value, nil
	}
	n := ir.Nodes[index]
	unknown := func() runtimeValue { return runtimeValue{state: EvalStateUnknown} }
	var result runtimeValue
	var err error
	switch n.Opcode {
	case OpcodeLiteral:
		result = runtimeValue{state: EvalStatePresent, value: n.Value}
	case OpcodeUnknown:
		result = unknown()
	case OpcodeVariable:
		input, ok := inputTypes[n.Name]
		if !ok {
			return runtimeValue{}, fmt.Errorf("%w: %q", ErrExpressionVariable, n.Name)
		}
		value, present := inputs[n.Name]
		if !present {
			result = unknown()
			break
		}
		if err := value.Validate(); err != nil {
			return runtimeValue{}, fmt.Errorf("%w: input %q: %v", ErrExpressionEvaluation, n.Name, err)
		}
		if !runtimeValueMatches(input.Type, value) {
			return runtimeValue{}, fmt.Errorf("%w: input %q declares %s, carries %s", ErrExpressionType, n.Name, input.Type, value.Kind())
		}
		result = runtimeValue{state: EvalStatePresent, value: value}
	case OpcodeUnary:
		child, err := evaluateNode(ir, n.Children[0], inputs, inputTypes, cache, steps, runtimeCost, maxCost, maxList, dependencies)
		if err != nil {
			return runtimeValue{}, err
		}
		if child.state == EvalStateUnknown {
			result = unknown()
			break
		}
		switch n.Unary {
		case UnaryOpNot:
			b, ok := child.value.Bool()
			if !ok {
				return runtimeValue{}, fmt.Errorf("%w: NOT operand", ErrExpressionType)
			}
			result = runtimeValue{state: EvalStatePresent, value: BoolValue(!b)}
		case UnaryOpNegate:
			result, err = negateRuntime(child)
			if err != nil {
				return runtimeValue{}, err
			}
		default:
			return runtimeValue{}, fmt.Errorf("%w: unary operator", ErrExpressionEvaluation)
		}
	case OpcodeBinary:
		left, err := evaluateNode(ir, n.Children[0], inputs, inputTypes, cache, steps, runtimeCost, maxCost, maxList, dependencies)
		if err != nil {
			return runtimeValue{}, err
		}
		// Short-circuiting also preserves Kleene logic and avoids evaluating an
		// irrelevant right branch with a missing input.
		if n.Binary == BinaryOpAnd && left.state == EvalStatePresent {
			b, _ := left.value.Bool()
			if !b {
				result = runtimeValue{state: EvalStatePresent, value: BoolValue(false)}
				break
			}
		}
		if n.Binary == BinaryOpOr && left.state == EvalStatePresent {
			b, _ := left.value.Bool()
			if b {
				result = runtimeValue{state: EvalStatePresent, value: BoolValue(true)}
				break
			}
		}
		right, err := evaluateNode(ir, n.Children[1], inputs, inputTypes, cache, steps, runtimeCost, maxCost, maxList, dependencies)
		if err != nil {
			return runtimeValue{}, err
		}
		result, err = evaluateBinary(n.Binary, left, right)
		if err != nil {
			return runtimeValue{}, err
		}
	case OpcodeCall:
		args := make([]runtimeValue, len(n.Children))
		for i, childIndex := range n.Children {
			args[i], err = evaluateNode(ir, childIndex, inputs, inputTypes, cache, steps, runtimeCost, maxCost, maxList, dependencies)
			if err != nil {
				return runtimeValue{}, err
			}
		}
		result, err = evaluateCall(n.Function, args, runtimeCost, maxCost, maxList, dependencies)
		if err != nil {
			return runtimeValue{}, err
		}
	case OpcodeIterate:
		collection, err := evaluateNode(ir, n.Children[0], inputs, inputTypes, cache, steps, runtimeCost, maxCost, maxList, dependencies)
		if err != nil {
			return runtimeValue{}, err
		}
		if collection.state == EvalStateUnknown {
			result = unknown()
			break
		}
		if collection.value.Kind() != KindList || len(collection.value.list) > n.IterationBound {
			return runtimeValue{}, fmt.Errorf("%w: iteration list exceeds bound", ErrExpressionUnbounded)
		}
		items := collection.value.list
		result = runtimeValue{state: EvalStatePresent, value: BoolValue(n.Iteration == IterationOpAll)}
		for _, item := range items {
			localInputs := make(map[string]Value, len(inputs)+1)
			for k, v := range inputs {
				localInputs[k] = v
			}
			localInputs["$item"] = item
			localCache := make(map[int]runtimeValue)
			localTypes := make(map[string]Input, len(inputTypes)+1)
			for k, v := range inputTypes {
				localTypes[k] = v
			}
			localTypes["$item"] = Input{Name: "$item", Type: ir.Nodes[n.Children[0]].ElementType}
			predicate, evalErr := evaluateNode(ir, n.Children[1], localInputs, localTypes, localCache, steps, runtimeCost, maxCost, maxList, dependencies)
			if evalErr != nil {
				return runtimeValue{}, evalErr
			}
			if predicate.state == EvalStateUnknown {
				result = unknown()
				break
			}
			b, _ := predicate.value.Bool()
			if n.Iteration == IterationOpAny && b {
				result = runtimeValue{state: EvalStatePresent, value: BoolValue(true)}
				break
			}
			if n.Iteration == IterationOpAll && !b {
				result = runtimeValue{state: EvalStatePresent, value: BoolValue(false)}
				break
			}
		}
	default:
		return runtimeValue{}, fmt.Errorf("%w: opcode %d is not evaluatable", ErrExpressionEvaluation, n.Opcode)
	}
	cache[index] = result
	*steps = append(*steps, EvaluationStep{Index: index, Opcode: n.Opcode, Type: n.Type, State: result.state})
	return result, nil
}

func runtimeValueMatches(typ ExpressionType, value Value) bool {
	switch typ {
	case ExpressionTypeBool:
		return value.Kind() == KindBool
	case ExpressionTypeInt:
		return value.Kind() == KindInt
	case ExpressionTypeDecimal:
		return value.Kind() == KindDecimal
	case ExpressionTypeString:
		return value.Kind() == KindString
	case ExpressionTypeDate:
		if value.Kind() != KindString {
			return false
		}
		_, err := values.ParseLocalDate(value.String())
		return err == nil
	case ExpressionTypeList:
		return value.Kind() == KindList
	default:
		return false
	}
}

func negateRuntime(v runtimeValue) (runtimeValue, error) {
	switch v.value.Kind() {
	case KindInt:
		i, _ := v.value.Int()
		return runtimeValue{state: EvalStatePresent, value: IntValue(-i)}, nil
	case KindDecimal:
		d, err := v.value.Decimal()
		if err != nil {
			return runtimeValue{}, err
		}
		nd, err := d.Neg()
		if err != nil {
			return runtimeValue{}, err
		}
		return runtimeValue{state: EvalStatePresent, value: DecimalValue(nd)}, nil
	default:
		return runtimeValue{}, fmt.Errorf("%w: NEGATE operand", ErrExpressionType)
	}
}

func evaluateBinary(op BinaryOp, left, right runtimeValue) (runtimeValue, error) {
	if op == BinaryOpAnd || op == BinaryOpOr {
		return evaluateLogic(op, left, right)
	}
	if left.state == EvalStateUnknown || right.state == EvalStateUnknown {
		return runtimeValue{state: EvalStateUnknown}, nil
	}
	if op == BinaryOpEqual || op == BinaryOpNotEqual || op == BinaryOpLess || op == BinaryOpLessOrEqual || op == BinaryOpGreater || op == BinaryOpGreaterOrEqual {
		cmp, err := compareRuntime(left.value, right.value)
		if err != nil {
			return runtimeValue{}, err
		}
		match := false
		switch op {
		case BinaryOpEqual:
			match = cmp == 0
		case BinaryOpNotEqual:
			match = cmp != 0
		case BinaryOpLess:
			match = cmp < 0
		case BinaryOpLessOrEqual:
			match = cmp <= 0
		case BinaryOpGreater:
			match = cmp > 0
		case BinaryOpGreaterOrEqual:
			match = cmp >= 0
		}
		return runtimeValue{state: EvalStatePresent, value: BoolValue(match)}, nil
	}
	switch op {
	case BinaryOpAdd, BinaryOpSubtract, BinaryOpMultiply, BinaryOpDivide:
		return arithmeticRuntime(op, left.value, right.value)
	default:
		return runtimeValue{}, fmt.Errorf("%w: binary operator %s", ErrExpressionEvaluation, op)
	}
}

func evaluateLogic(op BinaryOp, left, right runtimeValue) (runtimeValue, error) {
	if left.state == EvalStatePresent && right.state == EvalStatePresent {
		lb, lok := left.value.Bool()
		rb, rok := right.value.Bool()
		if !lok || !rok {
			return runtimeValue{}, ErrExpressionType
		}
		if op == BinaryOpAnd {
			return runtimeValue{state: EvalStatePresent, value: BoolValue(lb && rb)}, nil
		}
		return runtimeValue{state: EvalStatePresent, value: BoolValue(lb || rb)}, nil
	}
	if op == BinaryOpAnd && right.state == EvalStatePresent {
		rb, ok := right.value.Bool()
		if ok && !rb {
			return runtimeValue{state: EvalStatePresent, value: BoolValue(false)}, nil
		}
	}
	if op == BinaryOpOr && right.state == EvalStatePresent {
		rb, ok := right.value.Bool()
		if ok && rb {
			return runtimeValue{state: EvalStatePresent, value: BoolValue(true)}, nil
		}
	}
	return runtimeValue{state: EvalStateUnknown}, nil
}

func compareRuntime(left, right Value) (int, error) {
	if left.Kind() != right.Kind() {
		return 0, fmt.Errorf("%w: %s and %s", ErrExpressionType, left.Kind(), right.Kind())
	}
	if left.Kind() == KindString {
		if left.String() < right.String() {
			return -1, nil
		}
		if left.String() > right.String() {
			return 1, nil
		}
		return 0, nil
	}
	return left.Cmp(right)
}

func arithmeticRuntime(op BinaryOp, left, right Value) (runtimeValue, error) {
	if left.Kind() == KindString && right.Kind() == KindString && op == BinaryOpAdd {
		return runtimeValue{state: EvalStatePresent, value: StringValue(left.String() + right.String())}, nil
	}
	if left.Kind() == KindInt && right.Kind() == KindInt {
		li, _ := left.Int()
		ri, _ := right.Int()
		switch op {
		case BinaryOpAdd:
			return runtimeValue{state: EvalStatePresent, value: IntValue(li + ri)}, nil
		case BinaryOpSubtract:
			return runtimeValue{state: EvalStatePresent, value: IntValue(li - ri)}, nil
		case BinaryOpMultiply:
			return runtimeValue{state: EvalStatePresent, value: IntValue(li * ri)}, nil
		case BinaryOpDivide:
			if ri == 0 {
				return runtimeValue{}, fmt.Errorf("%w: integer division by zero", ErrExpressionEvaluation)
			}
			return runtimeValue{state: EvalStatePresent, value: IntValue(li / ri)}, nil
		}
	}
	if left.Kind() != KindDecimal || right.Kind() != KindDecimal {
		return runtimeValue{}, fmt.Errorf("%w: numeric operands must both be decimal or int", ErrExpressionType)
	}
	ld, _ := left.Decimal()
	rd, _ := right.Decimal()
	scale := ld.Scale()
	if rd.Scale() > scale {
		scale = rd.Scale()
	}
	var result values.Decimal
	var err error
	switch op {
	case BinaryOpAdd:
		if ld.Scale() != rd.Scale() {
			return runtimeValue{}, fmt.Errorf("%w: decimal addition requires equal scale", ErrExpressionType)
		}
		result, err = ld.Add(rd)
	case BinaryOpSubtract:
		if ld.Scale() != rd.Scale() {
			return runtimeValue{}, fmt.Errorf("%w: decimal subtraction requires equal scale", ErrExpressionType)
		}
		result, err = ld.Sub(rd)
	case BinaryOpMultiply:
		result, err = ld.Mul(rd, scale, values.RoundingHalfEven)
	case BinaryOpDivide:
		result, err = ld.Div(rd, scale, values.RoundingHalfEven)
	}
	if err != nil {
		return runtimeValue{}, fmt.Errorf("%w: %v", ErrExpressionEvaluation, err)
	}
	return runtimeValue{state: EvalStatePresent, value: DecimalValue(result)}, nil
}

// Accessors keep Value's representation private while allowing the owned
// evaluator to inspect it.
func (v Value) Bool() (bool, bool) {
	if v.Kind() != KindBool {
		return false, false
	}
	return v.b, true
}
func (v Value) Int() (int64, bool) {
	if v.Kind() != KindInt {
		return 0, false
	}
	return v.i, true
}
func (v Value) Decimal() (values.Decimal, error) {
	if v.Kind() != KindDecimal {
		return values.Decimal{}, fmt.Errorf("%w: expected decimal", ErrExpressionType)
	}
	return v.dec, nil
}

func evaluateCall(name string, args []runtimeValue, runtimeCost *int, maxCost, maxList int, dependencies []Dependency) (runtimeValue, error) {
	for _, arg := range args {
		if arg.state == EvalStateUnknown {
			return runtimeValue{state: EvalStateUnknown}, nil
		}
	}
	lower := strings.ToLower(name)
	entry, ok := DefaultExpressionFunctionLibrary().entry(lower)
	if !ok {
		return runtimeValue{}, fmt.Errorf("%w: %s", ErrExpressionFunction, name)
	}
	dynamicCost := functionDynamicCost(entry, args)
	if dynamicCost > maxCost-*runtimeCost {
		return runtimeValue{}, fmt.Errorf("%w: runtime cost exceeds %d", ErrExpressionUnbounded, maxCost)
	}
	*runtimeCost += dynamicCost
	if lower == "lower" || lower == "upper" {
		if len(args) != 1 || args[0].value.Kind() != KindString {
			return runtimeValue{}, ErrExpressionType
		}
		text := args[0].value.String()
		if lower == "lower" {
			text = strings.ToLower(text)
		} else {
			text = strings.ToUpper(text)
		}
		return runtimeValue{state: EvalStatePresent, value: StringValue(text)}, nil
	}
	if lower == "contains" || lower == "starts_with" || lower == "ends_with" {
		if len(args) != 2 || args[0].value.Kind() != KindString || args[1].value.Kind() != KindString {
			return runtimeValue{}, ErrExpressionType
		}
		a, b := args[0].value.String(), args[1].value.String()
		match := false
		if lower == "contains" {
			match = strings.Contains(a, b)
		}
		if lower == "starts_with" {
			match = strings.HasPrefix(a, b)
		}
		if lower == "ends_with" {
			match = strings.HasSuffix(a, b)
		}
		return runtimeValue{state: EvalStatePresent, value: BoolValue(match)}, nil
	}
	if lower == "len" {
		if len(args) != 1 || (args[0].value.Kind() != KindString && args[0].value.Kind() != KindList) {
			return runtimeValue{}, ErrExpressionType
		}
		if args[0].value.Kind() == KindList {
			return runtimeValue{state: EvalStatePresent, value: IntValue(int64(len(args[0].value.list)))}, nil
		}
		return runtimeValue{state: EvalStatePresent, value: IntValue(int64(len(args[0].value.String())))}, nil
	}
	if lower == "list_contains" {
		if len(args) != 2 || args[0].value.Kind() != KindList {
			return runtimeValue{}, ErrExpressionType
		}
		list := args[0].value.list
		if len(list) > maxList {
			return runtimeValue{}, ErrExpressionUnbounded
		}
		for _, item := range list {
			eq, err := item.Equal(args[1].value)
			if err != nil {
				return runtimeValue{}, err
			}
			if eq {
				return runtimeValue{state: EvalStatePresent, value: BoolValue(true)}, nil
			}
		}
		return runtimeValue{state: EvalStatePresent, value: BoolValue(false)}, nil
	}
	if lower == "interval_overlaps" {
		if len(args) != 4 {
			return runtimeValue{}, ErrExpressionType
		}
		dates := make([]values.LocalDate, 4)
		for i := range args {
			d, err := values.ParseLocalDate(args[i].value.String())
			if err != nil {
				return runtimeValue{}, fmt.Errorf("%w: interval date: %v", ErrExpressionValue, err)
			}
			dates[i] = d
		}
		if dates[0].Compare(dates[1]) >= 0 || dates[2].Compare(dates[3]) >= 0 {
			return runtimeValue{}, fmt.Errorf("%w: date interval must be nonempty and half-open", ErrExpressionValue)
		}
		overlaps := dates[0].Compare(dates[3]) < 0 && dates[2].Compare(dates[1]) < 0
		return runtimeValue{state: EvalStatePresent, value: BoolValue(overlaps)}, nil
	}
	if lower == "business_day_diff" {
		if len(args) != 6 || args[5].value.Kind() != KindList {
			return runtimeValue{}, ErrExpressionType
		}
		start, err := values.ParseLocalDate(args[0].value.String())
		if err != nil {
			return runtimeValue{}, ErrExpressionValue
		}
		end, err := values.ParseLocalDate(args[1].value.String())
		if err != nil {
			return runtimeValue{}, ErrExpressionValue
		}
		calendarRef, calendarVersion, calendarDigest := args[2].value.String(), args[3].value.String(), args[4].value.String()
		pinned := false
		for _, dep := range dependencies {
			if dep.Name == "calendar."+calendarRef && dep.Version == calendarVersion && dep.Digest == calendarDigest {
				pinned = true
				break
			}
		}
		if !pinned {
			return runtimeValue{}, fmt.Errorf("%w: business_day_diff calendar arguments do not match dependency", ErrExpressionDependency)
		}
		days := args[5].value.list
		if len(days) > maxList {
			return runtimeValue{}, ErrExpressionUnbounded
		}
		dateStrings := make([]string, len(days))
		for i, value := range days {
			dateStrings[i] = value.String()
		}
		actualDigest, digestErr := BusinessCalendarSnapshotDigest(calendarRef, calendarVersion, dateStrings)
		if digestErr != nil {
			return runtimeValue{}, digestErr
		}
		if actualDigest != calendarDigest {
			return runtimeValue{}, fmt.Errorf("%w: pinned calendar snapshot digest does not match working dates", ErrExpressionDependency)
		}
		count := int64(0)
		for _, v := range dateStrings {
			d, parseErr := values.ParseLocalDate(v)
			if parseErr != nil {
				return runtimeValue{}, fmt.Errorf("%w: invalid pinned calendar date", ErrExpressionValue)
			}
			if start.Compare(end) < 0 && d.Compare(start) > 0 && d.Compare(end) <= 0 {
				count++
			}
			if start.Compare(end) > 0 && d.Compare(end) > 0 && d.Compare(start) <= 0 {
				count--
			}
		}
		return runtimeValue{state: EvalStatePresent, value: IntValue(count)}, nil
	}
	if lower == "abs" {
		if len(args) != 1 {
			return runtimeValue{}, ErrExpressionType
		}
		if args[0].value.Kind() == KindInt {
			i, _ := args[0].value.Int()
			if i < 0 {
				i = -i
			}
			return runtimeValue{state: EvalStatePresent, value: IntValue(i)}, nil
		}
		d, err := args[0].value.Decimal()
		if err != nil {
			return runtimeValue{}, err
		}
		out, err := d.Abs()
		if err != nil {
			return runtimeValue{}, err
		}
		return runtimeValue{state: EvalStatePresent, value: DecimalValue(out)}, nil
	}
	return runtimeValue{}, fmt.Errorf("%w: %s", ErrExpressionFunction, name)
}

// functionDynamicCost charges scanned bytes and list elements in addition to
// each registry entry's fixed base cost.
func functionDynamicCost(entry FunctionEntry, args []runtimeValue) int {
	if entry.PerElementCost == 0 {
		return 0
	}
	maxInt := int(^uint(0) >> 1)
	add := func(a, b int) int {
		if b > maxInt-a {
			return maxInt
		}
		return a + b
	}
	switch entry.operation {
	case "lower", "upper":
		return len(args[0].value.String()) * entry.PerElementCost
	case "contains", "starts_with", "ends_with":
		return add(len(args[0].value.String()), len(args[1].value.String()))
	case "list_contains":
		list := args[0].value.list
		cost := 0
		for _, item := range list {
			itemCost := entry.PerElementCost
			if item.Kind() == KindString {
				itemCost = add(itemCost, len(item.String()))
			}
			if args[1].value.Kind() == KindString {
				itemCost = add(itemCost, len(args[1].value.String()))
			}
			cost = add(cost, itemCost)
		}
		return cost
	case "business_day_diff":
		list := args[5].value.list
		if entry.PerElementCost != 0 && len(list) > int(^uint(0)>>1)/entry.PerElementCost {
			return int(^uint(0) >> 1)
		}
		count := len(list)
		// Snapshot hashing sorts dates, bounded to the list input limit.
		levels := 0
		for n := len(list); n > 1; n >>= 1 {
			levels++
		}
		if levels != 0 && count > int(^uint(0)>>1)/levels {
			return int(^uint(0) >> 1)
		}
		return count * levels
	default:
		return 0
	}
}
