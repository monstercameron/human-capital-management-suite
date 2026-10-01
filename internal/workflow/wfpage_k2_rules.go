package workflow

// This file contains the small, data-only rule language used by workflow
// input pages. It intentionally has no reflection, templates, or callbacks:
// the same compiled program can be evaluated by the server and the wasm
// client, and a rule can only read the paths declared by its author.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const MaxPageRuleCost = 256

var (
	ErrPageRuleInvalid       = errors.New("workflow page rule: invalid rule")
	ErrPageRuleType          = errors.New("workflow page rule: type error")
	ErrPageRuleCost          = errors.New("workflow page rule: cost bound exceeded")
	ErrPageRuleInput         = errors.New("workflow page rule: undeclared input")
	ErrPageRuleRuntime       = errors.New("workflow page rule: runtime error")
	ErrPageRuleMoneyCurrency = errors.New("workflow page rule: money currency mismatch")
)

// PageRule is a closed, declarative validation rule. Inputs are the complete
// read boundary; References names bounded tenant reference sets.
type PageRule struct {
	Code          string
	Message       string
	Expression    string
	Inputs        map[string]ValueType
	Parameters    map[string]ValueType
	References    map[string]ValueType
	RepeatingCaps map[string]int
	CostLimit     int
}

// PageRuleInput is the only runtime data visible to a rule.
type PageRuleInput struct {
	Fields     map[string]any
	Parameters map[string]any
	Repeating  map[string][]map[string]any
	References map[string][]any
}

type PageRuleFinding struct {
	Code    string
	Field   string
	Message string
}

type PageRuleResult struct {
	Valid    bool
	Findings []PageRuleFinding
}

type MoneyValue struct {
	Currency string
	Amount   *big.Rat
}

func NewMoneyValue(currency, amount string) (MoneyValue, error) {
	if len(currency) != 3 || strings.ToUpper(currency) != currency || strings.TrimSpace(amount) == "" {
		return MoneyValue{}, fmt.Errorf("%w: invalid money literal", ErrPageRuleInvalid)
	}
	r, ok := new(big.Rat).SetString(amount)
	if !ok {
		return MoneyValue{}, fmt.Errorf("%w: invalid money amount", ErrPageRuleInvalid)
	}
	return MoneyValue{Currency: currency, Amount: r}, nil
}

type compiledPageRule struct {
	definition PageRule
	root       *ruleNode
	cost       int
	fields     []string
}

// CompiledPageRule is immutable and safe to share between a browser client
// and a server handler.
type CompiledPageRule struct{ compiledPageRule }

func (r PageRule) Compile() (CompiledPageRule, error) {
	if strings.TrimSpace(r.Code) == "" || strings.TrimSpace(r.Message) == "" || strings.TrimSpace(r.Expression) == "" {
		return CompiledPageRule{}, fmt.Errorf("%w: code, message and expression are required", ErrPageRuleInvalid)
	}
	if len(r.Inputs) == 0 {
		return CompiledPageRule{}, fmt.Errorf("%w: at least one declared input is required", ErrPageRuleInvalid)
	}
	for path, typ := range r.Inputs {
		if strings.TrimSpace(path) == "" || strings.ContainsAny(path, " \t\n") {
			return CompiledPageRule{}, fmt.Errorf("%w: invalid input path %q", ErrPageRuleInvalid, path)
		}
		if err := typ.Validate(); err != nil {
			return CompiledPageRule{}, fmt.Errorf("%w: input %s: %v", ErrPageRuleType, path, err)
		}
	}
	for key, typ := range r.Parameters {
		if strings.TrimSpace(key) == "" {
			return CompiledPageRule{}, fmt.Errorf("%w: parameter key is required", ErrPageRuleInvalid)
		}
		if err := typ.Validate(); err != nil {
			return CompiledPageRule{}, fmt.Errorf("%w: parameter %s: %v", ErrPageRuleType, key, err)
		}
	}
	for name, typ := range r.References {
		if strings.TrimSpace(name) == "" || (typ.Kind != KindString && typ.Kind != KindEnum) {
			return CompiledPageRule{}, fmt.Errorf("%w: reference %q must contain strings or enums", ErrPageRuleType, name)
		}
	}
	for name, cap := range r.RepeatingCaps {
		if cap <= 0 || cap > 1000 {
			return CompiledPageRule{}, fmt.Errorf("%w: repeating cap for %s must be 1..1000", ErrPageRuleInvalid, name)
		}
	}
	limit := r.CostLimit
	if limit == 0 {
		limit = MaxPageRuleCost
	}
	if limit < 1 || limit > MaxPageRuleCost {
		return CompiledPageRule{}, fmt.Errorf("%w: limit must be 1..%d", ErrPageRuleCost, MaxPageRuleCost)
	}
	root, err := parsePageRule(r.Expression)
	if err != nil {
		return CompiledPageRule{}, err
	}
	fields := make(map[string]bool)
	cost, err := typeCheckPageRule(root, r, fields)
	if err != nil {
		return CompiledPageRule{}, err
	}
	if cost > limit {
		return CompiledPageRule{}, fmt.Errorf("%w: cost %d exceeds %d", ErrPageRuleCost, cost, limit)
	}
	used := make([]string, 0, len(fields))
	for field := range fields {
		used = append(used, field)
	}
	sort.Strings(used)
	return CompiledPageRule{compiledPageRule: compiledPageRule{definition: clonePageRule(r), root: root, cost: cost, fields: used}}, nil
}

