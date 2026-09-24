package archrules_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/archrules"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

func matchesAnyImportPrefix(importPath string, prefixes []string) bool {
	for _, p := range prefixes {
		if importPath == p || (len(importPath) > len(p) && importPath[:len(p)+1] == p+"/") {
			return true
		}
	}
	return false
}

// TestLedgerPackageRejectsConcreteStoreDependency is the ARCH-GO-012
// primary test: internal/ledger's event/authority/stream/append/query/
// correction/chain/outbox/projection semantics must never import
// PostgreSQL (pgx, lib/pq) or even database/sql directly -- ledger defines
// semantic models and ports; internal/store/postgres (or internal/data)
// implements them.
func TestLedgerPackageRejectsConcreteStoreDependency(t *testing.T) {
	cfg := loadArchConfig(t)
	li := cfg.LedgerIndependent

	cases := []struct {
		name     string
		imported string
		wantV    bool
	}{
		{"pgx", "github.com/jackc/pgx/v5", true},
		{"pgx subpackage", "github.com/jackc/pgx/v5/pgxpool", true},
		{"lib/pq", "github.com/lib/pq", true},
		{"database/sql", "database/sql", true},
		{"an unrelated stdlib package", "crypto/sha256", false},
		{"the kernel", "internal/kernel/values", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := matchesAnyImportPrefix(c.imported, li.ForbiddenImportPrefixes)
			if got != c.wantV {
				t.Errorf("matchesAnyImportPrefix(%q) = %v, want %v", c.imported, got, c.wantV)
			}
		})
	}
}

// TestTodo_ARCH_GO_012_Integration runs the check against every real
// package under internal/ledger and reports any real PostgreSQL/database-sql
// import found in HEAD.
func TestTodo_ARCH_GO_012_Integration(t *testing.T) {
	cfg := loadArchConfig(t)
	li := cfg.LedgerIndependent
	root := repopath.RootDir()

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var total int
	for _, pkg := range pkgs {
		rel, ok := archrules.TrimModule(cfg.Module, pkg.ImportPath)
		if !ok || !archrules.UnderRoot(rel, li.LedgerRoot) {
			continue
		}
		for _, imp := range pkg.Imports {
			if matchesAnyImportPrefix(imp, li.ForbiddenImportPrefixes) {
				total++
				t.Errorf("ARCH-GO-012 violation: ledger package %s imports %s directly", rel, imp)
			}
		}
	}
	t.Logf("ARCH-GO-012: %d ledger-imports-postgres violations", total)
}

// TestTodo_ARCH_GO_012_Conformance checks every declared forbidden prefix
// independently against both a bare module path and a subpackage path.
func TestTodo_ARCH_GO_012_Conformance(t *testing.T) {
	cfg := loadArchConfig(t)
	li := cfg.LedgerIndependent
	if len(li.ForbiddenImportPrefixes) == 0 {
		t.Fatalf("ledger_independent.forbidden_import_prefixes is empty")
	}
	for _, prefix := range li.ForbiddenImportPrefixes {
		if !matchesAnyImportPrefix(prefix, li.ForbiddenImportPrefixes) {
			t.Errorf("prefix %q does not match itself", prefix)
		}
		if !matchesAnyImportPrefix(prefix+"/sub", li.ForbiddenImportPrefixes) {
			t.Errorf("prefix %q does not match its own subpackage form", prefix)
		}
	}
}

func TestTodo_ARCH_GO_012_Golden(t *testing.T) {
	li := loadArchConfig(t).LedgerIndependent
	if li.LedgerRoot != "internal/ledger" {
		t.Fatalf("ledger_root = %q", li.LedgerRoot)
	}
	want := []string{"github.com/jackc/pgx", "github.com/lib/pq", "database/sql"}
	if len(li.ForbiddenImportPrefixes) != len(want) {
		t.Fatalf("forbidden import prefixes = %v, want %v", li.ForbiddenImportPrefixes, want)
	}
	for i := range want {
		if li.ForbiddenImportPrefixes[i] != want[i] {
			t.Errorf("forbidden prefix %d = %q, want %q", i, li.ForbiddenImportPrefixes[i], want[i])
		}
	}
}

func TestTodo_ARCH_GO_012_Mutation(t *testing.T) {
	li := loadArchConfig(t).LedgerIndependent
	for _, prefix := range li.ForbiddenImportPrefixes {
		if !matchesAnyImportPrefix(prefix+"/mutated", li.ForbiddenImportPrefixes) {
			t.Errorf("mutated forbidden import %q escaped prefix rule", prefix+"/mutated")
		}
		nearMiss := prefix + "evil"
		if matchesAnyImportPrefix(nearMiss, li.ForbiddenImportPrefixes) {
			t.Errorf("prefix rule overmatched near miss %q", nearMiss)
		}
	}
}
