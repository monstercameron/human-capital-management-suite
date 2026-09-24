package adapters_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/mapping"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops/importing"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/adapters"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/conformance"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/exec"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/ir"
)

// loweredFixtures returns one lowered artifact per declared site, built from
// the same fixtures the per-site migration proofs use.
func loweredFixtures(t *testing.T) map[adapters.Site]adapters.Lowered {
	t.Helper()
	out := map[adapters.Site]adapters.Lowered{}

	nodes, _ := promotionTransformNodes(t)
	workflowLowered, err := adapters.LowerWorkflowTransform(stepFromCompiled(t, nodes[0]))
	if err != nil {
		t.Fatalf("LowerWorkflowTransform: %v", err)
	}
	out[adapters.SiteWorkflowTransform] = workflowLowered

	compiled, err := mapping.Compile(intg006Profile())
	if err != nil {
		t.Fatalf("mapping.Compile: %v", err)
	}
	profileLowered, err := adapters.LowerConnectivityProfile(profileFromCompiled(t, compiled))
	if err != nil {
		t.Fatalf("LowerConnectivityProfile: %v", err)
	}
	out[adapters.SiteConnectivityProfile] = profileLowered

	rulesLowered, err := adapters.LowerConnectivityRules(rulesFromSite(connectivityRulesFixture()))
	if err != nil {
		t.Fatalf("LowerConnectivityRules: %v", err)
	}
	out[adapters.SiteConnectivityRules] = rulesLowered

	profile, err := importing.Compile(dataOpsCatalog(t), dataOpsFixtureSpec())
	if err != nil {
		t.Fatalf("importing.Compile: %v", err)
	}
	dataOpsLowered, err := adapters.LowerDataOpsImport(importMappingFrom(profile))
	if err != nil {
		t.Fatalf("LowerDataOpsImport: %v", err)
	}
	out[adapters.SiteDataOpsImport] = dataOpsLowered

	return out
}

