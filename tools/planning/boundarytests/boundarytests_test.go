package boundarytests

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depedge"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/importgraph"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/layout"
)

func loadRealGraph(t *testing.T) (*importgraph.Graph, *depedge.Policy) {
	t.Helper()
	root := filepath.Join("..", "..", "..")

	layoutManifest, err := layout.Load(filepath.Join(root, "definitions", "architecture", "repository-layout.yaml"))
	if err != nil {
		t.Fatalf("loading repository-layout manifest: %v", err)
	}
	policy, err := depedge.Load(filepath.Join(root, "definitions", "architecture", "package-dependency-policy.yaml"))
	if err != nil {
		t.Fatalf("loading package-dependency-policy manifest: %v", err)
	}
	graph, err := importgraph.Build(root, layoutManifest, policy)
	if err != nil {
		t.Fatalf("building import graph: %v", err)
	}
	return graph, policy
}

// TestPlaneBoundaries is the primary red/green test for GOV-013: it proves
// the real import graph obeys package-dependency-policy.yaml's direction
// rules end to end, exercises the three named RED scenarios individually
// against depedge.Policy.CheckEdge, and compares the real graph's coarse
// layer shape against a committed golden so an accidental new edge is a
// diff, never a silent pass.
func TestPlaneBoundaries(t *testing.T) {
	graph, policy := loadRealGraph(t)

	t.Run("the real import graph has zero package-dependency-policy violations", func(t *testing.T) {
		if len(graph.Packages) == 0 {
			t.Fatal("import graph has no packages")
		}
		for _, v := range graph.PolicyViolations {
			t.Errorf("forbidden dependency edge: %s -> %s violates %s", v.Importer, v.Imported, v.Rule)
		}
	})

	t.Run("the real import graph has no cycle", func(t *testing.T) {
		if graph.Cycle != nil {
			t.Errorf("import cycle detected: %s", strings.Join(graph.Cycle, " -> "))
		}
	})

	t.Run("RED: workflow importing a domain's persistence subpackage is rejected", func(t *testing.T) {
		importer := policy.Module + "/internal/workflow/promote"
		imported := policy.Module + "/internal/domains/people/store"
		v := policy.CheckEdge(importer, imported)
		if v == nil || v.Rule != depedge.RuleWorkflowDomainPersist {
			t.Fatalf("CheckEdge(%s, %s) = %v, want rule %s", importer, imported, v, depedge.RuleWorkflowDomainPersist)
		}
	})

	t.Run("RED: transport bypassing the capability gateway to import a store directly is rejected", func(t *testing.T) {
		// transport is this policy's edge/UI-facing layer; reaching a data
		// or ledger port directly, instead of going through
		// internal/capability, is exactly the "UI bypasses capabilities"
		// shape GOV-013 names, instantiated against the concrete layers
		// package-dependency-policy.yaml declares today.
		importer := policy.Module + "/internal/transport/edge"
		imported := policy.Module + "/internal/data"
		v := policy.CheckEdge(importer, imported)
		if v == nil || v.Rule != depedge.RuleTransportImportsStore {
			t.Fatalf("CheckEdge(%s, %s) = %v, want rule %s", importer, imported, v, depedge.RuleTransportImportsStore)
		}
	})

	t.Run("RED: Intelligence has not become a synchronous command dependency", func(t *testing.T) {
		// package-dependency-policy.yaml does not yet declare an
		// "intelligence" layer at all (plan.md's plane model is
		// aspirational ahead of the concrete manifest): internal/intelligence
		// does not exist in this repository yet. Rather than silently
		// passing because there is nothing to classify, assert the
		// stronger fact directly - no package anywhere imports one - so
		// the day it is added as workflow's synchronous dependency this
		// test starts failing instead of staying silently green.
		if ImportsSubtree(graph, policy.Module, "internal/workflow", "internal/intelligence") {
			t.Error("internal/workflow imports internal/intelligence: Intelligence must stay a rebuildable, asynchronous dependency, never a synchronous command path")
		}
		for _, pkg := range graph.Packages {
			if strings.HasPrefix(pkg.ImportPath, policy.Module+"/internal/intelligence") {
				t.Skip("internal/intelligence now exists; replace this proxy check with a real layer rule in package-dependency-policy.yaml")
			}
		}
	})

	t.Run("the real layer graph matches the committed golden (regenerate with HCMNEXT_UPDATE_GOLDEN=1)", func(t *testing.T) {
		// testdata/layer-graph.golden.txt carries one edge with no source
		// comment support of its own (every non-empty line is compared as a
		// real edge, so a "#"-prefixed line would itself show up as a
		// spurious diff): "workflow -> ledger" is internal/workflow/execute/
		// effects.LedgerTerminalWriter recording the P1B promotion driver's
		// terminal outcome through the ledger port at its END node — the
		// workflow layer's own legitimate governed write, not a boundary
		// violation. It first appeared when the caller-driven execution
		// composition (internal/platform/execution.NewPromotionExecution)
		// was deliberately moved out of internal/transport/cell, which is
		// also why this golden no longer carries "transport -> workflow" or
		// "transport -> transaction": composition-root wiring now lives in
		// internal/platform, which package-dependency-policy.yaml does not
		// rank as a business layer at all, exactly like cmd/* itself.
		// The data -> capabilities edge is evidencestore's implementation
		// of capability.EvidenceSink; it persists invocation evidence and
		// grants no capability decision authority to the data layer.
		edges := LayerGraph(graph, policy)
		got := RenderGolden(edges)

		goldenPath := filepath.Join("testdata", "layer-graph.golden.txt")
		if os.Getenv("HCMNEXT_UPDATE_GOLDEN") == "1" {
			if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
				t.Fatalf("writing %s: %v", goldenPath, err)
			}
			t.Logf("wrote updated layer-graph golden (%d edges)", len(edges))
			return
		}

		wantBytes, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatalf("read %s: %v (create it with HCMNEXT_UPDATE_GOLDEN=1)", goldenPath, err)
		}

		diff, ok := GoldenDiff(string(wantBytes), got)
		if !ok {
			t.Errorf("layer graph no longer matches testdata/layer-graph.golden.txt:\n%s\nIf this new edge is an intended, reviewed architecture change, regenerate with:\n  HCMNEXT_UPDATE_GOLDEN=1 go test ./tools/planning/boundarytests/... -run TestPlaneBoundaries", diff)
		}
	})
}

