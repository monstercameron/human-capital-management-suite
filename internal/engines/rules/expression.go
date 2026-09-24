package rules

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ExpressionVersion is the owned expression/IR contract version. It is
// independent from the decision-table schema version in rules.go.
const ExpressionVersion = 2

// Expression errors are stable categories callers may match with errors.Is.
var (
	ErrExpressionInvalid    = errors.New("rules: expression is invalid")
	ErrExpressionSyntax     = errors.New("rules: expression syntax is invalid")
	ErrExpressionType       = errors.New("rules: expression type mismatch")
	ErrExpressionArbitrary  = errors.New("rules: arbitrary code is forbidden")
	ErrExpressionIO         = errors.New("rules: I/O is forbidden")
	ErrExpressionTime       = errors.New("rules: ambient time is forbidden")
	ErrExpressionRandom     = errors.New("rules: randomness is forbidden")
	ErrExpressionUnknown    = errors.New("rules: unknown semantics are not declared")
	ErrExpressionUnbounded  = errors.New("rules: expression exceeds bounded complexity")
	ErrExpressionIteration  = errors.New("rules: iteration bound is invalid")
	ErrExpressionVariable   = errors.New("rules: expression variable is not declared")
	ErrExpressionDependency = errors.New("rules: expression dependency is invalid")
	ErrExpressionFunction   = errors.New("rules: expression function is not allowed")
	ErrExpressionValue      = errors.New("rules: expression value is invalid")
	ErrExpressionEvaluation = errors.New("rules: expression evaluation failed")
)

// ExpressionType is the type system used by the owned AST and IR.
type ExpressionType uint8

const (
	ExpressionTypeUnspecified ExpressionType = iota
	ExpressionTypeBool
	ExpressionTypeInt
	ExpressionTypeDecimal
	ExpressionTypeString
	ExpressionTypeDate
	ExpressionTypeList
	ExpressionTypeUnknown
)

var expressionTypeWire = map[ExpressionType]string{
	ExpressionTypeBool:    "BOOL",
	ExpressionTypeInt:     "INT",
	ExpressionTypeDecimal: "DECIMAL",
	ExpressionTypeString:  "STRING",
	ExpressionTypeDate:    "LOCAL_DATE",
	ExpressionTypeList:    "LIST",
	ExpressionTypeUnknown: "UNKNOWN",
}

func (t ExpressionType) String() string {
	if s, ok := expressionTypeWire[t]; ok {
		return s
	}
	return "TYPE_UNSPECIFIED"
}

func (t ExpressionType) Valid() bool { _, ok := expressionTypeWire[t]; return ok }

// UnknownSemantics makes absence and unknown values explicit. Propagate is
// Kleene three-valued logic: UNKNOWN is neither true nor false. Reject makes
// an unknown input an evaluation error. There is no implicit false default.
type UnknownSemantics uint8

const (
	UnknownSemanticsUnspecified UnknownSemantics = iota
	UnknownSemanticsPropagate
	UnknownSemanticsReject
)

func (s UnknownSemantics) String() string {
	switch s {
	case UnknownSemanticsPropagate:
		return "PROPAGATE"
	case UnknownSemanticsReject:
		return "REJECT"
	default:
		return "UNKNOWN_SEMANTICS_UNSPECIFIED"
	}
}

func (s UnknownSemantics) Valid() bool {
	return s == UnknownSemanticsPropagate || s == UnknownSemanticsReject
}

// ExprKind identifies one owned AST node. The AST contains no CEL or other
// public expression-language type.
type ExprKind uint8

const (
	ExprKindUnspecified ExprKind = iota
	ExprKindLiteral
	ExprKindVariable
	ExprKindUnary
	ExprKindBinary
	ExprKindCall
	ExprKindIterate
	ExprKindUnknown
)

// Expr kinds.
const (
	ExprLiteral  = ExprKindLiteral
	ExprVariable = ExprKindVariable
	ExprUnary    = ExprKindUnary
	ExprBinary   = ExprKindBinary
	ExprCall     = ExprKindCall
	ExprIterate  = ExprKindIterate
	ExprUnknown  = ExprKindUnknown
)