// TestTodo_XFORM_008 is the primary acceptance test: every migrating site's
// mapping form lowers onto the shared engine, each lowering is a valid,
// digested IR program with a declared limits mapping, and a site feature
// with no IR equivalent is a typed refusal that names it.
func TestTodo_XFORM_008(t *testing.T) {
	fixtures := loweredFixtures(t)

	for _, site := range adapters.Sites() {
		lowered, ok := fixtures[site]
		if !ok {
			t.Fatalf("no fixture lowering for declared site %q", site)
		}
		t.Run(string(site), func(t *testing.T) {
			if lowered.Site != site {
				t.Fatalf("lowered.Site = %q, want %q", lowered.Site, site)
			}
			if err := lowered.Program.Validate(); err != nil {
				t.Fatalf("lowered program does not validate: %v", err)
			}
			programDigest, err := lowered.Program.Digest()
			if err != nil {
				t.Fatalf("program digest: %v", err)
			}
			if lowered.ProgramDigest != programDigest {
				t.Fatalf("recorded program digest %s, recomputed %s", lowered.ProgramDigest, programDigest)
			}
			digest, err := lowered.Digest()
			if err != nil {
				t.Fatalf("lowered digest: %v", err)
			}
			if !strings.HasPrefix(digest, adapters.DigestAlgorithm+":") {
				t.Fatalf("lowered digest %q is not a %s digest", digest, adapters.DigestAlgorithm)
			}

			// The limits mapping is declared, positive and explained.
			def := lowered.Limits.Definition
			if def.MaxOperations < len(lowered.Program.Instructions) ||
				def.MaxInputBytes <= 0 || def.MaxOutputBytes <= 0 || def.MaxExpansion <= 0 {
				t.Fatalf("limits are not declared and positive: %+v", def)
			}
			if len(lowered.Limits.Notes) == 0 {
				t.Fatal("the limits mapping cites no source for its numbers")
			}
			if _, err := exec.New(lowered.Program); err != nil {
				t.Fatalf("the lowered program is not executable: %v", err)
			}

			// Every instruction is accounted for by a binding, and every
			// binding by an instruction.
			if len(lowered.Bindings) != len(lowered.Program.Instructions) {
				t.Fatalf("%d bindings for %d instructions", len(lowered.Bindings), len(lowered.Program.Instructions))
			}

			// Explain names the digest, every binding, and every carrier,
			// delegation and divergence.
			explanation := lowered.Explain()
			for _, want := range []string{digest, lowered.ProgramDigest, string(site)} {
				if !strings.Contains(explanation, want) {
					t.Fatalf("Explain does not name %q:\n%s", want, explanation)
				}
			}
			for _, b := range lowered.Bindings {
				if !strings.Contains(explanation, b.Target) {
					t.Fatalf("Explain does not name binding %q:\n%s", b.Target, explanation)
				}
			}
			for _, c := range lowered.Carriers {
				if !strings.Contains(explanation, c.Loses) {
					t.Fatalf("Explain does not say what carrier %q loses:\n%s", c.Target, explanation)
				}
			}
			for _, d := range lowered.Delegations {
				if !strings.Contains(explanation, d.ProgramDigest) {
					t.Fatalf("Explain does not name delegation %q:\n%s", d.Target, explanation)
				}
			}
			for _, d := range lowered.Divergences {
				if !strings.Contains(explanation, d.Feature) {
					t.Fatalf("Explain does not name divergence %q:\n%s", d.Feature, explanation)
				}
			}
		})
	}

	t.Run("a site feature with no IR equivalent is a named typed refusal", func(t *testing.T) {
		declared := map[string]bool{}
		for _, f := range adapters.RefusableFeatures() {
			declared[f] = true
		}
		cases := []struct {
			name string
			call func() (adapters.Lowered, error)
			want string
		}{
			{"workflow context read", func() (adapters.Lowered, error) {
				return adapters.LowerWorkflowTransform(adapters.WorkflowTransformStep{
					NodeID: "n", TransformRef: "t", Version: 1,
					Limits: adapters.WorkflowTransformLimits{MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxSteps: 8},
					Mappings: []adapters.WorkflowMapping{{
						Target: "f", TargetType: adapters.WorkflowValueType{Kind: adapters.WorkflowKindString},
						SourceKind: adapters.WorkflowSourceContext, SourceCtx: "worker", SourcePath: "id",
					}},
				})
			}, adapters.FeatureAmbientSource},
			{"connectivity profile naming neither identity nor a digest", func() (adapters.Lowered, error) {
				return adapters.LowerConnectivityProfile(adapters.ConnectivityProfile{
					MappingID: "p", Version: 1, SourceSystemRef: "s", TargetEntity: "e",
					Mappings: []adapters.ConnectivityFieldMapping{{SourceField: "a", TargetField: "b"}},
				})
			}, adapters.FeatureInvalidMapping},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				lowered, err := tc.call()
				var refusal adapters.Refusal
				if !errors.As(err, &refusal) {
					t.Fatalf("error = %v (lowered %+v), want an adapters.Refusal", err, lowered)
				}
				if refusal.Code != adapters.RefusalCode {
					t.Fatalf("refusal code = %q", refusal.Code)
				}
				if !declared[refusal.Feature] {
					t.Fatalf("refusal feature %q is outside RefusableFeatures()", refusal.Feature)
				}
				if refusal.Feature != tc.want {
					t.Fatalf("refusal feature = %q, want %q", refusal.Feature, tc.want)
				}
				if refusal.Reason == "" || !strings.Contains(refusal.Error(), refusal.Feature) {
					t.Fatalf("refusal does not name itself: %q", refusal.Error())
				}
			})
		}
	})
}

