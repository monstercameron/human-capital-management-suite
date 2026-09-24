// Package ir compiles a transformation.TransformationDefinition into a
// bounded, immutable intermediate representation: a finite instruction set
// with declared execution limits, a canonical digest, and a human-readable
// explanation.
//
// The instruction set is closed by construction (OpCode is one of six
// declared values; Function is one of a declared, named set) so there is no
// way to author "arbitrary code" through this package -- an instruction can
// only ever be one of the shapes Validate already knows how to check. The
// Program itself is a flat, non-recursive instruction list: nothing in this
// package lets one Program invoke another, so "no recursion" is a
// structural property, not a runtime check. What Validate does check is the
// remaining hazard for a flat instruction list: a later instruction reading
// a field an earlier instruction writes can still form a cycle (A depends
// on B's output, B depends on A's output), so Validate builds that
// dependency graph and rejects a cycle explicitly.
package ir

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
)

// IRVersion is this package's own contract version, independent of the
// transformation.ContractVersion a compiled definition happens to declare.
const IRVersion = 1

// DigestAlgorithm names the hash used by Program.Digest.
const DigestAlgorithm = "sha256"

var (
	// ErrUnresolved covers every "this name does not resolve" compilation
	// failure: an unresolved field, an unresolved/mismatched type, or a
	// Function value outside the declared vocabulary.
	ErrUnresolved = errors.New("transformation/ir: unresolved field, type, or function")
	// ErrCycle reports a dependency cycle between instructions.
	ErrCycle = errors.New("transformation/ir: dependency cycle")
	// ErrUnboundedProgram reports a program or instruction that exceeds a
	// declared bound (max steps, max fan-out).
	ErrUnboundedProgram = errors.New("transformation/ir: unbounded expansion")
	// ErrNondeterministic reports two instructions racing to write the same
	// destination: replaying the program could observably differ depending
	// on instruction order, which this IR never allows.
	ErrNondeterministic = errors.New("transformation/ir: nondeterministic operator")
	// ErrInvalidProgram covers a structurally malformed program or
	// instruction that is none of the above (e.g. a mis-shaped operand
	// list for its opcode, or non-positive declared limits).
	ErrInvalidProgram = errors.New("transformation/ir: invalid program")
)

// OpCode is the closed instruction vocabulary. There is no "eval" or
// "script" opcode, and Validate rejects any OpCode outside this list.
type OpCode string

const (
	// OpMap applies a declared Function to at most one source field (or a
	// declared Literal when there is no source) and writes Destination.
	OpMap OpCode = "map"
	// OpFilter applies a declared predicate Function to exactly one source
	// field and passes it through to Destination, or leaves it absent.
	OpFilter OpCode = "filter"
	// OpProject copies exactly one source field to Destination unchanged
	// (a straight field select or rename).
	OpProject OpCode = "project"
	// OpJoinByKey combines exactly two source fields on a declared JoinKey.
	OpJoinByKey OpCode = "join_by_key"
	// OpAggregate applies a declared Function across one or more source
	// fields (bounded by the program's fan-out limit) to produce
	// Destination.
	OpAggregate OpCode = "aggregate"
	// OpCoerce converts exactly one source field to a declared TargetType.
	OpCoerce OpCode = "coerce"
)

// Function is the closed vocabulary of declared functions an aggregate,
// filter predicate, or map may reference. Naming a function outside this
// set is exactly what "undeclared function" means here -- there is no
// mechanism to name or load code dynamically.
type Function string

const (
	FuncDefault    Function = "default"
	FuncConcat     Function = "concat"
	FuncEquals     Function = "equals"
	FuncNotNull    Function = "not_null"
	FuncSum        Function = "sum"
	FuncCount      Function = "count"
	FuncMin        Function = "min"
	FuncMax        Function = "max"
	FuncTrim       Function = "trim"
	FuncUpper      Function = "upper"
	FuncLower      Function = "lower"
	FuncLookup     Function = "lookup"
	FuncDateParse  Function = "date_parse"
	FuncMoneyParse Function = "money_parse"
	FuncCompose    Function = "compose"
)

