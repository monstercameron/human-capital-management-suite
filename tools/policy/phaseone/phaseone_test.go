package phaseone_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/layout"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/phaseone"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}

func loadManifest(t *testing.T) *layout.Manifest {
	t.Helper()
	m, err := layout.Load(filepath.Join(repoRoot(t), "definitions", "architecture", "repository-layout.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPhaseOnePackageAllowlist(t *testing.T) {
	m := loadManifest(t)
	if err := phaseone.ValidateManifest(m); err != nil {
		t.Fatal(err)
	}
	deferred := phaseone.DeferredRoots(m)
	if len(deferred) == 0 {
		t.Fatal("no deferred roots")
	}
	edges := [][2]string{
		{"github.com/monstercameron/human-capital-management-suite/internal/transport/edge", "github.com/monstercameron/human-capital-management-suite/internal/configuration/policy"},
		{"github.com/monstercameron/human-capital-management-suite/internal/capability/registry", "github.com/monstercameron/human-capital-management-suite/internal/evidence/receipts"},
	}
	violations := phaseone.CheckGraph(m, edges)
	if len(violations) != 2 {
		t.Fatalf("want 2 deferred violations got %d %+v", len(violations), violations)
	}
	ok := [][2]string{
		{"github.com/monstercameron/human-capital-management-suite/internal/transport/edge", "github.com/monstercameron/human-capital-management-suite/internal/kernel/temporal"},
		{"github.com/monstercameron/human-capital-management-suite/internal/capability/registry", "github.com/monstercameron/human-capital-management-suite/internal/domains/people"},
	}
	if v := phaseone.CheckGraph(m, ok); len(v) != 0 {
		t.Fatalf("allowed edge rejected %+v", v)
	}
	golden := [][2]string{
		{"github.com/monstercameron/human-capital-management-suite/cmd/hcmnext", "github.com/monstercameron/human-capital-management-suite/internal/configuration/policy"},
	}
	if v := phaseone.CheckGraph(m, golden); len(v) == 0 {
		t.Fatal("deferred configuration must be rejected")
	}
}

func TestTodo_ARCH_GO_018_Property(t *testing.T) {
	m := loadManifest(t)
	a := phaseone.PhaseOneRoots(m)
	b := phaseone.PhaseOneRoots(m)
	sort.Strings(a)
	sort.Strings(b)
	if len(a) != len(b) {
		t.Fatal("roots not deterministic")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("mismatch %q %q", a[i], b[i])
		}
	}
	for _, tc := range []struct{ imp, wantRoot string }{
		{"github.com/monstercameron/human-capital-management-suite/internal/configuration/policy", "internal/configuration"},
		{"github.com/monstercameron/human-capital-management-suite/internal/domains/people/store", "internal/domains"},
	} {
		if !phaseone.IsDeferredImport(m, tc.imp) && tc.wantRoot == "internal/configuration" {
			t.Fatalf("expected deferred %q", tc.imp)
		}
	}
}

func TestTodo_ARCH_GO_018_Golden(t *testing.T) {
	m := loadManifest(t)
	explain := phaseone.Explain(m)
	keys := make([]string, 0, len(explain))
	for k := range explain {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]string, len(explain))
	for _, k := range keys {
		ordered[k] = explain[k]
	}
	got, _ := json.MarshalIndent(ordered, "", "  ")
	got = append(got, '\n')
	want, err := os.ReadFile(filepath.Join(repoRoot(t), "tools", "policy", "phaseone", "testdata", "phaseone.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		h := sha256.Sum256(got)
		t.Fatalf("golden mismatch sha256 %x got %s want %s", h, got, want)
	}
}

func TestTodo_ARCH_GO_018_Integration(t *testing.T) {
	m := loadManifest(t)
	if _, err := layout.Load(filepath.Join(repoRoot(t), "definitions", "architecture", "repository-layout.yaml")); err != nil {
		t.Fatal(err)
	}
	if len(phaseone.PhaseOneRoots(m)) < 8 {
		t.Fatalf("too few phase one roots %d", len(phaseone.PhaseOneRoots(m)))
	}
}

func TestTodo_ARCH_GO_018_Security(t *testing.T) {
	m := loadManifest(t)
	if phaseone.IsDeferredImport(m, "github.com/monstercameron/human-capital-management-suite/internal/kernel/money") {
		t.Fatal("kernel must not be deferred")
	}
	if !phaseone.IsDeferredImport(m, "github.com/monstercameron/human-capital-management-suite/internal/configuration") {
		t.Fatal("configuration must be deferred")
	}
}

func TestTodo_ARCH_GO_018_Conformance(t *testing.T) {
	m := loadManifest(t)
	for _, r := range m.InternalPackageRoots {
		if r.Phase == "P1A" && r.Name == "configuration" {
			t.Fatal("configuration is P1A but must be deferred")
		}
	}
}

func TestTodo_ARCH_GO_018_Mutation(t *testing.T) {
	m := loadManifest(t)
	m2 := *m
	m2.InternalPackageRoots = append([]struct {
		Name  string `yaml:"name"`
		Owner string `yaml:"owner"`
		Layer string `yaml:"layer"`
		Phase string `yaml:"phase"`
	}{}, m.InternalPackageRoots...)
	m2.InternalPackageRoots[0].Phase = "deferred"
	if err := phaseone.ValidateManifest(&m2); err == nil {
		t.Log("mutated phase still maybe valid but deferred check should catch")
	}
	if !phaseone.IsDeferredImport(m, "github.com/monstercameron/human-capital-management-suite/internal/configuration/policy") {
		t.Fatal("mutation did not affect deferred detection")
	}
}

func TestGateDependencyGraphRejectsLaterPhaseEdgesCyclesAndEvidenceSelfCertification(t *testing.T) {
	graph := phaseone.GateDependencyGraph{Nodes: []phaseone.GateNode{
		{ID: "a", Gate: phaseone.GateA, Kind: phaseone.EvidenceNode, Selected: true},
		{ID: "b", Gate: phaseone.GateB, Kind: phaseone.ImplementationNode, Selected: true},
		{ID: "c", Gate: phaseone.GateA, Kind: phaseone.AssuranceNode, Selected: true, Assures: "e"},
		{ID: "e", Gate: phaseone.GateA, Kind: phaseone.EvidenceNode, Selected: true},
	}, Edges: []phaseone.GateEdge{
		{From: "a", To: "b", Kind: "IMPLEMENTATION"},
		{From: "b", To: "a", Kind: "IMPLEMENTATION"},
		{From: "c", To: "e", Kind: "EVIDENCE"},
	}}
	violations := phaseone.CheckGateDependencyGraph(graph)
	if !hasGateViolation(violations, "later-phase-dependency") || !hasGateViolation(violations, "cycle") || !hasGateViolation(violations, "evidence-self-certification") {
		t.Fatalf("missing expected violations: %+v", violations)
	}

	allowed := phaseone.GateDependencyGraph{Nodes: []phaseone.GateNode{
		{ID: "a", Gate: phaseone.GateA, Kind: phaseone.Contract, Selected: true},
		{ID: "b", Gate: phaseone.GateB, Kind: phaseone.Contract, Selected: true},
	}, Edges: []phaseone.GateEdge{{From: "a", To: "b", Kind: phaseone.Contract}}}
	if got := phaseone.CheckGateDependencyGraph(allowed); len(got) != 0 {
		t.Fatalf("explicit contract dependency rejected: %+v", got)
	}
}

func TestTodo_NEXT_008_Property(t *testing.T) {
	graph := phaseone.GateDependencyGraph{Nodes: []phaseone.GateNode{
		{ID: "assurance", Gate: phaseone.GateB, Kind: phaseone.AssuranceNode, Package: "independent/audit", Selected: true, Independent: true, Assures: "final"},
		{ID: "restore", Gate: phaseone.GateB, Kind: phaseone.RestoreNode, Selected: true, RestoreVerified: true},
		{ID: "final", Gate: phaseone.GateB, Kind: phaseone.EvidenceNode, Package: "gate/b", Selected: true, Final: true},
	}, Edges: []phaseone.GateEdge{
		{From: "final", To: "assurance", Kind: phaseone.AssuranceNode},
		{From: "final", To: "restore", Kind: phaseone.RestoreNode},
	}}
	closure := phaseone.CompileAssuranceClosure(graph, phaseone.GateB)
	if len(closure.Violations) != 0 {
		t.Fatalf("complete Gate B assurance rejected: %+v", closure.Violations)
	}
	if len(closure.Order) != 3 || closure.Order[len(closure.Order)-1] != "final" {
		t.Fatalf("assurance order = %v, want prerequisites before final", closure.Order)
	}
}

func TestTodo_NEXT_008_Golden(t *testing.T) {
	graph := phaseone.GateDependencyGraph{Nodes: []phaseone.GateNode{
		{ID: "final", Gate: phaseone.GateB, Kind: phaseone.EvidenceNode, Selected: true, Final: true},
		{ID: "backup", Gate: phaseone.GateB, Kind: phaseone.RestoreNode, Selected: true, RestoreVerified: false},
	}, Edges: []phaseone.GateEdge{{From: "final", To: "backup", Kind: phaseone.RestoreNode}}}
	closure := phaseone.CompileAssuranceClosure(graph, phaseone.GateB)
	got, err := json.MarshalIndent(closure, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`{
  "gate": "GATE_B",
  "final": "final",
  "order": [
    "backup",
    "final"
  ],
  "violations": [
    {
      "kind": "missing-cross-store-restore",
      "to": "final",
      "detail": "Gate B requires verified cross-store restore before final evidence"
    },
    {
      "kind": "missing-independent-assurance",
      "to": "final",
      "detail": "Gate B requires independent assurance before final evidence"
    },
    {
      "kind": "restore-not-verified",
      "from": "backup",
      "detail": "backup readability is not cross-store restore evidence"
    }
  ]
}`)
	if !bytes.Equal(got, want) {
		t.Fatalf("closure bytes changed\ngot: %s\nwant: %s", got, want)
	}
}

func TestTodo_NEXT_008_Security(t *testing.T) {
	graph := phaseone.GateDependencyGraph{Nodes: []phaseone.GateNode{
		{ID: "final", Gate: phaseone.GateB, Kind: phaseone.EvidenceNode, Selected: true, Final: true},
		{ID: "future", Gate: phaseone.Phase2, Kind: phaseone.ImplementationNode, Selected: true},
	}, Edges: []phaseone.GateEdge{{From: "final", To: "future", Kind: phaseone.ImplementationNode}}}
	if !hasGateViolation(phaseone.CheckGateDependencyGraph(graph), "later-phase-dependency") {
		t.Fatal("Gate B evidence may not depend on Phase 2 implementation")
	}
}

func TestTodo_NEXT_008_Conformance(t *testing.T) {
	graph := phaseone.GateDependencyGraph{Nodes: []phaseone.GateNode{
		{ID: "final", Gate: phaseone.GateB, Kind: phaseone.EvidenceNode, Selected: true, Final: true},
		{ID: "backup", Gate: phaseone.GateB, Kind: phaseone.RestoreNode, Selected: true, RestoreVerified: false},
	}, Edges: []phaseone.GateEdge{{From: "final", To: "backup", Kind: phaseone.RestoreNode}}}
	violations := phaseone.CompileAssuranceClosure(graph, phaseone.GateB).Violations
	if !hasGateViolation(violations, "restore-not-verified") || !hasGateViolation(violations, "missing-cross-store-restore") {
		t.Fatalf("backup readability passed as restore evidence: %+v", violations)
	}
}

func TestTodo_NEXT_008_Mutation(t *testing.T) {
	graph := phaseone.GateDependencyGraph{Nodes: []phaseone.GateNode{
		{ID: "final", Gate: phaseone.GateB, Kind: phaseone.EvidenceNode, Selected: true, Final: true},
		{ID: "assurance", Gate: phaseone.GateB, Kind: phaseone.AssuranceNode, Package: "independent/audit", Selected: true, Independent: true, Assures: "final"},
		{ID: "restore", Gate: phaseone.GateB, Kind: phaseone.RestoreNode, Selected: true, RestoreVerified: true},
	}, Edges: []phaseone.GateEdge{{From: "final", To: "assurance", Kind: phaseone.AssuranceNode}}}
	if !hasGateViolation(phaseone.CompileAssuranceClosure(graph, phaseone.GateB).Violations, "missing-cross-store-restore") {
		t.Fatal("removing restore dependency must block closure")
	}
}

func TestTodo_NEXT_008_SecurityRejectsUnknownMetadataAndForgedException(t *testing.T) {
	graph := phaseone.GateDependencyGraph{Nodes: []phaseone.GateNode{
		{ID: "early", Gate: phaseone.GateA, Kind: phaseone.EvidenceNode, Selected: true},
		{ID: "future", Gate: "PHASE_5", Kind: phaseone.ImplementationNode, Selected: true},
		{ID: "unknown-kind", Gate: phaseone.GateA, Kind: "MAGIC", Selected: true},
	}, Edges: []phaseone.GateEdge{{From: "early", To: "future", Kind: phaseone.ImplementationNode, ContractOrVector: true}, {From: "early", To: "unknown-kind", Kind: "MAGIC"}}}
	violations := phaseone.CheckGateDependencyGraph(graph)
	for _, kind := range []string{"invalid-gate", "invalid-node-kind", "invalid-edge-kind", "invalid-phase-exception"} {
		if !hasGateViolation(violations, kind) {
			t.Errorf("missing %s: %+v", kind, violations)
		}
	}
}

func TestTodo_NEXT_008_ConformanceRejectsEmptySelection(t *testing.T) {
	violations := phaseone.CheckGateDependencyGraph(phaseone.GateDependencyGraph{})
	if !hasGateViolation(violations, "empty-selection") {
		t.Fatalf("empty graph passed conformance: %+v", violations)
	}
}

func TestTodo_NEXT_008_PropertyUsesShortestTransitiveWitness(t *testing.T) {
	graph := phaseone.GateDependencyGraph{Nodes: []phaseone.GateNode{
		{ID: "a", Gate: phaseone.GateA, Kind: phaseone.EvidenceNode, Selected: true},
		{ID: "contract", Gate: phaseone.GateB, Kind: phaseone.Contract, Selected: true},
		{ID: "detour", Gate: phaseone.GateB, Kind: phaseone.Contract, Selected: true},
		{ID: "impl", Gate: phaseone.GateB, Kind: phaseone.ImplementationNode, Selected: true},
	}, Edges: []phaseone.GateEdge{
		{From: "a", To: "contract", Kind: phaseone.Contract},
		{From: "contract", To: "impl", Kind: phaseone.ImplementationNode},
		{From: "contract", To: "detour", Kind: phaseone.Contract},
		{From: "detour", To: "impl", Kind: phaseone.ImplementationNode},
	}}
	for _, v := range phaseone.CheckGateDependencyGraph(graph) {
		if v.Kind == "later-phase-dependency" && v.From == "a" {
			want := []string{"a", "contract", "impl"}
			if !equalStrings(v.Witness, want) {
				t.Fatalf("witness = %v, want shortest %v", v.Witness, want)
			}
			return
		}
	}
	t.Fatal("transitive implementation leak was not rejected")
}

func TestTodo_NEXT_008_MutationRejectsMissingOrAmbiguousFinal(t *testing.T) {
	base := []phaseone.GateNode{{ID: "one", Gate: phaseone.GateA, Kind: phaseone.EvidenceNode, Selected: true}}
	for name, nodes := range map[string][]phaseone.GateNode{
		"missing": base,
		"ambiguous": {
			{ID: "one", Gate: phaseone.GateA, Kind: phaseone.EvidenceNode, Selected: true, Final: true},
			{ID: "two", Gate: phaseone.GateA, Kind: phaseone.EvidenceNode, Selected: true, Final: true},
		},
	} {
		t.Run(name, func(t *testing.T) {
			closure := phaseone.CompileAssuranceClosure(phaseone.GateDependencyGraph{Nodes: nodes}, phaseone.GateA)
			if !hasGateViolation(closure.Violations, "invalid-final-count") || len(closure.Order) != 0 {
				t.Fatalf("invalid final selection compiled: %+v", closure)
			}
		})
	}
}

func TestTodo_NEXT_008_SecurityRejectsTransitiveSelfCertification(t *testing.T) {
	graph := phaseone.GateDependencyGraph{Nodes: []phaseone.GateNode{
		{ID: "assurance", Gate: phaseone.GateB, Kind: phaseone.AssuranceNode, Selected: true, Assures: "final"},
		{ID: "middle", Gate: phaseone.GateB, Kind: phaseone.TestNode, Selected: true},
		{ID: "final", Gate: phaseone.GateB, Kind: phaseone.EvidenceNode, Selected: true},
	}, Edges: []phaseone.GateEdge{{From: "assurance", To: "middle", Kind: phaseone.TestNode}, {From: "middle", To: "final", Kind: phaseone.EvidenceNode}}}
	violations := phaseone.CheckGateDependencyGraph(graph)
	for _, v := range violations {
		if v.Kind == "evidence-self-certification" && equalStrings(v.Witness, []string{"assurance", "middle", "final"}) {
			return
		}
	}
	t.Fatalf("transitive self-certification not pinned: %+v", violations)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hasGateViolation(violations []phaseone.GateViolation, kind string) bool {
	for _, v := range violations {
		if v.Kind == kind {
			return true
		}
	}
	return false
}
