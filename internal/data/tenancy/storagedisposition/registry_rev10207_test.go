package storagedisposition_test

// REV-102-07: the storage-disposition registry's header named a stale
// migration range, pseudonym_escrow_record held a ciphertext column while
// registered PLATFORM_MANAGED, and no test validated owner packages. These
// tests pin the corrections and prove the registry now fails closed on all
// three defects.

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
)

// rev10207RepoRoot locates the repository root (the directory holding
// go.mod) by walking up from this test file, so the tests work regardless
// of the working directory `go test` is run from.
func rev10207RepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the test source")
	}
	dir := filepath.Dir(thisFile)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot find go.mod walking up from the test source")
		}
		dir = parent
	}
}

var (
	rev10207CreateTable = regexp.MustCompile(`(?i)^\s*CREATE TABLE (?:IF NOT EXISTS )?([A-Za-z_][A-Za-z0-9_]*)\b`)
	rev10207CipherCol   = regexp.MustCompile(`(?i)^\s*(ciphertext|encrypted_[A-Za-z0-9_]*)\s+[A-Za-z]`)
	rev10207SourceLine  = regexp.MustCompile(`(?m)^source: (\S+) \.\. (\S+)\s*$`)
)

// rev10207CiphertextTables scans every migrations/*.sql file for CREATE
// TABLE blocks declaring a column named ciphertext or encrypted_*, and
// returns those table names sorted. A column definition starts the line;
// CHECK constraints and comments merely mentioning the word do not match.
func rev10207CiphertextTables(t *testing.T, migrationsDir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no migration files in %s", migrationsDir)
	}
	slices.Sort(files)
	var tables []string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		current := ""
		for _, line := range strings.Split(string(raw), "\n") {
			if m := rev10207CreateTable.FindStringSubmatch(line); m != nil {
				current = m[1]
				continue
			}
			if current != "" && rev10207CipherCol.MatchString(line) {
				tables = append(tables, current)
				current = ""
			}
		}
	}
	slices.Sort(tables)
	return slices.Compact(tables)
}

// rev10207FieldLevelError reports whether every ciphertext-bearing table is
// registered FIELD_LEVEL. It returns nil only when the registry agrees with
// the migration sources.
func rev10207FieldLevelError(reg *storagedisposition.Registry, cipherTables []string) error {
	for _, table := range cipherTables {
		entry, ok := reg.Lookup(table)
		if !ok {
			return &rev10207Mismatch{table: table, reason: "declares a ciphertext column in migrations but is not registered"}
		}
		if entry.EncryptionClass != storagedisposition.EncryptionFieldLevel {
			return &rev10207Mismatch{table: table, reason: "declares a ciphertext column in migrations but is registered " + entry.EncryptionClass}
		}
	}
	return nil
}

type rev10207Mismatch struct {
	table  string
	reason string
}

func (e *rev10207Mismatch) Error() string {
	return "storagedisposition: " + e.table + ": " + e.reason
}

// rev10207ExpectedSource derives the header's source range from the
// migration files on disk: first and last *.sql names, sorted.
func rev10207ExpectedSource(t *testing.T, migrationsDir string) (first, last string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("list migrations: %v", err)
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	slices.Sort(names)
	return names[0], names[len(names)-1]
}

// rev10207HeaderSource reads the raw registry YAML and returns the two
// migration names its source: line declares.
func rev10207HeaderSource(t *testing.T, registryFile string) (first, last string) {
	t.Helper()
	raw, err := os.ReadFile(registryFile)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	m := rev10207SourceLine.FindStringSubmatch(strings.ReplaceAll(string(raw), "\r\n", "\n"))
	if m == nil {
		t.Fatal("registry has no parseable `source: first .. last` header line")
	}
	return filepath.Base(m[1]), filepath.Base(m[2])
}

// rev10207RangeMatches reports whether a header's declared range names the
// migration range actually on disk.
func rev10207RangeMatches(gotFirst, gotLast, wantFirst, wantLast string) bool {
	return gotFirst == wantFirst && gotLast == wantLast
}

// TestTodo_REV_102_07 proves the three corrected facts against sources
// independent of the registry itself: owner packages exist on disk, every
// table whose migration declares a ciphertext column is FIELD_LEVEL, and
// the header range matches the migration directory.
func TestTodo_REV_102_07(t *testing.T) {
	t.Parallel()
	root := rev10207RepoRoot(t)
	regFile := filepath.Join(root, "definitions", "storage", "storage-disposition.yaml")
	reg, err := storagedisposition.Load(regFile)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if err := storagedisposition.Validate(reg); err != nil {
		t.Fatalf("checked-in registry fails validation: %v", err)
	}
	if err := storagedisposition.ValidateOwnerPackages(reg, root); err != nil {
		t.Fatalf("checked-in registry names a missing owner package: %v", err)
	}

	migrationsDir := filepath.Join(root, "migrations")
	cipherTables := rev10207CiphertextTables(t, migrationsDir)
	if !slices.Equal(cipherTables, []string{"pseudonym_escrow_record"}) {
		t.Fatalf("ciphertext-bearing tables = %v, want exactly [pseudonym_escrow_record]", cipherTables)
	}
	if err := rev10207FieldLevelError(reg, cipherTables); err != nil {
		t.Fatalf("ciphertext table is not FIELD_LEVEL: %v", err)
	}

	wantFirst, wantLast := rev10207ExpectedSource(t, migrationsDir)
	gotFirst, gotLast := rev10207HeaderSource(t, regFile)
	if gotFirst != wantFirst || gotLast != wantLast {
		t.Fatalf("header source range = %s .. %s, migrations on disk run %s .. %s",
			gotFirst, gotLast, wantFirst, wantLast)
	}
}

