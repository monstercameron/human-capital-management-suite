package archrules_test

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/archrules"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

func ceremonyRules(t *testing.T) archrules.CeremonyRules {
	t.Helper()
	cfg := loadArchConfig(t)
	if len(cfg.Ceremony.PackageSuffixes) == 0 {
		t.Fatal("ARCH-GO-027 ceremony manifest has no package suffixes")
	}
	return cfg.Ceremony
}

// TestArchitectureCeremonyRejectsInterfacePerStructAndEmptyLayers is the
// ARCH-GO-027 primary test. It exercises each RED shape with source fixtures
// and confirms a used interface, a single package and a package with semantic
// logic are not reported.
func TestArchitectureCeremonyRejectsInterfacePerStructAndEmptyLayers(t *testing.T) {
	rules := ceremonyRules(t)
	redInterface := archrules.SourcePackage{ImportPath: "internal/example", Source: `package example
type Reader interface { Read() error }
type reader struct{}
func (reader) Read() error { return nil }
`}
	if got := archrules.CheckCeremonySources([]archrules.SourcePackage{redInterface}, rules); len(got) != 1 || got[0].Kind != "one-method-interface-no-consumer" {
		t.Fatalf("one-method interface ceremony findings = %v, want one interface finding", got)
	}

	usedInterface := archrules.SourcePackage{ImportPath: "internal/example", Source: `package example
type Reader interface { Read() error }
type reader struct{}
func (reader) Read() error { return nil }
func Use(r Reader) error { return r.Read() }
`}
	if got := archrules.CheckCeremonySources([]archrules.SourcePackage{usedInterface}, rules); len(got) != 0 {
		t.Fatalf("consumer-owned interface was reported as ceremony: %v", got)
	}

	incompatibleImplementation := archrules.SourcePackage{ImportPath: "internal/example", Source: `package example
type Reader interface { Read() error }
type reader struct{}
func (reader) Read() int { return 0 }
`}
	if got := archrules.CheckCeremonySources([]archrules.SourcePackage{incompatibleImplementation}, rules); len(got) != 0 {
		t.Fatalf("same-named but incompatible method was counted as an implementation: %v", got)
	}

	redLayer := archrules.SourcePackage{ImportPath: "internal/foo_service", Source: `package foo_service
import "example.com/dependency"
func New(v int) int { return dependency.New(v) }
`}
	if got := archrules.CheckCeremonySources([]archrules.SourcePackage{redLayer}, rules); len(got) != 1 || got[0].Kind != "forwarding-only-layer" {
		t.Fatalf("forwarding-only layer findings = %v, want one layer finding", got)
	}

	redSuffixes := []archrules.SourcePackage{
		{ImportPath: "internal/people_service", Source: "package people_service"},
		{ImportPath: "internal/people_repository", Source: "package people_repository"},
	}
	findings := archrules.CheckCeremonySources(redSuffixes, rules)
	if len(findings) != 1 || findings[0].Kind != "parallel-ceremonial-packages" {
		t.Fatalf("parallel suffix findings = %v, want one family finding", findings)
	}

	greenLayer := archrules.SourcePackage{ImportPath: "internal/semantic", Source: `package semantic
import "example.com/dependency"
func New(v int) int { x := dependency.New(v); return x + 1 }
`}
	if got := archrules.CheckCeremonySources([]archrules.SourcePackage{greenLayer}, rules); len(got) != 0 {
		t.Fatalf("semantic layer was reported as ceremony: %v", got)
	}

	forwardingWithState := archrules.SourcePackage{ImportPath: "internal/foo_service", Source: `package foo_service
import "example.com/dependency"
var Default = dependency.Default
func New(v int) int { return dependency.New(v) }
`}
	if got := archrules.CheckCeremonySources([]archrules.SourcePackage{forwardingWithState}, rules); len(got) != 0 {
		t.Fatalf("forwarding package with a value declaration was reported as forwarding-only: %v", got)
	}
}

