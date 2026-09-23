package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestFilesListsEveryEmbeddedMigrationInOrder proves the manifest is exactly
// the embedded .sql set, strictly ascending by version, each entry named by
// its own version, and each checksum the sha256 of the file's exact bytes —
// the identity DB-006 journals.
func TestFilesListsEveryEmbeddedMigrationInOrder(t *testing.T) {
	files, err := Files()
	if err != nil {
		t.Fatalf("Files(): %v", err)
	}
	if len(files) == 0 {
		t.Fatal("Files() returned no migrations")
	}

	entries, err := FS.ReadDir(".")
	if err != nil {
		t.Fatalf("read embedded directory: %v", err)
	}
	var sqlNames []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			sqlNames = append(sqlNames, entry.Name())
		}
	}
	if len(files) != len(sqlNames) {
		t.Fatalf("Files() listed %d migrations, embedded directory holds %d .sql files", len(files), len(sqlNames))
	}

	seen := map[int64]string{}
	for i, f := range files {
		if i > 0 && files[i-1].Version >= f.Version {
			t.Errorf("versions not strictly ascending at index %d: %d follows %d", i, f.Version, files[i-1].Version)
		}
		if prev, dup := seen[f.Version]; dup {
			t.Errorf("version %d embedded twice: %s and %s", f.Version, prev, f.Name)
		}
		seen[f.Version] = f.Name

		idx := strings.Index(f.Name, "_")
		if idx <= 0 {
			t.Errorf("file %q is not named <version>_<name>.sql", f.Name)
			continue
		}
		declared, err := strconv.ParseInt(f.Name[:idx], 10, 64)
		if err != nil || declared != f.Version {
			t.Errorf("file %q names version %d, manifest says %d (err=%v)", f.Name, declared, f.Version, err)
		}

		body, err := FS.ReadFile(f.Name)
		if err != nil {
			t.Fatalf("read embedded %s: %v", f.Name, err)
		}
		sum := sha256.Sum256(body)
		if f.Checksum != hex.EncodeToString(sum[:]) {
			t.Errorf("file %q checksum = %s, want %s", f.Name, f.Checksum, hex.EncodeToString(sum[:]))
		}
	}
}

// TestEveryMigrationIsReversibilityDocumented proves the package doc's
// contract at file level: every embedded migration is a Goose file with
// exactly one Up and one Down section (DB-006 records irreversibility on the
// release, so an absent Down is a broken file, not a permitted shape).
func TestEveryMigrationIsReversibilityDocumented(t *testing.T) {
	files, err := Files()
	if err != nil {
		t.Fatalf("Files(): %v", err)
	}
	for _, f := range files {
		body, err := FS.ReadFile(f.Name)
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		text := string(body)
		if up, down := strings.Count(text, "-- +goose Up"), strings.Count(text, "-- +goose Down"); up != 1 || down != 1 {
			t.Errorf("file %q has %d Up and %d Down markers, want exactly one of each", f.Name, up, down)
		}
	}
}

// TestArtifactDigestIsStableAndDerivedFromTheFiles proves the release
// identity is deterministic and exactly the documented binding over the file
// manifest (version, name, per-file checksum, in version order) — reordering,
// renaming or truncating any file changes it.
func TestArtifactDigestIsStableAndDerivedFromTheFiles(t *testing.T) {
	first, err := ArtifactDigest()
	if err != nil {
		t.Fatalf("ArtifactDigest(): %v", err)
	}
	second, err := ArtifactDigest()
	if err != nil {
		t.Fatalf("ArtifactDigest() again: %v", err)
	}
	if first != second {
		t.Fatalf("ArtifactDigest() is not stable: %s vs %s", first, second)
	}
	if len(first) != sha256.Size*2 {
		t.Fatalf("ArtifactDigest() = %q, want 64 hex characters", first)
	}
	for i, c := range first {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("ArtifactDigest() %q is not lowercase hex at byte %d", first, i)
		}
	}

	files, err := Files()
	if err != nil {
		t.Fatalf("Files(): %v", err)
	}
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%d\x00%s\x00%s\x00", f.Version, f.Name, f.Checksum)
	}
	if want := hex.EncodeToString(h.Sum(nil)); first != want {
		t.Fatalf("ArtifactDigest() = %s, want the binding over Files() %s", first, want)
	}
}

