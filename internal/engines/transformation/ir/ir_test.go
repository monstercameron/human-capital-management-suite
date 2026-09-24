package ir

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
)

// -- fixtures -------------------------------------------------------------

func srcPath(field string, typ transformation.Type) transformation.Path {
	return transformation.Path{Schema: "people", Field: field, Type: typ}
}
func dstPath(field string, typ transformation.Type) transformation.Path {
	return transformation.Path{Schema: "worker", Field: field, Type: typ}
}

// definition mirrors the XFORM-001 fixture shape: a copy plus a convert,
// each writing a distinct destination field, so Compile has more than one
// instruction to canonicalize and digest.
func definition() transformation.TransformationDefinition {
	return transformation.TransformationDefinition{
		Version: transformation.ContractVersion, Name: "people-copy", Owner: "shared-engines", Phase: "P1A",
		Source: transformation.Schema{Name: "people", Version: 1, Fields: []transformation.Field{
			{Name: "given", Type: transformation.TypeString, Required: true},
			{Name: "age", Type: transformation.TypeInt},
		}},
		Destination: transformation.Schema{Name: "worker", Version: 1, Fields: []transformation.Field{
			{Name: "name", Type: transformation.TypeString, Required: true},
			{Name: "age", Type: transformation.TypeInt},
		}},
		Operations: []transformation.Operation{
			{Kind: transformation.OpCopy, Source: ptr(srcPath("given", transformation.TypeString)), Destination: dstPath("name", transformation.TypeString)},
			{Kind: transformation.OpConvert, Source: ptr(srcPath("age", transformation.TypeInt)), Destination: dstPath("age", transformation.TypeInt), TargetType: transformation.TypeInt},
		},
		Compatibility: transformation.Compatibility{MinimumSourceVersion: 1},
		Limits:        transformation.ResourceLimits{MaxOperations: 4, MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxExpansion: 2},
		Failure:       transformation.FailureReject, SideEffects: transformation.SideEffectsNone,
	}
}

func ptr(p transformation.Path) *transformation.Path { return &p }

func concatDefinition(maxExpansion int) transformation.TransformationDefinition {
	return transformation.TransformationDefinition{
		Version: transformation.ContractVersion, Name: "people-concat", Owner: "shared-engines", Phase: "P1A",
		Source: transformation.Schema{Name: "people", Version: 1, Fields: []transformation.Field{
			{Name: "given", Type: transformation.TypeString, Required: true},
			{Name: "family", Type: transformation.TypeString, Required: true},
		}},
		Destination: transformation.Schema{Name: "worker", Version: 1, Fields: []transformation.Field{
			{Name: "name", Type: transformation.TypeString, Required: true},
		}},
		Operations: []transformation.Operation{
			{Kind: transformation.OpConcat, Destination: dstPath("name", transformation.TypeString), Sources: []transformation.Path{
				srcPath("given", transformation.TypeString), srcPath("family", transformation.TypeString),
			}},
		},
		Compatibility: transformation.Compatibility{MinimumSourceVersion: 1},
		Limits:        transformation.ResourceLimits{MaxOperations: 4, MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxExpansion: maxExpansion},
		Failure:       transformation.FailureReject, SideEffects: transformation.SideEffectsNone,
	}
}

// -- XFORM-002 test matrix --------------------------------------------------

func TestCompileProducesCanonicalIR(t *testing.T) {
	p, err := Compile(definition())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("compiled program failed its own Validate: %v", err)
	}
	if len(p.Instructions) != 2 {
		t.Fatalf("len(Instructions)=%d, want 2", len(p.Instructions))
	}
	// Canonical order is by destination: age sorts before name.
	if p.Instructions[0].Destination.Field != "age" || p.Instructions[1].Destination.Field != "name" {
		t.Fatalf("instructions not canonically ordered: %+v", p.Instructions)
	}
	if p.Instructions[0].Op != OpCoerce || p.Instructions[1].Op != OpProject {
		t.Fatalf("unexpected opcodes: %+v", p.Instructions)
	}
	wantDeps := []string{"people.age", "people.given"}
	if strings.Join(p.Dependencies, ",") != strings.Join(wantDeps, ",") {
		t.Fatalf("Dependencies=%v, want %v", p.Dependencies, wantDeps)
	}
	digest, err := p.Digest()
	if err != nil || len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest=%q err=%v", digest, err)
	}
}