// TestTodo_XFORM_008_Property covers the properties every lowering must hold
// regardless of which site it came from.
func TestTodo_XFORM_008_Property(t *testing.T) {
	fixtures := loweredFixtures(t)

	t.Run("lowering is deterministic across repeats and goroutines", func(t *testing.T) {
		rules := rulesFromSite(connectivityRulesFixture())
		want, err := adapters.LowerConnectivityRules(rules)
		if err != nil {
			t.Fatalf("lower: %v", err)
		}
		wantDigest, err := want.Digest()
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		const n = 24
		digests := make([]string, n)
		var wg sync.WaitGroup
		for i := range digests {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				lowered, err := adapters.LowerConnectivityRules(rulesFromSite(connectivityRulesFixture()))
				if err != nil {
					t.Errorf("lower: %v", err)
					return
				}
				d, err := lowered.Digest()
				if err != nil {
					t.Errorf("digest: %v", err)
					return
				}
				digests[i] = d
			}(i)
		}
		wg.Wait()
		for _, got := range digests {
			if got != wantDigest {
				t.Fatalf("digest changed across goroutines: %s != %s", got, wantDigest)
			}
		}
	})

	t.Run("declaration order does not change a lowering", func(t *testing.T) {
		forward := connectivityRulesFixture()
		reversed := forward
		reversed.Rules = []mapping.Rule{forward.Rules[2], forward.Rules[0], forward.Rules[1]}
		a, err := adapters.LowerConnectivityRules(rulesFromSite(forward))
		if err != nil {
			t.Fatalf("lower forward: %v", err)
		}
		b, err := adapters.LowerConnectivityRules(rulesFromSite(reversed))
		if err != nil {
			t.Fatalf("lower reversed: %v", err)
		}
		if a.ProgramDigest != b.ProgramDigest {
			t.Fatalf("rule order changed the program digest: %s != %s", a.ProgramDigest, b.ProgramDigest)
		}
	})

	t.Run("every recorded fact is covered by the lowered digest", func(t *testing.T) {
		base := fixtures[adapters.SiteConnectivityProfile]
		baseDigest, err := base.Digest()
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		mutations := map[string]func(*adapters.Lowered){
			"name":        func(l *adapters.Lowered) { l.Name += "x" },
			"site":        func(l *adapters.Lowered) { l.Site = adapters.SiteDataOpsImport },
			"binding":     func(l *adapters.Lowered) { l.Bindings[0].SourceKey += "x" },
			"limits":      func(l *adapters.Lowered) { l.Limits.Definition.MaxOperations++ },
			"carrier":     func(l *adapters.Lowered) { l.Carriers = nil },
			"delegation":  func(l *adapters.Lowered) { l.Delegations = nil },
			"divergence":  func(l *adapters.Lowered) { l.Divergences = nil },
			"program ref": func(l *adapters.Lowered) { l.ProgramDigest += "x" },
		}
		for name, mutate := range mutations {
			t.Run(name, func(t *testing.T) {
				mutated := base
				mutated.Bindings = append([]adapters.Binding(nil), base.Bindings...)
				mutate(&mutated)
				got, err := mutated.Digest()
				if err != nil {
					t.Fatalf("digest: %v", err)
				}
				if got == baseDigest {
					t.Fatalf("mutating the %s left the lowered digest unchanged", name)
				}
			})
		}
	})

	t.Run("a projection is byte-preserving for any text", func(t *testing.T) {
		lowered, err := adapters.LowerConnectivityRules(adapters.ConnectivityRules{
			Version: "v1",
			Rules: []adapters.ConnectivityRule{
				{Source: "a", Target: "b", Op: adapters.ConnectivityOpIdentity, Null: adapters.ConnectivityNullError},
			},
		})
		if err != nil {
			t.Fatalf("lower: %v", err)
		}
		for _, text := range []string{"", " ", "Jane", "  padded  ", "ünïcodé", "\t\n", strings.Repeat("x", 4096), `{"json":true}`} {
			rows, err := lowered.Run([]map[string]string{{"connectivity.mapping_ir.v1.source.a": text}})
			if err != nil {
				t.Fatalf("Run(%q): %v", text, err)
			}
			got, err := lowered.Texts(rows[0])
			if err != nil {
				t.Fatalf("Texts(%q): %v", text, err)
			}
			if got["b"] != text {
				t.Fatalf("projection changed %q into %q", text, got["b"])
			}
		}
	})

	t.Run("execution is replay-stable", func(t *testing.T) {
		lowered := fixtures[adapters.SiteConnectivityRules]
		row := map[string]string{
			"connectivity.mapping_ir.workday.worker/v1.source.Worker_ID": "WD-1",
			"connectivity.mapping_ir.workday.worker/v1.source.Hire_Date": "2026-03-01T09:00:00Z",
		}
		first, err := lowered.Run([]map[string]string{row})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		wantDigest, err := exec.Digest(first)
		if err != nil {
			t.Fatalf("exec.Digest: %v", err)
		}
		for i := 0; i < 8; i++ {
			next, err := lowered.Run([]map[string]string{row})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			got, err := exec.Digest(next)
			if err != nil {
				t.Fatalf("exec.Digest: %v", err)
			}
			if got != wantDigest {
				t.Fatalf("replay %d produced %s, want %s", i, got, wantDigest)
			}
		}
	})
}

