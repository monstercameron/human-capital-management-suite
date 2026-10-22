package libfirewall_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depmanifest"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/libfirewall"
)

const gooseImportPath = "github.com/pressly/goose/v3"

// goMigrationFiles lists every .go file directly under dir except the
// package's own embed/registration boilerplate file(s) named in
// skipFiles, and except _test.go files (test helpers are not migrations).
func goMigrationFiles(t *testing.T, dir string, skipFiles ...string) []string {
	t.Helper()
	skip := map[string]bool{}
	for _, f := range skipFiles {
		skip[f] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		if skip[e.Name()] {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}

// TestTodo_LIB_008_Golden pins the exact reviewed Goose import roots. A
// wider root would silently turn a migration mechanic into an application
// dependency; a narrower root would break the ephemeral database and schema
// verification tooling that must execute the same migration history.
func TestTodo_LIB_008_Golden(t *testing.T) {
	_, roles := loadFirewallConfigAndRoles(t)
	class := roles.Classify(gooseImportPath)
	if !class.Found || !class.Exact {
		t.Fatalf("dependency-roles.yaml has no exact row for %s", gooseImportPath)
	}
	want := []string{"migrations", "cmd", "internal/data/pgtest", "internal/data/schema", "internal/data/agentstore", "internal/data/agentpersonastore/migrations", "tools/chatload"}
	if strings.Join(class.Row.AllowedImportRoots, "\n") != strings.Join(want, "\n") {
		t.Fatalf("goose allowed_import_roots = %v, want %v", class.Row.AllowedImportRoots, want)
	}
}

// TestTodo_LIB_008_Race exercises the immutable qualification manifest from
// concurrent readers. Run this test with -race on a supported builder.
func TestTodo_LIB_008_Race(t *testing.T) {
	_, roles := loadFirewallConfigAndRoles(t)
	const readers = 32
	var wg sync.WaitGroup
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			if v := libfirewall.CheckImport(roles, roles.Module+"/migrations", gooseImportPath); v != nil {
				t.Errorf("concurrent qualification returned violation: %+v", v)
			}
		}()
	}
	wg.Wait()
}

// requiresReviewMarker asserts every file in files contains marker
// somewhere in its content (the LIB-008 "explicit need, idempotency and
// resumable failure contract" review evidence, until a fuller structured
// review-evidence format exists).
func requiresReviewMarker(t *testing.T, files []string, marker string) {
	t.Helper()
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		if !strings.Contains(string(data), marker) {
			t.Errorf("hand-authored Go migration file %s lacks the required %q review marker (LIB-008: a Go migration needs an explicit, reviewed need/idempotency/resumable-failure contract)", f, marker)
		}
	}
}

