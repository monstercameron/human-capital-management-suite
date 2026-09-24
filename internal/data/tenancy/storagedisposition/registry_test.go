package storagedisposition_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

// registryPath locates definitions/storage/storage-disposition.yaml by
// walking up from this test file to the repository root (the same technique
// tools/quality/main.go uses to find go.mod), so the test works regardless of
// the working directory `go test` is run from.
func registryPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the test source")
	}
	dir := filepath.Dir(thisFile)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return filepath.Join(dir, "definitions", "storage", "storage-disposition.yaml")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot find go.mod walking up from the test source")
		}
		dir = parent
	}
}

func loadRegistry(t *testing.T) *storagedisposition.Registry {
	t.Helper()
	r, err := storagedisposition.Load(registryPath(t))
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	return r
}

// liveBaseTables returns the live schema's own base tables: real relations,
// excluding Goose's version table and the physical partitions of a
// partitioned table (the same exclusions internal/data/schema's own
// baseTables() helper applies, since a partition is physical storage for its
// parent's single logical table, not a distinct authoritative store).
func liveBaseTables(t *testing.T, db *pgtest.DB) []string {
	t.Helper()
	rows, err := db.Conn.Query(context.Background(), `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema()
		  AND c.relkind IN ('r', 'p')
		  AND NOT c.relispartition
		  AND c.relname <> 'goose_db_version'
		ORDER BY c.relname`)
	if err != nil {
		t.Fatalf("list live tables: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list live tables: %v", err)
	}
	return names
}

// TestTodo_STORE_001 proves the registry and the live, fully migrated schema
// agree in both directions: every live base table is registered, and every
// registered table actually exists.
func TestTodo_STORE_001(t *testing.T) {
	t.Parallel()
	reg := loadRegistry(t)
	if err := storagedisposition.Validate(reg); err != nil {
		t.Fatalf("checked-in registry fails validation: %v", err)
	}

	db := pgtest.New(t)
	live := liveBaseTables(t, db)

	diff := reg.CompareToLiveSchema(live)
	if len(diff.UnregisteredInLive) > 0 {
		t.Errorf("live schema holds unregistered tables: %v", diff.UnregisteredInLive)
	}
	if len(diff.StaleInRegistry) > 0 {
		t.Errorf("registry names tables the live schema does not hold: %v", diff.StaleInRegistry)
	}
}

// TestTodo_STORE_001_Integration cross-checks each entry's declared
// attributes against database facts, not just its table name: a declared
// tenant_scoping_column actually exists and is NOT NULL, a declared
// append_only table actually carries the forbid_mutation trigger, and the
// declared partition list actually matches pg_inherits.
func TestTodo_STORE_001_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reg := loadRegistry(t)
	db := pgtest.New(t)

	for _, e := range reg.Tables {
		e := e
		t.Run(e.Table, func(t *testing.T) {
			if e.TenantScoped() {
				assertLiveTenantRLS(t, db, e.Table, *e.TenantScopingColumn)
				var nullable string
				err := db.Conn.QueryRow(ctx, `
					SELECT is_nullable FROM information_schema.columns
					WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`,
					e.Table, *e.TenantScopingColumn).Scan(&nullable)
				if err != nil {
					t.Fatalf("%s declares tenant_scoping_column %q, which does not exist: %v",
						e.Table, *e.TenantScopingColumn, err)
				}
				if nullable != "NO" {
					t.Fatalf("%s.%s is nullable; a tenant-scoping column must be NOT NULL",
						e.Table, *e.TenantScopingColumn)
				}
			}

			if e.AppendOnly {
				var triggerCount int
				if err := db.Conn.QueryRow(ctx, `
					SELECT count(*) FROM pg_trigger t
					JOIN pg_class c ON c.oid = t.tgrelid
					JOIN pg_namespace n ON n.oid = c.relnamespace
					JOIN pg_proc p ON p.oid = t.tgfoid
					WHERE n.nspname = current_schema() AND c.relname = $1
					  AND NOT t.tgisinternal AND p.proname = 'forbid_mutation'`,
					e.Table).Scan(&triggerCount); err != nil {
					t.Fatalf("check forbid_mutation trigger on %s: %v", e.Table, err)
				}
				if triggerCount == 0 {
					t.Fatalf("%s is declared append_only but carries no forbid_mutation trigger", e.Table)
				}
			}

			if len(e.Partitions) > 0 {
				rows, err := db.Conn.Query(ctx, `
					SELECT child.relname
					FROM pg_inherits i
					JOIN pg_class parent ON parent.oid = i.inhparent
					JOIN pg_class child ON child.oid = i.inhrelid
					JOIN pg_namespace n ON n.oid = parent.relnamespace
					WHERE n.nspname = current_schema() AND parent.relname = $1
					ORDER BY child.relname`, e.Table)
				if err != nil {
					t.Fatalf("list partitions of %s: %v", e.Table, err)
				}
				var got []string
				for rows.Next() {
					var name string
					if err := rows.Scan(&name); err != nil {
						rows.Close()
						t.Fatalf("scan partition name: %v", err)
					}
					got = append(got, name)
				}
				rows.Close()
				want := slices.Clone(e.Partitions)
				slices.Sort(got)
				slices.Sort(want)
				if !slices.Equal(got, want) {
					t.Fatalf("%s's live partitions are %v, registry declares %v", e.Table, got, want)
				}
			}
		})
	}
}