// TestTodo_XFORM_008_Golden pins the lowered digest of each site's fixture.
// A change to any lowering, limits mapping, carrier, delegation or divergence
// changes one of these, so a silent semantic drift cannot land unnoticed.
func TestTodo_XFORM_008_Golden(t *testing.T) {
	fixtures := loweredFixtures(t)
	for _, site := range adapters.Sites() {
		t.Run(string(site), func(t *testing.T) {
			lowered := fixtures[site]
			digest, err := lowered.Digest()
			if err != nil {
				t.Fatalf("digest: %v", err)
			}
			pinned, ok := goldenDigests[site]
			if !ok {
				t.Fatalf("site %q has no pinned digest", site)
			}
			if lowered.ProgramDigest != pinned.program {
				t.Fatalf("program digest = %q, pinned %q", lowered.ProgramDigest, pinned.program)
			}
			if digest != pinned.lowered {
				t.Fatalf("lowered digest = %q, pinned %q", digest, pinned.lowered)
			}
		})
	}
}

// goldenDigests pins, per site, the digest of the fixture's compiled IR
// program and the digest of the whole lowered artifact (limits mapping,
// bindings, carriers, delegations and divergences included).
var goldenDigests = map[adapters.Site]struct{ program, lowered string }{
	adapters.SiteWorkflowTransform: {
		program: "sha256:eb6c339b1881d93fea70e0b279cde8ae0184e8c7d361624eb25bff1ea7274b93",
		lowered: "sha256:9288e68898bbc59c97391e8159d190a41edab6334d1ff2206211fda1ebb3d4e8",
	},
	adapters.SiteConnectivityProfile: {
		program: "sha256:ae98439f1512f6545cd731556fb14277225763e778b1c78c6f5dfa3e00fe19d2",
		lowered: "sha256:637a3d33eca30459475bb33b74e2bb13840467bbfc4d84e5579d923baffa1481",
	},
	adapters.SiteConnectivityRules: {
		program: "sha256:1c437d5d45679f86d51e8f7fc3f1ef7b1cd8b5508d6c7b29162e5fd78cbd97e1",
		lowered: "sha256:9a21dfb6856697496f6ded9f22236d1c00b2f6854119d80e53cb5e1d1eba1ffa",
	},
	adapters.SiteDataOpsImport: {
		program: "sha256:5f7536f4ca417b2fc903d4b619d581ad0a3749b811e1489bdce538ee59039dca",
		lowered: "sha256:fecf2a5656d2650b7be9da9b4c2c5981c6b158add8a0b2669d81cc138ffee6cb",
	},
}

