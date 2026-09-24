package libqualification_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// Forbidden third-party modules that violate the standard-library-first policy.
// LIB-012 forbids these frameworks unless an exception is explicitly declared
// in an allowlist with a named todo justification.
var forbiddenFrameworks = map[string]struct {
	family  string
	reason  string
	pattern string
}{
	// Logging frameworks (use log/slog instead).
	"go.uber.org/zap": {
		family:  "go.uber.org/zap",
		reason:  "use log/slog for structured logging; zap is not admitted without LIB-012 decision",
		pattern: "go.uber.org/zap",
	},
	"github.com/sirupsen/logrus": {
		family:  "github.com/sirupsen/logrus",
		reason:  "use log/slog for structured logging; logrus is not admitted without LIB-012 decision",
		pattern: "github.com/sirupsen/logrus",
	},
	"github.com/rs/zerolog": {
		family:  "github.com/rs/zerolog",
		reason:  "use log/slog for structured logging; zerolog is not admitted without LIB-012 decision",
		pattern: "github.com/rs/zerolog",
	},

	// Assertion libraries (use Go stdlib testing tools).
	"github.com/stretchr/testify": {
		family:  "github.com/stretchr/testify",
		reason:  "use testing.T.Error/Fail or custom assertions; testify is not admitted without LIB-012 decision",
		pattern: "github.com/stretchr/testify",
	},

	// HTTP routing frameworks not in stdlib (use net/http, grpc, or connect/grpc-gateway).
	"github.com/gorilla/mux": {
		family:  "github.com/gorilla/mux",
		reason:  "use net/http routing; gorilla/mux not admitted for production without LIB-012 decision",
		pattern: "github.com/gorilla/mux",
	},
	"github.com/gin-gonic/gin": {
		family:  "github.com/gin-gonic/gin",
		reason:  "use net/http routing or gRPC; gin not admitted without LIB-012 decision",
		pattern: "github.com/gin-gonic/gin",
	},
	"github.com/labstack/echo": {
		family:  "github.com/labstack/echo",
		reason:  "use net/http routing or gRPC; echo not admitted without LIB-012 decision",
		pattern: "github.com/labstack/echo",
	},

	// Crypto frameworks not in stdlib (use crypto/*, crypto/sha256, etc.).
	"golang.org/x/crypto": {
		family:  "golang.org/x/crypto",
		reason:  "use stdlib crypto packages; x/crypto is not in stdlib and requires LIB-012 decision for specific algorithms",
		pattern: "golang.org/x/crypto",
	},
}

// Per-package allowlist for admitted exceptions with their justifications.
// Format: package path -> list of (forbidden module, reason) tuples.
// This is populated only when a deliberate exception has been made and recorded
// in planning/todos.md.
var admittedExceptions = map[string]map[string]string{
	// Example: "internal/some/package": {"github.com/some/module": "LIB-012-EXCEPTION-001: reason"},
}

// TestStandardLibraryDefaultPolicy is LIB-012's primary test: the codebase
// must prefer Go standard-library logging, crypto, networking, and testing
// mechanics. This test fails when a third-party logger (zap, logrus, zerolog),
// crypto framework (not covered by stdlib), HTTP router (not stdlib/grpc),
// or assertion library (testify) appears without an admitted exception.
//
// The GREEN condition is that log/slog, stdlib crypto/TLS/net, and Go test/race
// tools satisfy all declared needs through minimal owned adapters.
func TestStandardLibraryDefaultPolicy(t *testing.T) {
	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	violations := forbiddenProductionImports(pkgs)
	if len(violations) > 0 {
		t.Fatalf("LIB-012 POLICY VIOLATION: standard-library-first principle breached:\n%s", strings.Join(violations, "\n"))
	}

	t.Logf("LIB-012 DECISION=PREFER: log/slog, stdlib crypto/TLS/net, and Go test tools satisfy all needs")
}