// UnaryOp is an operation on one child.
type UnaryOp uint8

const (
	UnaryOpUnspecified UnaryOp = iota
	UnaryOpNot
	UnaryOpNegate
)

func (o UnaryOp) String() string {
	switch o {
	case UnaryOpNot:
		return "NOT"
	case UnaryOpNegate:
		return "NEGATE"
	default:
		return "UNARY_UNSPECIFIED"
	}
}

// BinaryOp is an operation on two children.
type BinaryOp uint8

const (
	BinaryOpUnspecified BinaryOp = iota
	BinaryOpAnd
	BinaryOpOr
	BinaryOpEqual
	BinaryOpNotEqual
	BinaryOpLess
	BinaryOpLessOrEqual
	BinaryOpGreater
	BinaryOpGreaterOrEqual
	BinaryOpAdd
	BinaryOpSubtract
	BinaryOpMultiply
	BinaryOpDivide
)

func (o BinaryOp) String() string {
	labels := map[BinaryOp]string{
		BinaryOpAnd: "AND", BinaryOpOr: "OR", BinaryOpEqual: "EQUAL", BinaryOpNotEqual: "NOT_EQUAL",
		BinaryOpLess: "LESS", BinaryOpLessOrEqual: "LESS_OR_EQUAL", BinaryOpGreater: "GREATER", BinaryOpGreaterOrEqual: "GREATER_OR_EQUAL",
		BinaryOpAdd: "ADD", BinaryOpSubtract: "SUBTRACT", BinaryOpMultiply: "MULTIPLY", BinaryOpDivide: "DIVIDE",
	}
	if s, ok := labels[o]; ok {
		return s
	}
	return "BINARY_UNSPECIFIED"
}

// IterationOp is a bounded collection predicate.
type IterationOp uint8

const (
	IterationOpUnspecified IterationOp = iota
	IterationOpAll
	IterationOpAny
)

func (o IterationOp) String() string {
	if o == IterationOpAll {
		return "ALL"
	}
	if o == IterationOpAny {
		return "ANY"
	}
	return "ITERATION_UNSPECIFIED"
}

// Expr is one node in the package-owned typed expression AST. Children are
// values rather than interfaces, so a caller cannot inject executable code or
// a recursive runtime object into the compiler.
type Expr struct {
	Kind           ExprKind
	Type           ExpressionType
	Name           string
	Function       string
	Value          Value
	Unary          UnaryOp
	Binary         BinaryOp
	Iteration      IterationOp
	IterationBound int
	Children       []Expr
}

// Expression is the Human Capital Management Suite-owned expression definition passed to the
// compiler. Inputs and dependencies are declarations, not ambient lookups.
type Expression struct {
	Root             Expr
	Inputs           []Input
	Dependencies     []Dependency
	Version          string
	UnknownSemantics UnknownSemantics
}

// Input declares one variable and its type. List inputs use ElementType.
type Input struct {
	Name        string
	Type        ExpressionType
	ElementType ExpressionType
}

// Dependency pins a reference snapshot/function contract used by an
// expression. Both name and version participate in the compiled digest.
type Dependency struct {
	Name    string
	Version string
	Digest  string
}

// Constructors make valid ASTs easy to author without introducing a general
// scripting representation.
func Literal(v Value) Expr {
	return Expr{Kind: ExprKindLiteral, Type: valueExpressionType(v), Value: v}
}
func BoolLiteral(v bool) Expr              { return Literal(BoolValue(v)) }
func IntLiteral(v int64) Expr              { return Literal(IntValue(v)) }
func DecimalLiteral(v values.Decimal) Expr { return Literal(DecimalValue(v)) }
func StringLiteral(v string) Expr          { return Literal(StringValue(v)) }
func DateLiteral(v string) Expr {
	return Expr{Kind: ExprKindLiteral, Type: ExpressionTypeDate, Value: StringValue(v)}
}
func Variable(name string, typ ExpressionType) Expr {
	return Expr{Kind: ExprKindVariable, Name: name, Type: typ}
}
func Unary(op UnaryOp, child Expr) Expr {
	return Expr{Kind: ExprKindUnary, Unary: op, Children: []Expr{child}}
}
func Binary(op BinaryOp, left, right Expr) Expr {
	return Expr{Kind: ExprKindBinary, Binary: op, Children: []Expr{left, right}}
}
func Call(function string, args ...Expr) Expr {
	return Expr{Kind: ExprKindCall, Function: function, Children: append([]Expr(nil), args...)}
}
func Iterate(op IterationOp, collection, predicate Expr, bound int) Expr {
	return Expr{Kind: ExprKindIterate, Iteration: op, IterationBound: bound, Children: []Expr{collection, predicate}}
}
func Unknown() Expr { return Expr{Kind: ExprKindUnknown, Type: ExpressionTypeUnknown} }

