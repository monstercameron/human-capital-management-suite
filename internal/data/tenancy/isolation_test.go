package tenancy_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// TestTodo_DB_017 proves the two halves of tenant isolation together: a
// connection running as hcmnext_app that never scopes its transaction sees
// nothing from any tenant, and one that scopes to tenant A sees exactly
// tenant A's rows -- never tenant B's, and never even the count of them.
func TestTodo_DB_017(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	tenantA := insertTenant(t, db, "tenant-a")
	tenantB := insertTenant(t, db, "tenant-b")
	insertAuthorityAssignment(t, db, tenantA, "authority:a")
	insertAuthorityAssignment(t, db, tenantB, "authority:b")

	app := appRoleConn(t, db)

	t.Run("missing tenant context defaults closed, not open", func(t *testing.T) {
		if n := countAuthorityAssignments(t, ctx, app); n != 0 {
			t.Fatalf("an unscoped app-role connection saw %d rows, want 0", n)
		}
	})

	t.Run("a scoped transaction sees only its own tenant", func(t *testing.T) {
		tx := scopedTx(t, ctx, app, tenantA)
		defer func() { _ = tx.Rollback(ctx) }()

		if n := countAuthorityAssignments(t, ctx, tx); n != 1 {
			t.Fatalf("tenant A's transaction saw %d rows, want 1", n)
		}

		var ref string
		if err := tx.QueryRow(ctx, `SELECT authority_ref FROM authority_assignment`).Scan(&ref); err != nil {
			t.Fatalf("read the one visible row: %v", err)
		}
		if ref != "authority:a" {
			t.Fatalf("tenant A's transaction read authority_ref %q, want authority:a", ref)
		}

		// Naming tenant B's row by primary key must return "not found", not an
		// error and not the row: RLS filters it out of the result set exactly
		// as if it never existed, which is what closes the existence oracle.
		var count int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM authority_assignment WHERE tenant_id = $1 AND authority_ref = 'authority:b'`,
			tenantB).Scan(&count); err != nil {
			t.Fatalf("query tenant B's row by key: %v", err)
		}
		if count != 0 {
			t.Fatal("tenant A's transaction found tenant B's row by explicit key")
		}
	})

	t.Run("a forged cross-tenant write is refused, not silently rescoped", func(t *testing.T) {
		tx := scopedTx(t, ctx, app, tenantA)
		defer func() { _ = tx.Rollback(ctx) }()

		_, err := tx.Exec(ctx, `
			INSERT INTO authority_assignment (tenant_id, authority_ref, authority_kind, domain_scope, effective_from)
			VALUES ($1, 'authority:forged', 'INTERNAL', 'workforce.compensation', timestamptz '2026-01-01T00:00:00Z')`,
			tenantB)
		if err == nil {
			t.Fatal("a transaction scoped to tenant A inserted a row tagged with tenant B")
		}
	})

	t.Run("scope does not survive past its own transaction", func(t *testing.T) {
		txA := scopedTx(t, ctx, app, tenantA)
		if n := countAuthorityAssignments(t, ctx, txA); n != 1 {
			t.Fatalf("tenant A's transaction saw %d rows, want 1", n)
		}
		if err := txA.Commit(ctx); err != nil {
			t.Fatalf("commit tenant A's transaction: %v", err)
		}

		// The same connection, unscoped again, must default closed.
		if n := countAuthorityAssignments(t, ctx, app); n != 0 {
			t.Fatalf("after commit the connection saw %d rows with no scope set, want 0", n)
		}

		txB := scopedTx(t, ctx, app, tenantB)
		defer func() { _ = txB.Rollback(ctx) }()
		if n := countAuthorityAssignments(t, ctx, txB); n != 1 {
			t.Fatalf("tenant B's transaction saw %d rows, want 1", n)
		}
		var ref string
		if err := txB.QueryRow(ctx, `SELECT authority_ref FROM authority_assignment`).Scan(&ref); err != nil {
			t.Fatalf("read tenant B's row: %v", err)
		}
		if ref != "authority:b" {
			t.Fatalf("the same connection, rescoped to tenant B, read %q, want authority:b", ref)
		}
	})
}

// TestTodo_DB_017_Security drives an adversarial corpus of forged and
// out-of-band contexts against the app role and proves none of them widen
// access: an unregistered tenant id reads as empty rather than erroring in a
// way that would distinguish "no such tenant" from "tenant with no rows", and
// the role itself cannot elevate, disable or inspect the policy that confines
// it.
func TestTodo_DB_017_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	tenantA := insertTenant(t, db, "tenant-a")
	insertAuthorityAssignment(t, db, tenantA, "authority:a")

	app := appRoleConn(t, db)

	t.Run("a well-formed but unregistered tenant id reads as empty, not an error", func(t *testing.T) {
		unknown := uuid.New()
		tx := scopedTx(t, ctx, app, unknown)
		defer func() { _ = tx.Rollback(ctx) }()
		if n := countAuthorityAssignments(t, ctx, tx); n != 0 {
			t.Fatalf("an unregistered tenant id saw %d rows, want 0", n)
		}
	})

	t.Run("the app role cannot bypass row level security by attribute", func(t *testing.T) {
		var rolsuper, rolbypassrls bool
		if err := db.Conn.QueryRow(ctx, `
			SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = $1`,
			tenancy.AppRole).Scan(&rolsuper, &rolbypassrls); err != nil {
			t.Fatalf("read pg_roles for %s: %v", tenancy.AppRole, err)
		}
		if rolsuper {
			t.Fatal("hcmnext_app is a superuser")
		}
		if rolbypassrls {
			t.Fatal("hcmnext_app carries BYPASSRLS")
		}
	})

	t.Run("the app role holds no membership that would let anyone assume the migration identity", func(t *testing.T) {
		// A real, non-superuser bootstrap identity is not something the
		// embedded PostgreSQL server this harness starts can provide (it
		// ships exactly one login role, the superuser used to run
		// migrations), which is why this checks the catalog fact that makes
		// "SET ROLE postgres" refusable in production -- membership -- rather
		// than attempting SET ROLE itself: attempting it over this harness's
		// own superuser session would trivially "succeed" for a reason that
		// has nothing to do with hcmnext_app (a superuser session_user may
		// always SET ROLE to any role, including back to itself), and so
		// would prove nothing about the role's own privilege.
		var isMember bool
		if err := db.Conn.QueryRow(ctx, `SELECT pg_has_role($1, 'postgres', 'member')`,
			tenancy.AppRole).Scan(&isMember); err != nil {
			t.Fatalf("check pg_has_role: %v", err)
		}
		if isMember {
			t.Fatal("hcmnext_app is a member of postgres; any session holding it could SET ROLE postgres")
		}
	})

	t.Run("the app role cannot alter or disable the policy that confines it", func(t *testing.T) {
		conn := appRoleConn(t, db)
		if _, err := conn.Exec(ctx, `ALTER TABLE authority_assignment DISABLE ROW LEVEL SECURITY`); err == nil {
			t.Fatal("hcmnext_app disabled row level security on authority_assignment")
		}
		if _, err := conn.Exec(ctx, `DROP POLICY tenant_isolation ON authority_assignment`); err == nil {
			t.Fatal("hcmnext_app dropped the tenant_isolation policy")
		}
	})

	t.Run("the app role cannot delete a row of any tenant, even its own", func(t *testing.T) {
		conn := appRoleConn(t, db)
		tx := scopedTx(t, ctx, conn, tenantA)
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `DELETE FROM authority_assignment WHERE tenant_id = $1`, tenantA); err == nil {
			t.Fatal("hcmnext_app deleted a row; this data plane grants no DELETE at all")
		}
	})

	t.Run("controlled cleanup functions are usable only for the selected tenant", func(t *testing.T) {
		allowed := []struct {
			name string
			sql  string
			args func(uuid.UUID) []any
		}{
			{name: "worker role assignments", sql: `SELECT hcmnext_replace_worker_role_assignments($1,$2)`, args: func(tenant uuid.UUID) []any { return []any{tenant, "worker:none"} }},
			{name: "conflict scope fence", sql: `SELECT hcmnext_release_conflict_scope_fence($1,$2,$3)`, args: func(tenant uuid.UUID) []any { return []any{tenant, "intent:none", int64(1)} }},
			{name: "job trace links", sql: `SELECT hcmnext_prune_expired_job_trace_links($1,$2,$3)`, args: func(tenant uuid.UUID) []any { return []any{tenant, time.Now().UTC(), 1} }},
			{name: "draft redo history", sql: `SELECT hcmnext_discard_workflow_draft_redo($1,$2,$3)`, args: func(tenant uuid.UUID) []any { return []any{tenant, uuid.New(), int64(1)} }},
			{name: "expired drafts", sql: `SELECT hcmnext_purge_expired_workflow_drafts($1,$2)`, args: func(tenant uuid.UUID) []any { return []any{tenant, time.Now().UTC()} }},
		}

		for _, operation := range allowed {
			t.Run(operation.name, func(t *testing.T) {
				conn := appRoleConn(t, db)
				tx := scopedTx(t, ctx, conn, tenantA)
				var affected int64
				if err := tx.QueryRow(ctx, operation.sql, operation.args(tenantA)...).Scan(&affected); err != nil {
					t.Fatalf("selected-tenant operation failed: %v", err)
				}
				if affected != 0 {
					t.Fatalf("empty selected-tenant operation affected %d rows, want 0", affected)
				}
				if err := tx.Commit(ctx); err != nil {
					t.Fatalf("commit selected-tenant operation: %v", err)
				}

				conn = appRoleConn(t, db)
				tx = scopedTx(t, ctx, conn, tenantA)
				defer func() { _ = tx.Rollback(ctx) }()
				if err := tx.QueryRow(ctx, operation.sql, operation.args(uuid.New())...).Scan(&affected); err == nil {
					t.Fatal("controlled operation accepted a tenant other than the selected tenant")
				}
			})
		}
	})
}

// TestTodo_DB_017_Fault proves the failure modes fail closed rather than
// falling back to an ambiguous or exploitable state: a syntactically invalid
// forged session value is rejected outright (an error, never a match), and
// the Go-layer guard in WithTenant is what stands between a forgetful caller
// and a nil tenant scope.
func TestTodo_DB_017_Fault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	tenantA := insertTenant(t, db, "tenant-a")
	insertAuthorityAssignment(t, db, tenantA, "authority:a")

	app := appRoleConn(t, db)

	t.Run("a malformed session value fails the cast instead of matching anything", func(t *testing.T) {
		tx, err := app.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		// set_config is called directly (not through WithTenant, which only
		// ever emits a well-formed uuid.UUID) to model a forged or corrupted
		// value reaching the session parameter by some other path.
		if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`,
			tenancy.SessionSetting, "'; DROP TABLE authority_assignment; --"); err != nil {
			t.Fatalf("set the forged value: %v", err)
		}

		_, err = tx.Exec(ctx, `SELECT count(*) FROM authority_assignment`)
		if err == nil {
			t.Fatal("a non-UUID forged session value was silently accepted by the policy")
		}
		if !strings.Contains(err.Error(), "uuid") {
			t.Fatalf("expected a uuid cast failure, got: %v", err)
		}

		// The forged value must not have executed as SQL: the table is still
		// there and still holds its one row once the poisoned transaction is
		// abandoned and a clean one takes its place.
		_ = tx.Rollback(ctx)
		tx2 := scopedTx(t, ctx, app, tenantA)
		defer func() { _ = tx2.Rollback(ctx) }()
		if n := countAuthorityAssignments(t, ctx, tx2); n != 1 {
			t.Fatalf("authority_assignment holds %d rows after the forged value attempt, want 1", n)
		}
	})

	t.Run("WithTenant itself refuses the nil tenant before any SQL runs", func(t *testing.T) {
		tx, err := app.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, uuid.Nil); err == nil {
			t.Fatal("WithTenant accepted the nil UUID")
		}
		// Nothing was set, so the transaction is still unscoped and closed.
		if n := countAuthorityAssignments(t, ctx, tx); n != 0 {
			t.Fatalf("an unscoped transaction (after a rejected nil scope) saw %d rows, want 0", n)
		}
	})
}
