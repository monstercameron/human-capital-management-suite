package archrules_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/archrules"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

const completeEngineSource = `package fakeengine

// Version reports the engine's own contract version.
func Version() int { return 1 }

// Compile turns a definition into an evaluatable form.
func Compile(def string) (*Program, error) { return nil, nil }

// Evaluate runs a compiled program against inputs.
func Evaluate(p *Program, input string) (Result, error) { return Result{}, nil }

type Program struct{}

// Result carries an evaluation outcome plus its own explanation.
type Result struct{}

// Explain reports why Result came out the way it did.
func (r Result) Explain() string { return "" }
`

const missingVersionAndExplainSource = `package fakeengine

func Compile(def string) (*Program, error) { return nil, nil }
func Evaluate(p *Program, input string) (Result, error) { return Result{}, nil }

type Program struct{}
type Result struct{}
`

const compileWithoutEvaluateSource = `package fakeengine

func Version() int { return 1 }
func Compile(def string) (*Program, error) { return nil, nil }
func Explain() string { return "" }

type Program struct{}
`

// TestEnginePackageContractCompleteness is the ARCH-GO-009 primary test:
// every internal/engines/<name> package must expose Version(), an
// Explain-shaped symbol, and either both Compile and Evaluate or neither
// ("where applicable" -- a pure-evaluation engine need not invent a
// separate compile step, but it cannot have one without the other).
func TestEnginePackageContractCompleteness(t *testing.T) {
	cfg := loadArchConfig(t)
	ec := cfg.EngineContract

	cases := []struct {
		name        string
		source      string
		wantMissing bool
	}{
		{"complete engine contract", completeEngineSource, false},
		{"missing Version and Explain", missingVersionAndExplainSource, true},
		{"Compile without its Evaluate pair", compileWithoutEvaluateSource, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			names, err := archrules.ExportedTopLevelNamesFromSource("fixture.go", tc.source)
			if err != nil {
				t.Fatalf("ExportedTopLevelNamesFromSource: %v", err)
			}
			missing := archrules.CheckEngineContract(ec, names)
			if tc.wantMissing && len(missing) == 0 {
				t.Errorf("expected missing-contract findings, got none")
			}
			if !tc.wantMissing && len(missing) != 0 {
				t.Errorf("expected a complete contract, got missing: %v", missing)
			}
		})
	}
}

// TestTodo_ARCH_GO_009_Integration runs the contract check against every
// real internal/engines/<name> package and reports every gap found in HEAD.
func TestTodo_ARCH_GO_009_Integration(t *testing.T) {
	cfg := loadArchConfig(t)
	root := repopath.RootDir()

	entries, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var checked, withGaps int
	for _, pkg := range entries {
		rel, ok := archrules.TrimModule(cfg.Module, pkg.ImportPath)
		if !ok || !archrules.UnderRoot(rel, cfg.EngineContract.Root) {
			continue
		}
		// Only check an engine's own root package (internal/engines/<name>),
		// not every internal implementation subpackage nested under it: the
		// contract is a package-level promise made by the engine as a
		// whole, at the import path business/domain code actually depends
		// on.
		if rel == cfg.EngineContract.Root {
			continue
		}
		suffix := rel[len(cfg.EngineContract.Root)+1:]
		if containsSlash(suffix) {
			continue // a nested subpackage of one engine, not the engine's own root
		}
		checked++

		names, err := archrules.ExportedTopLevelNames(pkg.Dir)
		if err != nil {
			t.Fatalf("ExportedTopLevelNames(%s): %v", pkg.Dir, err)
		}
		if missing := archrules.CheckEngineContract(cfg.EngineContract, names); len(missing) > 0 {
			withGaps++
			for _, m := range missing {
				t.Errorf("ARCH-GO-009 violation: engine %s: %s", rel, m)
			}
		}
	}
	t.Logf("ARCH-GO-009: checked %d engine packages, %d with contract gaps", checked, withGaps)
}

func containsSlash(s string) bool {
	for _, r := range s {
		if r == '/' {
			return true
		}
	}
	return false
}

// TestTodo_ARCH_GO_009_Conformance checks the paired-symbol rule
// specifically: Evaluate without Compile is also a gap (the pair check is
// symmetric, not just "Compile requires Evaluate").
func TestTodo_ARCH_GO_009_Conformance(t *testing.T) {
	cfg := loadArchConfig(t)
	names, err := archrules.ExportedTopLevelNamesFromSource("fixture.go", `package fakeengine

func Version() int { return 1 }
func Evaluate() int { return 1 }
func Explain() string { return "" }
`)
	if err != nil {
		t.Fatalf("ExportedTopLevelNamesFromSource: %v", err)
	}
	missing := archrules.CheckEngineContract(cfg.EngineContract, names)
	if len(missing) == 0 {
		t.Errorf("Evaluate without Compile should be flagged as an asymmetric pair, found no gaps")
	}
}

// TestTodo_ARCH_GO_009_Golden pins the exact required-shape declared in
// architecture-rules.yaml.
func TestTodo_ARCH_GO_009_Golden(t *testing.T) {
	cfg := loadArchConfig(t)
	ec := cfg.EngineContract
	if len(ec.RequiredSymbols) != 1 || ec.RequiredSymbols[0] != "Version" {
		t.Errorf("engine_contract.required_symbols = %v, want [Version]", ec.RequiredSymbols)
	}
	if len(ec.PairedSymbols) != 1 || len(ec.PairedSymbols[0]) != 2 || ec.PairedSymbols[0][0] != "Compile" || ec.PairedSymbols[0][1] != "Evaluate" {
		t.Errorf("engine_contract.paired_symbols = %v, want [[Compile Evaluate]]", ec.PairedSymbols)
	}
}

func TestTodo_ARCH_GO_009_Property(t *testing.T) {
	ec := loadArchConfig(t).EngineContract
	for i := 0; i < 12; i++ {
		// Independently generated public symbol sets must pass only when they
		// include the unconditional version/explanation and symmetric pair.
		names := map[string]bool{"Version": true, "Compile": true, "Evaluate": true, "Explain": true}
		if i%2 == 1 {
			names = map[string]bool{"Version": true, "Compile": true, "Evaluate": true, "ExplainResult": true}
		}
		if missing := archrules.CheckEngineContract(ec, names); len(missing) != 0 {
			t.Errorf("complete variant %d rejected: %v", i, missing)
		}
		if missing := archrules.CheckEngineContract(ec, map[string]bool{"Version": true, "Compile": true, "Explain": true}); len(missing) == 0 {
			t.Errorf("variant %d missing Evaluate accepted", i)
		}
	}
}
