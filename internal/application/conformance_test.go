package application

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

// TestTodo_ARCH_GO_020_Conformance is the ARCH-GO-020 CONFORMANCE test: the
// clauses of the todo restated as checks against the delivered root.
//
// Each subtest is one clause. They are deliberately checks on this package
// rather than on the tree - the tree-wide property is the PRIMARY test's job -
// because the clauses here are about what a composition root is allowed to be:
// no production-only branches, one construction path shared by the binary and
// the suite, a closed role set, and a graph it can describe.
func TestTodo_ARCH_GO_020_Conformance(t *testing.T) {
	t.Run("no production-only branches", func(t *testing.T) {
		// "test composition swaps adapters, clocks and providers explicitly
		// without production-only branches". The way that claim fails in
		// practice is a root that knows it is under test: a testing import, a
		// build tag that swaps a file, or an environment variable that
		// selects a different wiring. None of the three may exist here.
		for name, file := range applicationProductionFiles(t) {
			for _, group := range file.Comments {
				for _, comment := range group.List {
					text := comment.Text
					if strings.HasPrefix(text, "//go:build") || strings.HasPrefix(text, "// +build") {
						t.Errorf("%s carries a build constraint (%s); a composition root that is a different "+
							"file under a different tag is two compositions", name, text)
					}
				}
			}
			for _, spec := range file.Imports {
				if spec.Path.Value == `"testing"` {
					t.Errorf("%s imports testing; the composed root would know it is under test", name)
				}
			}
			ast.Inspect(file, func(n ast.Node) bool {
				selector, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				ident, ok := selector.X.(*ast.Ident)
				if !ok || ident.Name != "os" {
					return true
				}
				switch selector.Sel.Name {
				case "Getenv", "LookupEnv":
					t.Errorf("%s reads the environment through os.%s; configuration reaches this root as a "+
						"validated value, not as ambient process state", name, selector.Sel.Name)
				}
				return true
			})
		}
	})

	t.Run("the default composition is the production one", func(t *testing.T) {
		// A seam whose default is a stub would make every test compose
		// something the binary never runs. With no Options at all, the store
		// this root builds has to be the real PostgreSQL adapter.
		store, err := composeStore(nil, stubServeConfig(), Options{})
		if err != nil {
			t.Fatalf("composeStore with no options: %v", err)
		}
		if _, ok := store.(*pgstore.Store); !ok {
			t.Errorf("the default store is %T, want the PostgreSQL adapter", store)
		}
		verifier, err := composeVerifier(stubServeConfig(), Options{})
		if err != nil {
			t.Fatalf("composeVerifier with no options: %v", err)
		}
		if verifier == nil {
			t.Error("the default composition has no credential verifier")
		}
	})

	t.Run("every role composes and describes itself", func(t *testing.T) {
		for _, role := range Roles() {
			spec, err := SpecFor(role, nil)
			if err != nil {
				t.Errorf("SpecFor(%q): %v", role, err)
				continue
			}
			if spec.Build == nil || spec.Validate == nil {
				t.Errorf("role %q composes a Spec with no build or no validation", role)
			}
			if spec.Role.Validate() != nil {
				t.Errorf("role %q maps to process role %q, which is not in the declared vocabulary", role, spec.Role)
			}
		}
	})

	t.Run("the composed graph is coherent and complete", func(t *testing.T) {
		composed, _, _ := composeStub(t, stubServeConfig())
		graph := composed.Graph()
		if err := graph.Validate(); err != nil {
			t.Fatalf("the composed graph: %v", err)
		}
		// The clause the todo spells out: registries, governance, workflows,
		// engines, domains, ports and adapters, and worker roles. Every one
		// of those has to be a named node, so "what is this process made of"
		// is answerable without reading the wiring.
		required := map[string]string{
			ComponentIntentDefinitions:   KindRegistry,
			ComponentCapabilityRegistry:  KindRegistry,
			ComponentEvidenceSink:        KindRegistry,
			ComponentCapabilityGateway:   KindGovernance,
			ComponentExecutionAuthority:  KindGovernance,
			ComponentProposalExecutor:    KindWorkflow,
			ComponentWorkflowResolver:    KindWorkflow,
			ComponentJourneyEngine:       KindWorkflow,
			ComponentIntentService:       KindEngine,
			ComponentTrustedClock:        KindEngine,
			ComponentDomainInputs:        KindPort,
			ComponentWorkerFacts:         KindPort,
			ComponentTransactionHistory:  KindPort,
			ComponentWorkflowControlRead: KindPort,
			ComponentWorkItemQueueRead:   KindPort,
			ComponentIntentStore:         KindAdapter,
			ComponentCredentialVerifier:  KindAdapter,
			ComponentTelemetryProvider:   KindAdapter,
			ComponentIncumbentConnector:  KindAdapter,
			ComponentObservationStore:    KindAdapter,
			ComponentGRPCSurface:         KindTransport,
			ComponentHTTPEdge:            KindTransport,
			ComponentWorkloadGRPC:        KindWorkload,
			ComponentWorkloadHTTP:        KindWorkload,
			ComponentShutdownHTTP:        KindShutdown,
			ComponentShutdownGRPC:        KindShutdown,
			ComponentShutdownTelemetry:   KindShutdown,
			ComponentConfig:              KindConfig,
		}
		for name, kind := range required {
			component, ok := graph.Component(name)
			if !ok {
				t.Errorf("the composed graph names no %q", name)
				continue
			}
			if component.Kind != kind {
				t.Errorf("component %q is kind %q, want %q", name, component.Kind, kind)
			}
		}
	})

	t.Run("two compositions share nothing", func(t *testing.T) {
		// A composition root that handed two processes the same registry
		// would be the global registry this todo forbids, just spelled
		// differently.
		first, _, _ := composeStub(t, stubServeConfig())
		second, _, _ := composeStub(t, stubServeConfig())
		if first.Cell() == second.Cell() {
			t.Fatal("two compositions share one cell")
		}
		if first.Cell().Evidence == second.Cell().Evidence {
			t.Error("two compositions share one evidence sink")
		}
		if first.Cell().Capabilities == second.Cell().Capabilities {
			t.Error("two compositions share one capability registry")
		}
		if first.Cell().Definitions == second.Cell().Definitions {
			t.Error("two compositions share one intent definition registry")
		}
		if first.GRPCAddr() == second.GRPCAddr() {
			t.Error("two compositions bound the same address")
		}
	})

	t.Run("the lifecycle is the bootstrap runtime", func(t *testing.T) {
		composed, _, _ := composeStub(t, stubServeConfig())
		// Satisfaction is proven by this assignment (a compile-time
		// assertion); a runtime nil comparison could never fail since the
		// composed value is a struct.
		var lifecycle Lifecycle = composed
		runtime := composed.Runtime()
		if len(runtime.Workloads) == 0 || len(runtime.Shutdown) == 0 {
			t.Fatal("the bootstrap view of the composition is empty")
		}
		// The deployed process runs runtime.Workloads through bootstrap and
		// a test drives the same composition through Start/Stop. What makes
		// that one composition rather than two is that the lists are the
		// same: same names, same order, same count.
		wantWorkloads := []string{workloadNameGRPC, workloadNameHTTP}
		for i, want := range wantWorkloads {
			if runtime.Workloads[i].Name != want {
				t.Errorf("bootstrap workload %d = %q, want %q", i, runtime.Workloads[i].Name, want)
			}
		}
		wantShutdown := []string{shutdownNameHTTP, shutdownNameGRPC, shutdownNameChat, shutdownNameTelemetry}
		for i, want := range wantShutdown {
			if runtime.Shutdown[i].Name != want {
				t.Errorf("bootstrap shutdown step %d = %q, want %q", i, runtime.Shutdown[i].Name, want)
			}
		}
		// Stopping a composition that was never started still runs the
		// ordered sequence and releases what it bound: a process that failed
		// between composition and start must not hold its ports.
		if err := lifecycle.Stop(context.Background()); err != nil {
			t.Fatalf("Stop on a composed but unstarted application: %v", err)
		}
	})
}

// applicationProductionFiles parses this package's own non-test sources.
func applicationProductionFiles(t *testing.T) map[string]*ast.File {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	out := map[string]*ast.File{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments|parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		out[name] = file
	}
	if len(out) == 0 {
		t.Fatal("this package has no production files; the scan would pass vacuously")
	}
	return out
}
