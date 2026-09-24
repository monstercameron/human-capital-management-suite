// Package deferredimports checks the production binary dependency graph for
// mandatory dependencies on Phase 1 deferred subsystems (GOV-009).
package deferredimports

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ProductionRoots mirrors the binaries emitted by scripts/build.sh. `go list
// -deps` follows only ordinary imports, so test-only and development packages
// are not included in this graph.
var productionRoots = [...]string{
	"./cmd/hcmnext", "./cmd/scheduler", "./cmd/worker",
	"./cmd/migrate", "./cmd/hcmctl", "./cmd/projector",
}

// Scope is one dependency family declared OUT OF PHASE by the Phase 1 depth
// matrix or the execution plan's explicit non-goals. MatrixRow and Source
// retain the planning authority for each path family; both Gate depths are
// explicit because a dependency is forbidden only when it is outside both
// Phase 1 gates.
type Scope struct {
	Subsystem string
	MatrixRow string
	Source    string
	GateA     string
	GateB     string
	Prefixes  []string
}

var scopeRegistry = []Scope{
	{Subsystem: "Kafka", MatrixRow: "Search/OLAP/vector/Kafka/full billing", Source: "planning/plan.md#phase-1-implementation-depth-matrix; planning/execution-plan.md#explicit-non-goals", GateA: "OUT OF PHASE", GateB: "OUT OF PHASE", Prefixes: []string{"github.com/segmentio/kafka-go", "github.com/confluentinc/confluent-kafka-go", "github.com/IBM/sarama", "github.com/Shopify/sarama"}},
	{Subsystem: "ClickHouse", MatrixRow: "Search/OLAP/vector/Kafka/full billing", Source: "planning/plan.md#phase-1-implementation-depth-matrix; planning/execution-plan.md#explicit-non-goals", GateA: "OUT OF PHASE", GateB: "OUT OF PHASE", Prefixes: []string{"github.com/ClickHouse/clickhouse-go"}},
	{Subsystem: "OpenSearch", MatrixRow: "Search/OLAP/vector/Kafka/full billing", Source: "planning/plan.md#phase-1-implementation-depth-matrix; planning/execution-plan.md#explicit-non-goals", GateA: "OUT OF PHASE", GateB: "OUT OF PHASE", Prefixes: []string{"github.com/opensearch-project/opensearch-go", "github.com/elastic/go-elasticsearch"}},
	{Subsystem: "Vector infrastructure", MatrixRow: "Search/OLAP/vector/Kafka/full billing", Source: "planning/plan.md#phase-1-implementation-depth-matrix; planning/execution-plan.md#explicit-non-goals", GateA: "OUT OF PHASE", GateB: "OUT OF PHASE", Prefixes: []string{"github.com/pgvector/pgvector-go", "github.com/milvus-io/milvus-sdk-go", "github.com/weaviate/weaviate-go-client", "github.com/pinecone-io/go-pinecone", "github.com/qdrant/go-client"}},
	{Subsystem: "Full billing", MatrixRow: "Search/OLAP/vector/Kafka/full billing", Source: "planning/plan.md#phase-1-implementation-depth-matrix; planning/execution-plan.md#explicit-non-goals", GateA: "OUT OF PHASE", GateB: "OUT OF PHASE", Prefixes: []string{"github.com/monstercameron/human-capital-management-suite/internal/billing/full"}},
	{Subsystem: "Payroll calculation", MatrixRow: "Full payroll", Source: "planning/execution-plan.md#explicit-non-goals; planning/plan.md#phase-1-changeops-overlay", GateA: "OUT OF PHASE", GateB: "OUT OF PHASE", Prefixes: []string{"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll/calculation", "github.com/monstercameron/human-capital-management-suite/internal/payroll/calculation"}},
	{Subsystem: "Omnichannel", MatrixRow: "Cases/e-signature/omnichannel", Source: "planning/plan.md#phase-1-implementation-depth-matrix; planning/execution-plan.md#explicit-non-goals", GateA: "OUT OF PHASE", GateB: "OUT OF PHASE", Prefixes: []string{"github.com/monstercameron/human-capital-management-suite/internal/omnichannel", "github.com/monstercameron/human-capital-management-suite/internal/messaging/omnichannel"}},
}