var declaredFunctions = map[Function]bool{
	FuncDefault: true, FuncConcat: true, FuncEquals: true, FuncNotNull: true,
	FuncSum: true, FuncCount: true, FuncMin: true, FuncMax: true,
	FuncTrim: true, FuncUpper: true, FuncLower: true, FuncLookup: true,
	FuncDateParse: true, FuncMoneyParse: true, FuncCompose: true,
}

// Instruction is one bounded step. Not every field applies to every OpCode;
// Validate enforces the exact operand shape each opcode requires.
type Instruction struct {
	Op          OpCode                `json:"op"`
	Sources     []transformation.Path `json:"sources,omitempty"`
	Destination transformation.Path   `json:"destination"`
	Function    Function              `json:"function,omitempty"`
	Literal     string                `json:"literal,omitempty"`
	TargetType  transformation.Type   `json:"target_type,omitempty"`
	JoinKey     string                `json:"join_key,omitempty"`
	Lookup      map[string]string     `json:"lookup,omitempty"`
}

// Limits are declared, checked bounds. There is no unbounded loop and no
// fan-out beyond MaxFanOut source paths on any one instruction.
type Limits struct {
	MaxSteps  int `json:"max_steps"`
	MaxFanOut int `json:"max_fan_out"`
}

// Program is the bounded, immutable compiled form of a transformation
// definition. Two definitions that mean the same thing compile to the same
// canonical bytes and therefore the same Digest, regardless of incidental
// field or operation ordering in the source definition.
type Program struct {
	IRVersion        int           `json:"ir_version"`
	DefinitionName   string        `json:"definition_name"`
	DefinitionDigest string        `json:"definition_digest"`
	Instructions     []Instruction `json:"instructions"`
	Dependencies     []string      `json:"dependencies"`
	Limits           Limits        `json:"limits"`
}

// Compile turns a validated transformation definition into a canonical,
// bounded Program. It fails compilation (rather than returning a program
// that Validate would then reject) for the same reasons Validate would
// reject a hand-built Program: an unresolved field/type/function, a
// dependency cycle, expansion beyond the definition's declared limits, or a
// nondeterministic (ambiguously ordered) write to the same destination.
func Compile(d transformation.TransformationDefinition) (Program, error) {
	if err := d.Validate(); err != nil {
		return Program{}, err
	}
	defDigest, err := d.Digest()
	if err != nil {
		return Program{}, err
	}
	instructions := make([]Instruction, 0, len(d.Operations))
	for _, op := range d.Operations {
		instr, err := compileOperation(op)
		if err != nil {
			return Program{}, err
		}
		instructions = append(instructions, instr)
	}
	canonicalizeInstructions(instructions)

	p := Program{
		IRVersion:        IRVersion,
		DefinitionName:   d.Name,
		DefinitionDigest: defDigest,
		Instructions:     instructions,
		Dependencies:     dependencies(instructions),
		Limits:           Limits{MaxSteps: d.Limits.MaxOperations, MaxFanOut: d.Limits.MaxExpansion},
	}
	if err := p.Validate(); err != nil {
		return Program{}, err
	}
	return p, nil
}

func compileOperation(op transformation.Operation) (Instruction, error) {
	switch op.Kind {
	case transformation.OpCopy, transformation.OpRename:
		return Instruction{Op: OpProject, Sources: []transformation.Path{*op.Source}, Destination: op.Destination}, nil
	case transformation.OpConvert:
		return Instruction{Op: OpCoerce, Sources: []transformation.Path{*op.Source}, Destination: op.Destination, TargetType: op.TargetType}, nil
	case transformation.OpDefault:
		return Instruction{Op: OpMap, Function: FuncDefault, Literal: op.Literal, Destination: op.Destination}, nil
	case transformation.OpTransform:
		return Instruction{Op: OpMap, Sources: []transformation.Path{*op.Source}, Destination: op.Destination,
			Function: Function(op.Function), Literal: op.Argument, Lookup: op.Lookup}, nil
	case transformation.OpConcat:
		return Instruction{Op: OpAggregate, Function: FuncConcat, Sources: append([]transformation.Path(nil), op.Sources...), Destination: op.Destination}, nil
	default:
		// Unreachable once d.Validate() has passed (it only accepts the
		// operation kinds above), but Compile stays total rather than
		// panicking on a future, still-unmapped XFORM-001 operation kind.
		return Instruction{}, fmt.Errorf("%w: operation kind %q has no IR mapping", ErrUnresolved, op.Kind)
	}
}

