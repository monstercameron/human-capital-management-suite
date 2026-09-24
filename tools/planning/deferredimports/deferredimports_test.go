package deferredimports

import "testing"

func TestPhaseOneImportsRejectDeferredSubsystems(t *testing.T) {
	t.Run("transitive production dependency is rejected", func(t *testing.T) {
		graph := Graph{
			Roots: []string{"example/cmd/service"},
			Packages: []Package{
				{ImportPath: "example/cmd/service", Imports: []string{"example/internal/connector"}},
				{ImportPath: "example/internal/connector", Imports: []string{"github.com/segmentio/kafka-go"}},
				{ImportPath: "github.com/segmentio/kafka-go"},
			},
		}
		violations := CheckGraph(graph)
		if len(violations) != 1 || violations[0].Subsystem != "Kafka" || violations[0].Importer != "example/internal/connector" {
			t.Fatalf("expected the transitive Kafka edge to be reported, got %v", violations)
		}
	})

	t.Run("test and unrelated unrooted packages are ignored", func(t *testing.T) {
		graph := Graph{
			Roots: []string{"example/cmd/service"},
			Packages: []Package{
				{ImportPath: "example/cmd/service", Imports: []string{"fmt"}},
				{ImportPath: "fmt"},
				{ImportPath: "example/internal/testhelper", Imports: []string{"github.com/segmentio/kafka-go"}},
				{ImportPath: "github.com/segmentio/kafka-go"},
			},
		}
		if got := CheckGraph(graph); len(got) != 0 {
			t.Fatalf("unreachable test-only dependency entered production graph: %v", got)
		}
	})

	t.Run("all declared deferred families are recognized", func(t *testing.T) {
		for _, scope := range DeferredScopes() {
			if scope.GateA != "OUT OF PHASE" || scope.GateB != "OUT OF PHASE" || scope.MatrixRow == "" || scope.Source == "" {
				t.Errorf("incomplete deferred scope metadata: %+v", scope)
			}
			for _, prefix := range scope.Prefixes {
				matched, ok := MatchForbidden(prefix + "/v1")
				if !ok || matched.Subsystem != scope.Subsystem {
					t.Errorf("MatchForbidden(%q) = %+v, %v; want %q", prefix+"/v1", matched, ok, scope.Subsystem)
				}
			}
		}
	})

	t.Run("unrelated or substring-only paths are not flagged", func(t *testing.T) {
		for _, importPath := range []string{
			"github.com/google/uuid",
			"example.org/notsegmentio/kafka-go",
			"github.com/example/opensearch-project-fork/opensearch-go",
		} {
			if scope, ok := MatchForbidden(importPath); ok {
				t.Errorf("MatchForbidden(%q) unexpectedly matched %+v", importPath, scope)
			}
		}
	})

	t.Run("cycle-safe graph traversal reports deterministic edges", func(t *testing.T) {
		graph := Graph{
			Roots: []string{"example/cmd/a"},
			Packages: []Package{
				{ImportPath: "example/cmd/a", Imports: []string{"example/internal/b"}},
				{ImportPath: "example/internal/b", Imports: []string{"example/cmd/a", "github.com/ClickHouse/clickhouse-go/v2"}},
			},
		}
		got := CheckGraph(graph)
		if len(got) != 1 || got[0].Subsystem != "ClickHouse" {
			t.Fatalf("cycle traversal returned %v", got)
		}
	})
}

func TestTodo_GOV_009_Property(t *testing.T) {
	for _, scope := range DeferredScopes() {
		for _, prefix := range scope.Prefixes {
			for _, suffix := range []string{"", "/v2", "/client/reader"} {
				got, ok := MatchForbidden(prefix + suffix)
				if !ok || got.Subsystem != scope.Subsystem {
					t.Fatalf("MatchForbidden(%q) = %+v, %v; want %q", prefix+suffix, got, ok, scope.Subsystem)
				}
			}
		}
	}
}

func TestTodo_GOV_009_Golden(t *testing.T) {
	v := Violation{Importer: "example/internal/connector", Import: "github.com/segmentio/kafka-go", Subsystem: "Kafka"}
	const want = `example/internal/connector: imports "github.com/segmentio/kafka-go" (Kafka is OUT OF PHASE for Gate A and Gate B)`
	if got := v.String(); got != want {
		t.Errorf("violation message changed:\n got:  %s\n want: %s", got, want)
	}
}

func TestProductionRootsAreBuildTargets(t *testing.T) {
	for _, root := range ProductionRootPatterns() {
		if root == "" {
			t.Error("empty production root")
		}
	}
}