func CompilePageRule(r PageRule) (CompiledPageRule, error) { return r.Compile() }

func (r CompiledPageRule) Definition() PageRule { return clonePageRule(r.definition) }
func (r CompiledPageRule) Cost() int            { return r.cost }
func (r CompiledPageRule) InputPaths() []string { return append([]string(nil), r.fields...) }

func (r CompiledPageRule) Evaluate(input PageRuleInput) (PageRuleResult, error) {
	if r.root == nil {
		return PageRuleResult{}, fmt.Errorf("%w: rule is not compiled", ErrPageRuleInvalid)
	}
	for name, rows := range input.Repeating {
		if cap, ok := r.definition.RepeatingCaps[name]; ok && len(rows) > cap {
			return PageRuleResult{}, fmt.Errorf("%w: repeating group %s has %d rows, cap is %d", ErrPageRuleRuntime, name, len(rows), cap)
		}
	}
	value, err := evalPageRule(r.root, input)
	if err != nil {
		return PageRuleResult{}, err
	}
	valid, ok := value.(bool)
	if !ok {
		return PageRuleResult{}, fmt.Errorf("%w: rule did not produce BOOL", ErrPageRuleRuntime)
	}
	result := PageRuleResult{Valid: valid}
	if !valid {
		field := ""
		if len(r.fields) > 0 {
			field = r.fields[0]
		}
		result.Findings = []PageRuleFinding{{Code: r.definition.Code, Field: field, Message: r.definition.Message}}
	}
	return result, nil
}

func EvaluatePageRule(rule PageRule, input PageRuleInput) (PageRuleResult, error) {
	compiled, err := rule.Compile()
	if err != nil {
		return PageRuleResult{}, err
	}
	return compiled.Evaluate(input)
}

// EvaluatePageExpression evaluates a declared scalar expression for a
// read-only/computed field. It shares the parser, type checker and limits of
// PageRule but does not require the root value to be BOOL.
func EvaluatePageExpression(expression string, inputs map[string]ValueType, references map[string]ValueType, repeatingCaps map[string]int, expected ValueType, input PageRuleInput) (any, error) {
	root, err := parsePageRule(expression)
	if err != nil {
		return nil, err
	}
	rule := PageRule{Code: "computed", Message: "computed", Expression: expression, Inputs: inputs, References: references, RepeatingCaps: repeatingCaps, CostLimit: MaxPageRuleCost}
	typ, cost, _, err := typeCheckNode(root, rule)
	if err != nil {
		return nil, err
	}
	if cost > MaxPageRuleCost {
		return nil, ErrPageRuleCost
	}
	if err := typ.AssignableTo(expected); err != nil {
		return nil, fmt.Errorf("%w: computed expression: %v", ErrPageRuleType, err)
	}
	return evalPageRule(root, input)
}

