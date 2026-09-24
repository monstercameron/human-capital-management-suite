package archrules_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/archrules"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

const adapterOwnInterfaceSource = `package fakepostgres

// Querier is an adapter authoring its own port; RED fixture. A port belongs
// beside its consumer, not its adapter.
type Querier interface {
	Query(sql string) error
}

type Store struct{}
`

const adapterImplementsOnlySource = `package fakepostgres

// Store implements a port declared elsewhere (no local interface type);
// GREEN fixture.
type Store struct{}

func (s *Store) Query(sql string) error { return nil }
`

// TestPortOwnershipRejectsCentralRepositoryAndProviderInterfaces is the
// ARCH-GO-013 primary test: no internal/repository package may exist, and a
// package matching an adapter_markers glob must not itself declare an
// exported interface type -- a port belongs to its consumer's package; an
// adapter only implements one.
func TestPortOwnershipRejectsCentralRepositoryAndProviderInterfaces(t *testing.T) {
	cfg := loadArchConfig(t)
	po := cfg.PortOwnership

	t.Run("no central repository package", func(t *testing.T) {
		cases := []struct {
			path  string
			wantV bool
		}{
			{"internal/repository", true},
			{"internal/repository/people", true},
			{"internal/data", false},
			{"internal/domains/people", false},
		}
		for _, tc := range cases {
			got := archrules.UnderRoot(tc.path, po.ForbiddenCentralRepoPackage)
			if got != tc.wantV {
				t.Errorf("UnderRoot(%q, %q) = %v, want %v", tc.path, po.ForbiddenCentralRepoPackage, got, tc.wantV)
			}
		}
	})

	t.Run("adapter marker matching", func(t *testing.T) {
		cases := []struct {
			path  string
			wantV bool
		}{
			{"internal/data/postgres", true},
			{"internal/data/people/postgres", true},
			{"internal/connectivity/observe/adapters/postgres", true},
			{"internal/data", false},
			{"internal/domains/people", false},
		}
		for _, tc := range cases {
			got := archrules.MatchesAnyGlob(po.AdapterMarkers, tc.path)
			if got != tc.wantV {
				t.Errorf("MatchesAnyGlob(%q) = %v, want %v", tc.path, got, tc.wantV)
			}
		}
	})

	t.Run("adapter must not declare its own exported interface", func(t *testing.T) {
		red, err := archrules.ExportedInterfacesFromSource("fixture.go", adapterOwnInterfaceSource)
		if err != nil {
			t.Fatalf("ExportedInterfacesFromSource: %v", err)
		}
		if len(red) == 0 {
			t.Errorf("expected the RED fixture to declare an exported interface")
		}

		green, err := archrules.ExportedInterfacesFromSource("fixture.go", adapterImplementsOnlySource)
		if err != nil {
			t.Fatalf("ExportedInterfacesFromSource: %v", err)
		}
		if len(green) != 0 {
			t.Errorf("GREEN fixture unexpectedly declares an exported interface: %v", green)
		}
	})
}

// TestTodo_ARCH_GO_013_Integration runs both halves of the check against
// the real tree and reports every real violation found in HEAD.
func TestTodo_ARCH_GO_013_Integration(t *testing.T) {
	cfg := loadArchConfig(t)
	po := cfg.PortOwnership
	root := repopath.RootDir()

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var centralRepoFound bool
	var adapterInterfaceViolations int
	for _, pkg := range pkgs {
		rel, ok := archrules.TrimModule(cfg.Module, pkg.ImportPath)
		if !ok {
			continue
		}
		if archrules.UnderRoot(rel, po.ForbiddenCentralRepoPackage) {
			centralRepoFound = true
			t.Errorf("ARCH-GO-013 violation: a central repository package exists at %s", rel)
		}
		if !archrules.MatchesAnyGlob(po.AdapterMarkers, rel) {
			continue
		}
		names, err := archrules.ExportedInterfaces(pkg.Dir)
		if err != nil {
			t.Fatalf("ExportedInterfaces(%s): %v", pkg.Dir, err)
		}
		for _, name := range names {
			adapterInterfaceViolations++
			t.Errorf("ARCH-GO-013 violation: adapter package %s declares its own exported interface %s (a port belongs beside its consumer, not its adapter)", rel, name)
		}
	}
	t.Logf("ARCH-GO-013: central repository package found=%v, %d adapter-owned-interface violations", centralRepoFound, adapterInterfaceViolations)
}

// TestTodo_ARCH_GO_013_Conformance checks every declared adapter marker
// glob against its own concrete instantiation, and confirms
// forbidden_central_repository_package is shared consistently with
// ARCH-GO-010's domain_ownership row (both name the same package: a single
// central repository violates both rules for the same underlying reason).
func TestTodo_ARCH_GO_013_Conformance(t *testing.T) {
	cfg := loadArchConfig(t)
	po := cfg.PortOwnership
	if po.ForbiddenCentralRepoPackage != cfg.DomainOwnership.ForbiddenCentralRepoPackage {
		t.Errorf("port_ownership.forbidden_central_repository_package (%q) disagrees with domain_ownership.forbidden_central_repository_package (%q)", po.ForbiddenCentralRepoPackage, cfg.DomainOwnership.ForbiddenCentralRepoPackage)
	}
	for _, marker := range po.AdapterMarkers {
		concrete := substituteStar(marker, "people")
		if !archrules.MatchesAnyGlob(po.AdapterMarkers, concrete) {
			t.Errorf("marker %q's own concrete instantiation %q was not matched", marker, concrete)
		}
	}
}

func TestTodo_ARCH_GO_013_Golden(t *testing.T) {
	po := loadArchConfig(t).PortOwnership
	if po.ForbiddenCentralRepoPackage != "internal/repository" {
		t.Fatalf("forbidden central repository package = %q", po.ForbiddenCentralRepoPackage)
	}
	want := []string{"internal/data/postgres", "internal/data/*/postgres", "internal/data/adapters", "internal/data/*/adapters", "internal/connectivity/*/adapters", "internal/transaction/adapters"}
	if len(po.AdapterMarkers) != len(want) {
		t.Fatalf("adapter markers = %v, want %v", po.AdapterMarkers, want)
	}
	for i := range want {
		if po.AdapterMarkers[i] != want[i] {
			t.Errorf("adapter marker %d = %q, want %q", i, po.AdapterMarkers[i], want[i])
		}
	}
}