// TestTodo_REV_102_07_Golden pins the corrected escrow row byte for byte:
// a future edit silently demoting it back to PLATFORM_MANAGED changes
// these bytes and fails here, not in production.
func TestTodo_REV_102_07_Golden(t *testing.T) {
	t.Parallel()
	root := rev10207RepoRoot(t)
	goldenFile := filepath.Join(root, "internal", "data", "tenancy", "storagedisposition", "testdata", "rev10207_escrow_row.golden")
	want, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf("golden file %s is missing; it must be checked in, never skipped: %v", goldenFile, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "definitions", "storage", "storage-disposition.yaml"))
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	start := strings.Index(text, "  - table: pseudonym_escrow_record\n")
	if start < 0 {
		t.Fatal("registry no longer contains a pseudonym_escrow_record entry")
	}
	rest := text[start:]
	next := strings.Index(rest, "\n  - table: ")
	var block string
	if next < 0 {
		block = rest
	} else {
		block = rest[:next+1]
	}
	if block != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("escrow row drifted from golden:\n--- golden ---\n%s\n--- live ---\n%s", want, block)
	}
}

// TestTodo_REV_102_07_Fault proves each new check is load bearing: a forged
// owner, a demoted escrow row, and a stale header range are each refused.
func TestTodo_REV_102_07_Fault(t *testing.T) {
	t.Parallel()
	root := rev10207RepoRoot(t)
	regFile := filepath.Join(root, "definitions", "storage", "storage-disposition.yaml")

	t.Run("missing owner package", func(t *testing.T) {
		reg, err := storagedisposition.Load(regFile)
		if err != nil {
			t.Fatalf("load registry: %v", err)
		}
		reg.Tables[0].OwnerPackage = "internal/data/does-not-exist"
		if err := storagedisposition.ValidateOwnerPackages(reg, root); err == nil {
			t.Fatal("an owner package naming nothing on disk passed validation")
		}
	})

	t.Run("ciphertext table demoted to PLATFORM_MANAGED", func(t *testing.T) {
		reg, err := storagedisposition.Load(regFile)
		if err != nil {
			t.Fatalf("load registry: %v", err)
		}
		for i, e := range reg.Tables {
			if e.Table == "pseudonym_escrow_record" {
				reg.Tables[i].EncryptionClass = storagedisposition.EncryptionPlatformManaged
			}
		}
		cipherTables := rev10207CiphertextTables(t, filepath.Join(root, "migrations"))
		if err := rev10207FieldLevelError(reg, cipherTables); err == nil {
			t.Fatal("a ciphertext-bearing table registered PLATFORM_MANAGED passed the field-level check")
		}
	})

	t.Run("stale header range", func(t *testing.T) {
		wantFirst, wantLast := rev10207ExpectedSource(t, filepath.Join(root, "migrations"))
		gotFirst, gotLast := rev10207HeaderSource(t, regFile)
		if !rev10207RangeMatches(gotFirst, gotLast, wantFirst, wantLast) {
			t.Fatalf("checked-in header %s .. %s does not match disk %s .. %s",
				gotFirst, gotLast, wantFirst, wantLast)
		}
		// The exact staleness REV-102-07 closed: a header ending at an
		// earlier migration than the directory holds must not match.
		if rev10207RangeMatches(gotFirst, "00096_identityprivacy.sql", wantFirst, wantLast) {
			t.Fatal("a header ending at 00096 was accepted against a newer migration directory")
		}
	})
}

// TestTodo_REV_102_07_Recovery proves the header range is recoverable from
// its source of truth rather than hand-maintained lore: recomputing the
// first and last migration filenames from the directory yields exactly the
// checked-in header, so any future drift is mechanically detectable.
func TestTodo_REV_102_07_Recovery(t *testing.T) {
	t.Parallel()
	root := rev10207RepoRoot(t)
	migrationsDir := filepath.Join(root, "migrations")
	wantFirst, wantLast := rev10207ExpectedSource(t, migrationsDir)
	regFile := filepath.Join(root, "definitions", "storage", "storage-disposition.yaml")
	gotFirst, gotLast := rev10207HeaderSource(t, regFile)
	if gotFirst != wantFirst || gotLast != wantLast {
		t.Fatalf("header cannot be recovered from disk: header %s .. %s, disk %s .. %s",
			gotFirst, gotLast, wantFirst, wantLast)
	}
}