// TestTodo_ARCH_GO_027_Golden pins the reviewed suffix family and violation
// vocabulary so a future broadening of this heuristic is visible in review.
func TestTodo_ARCH_GO_027_Golden(t *testing.T) {
	rules := ceremonyRules(t)
	want := []string{"*_service", "*_repository", "*_domain", "*_model", "*_impl"}
	if strings.Join(rules.PackageSuffixes, "|") != strings.Join(want, "|") {
		t.Fatalf("ceremony package suffixes = %v, want %v", rules.PackageSuffixes, want)
	}
	// Four reviewed, expiring exceptions as of 2026-09-03: the conformance
	// runner seam, the wasm qualification entrypoint, the ledger digester
	// port and the cmd/hcmctl process entrypoint (ADMIN-001).
	if len(rules.Exceptions) != 4 {
		t.Fatalf("current ceremony manifest has %d exceptions, want exactly 4 bounded exceptions", len(rules.Exceptions))
	}
}

// TestTodo_ARCH_GO_027_Race runs the pure detector concurrently over identical
// inputs; no package-global mutable state may influence the result.
func TestTodo_ARCH_GO_027_Race(t *testing.T) {
	rules := ceremonyRules(t)
	packages := []archrules.SourcePackage{{ImportPath: "internal/example", Source: `package example
type Reader interface { Read() error }
type reader struct{}
func (reader) Read() error { return nil }
`}}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := archrules.CheckCeremonySources(packages, rules)
			if len(got) != 1 || got[0].Kind != "one-method-interface-no-consumer" {
				t.Errorf("concurrent detector result = %v", got)
			}
		}()
	}
	wg.Wait()
}

// TestTodo_ARCH_GO_027_Conformance checks complete reviewed exceptions: an
// exception with owner/rationale/evidence and a future expiry may waive only
// its exact finding; missing evidence or an expired waiver may not.
func TestTodo_ARCH_GO_027_Conformance(t *testing.T) {
	base := archrules.SourcePackage{ImportPath: "internal/example", Source: `package example
type Reader interface { Read() error }
type reader struct{}
func (reader) Read() error { return nil }
`}
	asOf := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	rules := archrules.CeremonyRules{Exceptions: []archrules.CeremonyException{{
		Kind: "one-method-interface-no-consumer", Package: "internal/example", Owner: "platform-foundation",
		Rationale: "temporary adapter boundary while the consumer is extracted", Evidence: "test://ARCH-GO-027",
		Expiry: "2026-12-31",
	}}}
	if got := archrules.CheckCeremonyWithPolicy([]archrules.SourcePackage{base}, rules, asOf); len(got) != 0 {
		t.Fatalf("complete active exception did not waive its exact finding: %v", got)
	}
	rules.Exceptions[0].Evidence = ""
	if got := archrules.CheckCeremonyWithPolicy([]archrules.SourcePackage{base}, rules, asOf); len(got) != 1 {
		t.Fatalf("exception without evidence waived finding: %v", got)
	}
	rules.Exceptions[0].Evidence = "test://ARCH-GO-027"
	rules.Exceptions[0].Expiry = "2026-09-02"
	if got := archrules.CheckCeremonyWithPolicy([]archrules.SourcePackage{base}, rules, asOf); len(got) != 1 {
		t.Fatalf("expired exception waived finding: %v", got)
	}

	// The actual repository is the conformance subject: either it has no raw
	// ceremony findings or every finding is explicitly reviewed in the manifest.
	cfg := loadArchConfig(t)
	root := repopath.RootDir()
	raw, err := archrules.ScanCeremony(root, cfg.Ceremony)
	if err != nil {
		t.Fatalf("scan repository ceremony: %v", err)
	}
	actual, err := archrules.ScanCeremonyWithPolicy(root, cfg.Ceremony, asOf)
	if err != nil {
		t.Fatalf("scan repository ceremony with policy: %v", err)
	}
	if len(actual) != 0 {
		t.Fatalf("current tree has unreviewed ceremony findings: %v", actual)
	}
	for _, finding := range raw {
		if filepath.IsAbs(finding.Package) {
			t.Errorf("finding package should remain module-relative/import-relative: %s", finding.Package)
		}
	}
}