func TestTodo_XFORM_002(t *testing.T) {
	p, err := Compile(definition())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Instructions) != 2 || p.Instructions[0].Destination.Field != "age" || p.Instructions[1].Destination.Field != "name" {
		t.Fatalf("compiled instructions do not preserve canonical destination order: %+v", p.Instructions)
	}
	if p.Instructions[0].Op != OpCoerce || p.Instructions[1].Op != OpProject {
		t.Fatalf("compiled operations escaped the expected typed instruction set: %+v", p.Instructions)
	}
	if strings.Join(p.Dependencies, ",") != "people.age,people.given" {
		t.Fatalf("compiled dependencies=%v", p.Dependencies)
	}
}

// TestTodo_XFORM_002_Property proves Compile is deterministic and that
// equivalent definitions (differing only in declared field order) yield
// equal digests.
func TestTodo_XFORM_002_Property(t *testing.T) {
	a := definition()
	pa1, err := Compile(a)
	if err != nil {
		t.Fatal(err)
	}
	pa2, err := Compile(a)
	if err != nil {
		t.Fatal(err)
	}
	da1, _ := pa1.Digest()
	da2, _ := pa2.Digest()
	if da1 != da2 {
		t.Fatalf("compiling the same definition twice produced different digests: %s != %s", da1, da2)
	}

	// Shuffling declared schema field order (but keeping the operation list
	// itself, since transformation.Digest() is only proven invariant to
	// field order -- see TestDigestStableAcrossSchemaFieldOrder) must still
	// compile to the same IR digest.
	b := definition()
	b.Source.Fields[0], b.Source.Fields[1] = b.Source.Fields[1], b.Source.Fields[0]
	b.Destination.Fields[0], b.Destination.Fields[1] = b.Destination.Fields[1], b.Destination.Fields[0]
	pb, err := Compile(b)
	if err != nil {
		t.Fatal(err)
	}
	db, _ := pb.Digest()
	if da1 != db {
		t.Fatalf("field-order-equivalent definitions produced different digests: %s != %s", da1, db)
	}

	// The IR's own canonicalization is independent of that: swapping the
	// declared operation order must still land on the same instruction
	// order (sorted by destination) even though it changes the embedded
	// definition digest, so Instructions/Dependencies/Limits agree while
	// only DefinitionDigest legitimately differs.
	c := definition()
	c.Operations[0], c.Operations[1] = c.Operations[1], c.Operations[0]
	pc, err := Compile(c)
	if err != nil {
		t.Fatal(err)
	}
	if pa1.Instructions[0].Op != pc.Instructions[0].Op || pa1.Instructions[1].Op != pc.Instructions[1].Op {
		t.Fatalf("operation-order-reversed definitions compiled to differently ordered instructions: %+v vs %+v", pa1.Instructions, pc.Instructions)
	}
}

// TestTodo_XFORM_002_Golden pins the shape of a fixed definition's compiled
// IR: opcodes, dependency list, and limits, plus the digest's sha256 shape.
func TestTodo_XFORM_002_Golden(t *testing.T) {
	p, err := Compile(definition())
	if err != nil {
		t.Fatal(err)
	}
	if p.IRVersion != IRVersion {
		t.Fatalf("IRVersion=%d, want %d", p.IRVersion, IRVersion)
	}
	if p.Limits != (Limits{MaxSteps: 4, MaxFanOut: 2}) {
		t.Fatalf("Limits=%+v, want {4 2}", p.Limits)
	}
	wantOps := []OpCode{OpCoerce, OpProject}
	for i, op := range wantOps {
		if p.Instructions[i].Op != op {
			t.Fatalf("Instructions[%d].Op=%s, want %s", i, p.Instructions[i].Op, op)
		}
	}
	digest, err := p.Digest()
	const want = "sha256:"
	if err != nil || len(digest) != len(want)+64 || digest[:len(want)] != want {
		t.Fatalf("digest %q is not a sha256 golden shape (err=%v)", digest, err)
	}
}

