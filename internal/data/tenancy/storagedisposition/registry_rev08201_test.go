package storagedisposition_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
)

// assertLiveTenantRLS checks the final catalog state, including later ALTERs
// and policy changes.
func assertLiveTenantRLS(t *testing.T, db *pgtest.DB, table, tenantColumn string) {
	t.Helper()
	if err := checkLiveTenantRLS(db, table, tenantColumn); err != nil {
		t.Fatal(err)
	}
}

func checkLiveTenantRLS(db *pgtest.DB, table, tenantColumn string) error {
	var enabled, forced bool
	if err := db.Conn.QueryRow(context.Background(), `
		SELECT c.relrowsecurity, c.relforcerowsecurity
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relname = $1 AND c.relkind IN ('r', 'p')`, table,
	).Scan(&enabled, &forced); err != nil {
		return fmt.Errorf("read RLS flags for %s: %w", table, err)
	}
	if !enabled || !forced {
		return fmt.Errorf("%s RLS flags are enabled=%v forced=%v; want both true", table, enabled, forced)
	}
	var qual, withCheck, permissive, command string
	err := db.Conn.QueryRow(context.Background(), `
		SELECT qual, with_check, permissive, cmd FROM pg_policies
		WHERE schemaname = current_schema() AND tablename = $1 AND policyname = 'tenant_isolation'`, table,
	).Scan(&qual, &withCheck, &permissive, &command)
	if err != nil {
		return fmt.Errorf("%s has no readable tenant_isolation policy: %w", table, err)
	}
	if permissive != "PERMISSIVE" || command != "ALL" {
		return fmt.Errorf("%s tenant_isolation policy is %s for %s; want PERMISSIVE for ALL commands", table, permissive, command)
	}
	if !isTenantEqualityPolicy(qual, tenantColumn) || !isTenantEqualityPolicy(withCheck, tenantColumn) {
		return fmt.Errorf("%s tenant_isolation policy is not a tenant equality predicate in both USING and WITH CHECK for %q: qual=%q with_check=%q",
			table, tenantColumn, qual, withCheck)
	}
	// PostgreSQL combines permissive policies with OR. Any additional
	// permissive policy can therefore bypass tenant_isolation even when that
	// policy itself is exact. This registry currently has no reviewed
	// exceptions, so require tenant_isolation to be the only permissive policy.
	var extraPermissive int
	if err := db.Conn.QueryRow(context.Background(), `
		SELECT count(*) FROM pg_policies
		WHERE schemaname = current_schema() AND tablename = $1
		  AND permissive = 'PERMISSIVE' AND policyname <> 'tenant_isolation'`, table,
	).Scan(&extraPermissive); err != nil {
		return fmt.Errorf("count additional permissive policies on %s: %w", table, err)
	}
	if extraPermissive != 0 {
		return fmt.Errorf("%s has %d additional permissive row security policies that can OR around tenant_isolation", table, extraPermissive)
	}
	return nil
}

// isTenantEqualityPolicy recognizes the two equivalent policies used by the
// migration tree. Comparing the complete normalized expression is deliberate:
// a lexical mention of tenant_id/current_setting is insufficient because an
// OR such as "tenant_id = ... OR true" grants cross-tenant access. PostgreSQL
// deparses catalog expressions with redundant parentheses and text casts, so
// those presentation details are removed before comparison.
func isTenantEqualityPolicy(expr, tenantColumn string) bool {
	got := normalizePolicyExpression(expr)
	nullIfForm := normalizePolicyExpression(tenantColumn + ` = NULLIF(current_setting('app.tenant_id', true), '')::uuid`)
	directForm := normalizePolicyExpression(tenantColumn + ` = current_setting('app.tenant_id', true)::uuid`)
	// PostgreSQL inserts an explicit cast when a tenant column's declared
	// storage type is text, even though the policy compares UUID identities.
	castNullIfForm := normalizePolicyExpression(tenantColumn + `::uuid = NULLIF(current_setting('app.tenant_id', true), '')::uuid`)
	castDirectForm := normalizePolicyExpression(tenantColumn + `::uuid = current_setting('app.tenant_id', true)::uuid`)
	return got == nullIfForm || got == directForm || got == castNullIfForm || got == castDirectForm
}