func valueExpressionType(v Value) ExpressionType {
	switch v.Kind() {
	case KindBool:
		return ExpressionTypeBool
	case KindInt:
		return ExpressionTypeInt
	case KindDecimal:
		return ExpressionTypeDecimal
	case KindString:
		return ExpressionTypeString
	case KindList:
		return ExpressionTypeList
	default:
		return ExpressionTypeUnspecified
	}
}

// CostLimit bounds compiler work and evaluator iteration. Zero values are
// rejected; DefaultCostLimit is provided for callers that do not need a
// tighter tenant-specific budget.
type CostLimit struct {
	MaxNodes      int
	MaxDepth      int
	MaxIterations int
	MaxCost       int
}

var DefaultCostLimit = CostLimit{MaxNodes: 256, MaxDepth: 32, MaxIterations: 256, MaxCost: 1024}

// Cost records the measured shape of a compiled expression.
type Cost struct {
	Nodes      int
	Depth      int
	Iterations int
	Total      int
}

// Opcode identifies backend-neutral IR instructions.
type Opcode uint8

const (
	OpcodeLiteral Opcode = iota + 1
	OpcodeVariable
	OpcodeUnary
	OpcodeBinary
	OpcodeCall
	OpcodeIterate
	OpcodeUnknown
)

func (o Opcode) String() string {
	switch o {
	case OpcodeLiteral:
		return "LITERAL"
	case OpcodeVariable:
		return "VARIABLE"
	case OpcodeUnary:
		return "UNARY"
	case OpcodeBinary:
		return "BINARY"
	case OpcodeCall:
		return "CALL"
	case OpcodeIterate:
		return "ITERATE"
	case OpcodeUnknown:
		return "UNKNOWN"
	default:
		return "OPCODE_UNSPECIFIED"
	}
}

// IRNode is an indexed, backend-neutral instruction. Child indexes always
// point backwards, making the IR acyclic and straightforward to audit.
type IRNode struct {
	Opcode          Opcode
	Type            ExpressionType
	ElementType     ExpressionType
	Name            string
	Function        string
	FunctionVersion string
	FunctionCost    int
	Value           Value
	Unary           UnaryOp
	Binary          BinaryOp
	Iteration       IterationOp
	IterationBound  int
	Children        []int
}

// IR is the bounded backend-neutral program produced by compilation.
type IR struct {
	Nodes []IRNode
	Root  int
}

// CompiledExpression is immutable by convention: all slices are compiler
// owned copies and its digest cites the exact IR, dependencies and version.
type CompiledExpression struct {
	IR                     IR
	Inputs                 []Input
	Dependencies           []Dependency
	Version                string
	FunctionLibraryVersion string
	FunctionLibraryDigest  string
	UnknownSemantics       UnknownSemantics
	DependencyDigest       string
	Digest                 string
	Cost                   Cost
	Limits                 CostLimit
}

// Parse parses the small owned expression grammar. Variables remain
// type-unspecified until Input declarations are supplied to compilation.
func Parse(source string) (Expression, error) {
	lexer := expressionLexer{source: source}
	tokens, err := lexer.lex()
	if err != nil {
		return Expression{}, err
	}
	p := expressionParser{tokens: tokens}
	root, err := p.parse()
	if err != nil {
		return Expression{}, err
	}
	return Expression{Root: root, Version: "1"}, nil
}