// Canonical is stable across map iteration and is suitable for a published
// page digest. It is deliberately a plain, non-executable representation.
func (r PageRule) Canonical() string {
	keys := make([]string, 0, len(r.Inputs))
	for key := range r.Inputs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{"hcmnext.workflow.page-rule/v1", r.Code, r.Message, r.Expression, strconv.Itoa(r.CostLimit)}
	for _, key := range keys {
		parts = append(parts, "input", key, r.Inputs[key].String())
	}
	parameters := make([]string, 0, len(r.Parameters))
	for key := range r.Parameters {
		parameters = append(parameters, key)
	}
	sort.Strings(parameters)
	for _, key := range parameters {
		parts = append(parts, "parameter", key, r.Parameters[key].String())
	}
	refs := make([]string, 0, len(r.References))
	for key := range r.References {
		refs = append(refs, key)
	}
	sort.Strings(refs)
	for _, key := range refs {
		parts = append(parts, "ref", key, r.References[key].String())
	}
	caps := make([]string, 0, len(r.RepeatingCaps))
	for key := range r.RepeatingCaps {
		caps = append(caps, key)
	}
	sort.Strings(caps)
	for _, key := range caps {
		parts = append(parts, "cap", key, strconv.Itoa(r.RepeatingCaps[key]))
	}
	return strings.Join(parts, "\x00")
}