func normalizePolicyExpression(expr string) string {
	expr = strings.ToLower(expr)
	expr = strings.ReplaceAll(expr, `::text`, "")
	expr = strings.ReplaceAll(expr, `"`, "")
	expr = strings.Map(func(r rune) rune {
		if r == '(' || r == ')' || r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, expr)
	return expr
}

func TestTodo_REV_082_01(t *testing.T) {
	t.Parallel()
	reg := loadRegistry(t)
	if err := storagedisposition.Validate(reg); err != nil {
		t.Fatalf("checked-in registry fails validation: %v", err)
	}
	db := pgtest.New(t)
	for _, e := range reg.Tables {
		if e.TenantScoped() {
			t.Run(e.Table, func(t *testing.T) { assertLiveTenantRLS(t, db, e.Table, *e.TenantScopingColumn) })
		}
	}
}

func TestTodo_REV_082_01_Integration(t *testing.T) {
	t.Parallel()
	reg := loadRegistry(t)
	db := pgtest.New(t)
	count := 0
	for _, e := range reg.Tables {
		if e.TenantScoped() {
			count++
			assertLiveTenantRLS(t, db, e.Table, *e.TenantScopingColumn)
		}
	}
	if count == 0 {
		t.Fatal("registry contains no tenant-scoped tables")
	}
}

func TestTodo_REV_082_01_Security(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	db.Exec(t, `CREATE TABLE rev08201_scratch (tenant_id uuid NOT NULL, payload text NOT NULL)`)
	db.Exec(t, `ALTER TABLE rev08201_scratch ENABLE ROW LEVEL SECURITY`)
	db.Exec(t, `ALTER TABLE rev08201_scratch FORCE ROW LEVEL SECURITY`)
	db.Exec(t, `CREATE POLICY tenant_isolation ON rev08201_scratch USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)`)
	assertLiveTenantRLS(t, db, "rev08201_scratch", "tenant_id")

	// Exercise SELECT and both write paths as the application role. The
	// migration connection bypasses RLS, so this proves the policy's live
	// behavior from the same role production uses.
	tenantA, tenantB := "00000000-0000-0000-0000-00000000000a", "00000000-0000-0000-0000-00000000000b"
	db.Exec(t, `GRANT SELECT, INSERT, UPDATE ON rev08201_scratch TO hcmnext_app`)
	db.Exec(t, `INSERT INTO rev08201_scratch VALUES ($1, 'seed')`, tenantA)
	app := db.NewConn(t)
	ctx := context.Background()
	if _, err := app.Exec(ctx, `SET ROLE hcmnext_app`); err != nil {
		t.Fatalf("SET ROLE hcmnext_app: %v", err)
	}
	if _, err := app.Exec(ctx, `SELECT set_config('app.tenant_id', $1, false)`, tenantA); err != nil {
		t.Fatalf("set tenant A: %v", err)
	}
	var visible int
	if err := app.QueryRow(ctx, `SELECT count(*) FROM rev08201_scratch`).Scan(&visible); err != nil {
		t.Fatalf("count same-tenant rows: %v", err)
	}
	if visible != 1 {
		t.Fatalf("tenant A cannot see its own row: count=%d", visible)
	}
	if _, err := app.Exec(ctx, `INSERT INTO rev08201_scratch VALUES ($1, 'foreign')`, tenantB); err == nil {
		t.Fatal("tenant A inserted a row owned by tenant B")
	}
	if _, err := app.Exec(ctx, `UPDATE rev08201_scratch SET tenant_id = $1 WHERE tenant_id = $2`, tenantB, tenantA); err == nil {
		t.Fatal("tenant A changed its row to tenant B")
	}
	if _, err := app.Exec(ctx, `SELECT set_config('app.tenant_id', $1, false)`, tenantB); err != nil {
		t.Fatalf("set tenant B: %v", err)
	}
	if err := app.QueryRow(ctx, `SELECT count(*) FROM rev08201_scratch`).Scan(&visible); err != nil {
		t.Fatalf("count rows as tenant B: %v", err)
	}
	if visible != 0 {
		t.Fatalf("tenant B can see %d rows belonging to tenant A", visible)
	}
	db.Exec(t, `ALTER TABLE rev08201_scratch DISABLE ROW LEVEL SECURITY`)
	assertRLSRejected(t, db, "rev08201_scratch", "tenant_id")
}

func TestTodo_REV_082_01_Fault(t *testing.T) {
	t.Parallel()
	t.Run("missing policy is detected", func(t *testing.T) {
		db := pgtest.New(t)
		db.Exec(t, `CREATE TABLE rev08201_no_policy (tenant_id uuid NOT NULL)`)
		db.Exec(t, `ALTER TABLE rev08201_no_policy ENABLE ROW LEVEL SECURITY`)
		db.Exec(t, `ALTER TABLE rev08201_no_policy FORCE ROW LEVEL SECURITY`)
		assertRLSRejected(t, db, "rev08201_no_policy", "tenant_id")
	})
	t.Run("wrong policy column is detected", func(t *testing.T) {
		db := pgtest.New(t)
		db.Exec(t, `CREATE TABLE rev08201_wrong_column (tenant_id uuid NOT NULL, other_id uuid NOT NULL)`)
		db.Exec(t, `ALTER TABLE rev08201_wrong_column ENABLE ROW LEVEL SECURITY`)
		db.Exec(t, `ALTER TABLE rev08201_wrong_column FORCE ROW LEVEL SECURITY`)
		db.Exec(t, `CREATE POLICY tenant_isolation ON rev08201_wrong_column USING (other_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (other_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)`)
		assertRLSRejected(t, db, "rev08201_wrong_column", "tenant_id")
	})
	t.Run("permissive OR policy is detected", func(t *testing.T) {
		db := pgtest.New(t)
		db.Exec(t, `CREATE TABLE rev08201_permissive_or (tenant_id uuid NOT NULL)`)
		db.Exec(t, `ALTER TABLE rev08201_permissive_or ENABLE ROW LEVEL SECURITY`)
		db.Exec(t, `ALTER TABLE rev08201_permissive_or FORCE ROW LEVEL SECURITY`)
		db.Exec(t, `CREATE POLICY tenant_isolation ON rev08201_permissive_or USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid OR true) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid OR true)`)
		assertRLSRejected(t, db, "rev08201_permissive_or", "tenant_id")
	})
	t.Run("additional permissive policy is detected", func(t *testing.T) {
		db := pgtest.New(t)
		db.Exec(t, `CREATE TABLE rev08201_extra_permissive (tenant_id uuid NOT NULL)`)
		db.Exec(t, `ALTER TABLE rev08201_extra_permissive ENABLE ROW LEVEL SECURITY`)
		db.Exec(t, `ALTER TABLE rev08201_extra_permissive FORCE ROW LEVEL SECURITY`)
		db.Exec(t, `CREATE POLICY tenant_isolation ON rev08201_extra_permissive USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)`)
		db.Exec(t, `CREATE POLICY alternate_open ON rev08201_extra_permissive USING (true) WITH CHECK (true)`)
		assertRLSRejected(t, db, "rev08201_extra_permissive", "tenant_id")
	})
}

func assertRLSRejected(t *testing.T, db *pgtest.DB, table, tenantColumn string) {
	t.Helper()
	if err := checkLiveTenantRLS(db, table, tenantColumn); err == nil {
		t.Errorf("RLS checker accepted broken table %s.%s", table, tenantColumn)
	}
}

func TestTodo_REV_082_01_Golden(t *testing.T) {
	t.Parallel()
	reg := loadRegistry(t)
	var lines []string
	for _, e := range reg.Tables {
		if e.TenantScoped() {
			lines = append(lines, fmt.Sprintf("%s %s", e.Table, *e.TenantScopingColumn))
		}
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(registryPath(t))))
	wantPath := filepath.Join(root, "internal", "data", "tenancy", "storagedisposition", "testdata", "rev08201_tenant_columns.golden")
	want, err := os.ReadFile(filepath.Clean(wantPath))
	if err != nil {
		t.Fatalf("read tenant-column golden %s: %v", wantPath, err)
	}
	got := strings.Join(lines, "\n") + "\n"
	if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("tenant-scoped registry rows drifted from golden:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestTodo_REV_082_01_Recovery(t *testing.T) {
	t.Parallel()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).Up(context.Background()); err != nil {
		t.Fatalf("migrate empty schema to latest: %v", err)
	}
	reg, err := storagedisposition.Load(registryPath(t))
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	for _, e := range reg.Tables {
		if e.TenantScoped() {
			t.Run(e.Table, func(t *testing.T) { assertLiveTenantRLS(t, db, e.Table, *e.TenantScopingColumn) })
		}
	}
}
