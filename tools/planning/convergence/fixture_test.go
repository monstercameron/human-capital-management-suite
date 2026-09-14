package convergence

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Fixture owners. alpha and beta are selected P1A intents, gamma is an
// accepted intent outside the selection, SELECT-X and SELECT-Y are
// selecting todos.
const (
	fxAlpha   = "hcmnext.t.alpha/v1"
	fxBeta    = "hcmnext.t.beta/v1"
	fxGamma   = "hcmnext.t.gamma/v1"
	fxSelectX = "SELECT-X"
	fxSelectY = "SELECT-Y"
)

// fixtureSnapshot is the shared CLOSE-002 fixture. It genuinely contains:
//   - one missing-test gap for alpha reported by three compilers in three
//     different sentences;
//   - one workflow-design gap for beta reported by two compilers;
//   - one selection-gate gap for SELECT-X reported as a binding reason and,
//     in different words, as an unfilled slot;
//   - two ownerless gaps (an orphan wire method and a dangling DIRECT
//     token);
//   - one inert selected fact (SELECT-X's artifact, which nothing
//     downstream carries) beside a consumed one (SELECT-Y's, carried by the
//     threat register) and two consumed intent facts;
//   - an out-of-selection gap, an existing-todo claim and an explicit
//     rejection.
func fixtureSnapshot() Snapshot {
	return Snapshot{
		Selected: []SelectedOwner{
			{Owner: fxAlpha, Kind: OwnerKindIntent, Phase: "P1A"},
			{Owner: fxBeta, Kind: OwnerKindIntent, Phase: "P1A"},
			{Owner: fxSelectX, Kind: OwnerKindSelection, Phase: "P0"},
			{Owner: fxSelectY, Kind: OwnerKindSelection, Phase: "P0"},
		},
		KnownOwners: []string{fxGamma, "ALPHA-HANDLER-1"},
		Facts: []Fact{
			{ID: "intent:" + fxAlpha, Kind: FactSelectedIntent, Owner: fxAlpha, Tokens: []string{fxAlpha}},
			{ID: "intent:" + fxBeta, Kind: FactSelectedIntent, Owner: fxBeta, Tokens: []string{fxBeta}},
			{ID: "selection:" + fxSelectX, Kind: FactSelectionArtifact, Owner: fxSelectX, Tokens: []string{"definitions/planning/gates/select-x.yaml", "digest-x"}},
			{ID: "selection:" + fxSelectY, Kind: FactSelectionArtifact, Owner: fxSelectY, Tokens: []string{"definitions/planning/gates/select-y.yaml", "digest-y"}},
		},
		Consumers: []ConsumerRef{
			{Layer: LayerSlice, Ref: "definitions/planning/product-slices.yaml#promotion", Tokens: []string{fxAlpha}},
			{Layer: LayerTest, Ref: "internal/intent/app#TestBetaSimulates", Tokens: []string{fxBeta}},
			{Layer: LayerThreat, Ref: "definitions/planning/gates/threat-001-register.yaml", Tokens: []string{"definitions/planning/gates/select-y.yaml"}},
			// The SELECT-X artifact file refers to itself; that is not a
			// downstream consumer.
			{Layer: LayerImplementation, Ref: "definitions/planning/gates/select-x.yaml", Tokens: []string{"digest-x"}},
		},
		Observations: []Observation{
			{Compiler: CompilerDesignClosure, Code: "MISSING_TEST", Owner: fxAlpha, Contract: ContractTest, Detail: "evidence names TestAlphaProof, which exists in no scanned test source"},
			{Compiler: CompilerClosureWitness, Code: "EDGE_ABSENT", Owner: fxAlpha, Contract: ContractTest, Detail: "no executable test proves this definition"},
			{Compiler: CompilerIntentCoverage, Code: "intent_test", Owner: fxAlpha, Contract: ContractTest, Detail: "accepted intent has no executable oracle"},
			{Compiler: CompilerClosureWitness, Code: "EDGE_ABSENT", Owner: fxAlpha, Contract: ContractHandler, Detail: "the binding table produced no verified entry"},
			{Compiler: CompilerWorkflowMaturity, Code: "UNBOUND_DESIGN", Owner: fxBeta, Contract: ContractWorkflowDesign, Detail: "no design record joins this definition"},
			{Compiler: CompilerDesignClosure, Code: "MISSING_ARTIFACT", Owner: fxBeta, Contract: ContractWorkflowDesign, Detail: "bound scope item with no workflow artifact"},
			{Compiler: CompilerWorkflowMaturity, Code: "MISSING_ADVERSARIAL_SCENARIO", Owner: fxBeta, Contract: ContractScenario, Detail: "no adversarial scenario matrix"},
			{Compiler: CompilerSelectionBind, Code: "BINDING_NOT_READY", Owner: fxSelectX, Contract: ContractSelectionGate, Detail: "real provider-selection gate: vendor_id is a placeholder"},
			{Compiler: CompilerSelectionBind, Code: "SLOT_UNFILLED", Owner: fxSelectX, Contract: ContractSelectionGate, Detail: "slot provider: SELECT-X has not passed its own gate (1 reason(s))"},
			{Compiler: CompilerClosureWitness, Code: "EDGE_ORPHAN", Contract: ContractEndpoint, Subject: "ENDPOINT|WIRE_METHOD_UNBOUND||svc.v1.Service/Method", Detail: "binding gap belongs to no source-bound definition"},
			{Compiler: CompilerIntentCoverage, Code: "todo_direct_dangling", Contract: ContractTodo, Subject: "todo_direct_dangling:NEXT-9", Detail: "todo DIRECT names no accepted intent"},
			{Compiler: CompilerClosureWitness, Code: "REVERSE_EDGE_ABSENT", Owner: fxGamma, Contract: ContractEngine, Detail: "the named engine is not imported"},
		},
		Todos: []TodoRef{
			{ID: "ALPHA-HANDLER-1", Phase: "P1A"},
			{ID: "NEXT-9", Phase: "P0", Done: true},
		},
		Claims: []Claim{
			{TodoID: "ALPHA-HANDLER-1", Owner: fxAlpha, Contract: ContractHandler, Source: "fixture binding allowlist"},
		},
		Rejections: []Rejection{
			{Owner: fxBeta, Contract: ContractScenario, Rationale: "beta is read-only in P1A; adversarial writes are out of its contract", DecidedBy: "product-owner"},
		},
		Unknowns: []Unknown{
			{Code: UnknownSlotUnfilled, Ref: "slot:provider", Detail: "PHASE-001 selection slot provider is unfilled"},
		},
	}
}

func gapByKey(t testing.TB, r Report, owner string, contract Contract, subject string) Gap {
	t.Helper()
	id := GapIdentity(owner, contract, subject)
	for _, g := range r.Gaps {
		if g.Identity == id {
			return g
		}
	}
	t.Fatalf("no gap %s for %s/%s/%q", id, owner, contract, subject)
	return Gap{}
}

func identities(r Report) map[string]bool {
	out := map[string]bool{}
	for _, g := range r.Gaps {
		out[g.Identity] = true
	}
	return out
}

func unknownCodes(r Report) map[string]int {
	out := map[string]int{}
	for _, u := range r.Unknowns {
		out[u.Code]++
	}
	return out
}

func findingCodes(r Report) map[string]int {
	out := map[string]int{}
	for _, f := range r.Findings {
		out[f.Code]++
	}
	return out
}

func mustMarshal(t testing.TB, r Report) []byte {
	t.Helper()
	b, err := MarshalReport(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func repoRoot(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", file)
		}
		dir = parent
	}
}