// TestTodo_XFORM_008_Conformance is the migration's central claim: every
// operation any migrating site supports is one of the shared IR's
// instructions -- the site operation sets are a subset of the IR instruction
// set, with nothing left over on either side of the lowering.
func TestTodo_XFORM_008_Conformance(t *testing.T) {
	declared := adapters.SupportedOperations()
	if len(declared) == 0 {
		t.Fatal("no site operations are declared")
	}

	t.Run("every declared site operation names an IR instruction", func(t *testing.T) {
		for _, op := range declared {
			// conformance.ProgramFor is the IR instruction set's own
			// membership oracle: it builds a minimal valid program for an
			// opcode and refuses anything outside the closed set.
			if _, err := conformance.ProgramFor(op.IROp); err != nil {
				t.Fatalf("%s/%s lowers to %q, which is not in the IR instruction set: %v", op.Site, op.Name, op.IROp, err)
			}
		}
	})

	t.Run("an opcode outside the IR instruction set is rejected", func(t *testing.T) {
		if _, err := conformance.ProgramFor(ir.OpCode("lookup")); err == nil {
			t.Fatal("the membership oracle accepted an opcode the IR does not declare")
		}
	})

	t.Run("every declared IR function is in the IR's declared function set", func(t *testing.T) {
		for _, op := range declared {
			if op.IRFunction == "" {
				continue
			}
			instruction := ir.Instruction{Op: op.IROp, Function: op.IRFunction, Literal: "x",
				Destination: transformation.Path{Schema: "out", Field: "value", Type: transformation.TypeString}}
			if op.IRFunction == ir.FuncTrim || op.IRFunction == ir.FuncUpper || op.IRFunction == ir.FuncLower || op.IRFunction == ir.FuncLookup || op.IRFunction == ir.FuncDateParse || op.IRFunction == ir.FuncMoneyParse || op.IRFunction == ir.FuncCompose {
				instruction.Sources = []transformation.Path{{Schema: "in", Field: "source", Type: transformation.TypeString}}
			}
			switch op.IRFunction {
			case ir.FuncLookup:
				instruction.Literal = ""
				instruction.Lookup = map[string]string{"a": "b"}
			case ir.FuncDateParse:
				instruction.Literal = "2006-01-02"
			case ir.FuncMoneyParse:
				instruction.Literal = "DATAOPS:USD"
			}
			program := ir.Program{
				IRVersion: ir.IRVersion, DefinitionName: "conformance",
				Instructions: []ir.Instruction{instruction},
				Limits:       ir.Limits{MaxSteps: 4, MaxFanOut: 4},
			}
			if err := program.Validate(); err != nil {
				t.Fatalf("%s/%s names function %q, which the IR does not declare: %v", op.Site, op.Name, op.IRFunction, err)
			}
		}
	})

	t.Run("every opcode a lowered fixture emits is a declared site operation", func(t *testing.T) {
		allowed := map[ir.OpCode]bool{}
		for _, op := range declared {
			allowed[op.IROp] = true
		}
		for site, lowered := range loweredFixtures(t) {
			for _, instr := range lowered.Program.Instructions {
				if !allowed[instr.Op] {
					t.Fatalf("%s emitted opcode %q, which SupportedOperations does not declare", site, instr.Op)
				}
			}
		}
	})

	t.Run("every declared site is covered by a declared operation", func(t *testing.T) {
		covered := map[adapters.Site]bool{}
		for _, op := range declared {
			covered[op.Site] = true
		}
		for _, site := range adapters.Sites() {
			if !covered[site] {
				t.Fatalf("site %q declares no supported operation", site)
			}
		}
	})
}