// ParseExpression is the explicit-name alias used at API boundaries.
func ParseExpression(source string) (Expression, error) { return Parse(source) }

// Canonical returns the stable AST encoding, or nil when the expression
// cannot be represented as a canonical stream. Compilation remains the
// authority for type and cost validation.
func (e Expression) Canonical() []byte {
	raw, err := expressionCanonical(e)
	if err != nil {
		return nil
	}
	return raw
}

// ParseWithInputs parses and attaches an ordered copy of the input schema.
func ParseWithInputs(source string, inputs []Input) (Expression, error) {
	expr, err := Parse(source)
	if err != nil {
		return Expression{}, err
	}
	expr.Inputs = append([]Input(nil), inputs...)
	return expr, nil
}

// ParseTyped is the map-friendly form of ParseWithInputs. Map keys are sorted
// before being placed in the expression so construction is deterministic.
func ParseTyped(source string, inputs map[string]ExpressionType) (Expression, error) {
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	decls := make([]Input, 0, len(names))
	for _, name := range names {
		decls = append(decls, Input{Name: name, Type: inputs[name]})
	}
	return ParseWithInputs(source, decls)
}

// Explain is an audit-safe description of the expression contract and its
// backend. It never claims CEL execution: the current implementation is the
// package's own evaluator.
func (c CompiledExpression) Explain() string {
	return fmt.Sprintf("rules expression v%d backend=owned-evaluator digest=%s dependencies=%s unknown=%s nodes=%d depth=%d iterations=%d", ExpressionVersion, c.Digest, c.DependencyDigest, c.UnknownSemantics, c.Cost.Nodes, c.Cost.Depth, c.Cost.Iterations)
}

// Canonical renders a stable text form useful in golden fixtures.
func (ir IR) Canonical() string {
	parts := make([]string, len(ir.Nodes))
	for i, n := range ir.Nodes {
		children := make([]string, len(n.Children))
		for j, child := range n.Children {
			children[j] = strconv.Itoa(child)
		}
		value := ""
		if n.Opcode == OpcodeLiteral {
			value = n.Value.String()
		}
		parts[i] = fmt.Sprintf("%d:%s:%s:%s:%s:[%s]", i, n.Opcode, n.Type, n.Name, value, strings.Join(children, ","))
	}
	return fmt.Sprintf("root=%d nodes=%s", ir.Root, strings.Join(parts, ";"))
}

// expressionCanonical writes the AST/declarations in a stable stream.
func expressionCanonical(expr Expression) ([]byte, error) {
	w := canonicalbytes.New("hcmnext.engines.rules.Expression", ExpressionVersion).
		String("version", expr.Version).
		String("unknown_semantics", expr.UnknownSemantics.String())
	inputs := append([]Input(nil), expr.Inputs...)
	sort.SliceStable(inputs, func(i, j int) bool { return inputs[i].Name < inputs[j].Name })
	w.Count("inputs", len(inputs))
	for _, input := range inputs {
		w.String("input.name", input.Name).String("input.type", input.Type.String()).String("input.element_type", input.ElementType.String())
	}
	deps := append([]Dependency(nil), expr.Dependencies...)
	sort.SliceStable(deps, func(i, j int) bool { return dependencyLess(deps[i], deps[j]) })
	w.Count("dependencies", len(deps))
	for _, dep := range deps {
		w.String("dependency.name", dep.Name).String("dependency.version", dep.Version).String("dependency.digest", dep.Digest)
	}
	writeExprCanonical(w, expr.Root)
	return w.Bytes()
}

func dependencyLess(a, b Dependency) bool {
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	if a.Version != b.Version {
		return a.Version < b.Version
	}
	return a.Digest < b.Digest
}

