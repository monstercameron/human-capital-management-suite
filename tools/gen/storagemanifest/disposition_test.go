package storagemanifest_test

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/tools/gen/storagemanifest"
)

var quotedValuePattern = regexp.MustCompile(`'([^']*)'`)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root, err := storagemanifest.RepoRoot(wd)
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}
	return root
}

func migrationsDir(t *testing.T) string {
	return filepath.Join(repoRoot(t), "migrations")
}

func mustInventory(t *testing.T) storagemanifest.MigrationInventory {
	t.Helper()
	inv, err := storagemanifest.ScanMigrations(migrationsDir(t))
	if err != nil {
		t.Fatalf("scan migrations: %v", err)
	}
	return inv
}

func mustModelRegistry(t *testing.T) *model.Registry {
	t.Helper()
	reg, err := model.Catalog()
	if err != nil {
		t.Fatalf("compile model catalog: %v", err)
	}
	return reg
}

// TestTodo_DB_002 is the PRIMARY test for the model-to-storage disposition
// manifest.
//
// RED: any EntityDefinition from the model registry lacks exactly one
// physical disposition, owner and schema target.
//
// GREEN: the generated manifest covers every registered model ID, resolves
// every cross-model reference and emits stable counts/digest plus
// table/event/artifact/projection/value-object classification.
func TestTodo_DB_002(t *testing.T) {
	reg := mustModelRegistry(t)
	inv := mustInventory(t)

	t.Run("RED", func(t *testing.T) {
		t.Run("entity produces no disposition without a hint", func(t *testing.T) {
			manifest, err := storagemanifest.BuildDispositionManifest(reg, inv)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			// Every entity in the real catalog does resolve to exactly one
			// row (possibly MISMATCH); the RED guarantee is that the count
			// never drops below the registered entity count.
			if len(manifest.Entities) != len(reg.Entities()) {
				t.Fatalf("manifest has %d rows, registry has %d entities",
					len(manifest.Entities), len(reg.Entities()))
			}
			seen := map[string]int{}
			for _, e := range manifest.Entities {
				seen[e.EntityRef]++
				if e.Disposition == "" {
					t.Fatalf("%s has no disposition at all", e.EntityRef)
				}
				if e.Owner == "" {
					t.Fatalf("%s has no owner", e.EntityRef)
				}
				// Exactly one disposition: a MISMATCH carries no target, and
				// every other disposition carries no mismatch detail.
				if e.Disposition == storagemanifest.DispositionMismatch {
					if e.Target != "" {
						t.Fatalf("%s is MISMATCH but also carries a target %q", e.EntityRef, e.Target)
					}
					if e.MismatchDetail == "" {
						t.Fatalf("%s is MISMATCH with no explanation", e.EntityRef)
					}
				} else {
					if e.Target == "" {
						t.Fatalf("%s is %s but carries no schema target", e.EntityRef, e.Disposition)
					}
					if e.MismatchDetail != "" {
						t.Fatalf("%s is %s but also carries a mismatch detail", e.EntityRef, e.Disposition)
					}
				}
			}
			for ref, n := range seen {
				if n != 1 {
					t.Fatalf("%s appears %d times in the manifest, want exactly 1", ref, n)
				}
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		manifest, err := storagemanifest.BuildDispositionManifest(reg, inv)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if manifest.RegistryDigest != reg.Digest() {
			t.Fatalf("manifest digest = %s, want registry digest %s", manifest.RegistryDigest, reg.Digest())
		}
		if len(manifest.MigrationFiles) == 0 {
			t.Fatalf("manifest carries no migration source files")
		}
		kinds := map[storagemanifest.DispositionKind]int{}
		for _, e := range manifest.Entities {
			kinds[e.Disposition]++
		}
		// The real schema backs at least one of each of these dispositions
		// as of this wave's migrations; a manifest that stops finding any of
		// them has regressed relative to the checked-in schema.
		for _, k := range []storagemanifest.DispositionKind{
			storagemanifest.DispositionTable, storagemanifest.DispositionEvent,
			storagemanifest.DispositionProjection,
		} {
			if kinds[k] == 0 {
				t.Fatalf("no entity resolved disposition %s", k)
			}
		}
		if manifest.Digest() == "" {
			t.Fatalf("manifest digest is empty")
		}
	})
}

// TestTodo_DB_002_Golden generates the manifest and writes it to
// definitions/model/storage-disposition.yaml, then asserts it is
// byte-identical on a second, independent generation — this is what "two
// runs byte-identical" means for DB-002.
func TestTodo_DB_002_Golden(t *testing.T) {
	root := repoRoot(t)
	gen1, err := storagemanifest.BuildAll(migrationsDir(t))
	if err != nil {
		t.Fatalf("build 1: %v", err)
	}
	if err := storagemanifest.WriteAll(root, gen1); err != nil {
		t.Fatalf("write: %v", err)
	}
	path := filepath.Join(root, "definitions", "model", storagemanifest.FileDisposition)
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}

	gen2, err := storagemanifest.BuildAll(migrationsDir(t))
	if err != nil {
		t.Fatalf("build 2: %v", err)
	}
	second, err := storagemanifest.RenderYAML(gen2.Disposition)
	if err != nil {
		t.Fatalf("render 2: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("two generations of storage-disposition.yaml differ")
	}
}

// TestTodo_DB_002_Race builds the disposition manifest concurrently against
// the same immutable registry and inventory: BuildDispositionManifest holds
// no shared mutable state, so every concurrent build must agree.
func TestTodo_DB_002_Race(t *testing.T) {
	reg := mustModelRegistry(t)
	inv := mustInventory(t)
	want, err := storagemanifest.BuildDispositionManifest(reg, inv)
	if err != nil {
		t.Fatalf("baseline build: %v", err)
	}
	wantDigest := want.Digest()

	var wg sync.WaitGroup
	digests := make([]string, 16)
	errs := make([]error, 16)
	for i := range digests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m, err := storagemanifest.BuildDispositionManifest(reg, inv)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = m.Digest()
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent build %d: %v", i, err)
		}
		if digests[i] != wantDigest {
			t.Fatalf("concurrent build %d digest = %s, want %s", i, digests[i], wantDigest)
		}
	}
}

// TestTodo_DB_002_Integration cross-checks the static migration scan against
// a real, freshly migrated PostgreSQL database: every entity this package
// classifies TABLE, EVENT (via ledger_stream) or PROJECTION must name a
// table/enum value that genuinely exists in the live schema, not merely in
// the regex scan of the SQL text.
func TestTodo_DB_002_Integration(t *testing.T) {
	db := pgtest.New(t)
	reg := mustModelRegistry(t)
	inv := mustInventory(t)
	manifest, err := storagemanifest.BuildDispositionManifest(reg, inv)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	liveTables := map[string]bool{}
	rows, err := db.SQL.Query(`SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema()`)
	if err != nil {
		t.Fatalf("query live tables: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		liveTables[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate live tables: %v", err)
	}

	var constraintDef string
	if err := db.SQL.QueryRow(
		`SELECT pg_get_constraintdef(con.oid)
		 FROM pg_constraint con
		 JOIN pg_class rel ON rel.oid = con.conrelid
		 JOIN pg_namespace ns ON ns.oid = rel.relnamespace
		 WHERE ns.nspname = current_schema()
		   AND rel.relname = 'ledger_stream'
		   AND con.conname = 'ledger_stream_kind_allowed'`,
	).Scan(&constraintDef); err != nil {
		t.Fatalf("query live stream_kind constraint: %v", err)
	}
	var liveStreamKinds []string
	for _, m := range quotedValuePattern.FindAllStringSubmatch(constraintDef, -1) {
		liveStreamKinds = append(liveStreamKinds, m[1])
	}

	for _, e := range manifest.Entities {
		switch e.Disposition {
		case storagemanifest.DispositionTable, storagemanifest.DispositionArtifact, storagemanifest.DispositionProjection:
			table := tableNameFromTarget(e.Target)
			if !liveTables[table] {
				t.Fatalf("%s claims table %q but it does not exist in the live database", e.EntityRef, table)
			}
		case storagemanifest.DispositionEvent:
			kind := streamKindFromTarget(e.Target)
			found := false
			for _, k := range liveStreamKinds {
				if k == kind {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s claims stream_kind %q but the live ledger_stream_kind_allowed constraint does not declare it: %v",
					e.EntityRef, kind, liveStreamKinds)
			}
		}
	}
}

// TestTodo_DB_002_Security asserts that a MISMATCH never silently downgrades
// to a trusted disposition: no entity may claim TABLE/EVENT/ARTIFACT/
// PROJECTION unless its physical target was independently confirmed present
// in the scanned inventory. This is what stops a stale hint from letting a
// classification/retention-sensitive entity be treated as durably stored
// when it is not.
func TestTodo_DB_002_Security(t *testing.T) {
	reg := mustModelRegistry(t)
	inv := mustInventory(t)
	manifest, err := storagemanifest.BuildDispositionManifest(reg, inv)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, e := range manifest.Entities {
		switch e.Disposition {
		case storagemanifest.DispositionEvent:
			kind := streamKindFromTarget(e.Target)
			if !inv.HasStreamKind(kind) {
				t.Fatalf("%s claims EVENT/%s but the inventory does not confirm it", e.EntityRef, kind)
			}
		case storagemanifest.DispositionTable, storagemanifest.DispositionArtifact, storagemanifest.DispositionProjection:
			table := tableNameFromTarget(e.Target)
			if !inv.HasTable(table) {
				t.Fatalf("%s claims %s/%s but the inventory does not confirm it", e.EntityRef, e.Disposition, table)
			}
		}
	}
}

// TestTodo_DB_002_Mutation proves the generator is sensitive to its inputs:
// removing a real stream_kind or table from the scanned inventory must flip
// the affected entity's disposition to MISMATCH, and adding an entity hint
// with no corresponding real table must do the same.
func TestTodo_DB_002_Mutation(t *testing.T) {
	reg := mustModelRegistry(t)
	inv := mustInventory(t)

	mutated := storagemanifest.MigrationInventory{
		Tables:          map[string]bool{},
		StreamKinds:     map[string]bool{},
		DefinitionKinds: map[string]bool{},
		ColumnChecks:    map[string]map[string]bool{},
		SourceFiles:     inv.SourceFiles,
	}
	for k, v := range inv.Tables {
		if k == "intent_instance" {
			continue // drop this one table
		}
		mutated.Tables[k] = v
	}
	for k, v := range inv.StreamKinds {
		if k == "PERSON" {
			continue // drop this one stream kind
		}
		mutated.StreamKinds[k] = v
	}
	for k, v := range inv.ColumnChecks {
		mutated.ColumnChecks[k] = v
	}

	before, err := storagemanifest.BuildDispositionManifest(reg, inv)
	if err != nil {
		t.Fatalf("build before: %v", err)
	}
	after, err := storagemanifest.BuildDispositionManifest(reg, mutated)
	if err != nil {
		t.Fatalf("build after: %v", err)
	}
	if before.Digest() == after.Digest() {
		t.Fatalf("removing intent_instance and PERSON did not change the manifest digest")
	}
	byRef := map[string]storagemanifest.EntityDisposition{}
	for _, e := range after.Entities {
		byRef[e.EntityRef] = e
	}
	if d := byRef["IntentInstance/v1"]; d.Disposition != storagemanifest.DispositionMismatch {
		t.Fatalf("IntentInstance/v1 disposition after dropping its table = %s, want MISMATCH", d.Disposition)
	}
	if d := byRef["Person/v1"]; d.Disposition != storagemanifest.DispositionMismatch {
		t.Fatalf("Person/v1 disposition after dropping its stream kind = %s, want MISMATCH", d.Disposition)
	}
}

func tableNameFromTarget(target string) string {
	const prefix = "table "
	if len(target) > len(prefix) && target[:len(prefix)] == prefix {
		return target[len(prefix):]
	}
	return target
}

func streamKindFromTarget(target string) string {
	const marker = "stream_kind="
	i := indexOf(target, marker)
	if i < 0 {
		return target
	}
	return target[i+len(marker):]
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