// TestTargetVersionIsTheHighestEmbeddedVersion proves TargetVersion is the
// manifest's own maximum, not a separately maintained constant.
func TestTargetVersionIsTheHighestEmbeddedVersion(t *testing.T) {
	target, err := TargetVersion()
	if err != nil {
		t.Fatalf("TargetVersion(): %v", err)
	}
	files, err := Files()
	if err != nil {
		t.Fatalf("Files(): %v", err)
	}
	if target != files[len(files)-1].Version {
		t.Errorf("TargetVersion() = %d, want the highest listed version %d", target, files[len(files)-1].Version)
	}
	var max int64
	for _, f := range files {
		if f.Version > max {
			max = f.Version
		}
	}
	if target != max {
		t.Errorf("TargetVersion() = %d, want the manifest maximum %d", target, max)
	}
}

// TestNewestReversibleVersionFindsHighestReversibleMigration proves the
// helper returns the highest version whose Down section lacks the declared
// "is irreversible" marker. Older irreversible migrations can be followed
// by reversible migrations, so irreversibility is not a monotonic property
// of the migration history.
func TestNewestReversibleVersionFindsHighestReversibleMigration(t *testing.T) {
	reversible, err := NewestReversibleVersion()
	if err != nil {
		t.Fatalf("NewestReversibleVersion(): %v", err)
	}
	files, err := Files()
	if err != nil {
		t.Fatalf("Files(): %v", err)
	}
	var highestReversible int64
	for _, f := range files {
		body, err := FS.ReadFile(f.Name)
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		down := string(body)
		if idx := strings.Index(down, "-- +goose Down"); idx >= 0 {
			down = down[idx:]
		}
		if !strings.Contains(down, "is irreversible") && f.Version > highestReversible {
			highestReversible = f.Version
		}
	}
	if reversible != highestReversible {
		t.Errorf("NewestReversibleVersion() = %d, want highest migration with reversible Down %d", reversible, highestReversible)
	}
}

// TestParseVersion proves the <version>_<name>.sql contract: the leading run
// before the first underscore must be a positive integer version.
func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		name string
		want int64
		ok   bool
	}{
		{"00005_ledger.sql", 5, true},
		{"1_a_b_c.sql", 1, true},
		{"10_next.sql", 10, true},
		{"ledger.sql", 0, false},  // no underscore separator
		{"_x.sql", 0, false},      // version part is empty
		{"abc_x.sql", 0, false},   // version is not a number
		{"00000_x.sql", 0, false}, // zero is not a version
		{"-1_x.sql", 0, false},    // negative is not a version
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseVersion(tc.name)
			if tc.ok {
				if err != nil {
					t.Fatalf("parseVersion(%q) error: %v", tc.name, err)
				}
				if got != tc.want {
					t.Errorf("parseVersion(%q) = %d, want %d", tc.name, got, tc.want)
				}
			} else if err == nil {
				t.Errorf("parseVersion(%q) = %d, want an error", tc.name, got)
			}
		})
	}
}

// TestEmbeddedFilesAreSortedListable is a light guard that the embedded set
// survives its own filesystem contract: every entry is a regular file (no
// subdirectories slipped in beside the SQL) and the .sql set is non-empty.
func TestEmbeddedFilesAreSortedListable(t *testing.T) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("fs.ReadDir(FS, \".\"): %v", err)
	}
	var sqls []string
	for _, entry := range entries {
		if entry.IsDir() {
			t.Errorf("embedded entry %q is a directory", entry.Name())
		}
		if strings.HasSuffix(entry.Name(), ".sql") {
			sqls = append(sqls, entry.Name())
		}
	}
	if len(sqls) == 0 {
		t.Fatal("no .sql files embedded")
	}
	if !sort.StringsAreSorted(sqls) {
		// fs.ReadDir reports entries sorted by name; if this fails the embed
		// contract itself is broken and every version-ordering assumption in
		// Files() is suspect.
		t.Errorf("embedded entries not sorted: %v", sqls)
	}
}