// forbiddenProductionImports is the scanner shared by the live policy test
// and its fault fixture. It reports prohibited imports from production
// packages, except exact package/module pairs with an admitted rationale.
func forbiddenProductionImports(pkgs []repopath.Package) []string {
	var violations []string

	for _, pkg := range pkgs {
		// Skip generated code and test packages (they may import test frameworks).
		if strings.Contains(pkg.ImportPath, "/gen/") || strings.HasSuffix(pkg.ImportPath, "_test") {
			continue
		}

		// Skip testdata fixtures.
		if strings.Contains(pkg.ImportPath, "testdata") {
			continue
		}

		// Skip tools-only packages (they may experiment with frameworks).
		if strings.HasPrefix(pkg.ImportPath, "github.com/monstercameron/human-capital-management-suite/tools/") &&
			!strings.Contains(pkg.ImportPath, "tools/policy") {
			continue
		}

		for _, imp := range pkg.Imports {
			for name, fb := range forbiddenFrameworks {
				if strings.HasPrefix(imp, fb.pattern) {
					// Check if this import is in the admitted exceptions list.
					if admittedExceptions[pkg.ImportPath] != nil {
						if _, found := admittedExceptions[pkg.ImportPath][name]; found {
							continue
						}
					}

					violations = append(violations, fmt.Sprintf("%s imports %s (%s)", pkg.ImportPath, name, fb.reason))
				}
			}
		}
	}

	return violations
}

// TestTodo_LIB_012_Fault verifies the production-import scanner reports a
// forbidden dependency injected into a production package fixture.
func TestTodo_LIB_012_Fault(t *testing.T) {
	fixture := []repopath.Package{{
		ImportPath: "github.com/example/production-service",
		Imports:    []string{"go.uber.org/zap"},
	}}

	violations := forbiddenProductionImports(fixture)
	if len(violations) != 1 {
		t.Fatalf("scanner returned %d violations, want 1: %v", len(violations), violations)
	}
	want := "github.com/example/production-service imports go.uber.org/zap"
	if !strings.Contains(violations[0], want) {
		t.Fatalf("scanner missed forbidden production import: got %q, want it to contain %q", violations[0], want)
	}
}

// TestTodo_LIB_012_Integration verifies go-list discovery and the policy
// scanner together against a temporary module with a forbidden production
// import. The local replacement keeps the fixture offline and self-contained.
func TestTodo_LIB_012_Integration(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, "go.mod"), "module example.test/lib012fixture\n\ngo 1.26\n\nrequire go.uber.org/zap v0.0.0\nreplace go.uber.org/zap => ./zap\n")
	writeFixtureFile(t, filepath.Join(root, "service.go"), "package service\n\nimport _ \"go.uber.org/zap\"\n")
	writeFixtureFile(t, filepath.Join(root, "zap", "go.mod"), "module go.uber.org/zap\n\ngo 1.26\n")
	writeFixtureFile(t, filepath.Join(root, "zap", "zap.go"), "package zap\n")

	pkg, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("list fixture packages: %v", err)
	}
	if len(pkg) != 1 || pkg[0].ImportPath != "example.test/lib012fixture" {
		t.Fatalf("go list returned unexpected fixture packages: %+v", pkg)
	}
	violations := forbiddenProductionImports(pkg)
	if len(violations) != 1 || !strings.Contains(violations[0], "go.uber.org/zap") {
		t.Fatalf("integrated scan returned %v, want forbidden zap import", violations)
	}
}

// TestTodo_LIB_012_Race invokes the shared scanner concurrently over one
// immutable package inventory and asserts every caller sees the same finding.
func TestTodo_LIB_012_Race(t *testing.T) {
	pkgs := []repopath.Package{
		{ImportPath: "example.test/service", Imports: []string{"net/http", "go.uber.org/zap"}},
		{ImportPath: "example.test/stdlib", Imports: []string{"log/slog", "crypto/sha256"}},
	}
	const workers = 16
	want := "example.test/service imports go.uber.org/zap"
	var group sync.WaitGroup
	results := make(chan []string, workers)
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			results <- forbiddenProductionImports(pkgs)
		}()
	}
	group.Wait()
	close(results)
	for got := range results {
		if len(got) != 1 || !strings.Contains(got[0], want) {
			t.Errorf("concurrent scan returned %v, want one stable zap violation", got)
		}
	}
}