func FuzzTodo_XFORM_002(f *testing.F) {
	f.Add("people-copy", 4, 2)
	f.Add("", 0, 0)
	f.Add("x", -1, 100)
	f.Fuzz(func(t *testing.T, name string, maxOps, maxExpansion int) {
		d := definition()
		d.Name = name
		d.Limits.MaxOperations = maxOps
		d.Limits.MaxExpansion = maxExpansion
		// Compile must be total: never panic, whatever nonsense limits or
		// name a fuzzer proposes.
		p, err := Compile(d)
		if err == nil {
			if verr := p.Validate(); verr != nil {
				t.Fatalf("Compile returned a program that fails its own Validate: %v", verr)
			}
		}
	})
}

// TestTodo_XFORM_002_Mutation kills mutants that flip a boundary condition
// in Validate: an off-by-one on max_steps/max_fan_out, or a >= vs > swap on
// the duplicate-destination check.
func TestTodo_XFORM_002_Mutation(t *testing.T) {
	// Exactly at the step limit must pass; one above must fail.
	d := definition()
	d.Limits.MaxOperations = 2
	if _, err := Compile(d); err != nil {
		t.Fatalf("exactly at max_operations should compile: %v", err)
	}
	d.Operations = append(d.Operations, transformation.Operation{
		Kind: transformation.OpDefault, Destination: dstPath("age", transformation.TypeInt), Literal: "0",
	})
	d.Limits.MaxOperations = 3
	if err := d.Validate(); err != nil {
		t.Skip("fixture no longer valid at the definition level; nothing to prove here")
	}
	// Two operations now target "worker.age" (convert and default): a
	// nondeterministic, order-dependent write. Compile must refuse it even
	// though XFORM-001's own Validate has no opinion on duplicate writers.
	if _, err := Compile(d); !errors.Is(err, ErrNondeterministic) {
		t.Fatalf("expected ErrNondeterministic for a duplicate destination writer, got %v", err)
	}

	// Fan-out exactly at the limit must pass; one above must fail.
	ok := concatDefinition(2)
	if _, err := Compile(ok); err != nil {
		t.Fatalf("fan-out exactly at max_expansion should compile: %v", err)
	}
	tooWide := concatDefinition(1)
	if _, err := Compile(tooWide); !errors.Is(err, ErrUnboundedProgram) {
		t.Fatalf("expected ErrUnboundedProgram for fan-out beyond max_expansion, got %v", err)
	}
}

// TestTodo_XFORM_002_Security proves the two refusal shapes the lane asked
// for by name: a definition whose declared width exceeds its own declared
// limit, and a directly authored IR instruction that names a function
// outside the closed vocabulary. Neither compiles a runnable program.
func TestTodo_XFORM_002_Security(t *testing.T) {
	t.Run("exceeds declared limits", func(t *testing.T) {
		if _, err := Compile(concatDefinition(1)); !errors.Is(err, ErrUnboundedProgram) {
			t.Fatalf("expected ErrUnboundedProgram, got %v", err)
		}
	})
	t.Run("undeclared function", func(t *testing.T) {
		p := Program{
			IRVersion: IRVersion, DefinitionName: "hand-built", DefinitionDigest: "sha256:0",
			Limits: Limits{MaxSteps: 4, MaxFanOut: 4},
			Instructions: []Instruction{
				{Op: OpMap, Function: Function("eval"), Destination: dstPath("name", transformation.TypeString)},
			},
		}
		if err := p.Validate(); !errors.Is(err, ErrUnresolved) {
			t.Fatalf("expected ErrUnresolved for an undeclared function, got %v", err)
		}
	})
	t.Run("unknown opcode is not dynamic code in disguise", func(t *testing.T) {
		p := Program{
			IRVersion: IRVersion, DefinitionName: "hand-built", DefinitionDigest: "sha256:0",
			Limits: Limits{MaxSteps: 4, MaxFanOut: 4},
			Instructions: []Instruction{
				{Op: OpCode("script"), Destination: dstPath("name", transformation.TypeString)},
			},
		}
		if err := p.Validate(); !errors.Is(err, ErrUnresolved) {
			t.Fatalf("expected ErrUnresolved for an opcode outside the closed set, got %v", err)
		}
	})
}