// TestTodo_STORE_001_Fault drives the RED cases the registry exists to
// reject: a live table with no registry entry ("unregistered storage"), a
// registry entry naming a table that does not exist, and two entries
// claiming the same table name ("accidental second authority").
func TestTodo_STORE_001_Fault(t *testing.T) {
	t.Parallel()

	t.Run("an unregistered live table is detected", func(t *testing.T) {
		db := pgtest.New(t)
		db.Exec(t, `CREATE TABLE ad_hoc_leak (id uuid PRIMARY KEY)`)

		reg := loadRegistry(t)
		diff := reg.CompareToLiveSchema(liveBaseTables(t, db))
		if !slices.Contains(diff.UnregisteredInLive, "ad_hoc_leak") {
			t.Fatalf("an ad-hoc table with no registry entry was not flagged; diff = %+v", diff)
		}
	})

	t.Run("a registry entry naming a table that does not exist is detected", func(t *testing.T) {
		db := pgtest.New(t)
		reg := loadRegistry(t)
		reg.Tables = append(reg.Tables, storagedisposition.TableEntry{
			Table:           "never_created",
			Migration:       "nonexistent.sql",
			OwnerPackage:    "nobody",
			Plane:           "DATA",
			DataRole:        storagedisposition.RoleControl,
			RetentionClass:  storagedisposition.RetentionOperational,
			EncryptionClass: storagedisposition.EncryptionPlatformManaged,
		})
		diff := reg.CompareToLiveSchema(liveBaseTables(t, db))
		if !slices.Contains(diff.StaleInRegistry, "never_created") {
			t.Fatalf("a registry entry for a nonexistent table was not flagged; diff = %+v", diff)
		}
	})

	t.Run("two entries claiming the same table is a rejected second authority", func(t *testing.T) {
		reg := loadRegistry(t)
		dup := reg.Tables[0]
		dup.Table = reg.Tables[1].Table
		reg.Tables = append(reg.Tables, dup)
		if err := storagedisposition.Validate(reg); err == nil {
			t.Fatal("a registry with two entries for the same table passed validation")
		}
	})
}