func (r PageRule) Digest() string {
	sum := sha256.Sum256([]byte(r.Canonical()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func clonePageRule(r PageRule) PageRule {
	r.Inputs = cloneRuleTypes(r.Inputs)
	r.Parameters = cloneRuleTypes(r.Parameters)
	r.References = cloneRuleTypes(r.References)
	r.RepeatingCaps = cloneRuleCaps(r.RepeatingCaps)
	return r
}
func cloneRuleTypes(in map[string]ValueType) map[string]ValueType {
	if in == nil {
		return nil
	}
	out := make(map[string]ValueType, len(in))
	for key, typ := range in {
		out[key] = typ.clone()
	}
	return out
}
func cloneRuleCaps(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, cap := range in {
		out[key] = cap
	}
	return out
}

type ruleNode struct {
	kind  string
	text  string
	left  *ruleNode
	right *ruleNode
	args  []*ruleNode
}

type ruleToken struct{ kind, text string }

func tokenizePageRule(source string) ([]ruleToken, error) {
	var out []ruleToken
	for i := 0; i < len(source); {
		if unicode.IsSpace(rune(source[i])) {
			i++
			continue
		}
		c := source[i]
		switch {
		case c == '(' || c == ')' || c == ',':
			out = append(out, ruleToken{kind: string(c), text: string(c)})
			i++
		case c == '\'' || c == '"':
			quote := c
			start := i
			i++
			var b strings.Builder
			for i < len(source) && source[i] != quote {
				if source[i] == '\\' && i+1 < len(source) {
					i++
					b.WriteByte(source[i])
					i++
					continue
				}
				b.WriteByte(source[i])
				i++
			}
			if i >= len(source) {
				return nil, fmt.Errorf("%w: unterminated string at %d", ErrPageRuleInvalid, start)
			}
			i++
			out = append(out, ruleToken{kind: "string", text: b.String()})
		case strings.ContainsRune("=!<>", rune(c)):
			start := i
			i++
			if i < len(source) && source[i] == '=' {
				i++
			}
			out = append(out, ruleToken{kind: "op", text: source[start:i]})
		case c == '&' || c == '|':
			if i+1 >= len(source) || source[i+1] != c {
				return nil, fmt.Errorf("%w: boolean operators need two characters", ErrPageRuleInvalid)
			}
			out = append(out, ruleToken{kind: "op", text: source[i : i+2]})
			i += 2
		case c == '!':
			out = append(out, ruleToken{kind: "op", text: "!"})
			i++
		case unicode.IsLetter(rune(c)) || c == '_':
			start := i
			i++
			for i < len(source) && (unicode.IsLetter(rune(source[i])) || unicode.IsDigit(rune(source[i])) || source[i] == '_' || source[i] == '.') {
				i++
			}
			out = append(out, ruleToken{kind: "ident", text: source[start:i]})
		case unicode.IsDigit(rune(c)) || c == '-':
			start := i
			i++
			for i < len(source) && (unicode.IsDigit(rune(source[i])) || source[i] == '.' || source[i] == '-') {
				i++
			}
			out = append(out, ruleToken{kind: "number", text: source[start:i]})
		default:
			return nil, fmt.Errorf("%w: unexpected character %q", ErrPageRuleInvalid, c)
		}
	}
	return append(out, ruleToken{kind: "eof"}), nil
}

type pageRuleParser struct {
	tokens []ruleToken
	pos    int
}

func parsePageRule(source string) (*ruleNode, error) {
	tokens, err := tokenizePageRule(source)
	if err != nil {
		return nil, err
	}
	p := &pageRuleParser{tokens: tokens}
	root, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != "eof" {
		return nil, fmt.Errorf("%w: unexpected token %q", ErrPageRuleInvalid, p.peek().text)
	}
	return root, nil
}
func (p *pageRuleParser) peek() ruleToken { return p.tokens[p.pos] }
func (p *pageRuleParser) take() ruleToken { token := p.tokens[p.pos]; p.pos++; return token }
func (p *pageRuleParser) accept(kind, text string) bool {
	if p.peek().kind == kind && (text == "" || p.peek().text == text) {
		p.pos++
		return true
	}
	return false
}
func (p *pageRuleParser) parseOr() (*ruleNode, error) {
	left, err := p.parseAnd()
	for err == nil && p.accept("op", "||") {
		var right *ruleNode
		right, err = p.parseAnd()
		left = &ruleNode{kind: "or", left: left, right: right}
	}
	return left, err
}
func (p *pageRuleParser) parseAnd() (*ruleNode, error) {
	left, err := p.parseCompare()
	for err == nil && p.accept("op", "&&") {
		var right *ruleNode
		right, err = p.parseCompare()
		left = &ruleNode{kind: "and", left: left, right: right}
	}
	return left, err
}
func (p *pageRuleParser) parseCompare() (*ruleNode, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	if p.peek().kind == "ident" && p.peek().text == "in" {
		p.take()
		right, e := p.parseUnary()
		if e != nil {
			return nil, e
		}
		return &ruleNode{kind: "in", left: left, right: right}, nil
	}
	if p.peek().kind == "op" && strings.Contains("== != < <= > >=", p.peek().text) {
		op := p.take().text
		right, e := p.parseUnary()
		if e != nil {
			return nil, e
		}
		return &ruleNode{kind: op, left: left, right: right}, nil
	}
	return left, nil
}
func (p *pageRuleParser) parseUnary() (*ruleNode, error) {
	if p.accept("op", "!") {
		child, err := p.parseUnary()
		return &ruleNode{kind: "not", left: child}, err
	}
	return p.parsePrimary()
}
func (p *pageRuleParser) parsePrimary() (*ruleNode, error) {
	if p.accept("(", "") {
		n, err := p.parseOr()
		if !p.accept(")", "") && err == nil {
			return nil, fmt.Errorf("%w: missing closing parenthesis", ErrPageRuleInvalid)
		}
		return n, err
	}
	t := p.take()
	switch t.kind {
	case "string":
		return &ruleNode{kind: "string", text: t.text}, nil
	case "number":
		return &ruleNode{kind: "number", text: t.text}, nil
	case "ident":
		if t.text == "true" || t.text == "false" {
			return &ruleNode{kind: "bool", text: t.text}, nil
		}
		if p.accept("(", "") {
			var args []*ruleNode
			if !p.accept(")", "") {
				for {
					arg, err := p.parseOr()
					if err != nil {
						return nil, err
					}
					args = append(args, arg)
					if p.accept(")", "") {
						break
					}
					if !p.accept(",", "") {
						return nil, fmt.Errorf("%w: function arguments need commas", ErrPageRuleInvalid)
					}
				}
			}
			return &ruleNode{kind: "call", text: t.text, args: args}, nil
		}
		return &ruleNode{kind: "field", text: t.text}, nil
	default:
		return nil, fmt.Errorf("%w: expected expression, got %q", ErrPageRuleInvalid, t.text)
	}
}

func typeCheckPageRule(n *ruleNode, rule PageRule, fields map[string]bool) (int, error) {
	typ, cost, refs, err := typeCheckNode(n, rule)
	for ref := range refs {
		fields[ref] = true
	}
	if err != nil {
		return 0, err
	}
	if typ.Kind != KindBool {
		return 0, fmt.Errorf("%w: root expression is %s, want BOOL", ErrPageRuleType, typ)
	}
	return cost, nil
}
func typeCheckNode(n *ruleNode, rule PageRule) (ValueType, int, map[string]bool, error) {
	refs := map[string]bool{}
	if n == nil {
		return ValueType{}, 0, refs, fmt.Errorf("%w: empty expression", ErrPageRuleInvalid)
	}
	join := func(a, b map[string]bool) map[string]bool {
		for key := range b {
			a[key] = true
		}
		return a
	}
	switch n.kind {
	case "bool":
		return ValueType{Kind: KindBool}, 1, refs, nil
	case "string":
		return ValueType{Kind: KindString}, 1, refs, nil
	case "number":
		if strings.Contains(n.text, ".") {
			return ValueType{Kind: KindDecimal}, 1, refs, nil
		}
		return ValueType{Kind: KindInteger}, 1, refs, nil
	case "field":
		typ, ok := rule.Inputs[n.text]
		if !ok {
			return ValueType{}, 0, refs, fmt.Errorf("%w: %s", ErrPageRuleInput, n.text)
		}
		refs[n.text] = true
		return typ, 1, refs, nil
	case "and", "or":
		left, lc, lr, err := typeCheckNode(n.left, rule)
		if err != nil {
			return ValueType{}, 0, refs, err
		}
		right, rc, rr, err := typeCheckNode(n.right, rule)
		if err != nil {
			return ValueType{}, 0, refs, err
		}
		join(refs, lr)
		join(refs, rr)
		if left.Kind != KindBool || right.Kind != KindBool {
			return ValueType{}, 0, refs, fmt.Errorf("%w: boolean operands required", ErrPageRuleType)
		}
		return ValueType{Kind: KindBool}, lc + rc + 1, refs, nil
	case "not":
		child, c, cr, err := typeCheckNode(n.left, rule)
		join(refs, cr)
		if err != nil {
			return ValueType{}, 0, refs, err
		}
		if child.Kind != KindBool {
			return ValueType{}, 0, refs, fmt.Errorf("%w: ! requires BOOL", ErrPageRuleType)
		}
		return ValueType{Kind: KindBool}, c + 1, refs, nil
	case "==", "!=", "<", "<=", ">", ">=":
		left, lc, lr, err := typeCheckNode(n.left, rule)
		if err != nil {
			return ValueType{}, 0, refs, err
		}
		right, rc, rr, err := typeCheckNode(n.right, rule)
		if err != nil {
			return ValueType{}, 0, refs, err
		}
		join(refs, lr)
		join(refs, rr)
		if left.Kind != right.Kind || (left.Kind != KindMoney && left.Brand != right.Brand) {
			return ValueType{}, 0, refs, fmt.Errorf("%w: cannot compare %s and %s", ErrPageRuleType, left, right)
		}
		if left.Kind == KindMoney && left.Brand != "" && right.Brand != "" && left.Brand != right.Brand {
			return ValueType{}, 0, refs, ErrPageRuleMoneyCurrency
		}
		return ValueType{Kind: KindBool}, lc + rc + 1, refs, nil
	case "in":
		left, lc, lr, err := typeCheckNode(n.left, rule)
		if err != nil {
			return ValueType{}, 0, refs, err
		}
		_, rc, rr, err := typeCheckNode(n.right, rule)
		if err != nil {
			return ValueType{}, 0, refs, err
		}
		join(refs, lr)
		join(refs, rr)
		if n.right.kind != "call" || n.right.text != "ref" || len(n.right.args) != 1 || n.right.args[0].kind != "string" {
			return ValueType{}, 0, refs, fmt.Errorf("%w: membership requires ref(\"name\")", ErrPageRuleType)
		}
		name := n.right.args[0].text
		declared, ok := rule.References[name]
		if !ok || declared.Kind != left.Kind {
			return ValueType{}, 0, refs, fmt.Errorf("%w: reference %s is not declared for %s", ErrPageRuleType, name, left)
		}
		_ = rc
		return ValueType{Kind: KindBool}, lc + rc + 2, refs, nil
	case "call":
		switch n.text {
		case "param":
			if len(n.args) != 1 || n.args[0].kind != "string" {
				return ValueType{}, 0, refs, fmt.Errorf("%w: param requires one string", ErrPageRuleType)
			}
			typ, ok := rule.Parameters[n.args[0].text]
			if !ok {
				return ValueType{}, 0, refs, fmt.Errorf("%w: parameter %s", ErrPageRuleInput, n.args[0].text)
			}
			return typ, 2, refs, nil
		case "ref":
			if len(n.args) != 1 || n.args[0].kind != "string" {
				return ValueType{}, 0, refs, fmt.Errorf("%w: ref requires one string", ErrPageRuleType)
			}
			if _, ok := rule.References[n.args[0].text]; !ok {
				return ValueType{}, 0, refs, fmt.Errorf("%w: reference %s is not declared", ErrPageRuleInput, n.args[0].text)
			}
			return ValueType{Kind: KindList, Element: &ValueType{Kind: KindString}}, 2, refs, nil
		case "date":
			if len(n.args) != 1 || n.args[0].kind != "string" {
				return ValueType{}, 0, refs, fmt.Errorf("%w: date requires one string", ErrPageRuleType)
			}
			if _, err := time.Parse("2006-01-02", n.args[0].text); err != nil {
				return ValueType{}, 0, refs, fmt.Errorf("%w: invalid date", ErrPageRuleType)
			}
			return ValueType{Kind: KindLocalDate}, 3, refs, nil
		case "date_add":
			if len(n.args) != 2 {
				return ValueType{}, 0, refs, fmt.Errorf("%w: date_add requires date and integer", ErrPageRuleType)
			}
			dateType, dc, dr, err := typeCheckNode(n.args[0], rule)
			if err != nil {
				return ValueType{}, 0, refs, err
			}
			numberType, nc, nr, err := typeCheckNode(n.args[1], rule)
			join(refs, dr)
			join(refs, nr)
			if err != nil {
				return ValueType{}, 0, refs, err
			}
			if dateType.Kind != KindLocalDate || numberType.Kind != KindInteger {
				return ValueType{}, 0, refs, fmt.Errorf("%w: date_add requires LOCAL_DATE and INTEGER", ErrPageRuleType)
			}
			return dateType, dc + nc + 3, refs, nil
		case "sum":
			if len(n.args) != 1 || n.args[0].kind != "field" {
				return ValueType{}, 0, refs, fmt.Errorf("%w: sum requires a declared repeating field", ErrPageRuleType)
			}
			typ, ok := rule.Inputs[n.args[0].text]
			if !ok || (typ.Kind != KindInteger && typ.Kind != KindDecimal && typ.Kind != KindMoney) {
				return ValueType{}, 0, refs, fmt.Errorf("%w: sum input is not numeric", ErrPageRuleType)
			}
			group := strings.SplitN(n.args[0].text, ".", 2)[0]
			if _, ok := rule.RepeatingCaps[group]; !ok {
				return ValueType{}, 0, refs, fmt.Errorf("%w: sum requires a repeating cap for %s", ErrPageRuleCost, group)
			}
			refs[n.args[0].text] = true
			return typ, 5, refs, nil
		case "count":
			if len(n.args) != 1 || n.args[0].kind != "string" {
				return ValueType{}, 0, refs, fmt.Errorf("%w: count requires a repeating group name", ErrPageRuleType)
			}
			if _, ok := rule.RepeatingCaps[n.args[0].text]; !ok {
				return ValueType{}, 0, refs, fmt.Errorf("%w: count group is not bounded", ErrPageRuleCost)
			}
			return ValueType{Kind: KindInteger}, 3, refs, nil
		case "money":
			if len(n.args) != 2 || n.args[0].kind != "string" || n.args[1].kind != "string" {
				return ValueType{}, 0, refs, fmt.Errorf("%w: money requires currency and amount strings", ErrPageRuleType)
			}
			if _, err := NewMoneyValue(n.args[0].text, n.args[1].text); err != nil {
				return ValueType{}, 0, refs, err
			}
			return ValueType{Kind: KindMoney, Brand: n.args[0].text}, 4, refs, nil
		default:
			return ValueType{}, 0, refs, fmt.Errorf("%w: function %s is not allowed", ErrPageRuleInvalid, n.text)
		}
	default:
		return ValueType{}, 0, refs, fmt.Errorf("%w: node %s is not allowed", ErrPageRuleInvalid, n.kind)
	}
}

func evalPageRule(n *ruleNode, input PageRuleInput) (any, error) {
	switch n.kind {
	case "bool":
		return n.text == "true", nil
	case "string":
		return n.text, nil
	case "number":
		if strings.Contains(n.text, ".") {
			r, ok := new(big.Rat).SetString(n.text)
			if !ok {
				return nil, fmt.Errorf("%w: bad decimal", ErrPageRuleRuntime)
			}
			return r, nil
		}
		i, err := strconv.ParseInt(n.text, 10, 64)
		return i, err
	case "field":
		value, ok := input.Fields[n.text]
		if !ok {
			return nil, fmt.Errorf("%w: missing field %s", ErrPageRuleRuntime, n.text)
		}
		return value, nil
	case "and", "or":
		left, err := evalPageRule(n.left, input)
		if err != nil {
			return nil, err
		}
		right, err := evalPageRule(n.right, input)
		if err != nil {
			return nil, err
		}
		lb, lok := left.(bool)
		rb, rok := right.(bool)
		if !lok || !rok {
			return nil, fmt.Errorf("%w: boolean value required", ErrPageRuleRuntime)
		}
		if n.kind == "and" {
			return lb && rb, nil
		}
		return lb || rb, nil
	case "not":
		value, err := evalPageRule(n.left, input)
		b, ok := value.(bool)
		if err != nil || !ok {
			return nil, fmt.Errorf("%w: boolean value required", ErrPageRuleRuntime)
		}
		return !b, nil
	case "in":
		left, err := evalPageRule(n.left, input)
		if err != nil {
			return nil, err
		}
		if n.right.kind != "call" || len(n.right.args) != 1 {
			return nil, fmt.Errorf("%w: invalid membership", ErrPageRuleRuntime)
		}
		name := n.right.args[0].text
		values := input.References[name]
		for _, value := range values {
			if fmt.Sprint(value) == fmt.Sprint(left) {
				return true, nil
			}
		}
		return false, nil
	case "==", "!=", "<", "<=", ">", ">=":
		left, err := evalPageRule(n.left, input)
		if err != nil {
			return nil, err
		}
		right, err := evalPageRule(n.right, input)
		if err != nil {
			return nil, err
		}
		cmp, err := compareRuleValues(left, right)
		if err != nil {
			return nil, err
		}
		switch n.kind {
		case "==":
			return cmp == 0, nil
		case "!=":
			return cmp != 0, nil
		case "<":
			return cmp < 0, nil
		case "<=":
			return cmp <= 0, nil
		case ">":
			return cmp > 0, nil
		default:
			return cmp >= 0, nil
		}
	case "call":
		switch n.text {
		case "param":
			value, ok := input.Parameters[n.args[0].text]
			if !ok {
				return nil, fmt.Errorf("%w: missing parameter %s", ErrPageRuleRuntime, n.args[0].text)
			}
			return value, nil
		case "ref":
			return input.References[n.args[0].text], nil
		case "date":
			return time.Parse("2006-01-02", n.args[0].text)
		case "date_add":
			value, err := evalPageRule(n.args[0], input)
			if err != nil {
				return nil, err
			}
			date, ok := value.(time.Time)
			if !ok {
				return nil, fmt.Errorf("%w: date_add input", ErrPageRuleRuntime)
			}
			days, err := evalPageRule(n.args[1], input)
			if err != nil {
				return nil, err
			}
			number, ok := integerValue(days)
			if !ok {
				return nil, fmt.Errorf("%w: date_add days", ErrPageRuleRuntime)
			}
			return date.AddDate(0, 0, int(number)), nil
		case "sum":
			parts := strings.SplitN(n.args[0].text, ".", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("%w: sum path", ErrPageRuleRuntime)
			}
			field, group, column := n.args[0].text, parts[0], parts[1]
			var total *big.Rat
			var money MoneyValue
			for index, row := range input.Repeating[group] {
				value, ok := row[column]
				if !ok {
					return nil, fmt.Errorf("%w: missing row %d field %s", ErrPageRuleRuntime, index, field)
				}
				if mv, ok := value.(MoneyValue); ok {
					if total != nil && money.Currency != mv.Currency {
						return nil, ErrPageRuleMoneyCurrency
					}
					if total == nil {
						money = mv
						total = new(big.Rat)
					}
					total.Add(total, mv.Amount)
					continue
				}
				number, ok := rationalValue(value)
				if !ok {
					return nil, fmt.Errorf("%w: sum value %s", ErrPageRuleRuntime, field)
				}
				if total == nil {
					total = new(big.Rat)
				}
				total.Add(total, number)
			}
			if total == nil {
				total = new(big.Rat)
			}
			if money.Currency != "" {
				money.Amount = total
				return money, nil
			}
			return total, nil
		case "count":
			return int64(len(input.Repeating[n.args[0].text])), nil
		case "money":
			return NewMoneyValue(n.args[0].text, n.args[1].text)
		}
	}
	return nil, fmt.Errorf("%w: unsupported expression", ErrPageRuleRuntime)
}

func integerValue(value any) (int64, bool) {
	switch n := value.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case int32:
		return int64(n), true
	}
	return 0, false
}
func rationalValue(value any) (*big.Rat, bool) {
	switch n := value.(type) {
	case *big.Rat:
		return new(big.Rat).Set(n), true
	case int:
		return new(big.Rat).SetInt64(int64(n)), true
	case int64:
		return new(big.Rat).SetInt64(n), true
	case string:
		r, ok := new(big.Rat).SetString(n)
		return r, ok
	}
	return nil, false
}
func compareRuleValues(left, right any) (int, error) {
	if lm, ok := left.(MoneyValue); ok {
		rm, rok := right.(MoneyValue)
		if !rok || lm.Currency != rm.Currency {
			return 0, ErrPageRuleMoneyCurrency
		}
		return lm.Amount.Cmp(rm.Amount), nil
	}
	if ld, ok := left.(time.Time); ok {
		rd, rok := right.(time.Time)
		if !rok {
			return 0, fmt.Errorf("%w: date comparison", ErrPageRuleRuntime)
		}
		if ld.Before(rd) {
			return -1, nil
		}
		if ld.After(rd) {
			return 1, nil
		}
		return 0, nil
	}
	if lr, ok := rationalValue(left); ok {
		rr, rok := rationalValue(right)
		if !rok {
			return 0, fmt.Errorf("%w: numeric comparison", ErrPageRuleRuntime)
		}
		return lr.Cmp(rr), nil
	}
	ls, lok := left.(string)
	rs, rok := right.(string)
	if lok && rok {
		return strings.Compare(ls, rs), nil
	}
	if lb, lok := left.(bool); lok {
		rb, rok := right.(bool)
		if rok {
			if lb == rb {
				return 0, nil
			}
			if !lb {
				return -1, nil
			}
			return 1, nil
		}
	}
	return 0, fmt.Errorf("%w: values are not comparable", ErrPageRuleRuntime)
}