// TestTodo_XFORM_008_Mutation seeds defects that a weaker implementation
// would let through: a dropped delegation, a silently approximated
// transform, a refusal outside the declared vocabulary, and a limits mapping
// that quietly widens a site's own bound.
func TestTodo_XFORM_008_Mutation(t *testing.T) {
	t.Run("a delegated field is never dropped", func(t *testing.T) {
		compiled, err := mapping.Compile(intg006Profile())
		if err != nil {
			t.Fatalf("mapping.Compile: %v", err)
		}
		profile := profileFromCompiled(t, compiled)
		lowered, err := adapters.LowerConnectivityProfile(profile)
		if err != nil {
			t.Fatalf("lower: %v", err)
		}
		accounted := len(lowered.Bindings) + len(lowered.Delegations)
		if accounted != len(profile.Mappings) {
			t.Fatalf("%d of %d profile mappings are accounted for by a binding or a delegation",
				accounted, len(profile.Mappings))
		}
	})

	t.Run("normalizing transforms lower to named shared functions", func(t *testing.T) {
		for _, op := range []adapters.ConnectivityOp{adapters.ConnectivityOpTrim, adapters.ConnectivityOpUpper, adapters.ConnectivityOpLower} {
			lowered, err := adapters.LowerConnectivityRules(adapters.ConnectivityRules{
				Version: "v1",
				Rules: []adapters.ConnectivityRule{
					{Source: "a", Target: "b", Op: op, Null: adapters.ConnectivityNullError},
				},
			})
			if err != nil {
				t.Fatalf("%s lower: %v", op, err)
			}
			want := map[adapters.ConnectivityOp]ir.Function{adapters.ConnectivityOpTrim: ir.FuncTrim, adapters.ConnectivityOpUpper: ir.FuncUpper, adapters.ConnectivityOpLower: ir.FuncLower}[op]
			if len(lowered.Program.Instructions) != 1 || lowered.Program.Instructions[0].Function != want {
				t.Fatalf("%s lowered to %+v, want shared function %s", op, lowered.Program.Instructions, want)
			}
		}
	})

	t.Run("every refusal a sweep produces names a declared feature", func(t *testing.T) {
		declared := map[string]bool{}
		for _, f := range adapters.RefusableFeatures() {
			declared[f] = true
		}
		var errs []error
		for _, op := range []adapters.ConnectivityOp{adapters.ConnectivityOp("UNKNOWN")} {
			_, err := adapters.LowerConnectivityRules(adapters.ConnectivityRules{
				Version: "v1",
				Rules:   []adapters.ConnectivityRule{{Source: "a", Target: "b", Op: op, Argument: "2006-01-02"}},
			})
			errs = append(errs, err)
		}
		for _, kind := range []adapters.DataOpsTransformKind{adapters.DataOpsTransformKind("UNKNOWN")} {
			_, err := adapters.LowerDataOpsImport(adapters.DataOpsImportMapping{
				Version: "v1",
				Fields:  []adapters.DataOpsFieldMapping{{SourceColumn: "a", Target: "b", Transform: adapters.DataOpsTransform{Kind: kind}}},
			})
			errs = append(errs, err)
		}
		for _, err := range errs {
			if err == nil {
				t.Fatal("an unsupported operation lowered without a refusal")
			}
			var refusal adapters.Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("error = %v, want an adapters.Refusal", err)
			}
			if !declared[refusal.Feature] {
				t.Fatalf("refusal feature %q is outside RefusableFeatures()", refusal.Feature)
			}
		}
	})

	t.Run("the limits mapping never quietly widens the site's own bound", func(t *testing.T) {
		step := adapters.WorkflowTransformStep{
			NodeID: "n", TransformRef: "t", Version: 1,
			Limits: adapters.WorkflowTransformLimits{MaxInputBytes: 4096, MaxOutputBytes: 2048, MaxSteps: 11},
			Mappings: []adapters.WorkflowMapping{{
				Target: "f", TargetType: adapters.WorkflowValueType{Kind: adapters.WorkflowKindString},
				SourceKind: adapters.WorkflowSourceWorkflowInput, SourcePath: "f",
			}},
		}
		lowered, err := adapters.LowerWorkflowTransform(step)
		if err != nil {
			t.Fatalf("lower: %v", err)
		}
		if lowered.Limits.Definition.MaxInputBytes != 4096 || lowered.Limits.Definition.MaxOutputBytes != 2048 {
			t.Fatalf("byte bounds were not carried verbatim: %+v", lowered.Limits.Definition)
		}
		if lowered.Limits.Execution.MaxSteps != 11 || lowered.Limits.Execution.MaxRows != 1 {
			t.Fatalf("execution bounds were not carried verbatim: %+v", lowered.Limits.Execution)
		}
		if _, err := lowered.Run([]map[string]string{
			{"workflow.transform.n.source.input:f": "a"},
			{"workflow.transform.n.source.input:f": "b"},
		}); err == nil {
			t.Fatal("two rows were accepted under a one-row dataset bound")
		}
	})
}