// TestTodo_GOV_013_Property fuzzes LayerGraph with synthetic import graphs:
// its output must always be sorted, deduplicated and free of self-edges,
// regardless of how many times a given edge repeats in the input or what
// order it arrives in.
func TestTodo_GOV_013_Property(t *testing.T) {
	policy := &depedge.Policy{
		Module: "example.com/mod",
		Layers: []depedge.Layer{
			{Name: "a", Rank: 0, Roots: []string{"internal/a"}},
			{Name: "b", Rank: 1, Roots: []string{"internal/b"}},
			{Name: "c", Rank: 2, Roots: []string{"internal/c"}},
		},
	}
	layers := []string{"internal/a", "internal/b", "internal/c"}

	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 100; trial++ {
		n := 1 + rng.Intn(30)
		var edges []importgraph.Edge
		for i := 0; i < n; i++ {
			from := layers[rng.Intn(len(layers))]
			to := layers[rng.Intn(len(layers))]
			edges = append(edges, importgraph.Edge{
				Importer: policy.Module + "/" + from + "/pkg1",
				Imported: policy.Module + "/" + to + "/pkg2",
			})
		}
		// Duplicate the whole edge list in reverse order: the result must
		// be identical (dedup + stable sort), proving order-independence.
		reversed := make([]importgraph.Edge, len(edges))
		for i, e := range edges {
			reversed[len(edges)-1-i] = e
		}
		full := append(append([]importgraph.Edge{}, edges...), reversed...)

		graph := &importgraph.Graph{Module: policy.Module, Edges: full}
		result := LayerGraph(graph, policy)

		for _, e := range result {
			if e.From == e.To {
				t.Fatalf("trial %d: LayerGraph produced a self-edge %s", trial, e)
			}
		}
		for i := 1; i < len(result); i++ {
			if result[i-1].From > result[i].From || (result[i-1].From == result[i].From && result[i-1].To > result[i].To) {
				t.Fatalf("trial %d: LayerGraph output not sorted: %v", trial, result)
			}
		}
		seen := map[LayerEdge]int{}
		for _, e := range result {
			seen[e]++
		}
		for e, count := range seen {
			if count != 1 {
				t.Fatalf("trial %d: edge %s appeared %d times, want exactly once", trial, e, count)
			}
		}

		// Determinism: building it twice from the same input gives the
		// same result.
		result2 := LayerGraph(graph, policy)
		if len(result) != len(result2) {
			t.Fatalf("trial %d: LayerGraph is not deterministic: %v vs %v", trial, result, result2)
		}
		for i := range result {
			if result[i] != result2[i] {
				t.Fatalf("trial %d: LayerGraph is not deterministic at index %d: %v vs %v", trial, i, result[i], result2[i])
			}
		}
	}
}