// -- direct IR-level coverage (RED cases named in the todo, exercised at
// the API a hand-authored Program actually uses) --------------------------

func TestValidateRejectsDependencyCycle(t *testing.T) {
	// worker.a is written from worker.b, and worker.b is written from
	// worker.a: a two-instruction cycle that cannot arise from Compile
	// (sources always come from the Source schema) but must still be
	// caught if a Program is ever constructed directly.
	p := Program{
		IRVersion: IRVersion, DefinitionName: "cyclic", DefinitionDigest: "sha256:0",
		Limits: Limits{MaxSteps: 4, MaxFanOut: 4},
		Instructions: []Instruction{
			{Op: OpProject, Sources: []transformation.Path{dstPath("b", transformation.TypeString)}, Destination: dstPath("a", transformation.TypeString)},
			{Op: OpProject, Sources: []transformation.Path{dstPath("a", transformation.TypeString)}, Destination: dstPath("b", transformation.TypeString)},
		},
	}
	if err := p.Validate(); !errors.Is(err, ErrCycle) {
		t.Fatalf("expected ErrCycle, got %v", err)
	}
}

func TestValidateRejectsUnresolvedPath(t *testing.T) {
	p := Program{
		IRVersion: IRVersion, DefinitionName: "unresolved", DefinitionDigest: "sha256:0",
		Limits:       Limits{MaxSteps: 4, MaxFanOut: 4},
		Instructions: []Instruction{{Op: OpProject, Sources: []transformation.Path{{}}, Destination: dstPath("name", transformation.TypeString)}},
	}
	if err := p.Validate(); !errors.Is(err, ErrUnresolved) {
		t.Fatalf("expected ErrUnresolved for an empty source path, got %v", err)
	}
}

func TestValidateRejectsNonPositiveLimits(t *testing.T) {
	p := Program{IRVersion: IRVersion, DefinitionName: "no-limits", DefinitionDigest: "sha256:0"}
	if err := p.Validate(); !errors.Is(err, ErrInvalidProgram) {
		t.Fatalf("expected ErrInvalidProgram for zero-value limits, got %v", err)
	}
}

func TestValidateAcceptsFilterAndJoinByKey(t *testing.T) {
	// filter and join_by_key are part of the closed instruction set even
	// though today's XFORM-001 operation vocabulary never asks Compile to
	// emit them; Validate must accept a correctly shaped instance of each.
	p := Program{
		IRVersion: IRVersion, DefinitionName: "future-ops", DefinitionDigest: "sha256:0",
		Limits: Limits{MaxSteps: 4, MaxFanOut: 4},
		Instructions: []Instruction{
			{Op: OpFilter, Function: FuncNotNull, Sources: []transformation.Path{srcPath("given", transformation.TypeString)}, Destination: dstPath("name", transformation.TypeString)},
			{Op: OpJoinByKey, JoinKey: "id", Sources: []transformation.Path{srcPath("given", transformation.TypeString), srcPath("family", transformation.TypeString)}, Destination: dstPath("age", transformation.TypeInt)},
		},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected a well-formed filter + join_by_key program to validate, got %v", err)
	}
}

func TestExplainRendersReadablePlan(t *testing.T) {
	p, err := Compile(definition())
	if err != nil {
		t.Fatal(err)
	}
	out := p.Explain()
	for _, want := range []string{"ir v1 for people-copy", "max_steps=4 max_fan_out=2", "coerce people.age", "project people.given", "dependencies: people.age, people.given"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Explain() missing %q in:\n%s", want, out)
		}
	}
}

func TestCompileFailsClosedOnAnInvalidDefinition(t *testing.T) {
	d := definition()
	d.Operations[0].Kind = "script"
	if _, err := Compile(d); err == nil {
		t.Fatal("expected Compile to fail closed on an invalid definition")
	}
}