// TestVersionAndExplain covers the ARCH-GO-009 engine package contract.
func TestVersionAndExplain(t *testing.T) {
	if got := adapters.Version(); got != adapters.ContractVersion {
		t.Fatalf("Version() = %d, want %d", got, adapters.ContractVersion)
	}
	lowered, err := adapters.LowerConnectivityRules(rulesFromSite(connectivityRulesFixture()))
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	if lowered.Explain() == "" {
		t.Fatal("Explain returned nothing")
	}
	if !adapters.ValidName("a.b:c/d-e_f") || adapters.ValidName("1bad") || adapters.ValidName("has space") {
		t.Fatal("ValidName does not mirror the transformation contract's name vocabulary")
	}
	if len(adapters.RefusableFeatures()) == 0 || len(adapters.Sites()) == 0 {
		t.Fatal("the declared vocabularies are empty")
	}
}

// FuzzTodo_XFORM_008 asserts the total-function property: for any mapping
// form, a lowering either produces a valid, digestible program or returns an
// error that unwraps to ErrNoIREquivalent. It never panics and never returns
// a program that the shared engine would then refuse.
func FuzzTodo_XFORM_008(f *testing.F) {
	f.Add("v1", "src", "dst", "IDENTITY", "", "ERROR")
	f.Add("workday/v1", "Worker_ID", "worker.external_id", "CONSTANT", "workday", "ERROR")
	f.Add("d", "s", "t", "DATE", time.RFC3339, "OMIT")
	f.Add("", "", "", "", "", "")
	f.Add("v1", "a b", "c d", "TRIM", "\x00", "NOPE")

	f.Fuzz(func(t *testing.T, version, source, target, op, argument, null string) {
		rules := adapters.ConnectivityRules{
			Version: version,
			Rules: []adapters.ConnectivityRule{{
				Source: source, Target: target, Op: adapters.ConnectivityOp(op),
				Argument: argument, Null: adapters.ConnectivityNullPolicy(null),
			}},
		}
		lowered, err := adapters.LowerConnectivityRules(rules)
		if err != nil {
			if !errors.Is(err, adapters.ErrNoIREquivalent) {
				t.Fatalf("error %v does not unwrap to ErrNoIREquivalent", err)
			}
			return
		}
		if err := lowered.Program.Validate(); err != nil {
			t.Fatalf("lowering produced a program the IR refuses: %v", err)
		}
		if _, err := lowered.Digest(); err != nil {
			t.Fatalf("lowering produced an undigestible artifact: %v", err)
		}
		if _, err := exec.New(lowered.Program); err != nil {
			t.Fatalf("lowering produced a program exec refuses: %v", err)
		}

		importMapping := adapters.DataOpsImportMapping{
			Version: version,
			Fields: []adapters.DataOpsFieldMapping{{
				SourceColumn: source, Target: target,
				Transform: adapters.DataOpsTransform{Kind: adapters.DataOpsTransformKind(op), Layout: argument, Constant: argument},
			}},
		}
		if lowered, err := adapters.LowerDataOpsImport(importMapping); err != nil {
			if !errors.Is(err, adapters.ErrNoIREquivalent) {
				t.Fatalf("error %v does not unwrap to ErrNoIREquivalent", err)
			}
		} else if err := lowered.Program.Validate(); err != nil {
			t.Fatalf("lowering produced a program the IR refuses: %v", err)
		}
	})
}