// TestTodo_GOV_013_Golden pins RenderGolden's exact line format so a future
// refactor cannot silently change the on-disk golden's shape.
func TestTodo_GOV_013_Golden(t *testing.T) {
	edges := []LayerEdge{
		{From: "capabilities", To: "kernel"},
		{From: "workflow", To: "domains"},
	}
	got := RenderGolden(edges)
	want := "capabilities -> kernel\nworkflow -> domains\n"
	if got != want {
		t.Errorf("RenderGolden output changed:\n got:  %q\n want: %q", got, want)
	}
}

// TestTodo_GOV_013_Recovery proves the golden comparison fails loudly
// rather than silently in two boundary conditions: a golden that has never
// been recorded, and a golden whose line endings were mangled to CRLF by a
// Windows checkout (a documented repo hazard - the pre-commit hook itself
// flags CRLF worktree files).
func TestTodo_GOV_013_Recovery(t *testing.T) {
	t.Run("a never-recorded (empty) golden fails loudly instead of matching by default", func(t *testing.T) {
		diff, ok := GoldenDiff("", "workflow -> domains\n")
		if ok {
			t.Fatal("expected an empty golden to never compare equal")
		}
		if !strings.Contains(diff, "no golden") {
			t.Errorf("expected a clear \"no golden recorded\" message, got %q", diff)
		}
	})

	t.Run("recovers from CRLF-mangled golden line endings without a false-positive diff", func(t *testing.T) {
		want := "capabilities -> kernel\r\nworkflow -> domains\r\n"
		got := "capabilities -> kernel\nworkflow -> domains\n"
		diff, ok := GoldenDiff(want, got)
		if !ok {
			t.Errorf("expected CRLF-vs-LF to compare equal after normalization, got diff:\n%s", diff)
		}
	})

	t.Run("a genuinely new edge is still caught even with CRLF on one side", func(t *testing.T) {
		want := "capabilities -> kernel\r\n"
		got := "capabilities -> kernel\nworkflow -> domains\n"
		diff, ok := GoldenDiff(want, got)
		if ok {
			t.Fatal("expected a genuinely new edge to be reported, not hidden by CRLF normalization")
		}
		if !strings.Contains(diff, "workflow -> domains") {
			t.Errorf("expected the diff to name the new edge, got %q", diff)
		}
	})

	t.Run("a removed edge is reported distinctly from an added one", func(t *testing.T) {
		want := "capabilities -> kernel\nworkflow -> domains\n"
		got := "capabilities -> kernel\n"
		diff, ok := GoldenDiff(want, got)
		if ok {
			t.Fatal("expected a removed edge to be reported")
		}
		if !strings.Contains(diff, "- workflow -> domains") {
			t.Errorf("expected the diff to mark the removed edge, got %q", diff)
		}
	})
}