func writeExprCanonical(w *canonicalbytes.Writer, e Expr) {
	w.String("node.kind", strconv.Itoa(int(e.Kind))).String("node.type", e.Type.String()).String("node.name", e.Name).String("node.function", e.Function)
	w.String("node.unary", e.Unary.String()).String("node.binary", e.Binary.String()).String("node.iteration", e.Iteration.String()).Int("node.bound", int64(e.IterationBound))
	if e.Kind == ExprKindLiteral {
		w.Value("node.value", e.Value)
	}
	w.Count("node.children", len(e.Children))
	for _, child := range e.Children {
		writeExprCanonical(w, child)
	}
}

// Lexer/parser implementation for the owned grammar.
type expressionToken struct {
	kind, text string
	pos        int
}

type expressionLexer struct{ source string }

func (l expressionLexer) lex() ([]expressionToken, error) {
	var out []expressionToken
	for i := 0; i < len(l.source); {
		if unicode.IsSpace(rune(l.source[i])) {
			i++
			continue
		}
		start := i
		if unicode.IsLetter(rune(l.source[i])) || l.source[i] == '_' {
			i++
			for i < len(l.source) && (unicode.IsLetter(rune(l.source[i])) || unicode.IsDigit(rune(l.source[i])) || l.source[i] == '_' || l.source[i] == '.') {
				i++
			}
			out = append(out, expressionToken{kind: "ident", text: l.source[start:i], pos: start})
			continue
		}
		if unicode.IsDigit(rune(l.source[i])) {
			i++
			for i < len(l.source) && unicode.IsDigit(rune(l.source[i])) {
				i++
			}
			if i < len(l.source) && l.source[i] == '.' {
				i++
				for i < len(l.source) && unicode.IsDigit(rune(l.source[i])) {
					i++
				}
			}
			out = append(out, expressionToken{kind: "number", text: l.source[start:i], pos: start})
			continue
		}
		if l.source[i] == '"' || l.source[i] == '\'' {
			quote := l.source[i]
			i++
			escaped := false
			closed := false
			for i < len(l.source) {
				if !escaped && l.source[i] == quote {
					i++
					closed = true
					break
				}
				if !escaped && l.source[i] == '\\' {
					escaped = true
					i++
					continue
				}
				escaped = false
				i++
			}
			if !closed {
				return nil, fmt.Errorf("%w at %d: unterminated string", ErrExpressionSyntax, start)
			}
			text := l.source[start:i]
			if quote == '\'' {
				text = `"` + strings.ReplaceAll(text[1:len(text)-1], `"`, `\"`) + `"`
			}
			value, err := strconv.Unquote(text)
			if err != nil {
				return nil, fmt.Errorf("%w at %d: %v", ErrExpressionSyntax, start, err)
			}
			out = append(out, expressionToken{kind: "string", text: value, pos: start})
			continue
		}
		matched := ""
		for _, op := range []string{"&&", "||", "==", "!=", "<=", ">="} {
			if strings.HasPrefix(l.source[i:], op) {
				matched = op
				break
			}
		}
		if matched != "" {
			out = append(out, expressionToken{kind: "op", text: matched, pos: start})
			i += len(matched)
			continue
		}
		if strings.ContainsRune("()+-*/!<>,", rune(l.source[i])) {
			out = append(out, expressionToken{kind: "op", text: l.source[i : i+1], pos: start})
			i++
			continue
		}
		return nil, fmt.Errorf("%w at %d: unexpected character %q", ErrExpressionSyntax, start, l.source[i])
	}
	out = append(out, expressionToken{kind: "eof", pos: len(l.source)})
	return out, nil
}

type expressionParser struct {
	tokens []expressionToken
	at     int
}