// TestTodo_STORE_001_Recovery proves the registry actually documents how to
// recover a REBUILDABLE table: rebuild_source must name a real, LEDGER-role
// table, which is the RED clause's "rebuild/restore decision" made concrete
// rather than left as a label.
func TestTodo_STORE_001_Recovery(t *testing.T) {
	t.Parallel()
	reg := loadRegistry(t)

	var rebuildableCount int
	for _, e := range reg.Tables {
		if e.RetentionClass != storagedisposition.RetentionRebuildable {
			continue
		}
		rebuildableCount++
		if e.RebuildSource == nil || *e.RebuildSource == "" {
			t.Errorf("%s is REBUILDABLE but declares no rebuild_source", e.Table)
			continue
		}
		source, ok := reg.Lookup(*e.RebuildSource)
		if !ok {
			t.Errorf("%s's rebuild_source %q is not itself a registered table", e.Table, *e.RebuildSource)
			continue
		}
		if source.DataRole != storagedisposition.RoleLedger {
			t.Errorf("%s's rebuild_source %q has data_role %q, want LEDGER", e.Table, *e.RebuildSource, source.DataRole)
		}
	}
	if rebuildableCount == 0 {
		t.Fatal("no table is classified REBUILDABLE; this test would pass vacuously")
	}

	// The validator itself must reject a REBUILDABLE table that forgot its
	// rebuild_source, so the checked-in registry's completeness above is not
	// merely coincidental.
	t.Run("the validator rejects a REBUILDABLE entry with no rebuild_source", func(t *testing.T) {
		reg := loadRegistry(t)
		for i, e := range reg.Tables {
			if e.RetentionClass == storagedisposition.RetentionRebuildable {
				reg.Tables[i].RebuildSource = nil
				break
			}
		}
		if err := storagedisposition.Validate(reg); err == nil {
			t.Fatal("a REBUILDABLE entry with no rebuild_source passed validation")
		}
	})

	t.Run("the validator rejects a rebuild_source that is not LEDGER-role", func(t *testing.T) {
		reg := loadRegistry(t)
		targetIdx := -1
		for i, e := range reg.Tables {
			if e.RetentionClass == storagedisposition.RetentionRebuildable {
				targetIdx = i
				break
			}
		}
		if targetIdx == -1 {
			t.Fatal("fixture requires at least one REBUILDABLE entry")
		}
		// tenant is PERMANENT/CONTROL-role in the checked-in registry: never a
		// valid rebuild_source.
		forged := "tenant"
		reg.Tables[targetIdx].RebuildSource = &forged
		if err := storagedisposition.Validate(reg); err == nil {
			t.Fatal("a rebuild_source pointing at a non-LEDGER-role table passed validation")
		}
	})
}

// requiredFieldMutations returns, for one representative table entry, a set
// of named mutations that each blank exactly one required field.
func requiredFieldMutations(storagedisposition.TableEntry) map[string]func(*storagedisposition.TableEntry) {
	return map[string]func(*storagedisposition.TableEntry){
		"owner_package":   func(e *storagedisposition.TableEntry) { e.OwnerPackage = "" },
		"plane":           func(e *storagedisposition.TableEntry) { e.Plane = "" },
		"migration":       func(e *storagedisposition.TableEntry) { e.Migration = "" },
		"data_role":       func(e *storagedisposition.TableEntry) { e.DataRole = "NOT_A_ROLE" },
		"retention_class": func(e *storagedisposition.TableEntry) { e.RetentionClass = "NOT_A_CLASS" },
		"encryption_class": func(e *storagedisposition.TableEntry) {
			e.EncryptionClass = "NOT_A_CLASS"
		},
	}
}

// TestTodo_STORE_001_Mutation proves each required dimension is load bearing:
// blanking any one of them, independently, is refused by Validate rather than
// silently accepted (the RED clause: "has no truth class, physical system,
// consistency, retention ... decision").
func TestTodo_STORE_001_Mutation(t *testing.T) {
	t.Parallel()
	base := loadRegistry(t)
	if len(base.Tables) == 0 {
		t.Fatal("fixture registry has no tables to mutate")
	}

	for name, mutate := range requiredFieldMutations(base.Tables[0]) {
		t.Run(name, func(t *testing.T) {
			reg := loadRegistry(t)
			mutate(&reg.Tables[0])
			if err := storagedisposition.Validate(reg); err == nil {
				t.Fatalf("blanking %s on one entry was accepted by Validate", name)
			}
		})
	}

	t.Run("tenant scoped with no isolation_package", func(t *testing.T) {
		reg := loadRegistry(t)
		idx := -1
		for i, e := range reg.Tables {
			if e.TenantScoped() {
				idx = i
				break
			}
		}
		if idx == -1 {
			t.Fatal("fixture registry has no tenant-scoped entry to mutate")
		}
		reg.Tables[idx].IsolationPackage = ""
		if err := storagedisposition.Validate(reg); err == nil {
			t.Fatal("a tenant-scoped entry with no isolation_package was accepted by Validate")
		}
	})

	// A well-formed registry (the checked-in one, unmutated) must still pass:
	// this is what makes the mutations above meaningful rather than a
	// validator that rejects everything.
	if err := storagedisposition.Validate(base); err != nil {
		t.Fatalf("the unmutated, checked-in registry failed validation: %v", err)
	}
}