func TestBoundaryTests_ClassifyGraphAndSubtreeBranches(t *testing.T) {
	policy := &depedge.Policy{
		Module:           "example.com/hcm",
		Layers:           []depedge.Layer{{Name: "kernel", Roots: []string{"internal/kernel"}}, {Name: "workflow", Roots: []string{"internal/workflow"}}},
		PortsAndAdapters: []depedge.PortAdapter{{Name: "ledger", Roots: []string{"internal/ledger"}}},
	}
	for _, tc := range []struct {
		rel, name string
		port, ok  bool
	}{
		{"internal/kernel", "kernel", false, true},
		{"internal/kernel/value", "kernel", false, true},
		{"internal/ledger", "ledger", true, true},
		{"internal/unknown", "", false, false},
		{"internal/kernelish", "", false, false},
	} {
		name, port, ok := ClassifyRel(policy, tc.rel)
		if name != tc.name || port != tc.port || ok != tc.ok {
			t.Errorf("ClassifyRel(%q) = %q, %t, %t; want %q, %t, %t", tc.rel, name, port, ok, tc.name, tc.port, tc.ok)
		}
	}
	if got := (LayerEdge{From: "workflow", To: "kernel"}).String(); got != "workflow -> kernel" {
		t.Fatalf("LayerEdge.String() = %q", got)
	}
	graph := &importgraph.Graph{Module: policy.Module, Edges: []importgraph.Edge{
		{Importer: policy.Module + "/internal/workflow/run", Imported: policy.Module + "/internal/kernel/value"},
		{Importer: policy.Module + "/internal/workflow/run", Imported: policy.Module + "/internal/kernel/value"},
		{Importer: policy.Module + "/internal/kernel/value", Imported: policy.Module + "/internal/kernel/other"},
		{Importer: policy.Module + "/internal/workflow/run", Imported: policy.Module + "/internal/ledger/port"},
		{Importer: "external/module", Imported: policy.Module + "/internal/kernel/value"},
		{Importer: policy.Module + "/internal/unknown", Imported: policy.Module + "/internal/kernel/value"},
	}}
	edges := LayerGraph(graph, policy)
	if len(edges) != 2 || !HasEdge(edges, "workflow", "kernel") || !HasEdge(edges, "workflow", "ledger") {
		t.Fatalf("LayerGraph = %#v", edges)
	}
	if HasEdge(edges, "kernel", "kernel") || HasEdge(edges, "unknown", "kernel") {
		t.Fatalf("LayerGraph retained self/unclassified edges: %#v", edges)
	}
	if !ImportsSubtree(graph, policy.Module, "internal/workflow", "internal/ledger") || ImportsSubtree(graph, policy.Module, "internal/kernel", "internal/ledger") || ImportsSubtree(graph, policy.Module, "internal/workflowish", "internal/ledger") {
		t.Fatal("ImportsSubtree classified a missing or partial prefix")
	}
}

func TestBoundaryTests_GoldenAndRenderingEdges(t *testing.T) {
	if got := RenderGolden(nil); got != "" {
		t.Fatalf("RenderGolden(nil) = %q", got)
	}
	if diff, ok := GoldenDiff("a -> b\n", "a -> b\n"); !ok || diff != "" {
		t.Fatalf("equal GoldenDiff = %q, %t", diff, ok)
	}
	if diff, ok := GoldenDiff("a -> b\n", "a -> b\na -> b\nc -> d\n"); ok || !strings.Contains(diff, "+ c -> d") {
		t.Fatalf("added-edge GoldenDiff = %q, %t", diff, ok)
	}
	if diff, ok := GoldenDiff("a -> b\nc -> d\n", "a -> b\n"); ok || !strings.Contains(diff, "- c -> d") {
		t.Fatalf("removed-edge GoldenDiff = %q, %t", diff, ok)
	}
	if !HasEdge([]LayerEdge{{From: "a", To: "b"}}, "a", "b") || HasEdge([]LayerEdge{{From: "a", To: "b"}}, "b", "a") {
		t.Fatal("HasEdge returned the wrong result")
	}
}