// canonicalizeInstructions orders instructions by destination so that
// definitions differing only in declared operation order compile to
// identical canonical bytes. Compile already rejects two operations that
// write the same destination, so this ordering is total.
func canonicalizeInstructions(instructions []Instruction) {
	sort.Slice(instructions, func(i, j int) bool {
		a, b := instructions[i].Destination, instructions[j].Destination
		if a.Schema != b.Schema {
			return a.Schema < b.Schema
		}
		return a.Field < b.Field
	})
}

func dependencies(instructions []Instruction) []string {
	set := map[string]bool{}
	for _, instr := range instructions {
		for _, s := range instr.Sources {
			set[s.Schema+"."+s.Field] = true
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Validate refuses any program that is unbounded, nondeterministic, or
// references an unresolved field, type, or function -- whether the program
// came from Compile or was constructed directly.
func (p Program) Validate() error {
	if p.IRVersion != IRVersion {
		return fmt.Errorf("%w: ir_version %d, want %d", ErrInvalidProgram, p.IRVersion, IRVersion)
	}
	if p.Limits.MaxSteps <= 0 || p.Limits.MaxFanOut <= 0 {
		return fmt.Errorf("%w: limits must be declared and positive", ErrInvalidProgram)
	}
	if len(p.Instructions) > p.Limits.MaxSteps {
		return fmt.Errorf("%w: %d instructions exceeds max_steps %d", ErrUnboundedProgram, len(p.Instructions), p.Limits.MaxSteps)
	}

	seenDest := map[string]bool{}
	destOf := map[string]int{}
	for i, instr := range p.Instructions {
		if instr.Destination.Schema == "" || instr.Destination.Field == "" || instr.Destination.Type == "" {
			return fmt.Errorf("%w: instruction %d has an unresolved destination", ErrUnresolved, i)
		}
		key := instr.Destination.Schema + "." + instr.Destination.Field
		if seenDest[key] {
			return fmt.Errorf("%w: destination %s is written by more than one instruction", ErrNondeterministic, key)
		}
		seenDest[key] = true
		destOf[key] = i

		if err := instr.validateShape(); err != nil {
			return err
		}
		if len(instr.Sources) > p.Limits.MaxFanOut {
			return fmt.Errorf("%w: instruction %d has %d sources, exceeds max_fan_out %d", ErrUnboundedProgram, i, len(instr.Sources), p.Limits.MaxFanOut)
		}
	}
	return detectCycle(p.Instructions, destOf)
}

func (instr Instruction) validateShape() error {
	if instr.Function != "" && !declaredFunctions[instr.Function] {
		return fmt.Errorf("%w: undeclared function %q", ErrUnresolved, instr.Function)
	}
	for _, s := range instr.Sources {
		if s.Schema == "" || s.Field == "" || s.Type == "" {
			return fmt.Errorf("%w: instruction references an unresolved source path", ErrUnresolved)
		}
	}
	switch instr.Op {
	case OpProject:
		if len(instr.Sources) != 1 || instr.Function != "" || instr.Literal != "" || instr.TargetType != "" || instr.JoinKey != "" || len(instr.Lookup) != 0 {
			return fmt.Errorf("%w: project takes exactly one source and no function/literal/target type/join key", ErrInvalidProgram)
		}
		if instr.Sources[0].Type != instr.Destination.Type {
			return fmt.Errorf("%w: project source and destination types differ", ErrUnresolved)
		}
	case OpCoerce:
		if len(instr.Sources) != 1 || instr.Function != "" || instr.JoinKey != "" || len(instr.Lookup) != 0 {
			return fmt.Errorf("%w: coerce takes exactly one source and no function/join key", ErrInvalidProgram)
		}
		if instr.TargetType == "" || instr.TargetType != instr.Destination.Type {
			return fmt.Errorf("%w: coerce target type must be declared and match the destination type", ErrUnresolved)
		}
	case OpMap:
		if len(instr.Sources) > 1 || instr.JoinKey != "" || instr.TargetType != "" {
			return fmt.Errorf("%w: map takes at most one source and no join key/target type", ErrInvalidProgram)
		}
		if instr.Function == "" {
			return fmt.Errorf("%w: map must declare a function", ErrUnresolved)
		}
		if len(instr.Sources) == 0 && instr.Literal == "" {
			return fmt.Errorf("%w: map with no source requires a literal", ErrInvalidProgram)
		}
		switch instr.Function {
		case FuncDefault:
			if len(instr.Lookup) != 0 {
				return fmt.Errorf("%w: default map cannot carry lookup entries", ErrInvalidProgram)
			}
		case FuncTrim, FuncUpper, FuncLower, FuncCompose:
			if len(instr.Sources) != 1 || len(instr.Lookup) != 0 {
				return fmt.Errorf("%w: %s map requires one source and no lookup", ErrInvalidProgram, instr.Function)
			}
		case FuncLookup:
			if len(instr.Sources) != 1 || len(instr.Lookup) == 0 || instr.Literal != "" {
				return fmt.Errorf("%w: lookup map requires one source and entries", ErrInvalidProgram)
			}
			for k, v := range instr.Lookup {
				if k == "" || v == "" {
					return fmt.Errorf("%w: lookup map has empty key or value", ErrInvalidProgram)
				}
			}
		case FuncDateParse, FuncMoneyParse:
			if len(instr.Sources) != 1 || instr.Literal == "" || len(instr.Lookup) != 0 {
				return fmt.Errorf("%w: %s map requires one source and a declared argument", ErrInvalidProgram, instr.Function)
			}
		default:
			return fmt.Errorf("%w: function %q is not defined for map", ErrUnresolved, instr.Function)
		}
	case OpFilter:
		if len(instr.Sources) != 1 || instr.JoinKey != "" || instr.TargetType != "" || len(instr.Lookup) != 0 {
			return fmt.Errorf("%w: filter takes exactly one source and no join key/target type", ErrInvalidProgram)
		}
		if instr.Function == "" {
			return fmt.Errorf("%w: filter must declare a predicate function", ErrUnresolved)
		}
	case OpJoinByKey:
		if len(instr.Sources) != 2 || instr.JoinKey == "" || instr.TargetType != "" || len(instr.Lookup) != 0 {
			return fmt.Errorf("%w: join_by_key takes exactly two sources and a declared join key", ErrInvalidProgram)
		}
	case OpAggregate:
		if len(instr.Sources) < 1 || instr.JoinKey != "" || instr.TargetType != "" || len(instr.Lookup) != 0 {
			return fmt.Errorf("%w: aggregate takes at least one source and no join key/target type", ErrInvalidProgram)
		}
		if instr.Function == "" {
			return fmt.Errorf("%w: aggregate must declare a function", ErrUnresolved)
		}
	default:
		return fmt.Errorf("%w: opcode %q is not in the closed instruction set", ErrUnresolved, instr.Op)
	}
	return nil
}

const (
	colorWhite = iota
	colorGray
	colorBlack
)

// detectCycle treats "instruction A reads a field instruction B writes" as
// an edge A -> B (A depends on B) and rejects any cycle in that graph. A
// Program built by Compile from a TransformationDefinition can never
// contain such an edge (every source comes from the definition's Source
// schema, every destination is in its Destination schema, and the two
// schemas are named separately), but Validate checks it unconditionally so
// a directly constructed Program cannot smuggle a cycle past it either.
func detectCycle(instructions []Instruction, destOf map[string]int) error {
	n := len(instructions)
	adj := make([][]int, n)
	for i, instr := range instructions {
		for _, s := range instr.Sources {
			if j, ok := destOf[s.Schema+"."+s.Field]; ok && j != i {
				adj[i] = append(adj[i], j)
			}
		}
	}
	color := make([]int, n)
	var visit func(i int) error
	visit = func(i int) error {
		color[i] = colorGray
		for _, j := range adj[i] {
			if color[j] == colorGray {
				return fmt.Errorf("%w: instruction %d and %d depend on each other", ErrCycle, i, j)
			}
			if color[j] == colorWhite {
				if err := visit(j); err != nil {
					return err
				}
			}
		}
		color[i] = colorBlack
		return nil
	}
	for i := 0; i < n; i++ {
		if color[i] == colorWhite {
			if err := visit(i); err != nil {
				return err
			}
		}
	}
	return nil
}

// Digest returns the canonical, content-addressed identity of a valid
// program. Compiling the same definition twice, or compiling two
// definitions that differ only in declared field/operation order, produces
// the same Digest.
func (p Program) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	b, err := p.canonicalBytes()
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return DigestAlgorithm + ":" + hex.EncodeToString(h[:]), nil
}

func (p Program) canonicalBytes() ([]byte, error) {
	c := p
	c.Instructions = append([]Instruction(nil), p.Instructions...)
	canonicalizeInstructions(c.Instructions)
	c.Dependencies = dependencies(c.Instructions)
	return json.Marshal(c)
}

// Explain renders the program as a readable plan: audit-log and
// review-screen shaped, not for programmatic branching (branch on
// Validate()'s error instead).
func (p Program) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "ir v%d for %s (definition digest %s)", p.IRVersion, p.DefinitionName, p.DefinitionDigest)
	fmt.Fprintf(&b, "\n  limits: max_steps=%d max_fan_out=%d", p.Limits.MaxSteps, p.Limits.MaxFanOut)
	fmt.Fprintf(&b, "\n  steps (%d):", len(p.Instructions))
	for i, instr := range p.Instructions {
		fmt.Fprintf(&b, "\n    %d. %s", i+1, instr.explain())
	}
	if len(p.Dependencies) > 0 {
		fmt.Fprintf(&b, "\n  dependencies: %s", strings.Join(p.Dependencies, ", "))
	}
	return b.String()
}