func (p *expressionParser) current() expressionToken { return p.tokens[p.at] }
func (p *expressionParser) take(text string) bool {
	if p.current().text == text {
		p.at++
		return true
	}
	return false
}
func (p *expressionParser) parse() (Expr, error) {
	if p.current().kind == "eof" {
		return Expr{}, fmt.Errorf("%w: empty expression", ErrExpressionSyntax)
	}
	e, err := p.parseOr()
	if err != nil {
		return Expr{}, err
	}
	if p.current().kind != "eof" {
		return Expr{}, fmt.Errorf("%w at %d: unexpected %q", ErrExpressionSyntax, p.current().pos, p.current().text)
	}
	return e, nil
}
func (p *expressionParser) parseOr() (Expr, error) {
	return p.parseBinary(p.parseAnd, map[string]BinaryOp{"||": BinaryOpOr})
}
func (p *expressionParser) parseAnd() (Expr, error) {
	return p.parseBinary(p.parseCompare, map[string]BinaryOp{"&&": BinaryOpAnd})
}
func (p *expressionParser) parseCompare() (Expr, error) {
	return p.parseBinary(p.parseAdd, map[string]BinaryOp{"==": BinaryOpEqual, "!=": BinaryOpNotEqual, "<": BinaryOpLess, "<=": BinaryOpLessOrEqual, ">": BinaryOpGreater, ">=": BinaryOpGreaterOrEqual})
}
func (p *expressionParser) parseAdd() (Expr, error) {
	return p.parseBinary(p.parseMultiply, map[string]BinaryOp{"+": BinaryOpAdd, "-": BinaryOpSubtract})
}
func (p *expressionParser) parseMultiply() (Expr, error) {
	return p.parseBinary(p.parseUnary, map[string]BinaryOp{"*": BinaryOpMultiply, "/": BinaryOpDivide})
}
func (p *expressionParser) parseBinary(next func() (Expr, error), ops map[string]BinaryOp) (Expr, error) {
	left, err := next()
	if err != nil {
		return Expr{}, err
	}
	for {
		op, ok := ops[p.current().text]
		if !ok {
			return left, nil
		}
		p.at++
		right, err := next()
		if err != nil {
			return Expr{}, err
		}
		left = Binary(op, left, right)
	}
}
func (p *expressionParser) parseUnary() (Expr, error) {
	if p.take("!") {
		child, err := p.parseUnary()
		if err != nil {
			return Expr{}, err
		}
		return Unary(UnaryOpNot, child), nil
	}
	if p.take("-") {
		child, err := p.parseUnary()
		if err != nil {
			return Expr{}, err
		}
		return Unary(UnaryOpNegate, child), nil
	}
	return p.parsePrimary()
}
func (p *expressionParser) parsePrimary() (Expr, error) {
	t := p.current()
	if p.take("(") {
		e, err := p.parseOr()
		if err != nil {
			return Expr{}, err
		}
		if !p.take(")") {
			return Expr{}, fmt.Errorf("%w at %d: missing )", ErrExpressionSyntax, p.current().pos)
		}
		return e, nil
	}
	p.at++
	switch t.kind {
	case "number":
		if strings.Contains(t.text, ".") {
			d, err := values.NewDecimal(t.text, int32(len(t.text)-strings.IndexByte(t.text, '.')-1), values.RoundingHalfEven)
			if err != nil {
				return Expr{}, fmt.Errorf("%w at %d: %v", ErrExpressionValue, t.pos, err)
			}
			return DecimalLiteral(d), nil
		}
		v, err := strconv.ParseInt(t.text, 10, 64)
		if err != nil {
			return Expr{}, fmt.Errorf("%w at %d: %v", ErrExpressionValue, t.pos, err)
		}
		return IntLiteral(v), nil
	case "string":
		return StringLiteral(t.text), nil
	case "ident":
		if t.text == "true" {
			return BoolLiteral(true), nil
		}
		if t.text == "false" {
			return BoolLiteral(false), nil
		}
		if t.text == "null" {
			return Unknown(), nil
		}
		if p.take("(") {
			var args []Expr
			if !p.take(")") {
				for {
					arg, err := p.parseOr()
					if err != nil {
						return Expr{}, err
					}
					args = append(args, arg)
					if p.take(")") {
						break
					}
					if !p.take(",") {
						return Expr{}, fmt.Errorf("%w at %d: call arguments require comma", ErrExpressionSyntax, p.current().pos)
					}
				}
			}
			return Call(t.text, args...), nil
		}
		return Variable(t.text, ExpressionTypeUnspecified), nil
	default:
		return Expr{}, fmt.Errorf("%w at %d: expected expression", ErrExpressionSyntax, t.pos)
	}
}