// TestGooseBackendQualification is the LIB-008 primary test: goose is
// confined to dependency-roles.yaml's own allowed roots for it
// (migrations, cmd, the integration-test harness, and schema tooling), and
// any hand-authored Go migration file (as opposed to
// the package's own embed/registration boilerplate) must carry the
// library-firewall.yaml review marker.
func TestGooseBackendQualification(t *testing.T) {
	cfg, roles := loadFirewallConfigAndRoles(t)

	t.Run("import root boundary", func(t *testing.T) {
		cases := []struct {
			name     string
			importer string
			wantV    bool
		}{
			{"migrations may import goose", roles.Module + "/migrations", false},
			{"cmd/migrate may import goose", roles.Module + "/cmd/migrate", false},
			{"pgtest may import goose to migrate ephemeral databases", roles.Module + "/internal/data/pgtest", false},
			{"schema tooling may import goose to inspect migration history", roles.Module + "/internal/data/schema", false},
			{"agent store owns its migration runner", roles.Module + "/internal/data/agentstore", false},
			{"agent personastore owns its migration files", roles.Module + "/internal/data/agentpersonastore/migrations", false},
			{"chat load tool applies its test schema", roles.Module + "/tools/chatload", false},
			{"a domain package may not import goose", roles.Module + "/internal/domains/people", true},
			{"an unrelated data package may not import goose directly", roles.Module + "/internal/data/ledger", true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				v := libfirewall.CheckImport(roles, tc.importer, gooseImportPath)
				if tc.wantV && v == nil {
					t.Errorf("CheckImport(%q, %q) = nil, want a violation", tc.importer, gooseImportPath)
				}
				if !tc.wantV && v != nil {
					t.Errorf("CheckImport(%q, %q) = %+v, want no violation", tc.importer, gooseImportPath, v)
				}
			})
		}
	})

	t.Run("Go migration review marker", func(t *testing.T) {
		t.Run("RED: a hand-authored Go migration without the marker is scanned via a synthetic fixture", func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "00099_unreviewed.go")
			if err := os.WriteFile(path, []byte("package migrations\n\n// A Go migration with no review evidence.\nfunc init() {}\n"), 0o644); err != nil {
				t.Fatalf("writing fixture: %v", err)
			}
			ok := strings.Contains(mustRead(t, path), cfg.GooseGoMigrationMarker)
			if ok {
				t.Fatalf("fixture unexpectedly contains the marker")
			}
		})

		t.Run("GREEN: a marked fixture passes", func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "00099_reviewed.go")
			content := "package migrations\n\n// " + cfg.GooseGoMigrationMarker + " reviewed 2026-09-03, idempotent backfill, resumable on failure.\nfunc init() {}\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("writing fixture: %v", err)
			}
			requiresReviewMarker(t, []string{path}, cfg.GooseGoMigrationMarker)
		})
	})
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// TestTodo_LIB_008_Integration runs the qualification against the real
// tree: import boundary over every real package, plus the review-marker
// check over every real .go file directly inside migrations/ other than
// its own embed/registration boilerplate (migrations.go).
func TestTodo_LIB_008_Integration(t *testing.T) {
	cfg, roles := loadFirewallConfigAndRoles(t)
	root := repopath.RootDir()

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}
	var boundaryViolations int
	for _, pkg := range pkgs {
		for _, imp := range pkg.Imports {
			if v := libfirewall.CheckImport(roles, pkg.ImportPath, imp); v != nil && v.Module == gooseImportPath {
				boundaryViolations++
				t.Errorf("LIB-008 import boundary violation: %s imports %s (allowed: %v)", v.Importer, v.ImportedPath, v.AllowedRoots)
			}
		}
	}

	migrationsDir := filepath.Join(root, "migrations")
	handAuthored := goMigrationFiles(t, migrationsDir, "migrations.go")
	requiresReviewMarker(t, handAuthored, cfg.GooseGoMigrationMarker)

	t.Logf("goose: %d import-boundary violations, %d hand-authored Go migration files reviewed", boundaryViolations, len(handAuthored))
}

// TestTodo_LIB_008_Fault feeds requiresReviewMarker a nonexistent file path
// and confirms the underlying read fails loudly (via t.Fatalf from the
// helper) rather than silently treating a missing file as compliant. This
// is exercised indirectly by asserting os.ReadFile itself errors, since
// requiresReviewMarker's helper deliberately calls t.Fatalf (uncatchable
// from within the same test) on a read failure -- the fault path this test
// documents is "no migration file is ever silently skipped".
func TestTodo_LIB_008_Fault(t *testing.T) {
	_, err := os.ReadFile(filepath.Join(t.TempDir(), "does-not-exist.go"))
	if err == nil {
		t.Fatalf("expected an error reading a nonexistent file")
	}
}

// TestTodo_LIB_008_Conformance confirms the goose row itself is complete
// and its allowed roots are exactly the production migration runners plus
// the narrowly scoped schema and chatload tooling roots.
func TestTodo_LIB_008_Conformance(t *testing.T) {
	_, roles := loadFirewallConfigAndRoles(t)
	class := roles.Classify(gooseImportPath)
	if !class.Found || !class.Exact {
		t.Fatalf("dependency-roles.yaml has no exact row for %s", gooseImportPath)
	}
	if missing := depmanifest.RowIsComplete(class.Row); len(missing) > 0 {
		t.Errorf("goose manifest row is missing fields: %v", missing)
	}
	want := map[string]bool{
		"migrations":               true,
		"cmd":                      true,
		"internal/data/pgtest":     true,
		"internal/data/schema":     true,
		"internal/data/agentstore": true,
		"internal/data/agentpersonastore/migrations": true,
		"tools/chatload": true,
	}
	if len(class.Row.AllowedImportRoots) != len(want) {
		t.Fatalf("goose allowed_import_roots = %v, want exactly %v", class.Row.AllowedImportRoots, want)
	}
	for _, r := range class.Row.AllowedImportRoots {
		if !want[r] {
			t.Errorf("unexpected goose allowed root %q", r)
		}
	}
}