func (instr Instruction) explain() string {
	srcs := make([]string, len(instr.Sources))
	for i, s := range instr.Sources {
		srcs[i] = fmt.Sprintf("%s.%s:%s", s.Schema, s.Field, s.Type)
	}
	dest := fmt.Sprintf("%s.%s:%s", instr.Destination.Schema, instr.Destination.Field, instr.Destination.Type)
	switch instr.Op {
	case OpProject:
		return fmt.Sprintf("project %s -> %s", srcs[0], dest)
	case OpCoerce:
		return fmt.Sprintf("coerce %s -> %s (as %s)", srcs[0], dest, instr.TargetType)
	case OpMap:
		if len(srcs) == 0 {
			return fmt.Sprintf("map %s(%q) -> %s", instr.Function, instr.Literal, dest)
		}
		return fmt.Sprintf("map %s(%s) -> %s", instr.Function, srcs[0], dest)
	case OpFilter:
		return fmt.Sprintf("filter %s(%s) -> %s", instr.Function, srcs[0], dest)
	case OpJoinByKey:
		return fmt.Sprintf("join_by_key[%s](%s) -> %s", instr.JoinKey, strings.Join(srcs, ", "), dest)
	case OpAggregate:
		return fmt.Sprintf("aggregate %s(%s) -> %s", instr.Function, strings.Join(srcs, ", "), dest)
	default:
		return fmt.Sprintf("%s(%s) -> %s", instr.Op, strings.Join(srcs, ", "), dest)
	}
}