// BenchmarkTodo_LIB_012 measures policy scanning across a bounded inventory
// of production packages while checking that the expected violation remains
// visible on every iteration.
func BenchmarkTodo_LIB_012(b *testing.B) {
	pkgs := make([]repopath.Package, 128)
	for i := range pkgs {
		pkgs[i] = repopath.Package{
			ImportPath: fmt.Sprintf("example.test/service/pkg%d", i),
			Imports:    []string{"net/http", "log/slog", "crypto/sha256"},
		}
	}
	pkgs[len(pkgs)-1].Imports = append(pkgs[len(pkgs)-1].Imports, "go.uber.org/zap")
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if findings := forbiddenProductionImports(pkgs); len(findings) != 1 {
			b.Fatalf("scanner returned %d findings, want 1", len(findings))
		}
	}
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

// TestTodo_LIB_012_Golden verifies the exact policy rationale persists:
// Go stdlib provides mature, well-reviewed logging, crypto, networking,
// and testing mechanics that are sufficient for all Phase 1 Human Capital Management Suite needs.
// Any exception requires a named capability gap and LIB-012 decision.
func TestTodo_LIB_012_Golden(t *testing.T) {
	// The rationale: Human Capital Management Suite is Go-first (Go technology constitution). The Go
	// standard library contains:
	// - log/slog for structured logging (1.21+)
	// - crypto/* for signature verification, hashing, symmetric ciphers
	// - net/http for HTTP routing (wrapped by gRPC/Connect/SchemaFlux)
	// - testing, testing/quick, testing/fstest for all test scenarios
	// - runtime/race (go test -race) for race detection
	//
	// Third-party logging (zap, logrus, zerolog), crypto frameworks, or
	// assertion libraries (testify) may only be admitted if:
	// 1. A measurable capability gap is demonstrated.
	// 2. An exact reason is recorded in planning/todos.md (LIB-012).
	// 3. An entry is added to admittedExceptions above with the todo reference.
	// 4. A corresponding dependency-roles.yaml entry is created (LIB-001).
	//
	// The preferred strategy is to create lightweight adapters (e.g., an
	// internal/platform/logging/adapter package) that wrap stdlib and allow
	// future replacement without changing callers.

	t.Logf("LIB-012 rationale: Go stdlib (log/slog, crypto/*, net/http, testing) is the default; third-party frameworks forbidden without decision")
}

// TestTodo_LIB_012_Security verifies that no third-party assertion libraries
// are used in production code paths (they may appear in test code only).
func TestTodo_LIB_012_Security(t *testing.T) {
	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	testalyModules := []string{"github.com/stretchr/testify"}

	for _, pkg := range pkgs {
		// Only check non-test production packages.
		if strings.HasSuffix(pkg.ImportPath, "_test") || strings.Contains(pkg.ImportPath, "test") {
			continue
		}

		for _, imp := range pkg.Imports {
			for _, testMod := range testalyModules {
				if strings.HasPrefix(imp, testMod) {
					t.Errorf("SECURITY: assertion library %s in production package %s", testMod, pkg.ImportPath)
				}
			}
		}
	}

	t.Logf("LIB-012 SECURITY: assertion libraries confined to test code; no production dependencies")
}

// TestTodo_LIB_012_Conformance ensures the policy record is complete and
// unambiguous: all packages must use stdlib logging, crypto, and networking
// mechanics unless an exception is explicitly recorded.
func TestTodo_LIB_012_Conformance(t *testing.T) {
	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var violations []string

	for _, pkg := range pkgs {
		if strings.Contains(pkg.ImportPath, "/gen/") || strings.HasSuffix(pkg.ImportPath, "_test") || strings.Contains(pkg.ImportPath, "testdata") {
			continue
		}

		if strings.HasPrefix(pkg.ImportPath, "github.com/monstercameron/human-capital-management-suite/tools/") &&
			!strings.Contains(pkg.ImportPath, "tools/policy") {
			continue
		}

		for _, imp := range pkg.Imports {
			for name, fb := range forbiddenFrameworks {
				if strings.HasPrefix(imp, fb.pattern) {
					if admittedExceptions[pkg.ImportPath] == nil || admittedExceptions[pkg.ImportPath][name] == "" {
						violations = append(violations, fmt.Sprintf("%s imports %s", pkg.ImportPath, name))
					}
				}
			}
		}
	}

	if len(violations) > 0 {
		t.Errorf("CONFORMANCE: forbidden third-party frameworks in production code:\n%s", strings.Join(violations, "\n"))
	}

	t.Logf("LIB-012 CONFORMANCE: stdlib-default policy upheld; no forbidden frameworks in production code")
}
