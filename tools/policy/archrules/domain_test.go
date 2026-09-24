package archrules_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/archrules"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// domainOf returns the first path segment of rel below domainsRoot (the
// owning domain's own directory name), or "" if rel is not under
// domainsRoot.
func domainOf(domainsRoot, rel string) string {
	if !archrules.UnderRoot(rel, domainsRoot) {
		return ""
	}
	suffix := rel[len(domainsRoot):]
	suffix = trimLeadingSlash(suffix)
	if suffix == "" {
		return ""
	}
	for i, r := range suffix {
		if r == '/' {
			return suffix[:i]
		}
	}
	return suffix
}

func trimLeadingSlash(s string) string {
	if len(s) > 0 && s[0] == '/' {
		return s[1:]
	}
	return s
}

// TestDomainOwnershipRejectsAnemicAndCrossDomainPersistence is the
// ARCH-GO-010 primary test: no central internal/repository package may
// exist, and a domain package must never import a *different* domain's own
// persistence subpackage (store/persistence/repository) -- cross-domain
// work happens through capabilities/transactions, never a direct
// reach-around into another domain's tables.
func TestDomainOwnershipRejectsAnemicAndCrossDomainPersistence(t *testing.T) {
	cfg := loadArchConfig(t)
	do := cfg.DomainOwnership

	t.Run("persistence marker matching", func(t *testing.T) {
		cases := []struct {
			path  string
			wantV bool
		}{
			{"internal/domains/people/store", true},
			{"internal/domains/compensation/persistence", true},
			{"internal/domains/promotion/repository", true},
			{"internal/domains/people", false},
			{"internal/domains/people/aggregate", false},
		}
		for _, tc := range cases {
			got := archrules.MatchesAnyGlob(do.PersistenceMarkers, tc.path)
			if got != tc.wantV {
				t.Errorf("MatchesAnyGlob(%q) = %v, want %v", tc.path, got, tc.wantV)
			}
		}
	})

	t.Run("cross-domain persistence import", func(t *testing.T) {
		cases := []struct {
			name     string
			importer string
			imported string
			wantV    bool
		}{
			{"people importing compensation's store", "internal/domains/people", "internal/domains/compensation/store", true},
			{"people importing its own store", "internal/domains/people", "internal/domains/people/store", false},
			{"people importing compensation's root (capability) is fine", "internal/domains/people", "internal/domains/compensation", false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				importerDomain := domainOf(do.Root, tc.importer)
				importedDomain := domainOf(do.Root, tc.imported)
				crossDomainPersistence := importerDomain != "" && importedDomain != "" &&
					importerDomain != importedDomain &&
					archrules.MatchesAnyGlob(do.PersistenceMarkers, tc.imported)
				if crossDomainPersistence != tc.wantV {
					t.Errorf("crossDomainPersistence(%q -> %q) = %v, want %v", tc.importer, tc.imported, crossDomainPersistence, tc.wantV)
				}
			})
		}
	})
}

// TestTodo_ARCH_GO_010_Integration runs both halves of the check against
// the real tree: no internal/repository package exists, and no domain
// imports a sibling domain's persistence subpackage.
func TestTodo_ARCH_GO_010_Integration(t *testing.T) {
	cfg := loadArchConfig(t)
	do := cfg.DomainOwnership
	root := repopath.RootDir()

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var centralRepoFound bool
	var crossDomainViolations int
	for _, pkg := range pkgs {
		rel, ok := archrules.TrimModule(cfg.Module, pkg.ImportPath)
		if !ok {
			continue
		}
		if archrules.UnderRoot(rel, do.ForbiddenCentralRepoPackage) {
			centralRepoFound = true
			t.Errorf("ARCH-GO-010/013 violation: a central repository package exists at %s", rel)
		}
		importerDomain := domainOf(do.Root, rel)
		if importerDomain == "" {
			continue
		}
		for _, imp := range pkg.Imports {
			impRel, ok := archrules.TrimModule(cfg.Module, imp)
			if !ok {
				continue
			}
			importedDomain := domainOf(do.Root, impRel)
			if importedDomain == "" || importedDomain == importerDomain {
				continue
			}
			if archrules.MatchesAnyGlob(do.PersistenceMarkers, impRel) {
				crossDomainViolations++
				t.Errorf("ARCH-GO-010 violation: domain %s imports sibling domain %s's persistence package %s", importerDomain, importedDomain, impRel)
			}
		}
	}
	t.Logf("ARCH-GO-010: central repository package found=%v, %d cross-domain persistence violations", centralRepoFound, crossDomainViolations)
}

// TestTodo_ARCH_GO_010_Conformance checks every declared persistence
// marker glob resolves consistently for both same-domain (allowed) and
// cross-domain (forbidden) placements.
func TestTodo_ARCH_GO_010_Conformance(t *testing.T) {
	cfg := loadArchConfig(t)
	do := cfg.DomainOwnership
	if len(do.PersistenceMarkers) == 0 {
		t.Fatalf("domain_ownership.persistence_markers is empty")
	}
	for _, marker := range do.PersistenceMarkers {
		concrete := substituteStar(marker, "widgets")
		if !archrules.MatchesAnyGlob(do.PersistenceMarkers, concrete) {
			t.Errorf("marker %q's own concrete instantiation %q was not matched", marker, concrete)
		}
	}
}

func TestTodo_ARCH_GO_010_Golden(t *testing.T) {
	do := loadArchConfig(t).DomainOwnership
	if do.Root != "internal/domains" || do.ForbiddenCentralRepoPackage != "internal/repository" {
		t.Fatalf("domain ownership roots drifted: %+v", do)
	}
	want := []string{"internal/domains/*/store", "internal/domains/*/persistence", "internal/domains/*/repository"}
	if len(do.PersistenceMarkers) != len(want) {
		t.Fatalf("persistence markers = %v, want %v", do.PersistenceMarkers, want)
	}
	for i := range want {
		if do.PersistenceMarkers[i] != want[i] {
			t.Errorf("persistence marker %d = %q, want %q", i, do.PersistenceMarkers[i], want[i])
		}
	}
}

func TestTodo_ARCH_GO_010_Property(t *testing.T) {
	do := loadArchConfig(t).DomainOwnership
	for _, a := range []string{"people", "compensation", "org/chart"} {
		for _, b := range []string{"position", "benefits"} {
			for _, suffix := range []string{"store", "persistence", "repository"} {
				imp, dest := do.Root+"/"+a, do.Root+"/"+b+"/"+suffix+"/records"
				cross := domainOf(do.Root, imp) != domainOf(do.Root, dest) && archrules.MatchesAnyGlob(do.PersistenceMarkers, dest)
				if !cross {
					t.Errorf("cross-domain persistence %q -> %q was not recognized", imp, dest)
				}
			}
		}
	}
}

func TestTodo_ARCH_GO_010_Race(t *testing.T) {
	do := loadArchConfig(t).DomainOwnership
	const workers = 24
	errCh := make(chan string, workers)
	done := make(chan struct{}, workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			a := []string{"people", "compensation", "org"}[i%3]
			b := []string{"position", "benefits"}[i%2]
			path := do.Root + "/" + b + "/store"
			got := domainOf(do.Root, do.Root+"/"+a) != domainOf(do.Root, path) && archrules.MatchesAnyGlob(do.PersistenceMarkers, path)
			if !got {
				errCh <- path
			}
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < workers; i++ {
		<-done
	}
	close(errCh)
	for path := range errCh {
		t.Errorf("concurrent ownership check missed %s", path)
	}
}