// ProductionRootPatterns returns the release binaries' package patterns.
func ProductionRootPatterns() []string { return append([]string(nil), productionRoots[:]...) }

// DeferredScopes returns a copy of the scoped registry so callers cannot
// change the policy used by later checks.
func DeferredScopes() []Scope {
	out := make([]Scope, len(scopeRegistry))
	for i, scope := range scopeRegistry {
		out[i] = scope
		out[i].Prefixes = append([]string(nil), scope.Prefixes...)
	}
	return out
}

// MatchForbidden reports the declared deferred subsystem for an exact import
// path prefix. The package path segment boundary prevents unrelated names
// that merely contain a fragment from being rejected.
func MatchForbidden(importPath string) (Scope, bool) {
	for _, scope := range scopeRegistry {
		if scope.GateA != "OUT OF PHASE" || scope.GateB != "OUT OF PHASE" {
			continue
		}
		for _, prefix := range scope.Prefixes {
			if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") || strings.HasSuffix(prefix, "/") && strings.HasPrefix(importPath, prefix) {
				return scope, true
			}
		}
	}
	return Scope{}, false
}

// Package is the subset of `go list -json` needed to walk production imports.
type Package struct {
	ImportPath string
	Imports    []string
	Dir        string
}

// Graph is the ordinary import closure of the declared production binaries.
type Graph struct {
	Roots    []string
	Packages []Package
}

// Violation identifies the importing package and forbidden dependency.
type Violation struct {
	Importer  string
	Import    string
	Subsystem string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: imports %q (%s is OUT OF PHASE for Gate A and Gate B)", v.Importer, v.Import, v.Subsystem)
}

// CheckGraph traverses only nodes reachable from the supplied production
// roots and reports every edge that enters a declared deferred subsystem.
func CheckGraph(graph Graph) []Violation {
	packages := make(map[string]Package, len(graph.Packages))
	for _, pkg := range graph.Packages {
		packages[pkg.ImportPath] = pkg
	}
	var violations []Violation
	visited := make(map[string]bool)
	var walk func(string)
	walk = func(importer string) {
		if visited[importer] {
			return
		}
		visited[importer] = true
		pkg, ok := packages[importer]
		if !ok {
			return
		}
		imports := append([]string(nil), pkg.Imports...)
		sort.Strings(imports)
		for _, imported := range imports {
			if scope, forbidden := MatchForbidden(imported); forbidden {
				violations = append(violations, Violation{Importer: importer, Import: imported, Subsystem: scope.Subsystem})
			}
			walk(imported)
		}
	}
	roots := append([]string(nil), graph.Roots...)
	sort.Strings(roots)
	for _, root := range roots {
		walk(root)
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Importer != violations[j].Importer {
			return violations[i].Importer < violations[j].Importer
		}
		return violations[i].Import < violations[j].Import
	})
	return violations
}

// ScanProductionGraph invokes `go list -deps -json` for the release binaries
// declared in ProductionRoots, then checks the resulting transitive graph.
func ScanProductionGraph(ctx context.Context, root string) ([]Violation, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	args := []string{"list", "-deps", "-json"}
	args = append(args, productionRoots[:]...)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list production dependency graph: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	decoder := json.NewDecoder(&stdout)
	graph := Graph{}
	for {
		var pkg Package
		if err := decoder.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode go list package: %w", err)
		}
		graph.Packages = append(graph.Packages, pkg)
		for _, rootPattern := range productionRoots {
			rootDir := filepath.Join(root, strings.TrimPrefix(rootPattern, "./"))
			if pkg.Dir == rootDir {
				graph.Roots = append(graph.Roots, pkg.ImportPath)
			}
		}
	}
	if len(graph.Roots) != len(productionRoots) {
		return nil, fmt.Errorf("go list returned %d production roots, want %d", len(graph.Roots), len(productionRoots))
	}
	return CheckGraph(graph), nil
}
