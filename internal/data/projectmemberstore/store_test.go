package projectmemberstore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	projectactivity "github.com/monstercameron/human-capital-management-suite/internal/domains/projectactivity"
	"github.com/pressly/goose/v3"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_PM_MembershipListingFiltersBeforeLimit(t *testing.T) {
	members, projects, _ := fixture(t)
	ctx := context.Background()
	if err := projects.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		for _, id := range []string{"project-b-hidden", "project-z-visible"} {
			if _, err := tx.Exec(ctx, `INSERT INTO project(tenant_id,id,owner_id,name,project_timezone) VALUES('tenant-a',$1,'someone-else',$1,'UTC')`, id); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := members.Invite(ctx, "tenant-a", "project-z-visible", "someone-else", "viewer", projectaccess.RoleViewer, 0, 1, "invite-viewer"); err != nil {
		t.Fatal(err)
	}
	if err := members.AcceptInvitation(ctx, "tenant-a", "project-z-visible", "viewer", 2, "accept-viewer"); err != nil {
		t.Fatal(err)
	}
	rows, err := members.ListAuthorizedProjects(ctx, "tenant-a", "viewer", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "project-z-visible" {
		t.Fatalf("bounded authorized page = %+v, want the visible project despite earlier hidden IDs", rows)
	}
}

func fixture(t *testing.T) (*Store, *projectstore.Store, string) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	fsys, err := fs.Sub(projectstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, fsys, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	projects, err := projectstore.New(context.Background(), projectstore.Config{DSN: db.URL, CoreDSN: "postgres://other:secret@127.0.0.1:5432/postgres?sslmode=disable", Schema: db.Schema, MaxConns: 4, MinConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(projects.Close)
	members, err := New(projects)
	if err != nil {
		t.Fatal(err)
	}
	if err := projects.RunTenantTx(context.Background(), "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO project(tenant_id,id,owner_id,name,project_timezone) VALUES('tenant-a','project-a','owner','Project','UTC')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return members, projects, db.Schema
}

func TestTodo_PM_006_Integration(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	if err := s.Authorize(ctx, "tenant-a", "project-a", "owner", projectaccess.ReadProject); err != nil {
		t.Fatalf("owner access: %v", err)
	}
	if err := s.Invite(ctx, "tenant-a", "project-a", "owner", "member", projectaccess.RoleViewer, 0, 1, "invite-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Invite(ctx, "tenant-a", "project-a", "owner", "member", projectaccess.RoleViewer, 0, 1, "invite-1"); err != nil {
		t.Fatalf("invite retry: %v", err)
	}
	if err := s.Invite(ctx, "tenant-a", "project-a", "owner", "other", projectaccess.RoleViewer, 0, 1, "invite-1"); !errors.Is(err, projectstore.ErrIdempotencyConflict) {
		t.Fatalf("changed invite replay err=%v", err)
	}
	if err := s.Invite(ctx, "tenant-a", "project-a", "owner", "stale", projectaccess.RoleViewer, 0, 1, "invite-stale"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale invite revision err=%v", err)
	}
	if err := s.Authorize(ctx, "tenant-a", "project-a", "member", projectaccess.ReadProject); !errors.Is(err, projectaccess.ErrUnauthorized) {
		t.Fatalf("invitation granted access: %v", err)
	}
	snap, err := s.GetSnapshot(ctx, "tenant-a", "project-a")
	if err != nil || snap.Revision != 2 {
		t.Fatalf("snapshot %#v err=%v", snap, err)
	}
	if err := s.AcceptInvitation(ctx, "tenant-a", "project-a", "member", 2, "accept-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", "project-a", "member", projectaccess.ReadProject); err != nil {
		t.Fatalf("accepted access: %v", err)
	}
	if err := s.ChangeRole(ctx, "tenant-a", "project-a", "owner", "member", projectaccess.RoleContributor, 3, "role-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", "project-a", "member", projectaccess.CreateTask); err != nil {
		t.Fatalf("role change not current: %v", err)
	}
	if err := s.Revoke(ctx, "tenant-a", "project-a", "owner", "member", 4, "revoke-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", "project-a", "member", projectaccess.ReadProject); !errors.Is(err, projectaccess.ErrUnauthorized) {
		t.Fatalf("revoked member retained access: %v", err)
	}
	if err := s.Revoke(ctx, "tenant-a", "project-a", "owner", "member", 4, "revoke-1"); err != nil {
		t.Fatalf("revoke retry: %v", err)
	}
	if err := s.Revoke(ctx, "tenant-a", "project-a", "owner", "owner", 5, "last-owner"); !errors.Is(err, projectaccess.ErrOwnerRequired) {
		t.Fatalf("last owner revoke err=%v", err)
	}
	if err := s.Invite(ctx, "tenant-a", "project-a", "owner", "other", projectaccess.RoleViewer, 0, 5, "invite-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptInvitation(ctx, "tenant-a", "project-a", "other", 6, "accept-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.TransferOwnership(ctx, "tenant-a", "project-a", "owner", "other", nil, time.Now(), 7, "transfer-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", "project-a", "other", projectaccess.ManageMembers); err != nil {
		t.Fatalf("new owner access: %v", err)
	}
	if err := s.Authorize(ctx, "tenant-a", "project-a", "owner", projectaccess.ManageMembers); err != nil {
		t.Fatalf("former owner manager access: %v", err)
	}
	var eventType string
	var payload []byte
	if err := s.projects.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT event_type,payload FROM project_membership_event WHERE tenant_id=$1 AND project_id=$2 AND event_type='membership.ownership_transferred'`, "tenant-a", "project-a").Scan(&eventType, &payload)
	}); err != nil || eventType != "membership.ownership_transferred" || !strings.Contains(string(payload), `"from": "owner"`) || !strings.Contains(string(payload), `"to": "other"`) {
		t.Fatalf("ownership audit type=%q payload=%s err=%v", eventType, payload, err)
	}
	snap, err = s.GetSnapshot(ctx, "tenant-a", "project-a")
	if err != nil || snap.Revision != 8 {
		t.Fatalf("post transfer snapshot %#v err=%v", snap, err)
	}
}

func TestTodo_PM_006_Security(t *testing.T) {
	s, projects, schema := fixture(t)
	ctx := context.Background()
	if err := s.Invite(ctx, "tenant-a", "project-a", "owner", "member", projectaccess.RoleViewer, 0, 1, "invite-sec"); err != nil {
		t.Fatal(err)
	}
	if err := projects.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT c.relname,c.relrowsecurity,c.relforcerowsecurity,COALESCE(pg_get_expr(p.polqual,p.polrelid),''),COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),'') FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_policy p ON p.polrelid=c.oid WHERE n.nspname=$1 AND c.relname IN ('project_membership_clock','project_membership','project_membership_event','project_membership_idempotency') ORDER BY c.relname`, schema)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var name, using, check string
			var enabled, forced bool
			if err := rows.Scan(&name, &enabled, &forced, &using, &check); err != nil {
				return err
			}
			count++
			if !enabled || !forced || !strings.Contains(using, "hcmnext.tenant_id") || !strings.Contains(check, "hcmnext.tenant_id") {
				return fmt.Errorf("table %s RLS enabled=%t forced=%t using=%q check=%q", name, enabled, forced, using, check)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count != 4 {
			return fmt.Errorf("found %d membership tables with RLS, want 4", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	role := createRLSRole(t, projects, schema)
	err := runAsTenantRole(ctx, projects, role, "tenant-a", func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_membership WHERE tenant_id='tenant-a'`).Scan(&count); err != nil {
			return err
		}
		if count != 2 {
			return fmt.Errorf("tenant-a sees %d membership rows, want owner plus invitee", count)
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_membership(tenant_id,project_id,user_id,role,state,revision) VALUES('tenant-b','project-b','intruder','VIEWER','ACTIVE',1)`)
		return err
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "row-level security") {
		t.Fatalf("cross-tenant membership write err=%v", err)
	}
	for _, stmt := range []string{`UPDATE project_membership_event SET event_type='forged' WHERE tenant_id='tenant-a'`, `DELETE FROM project_membership_event WHERE tenant_id='tenant-a'`} {
		err = runAsTenantRole(ctx, projects, role, "tenant-a", func(tx dbport.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "append-only") {
			t.Errorf("event mutation %q err=%v", stmt, err)
		}
	}
	// A tenant setting alone cannot make another tenant's rows visible.
	err = runAsTenantRole(ctx, projects, role, "tenant-b", func(tx dbport.Tx) error {
		var count int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM project_membership WHERE tenant_id='tenant-a'`).Scan(&count)
		if err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("foreign membership rows visible: %d", count)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PM_007_Recovery(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	if err := s.Invite(ctx, "tenant-a", "project-a", "owner", "successor", projectaccess.RoleManager, 0, 1, "recovery-invite"); err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptInvitation(ctx, "tenant-a", "project-a", "successor", 2, "recovery-accept"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	grant := &projectaccess.RecoveryGrant{Tenant: "tenant-a", Project: "project-a", Actor: "recovery-operator", Purpose: "owner unavailable; approved succession", Approver: "security-approver", StartsAt: now.Add(-time.Minute), EndsAt: now.Add(time.Minute)}
	if err := s.TransferOwnership(ctx, "tenant-a", "project-a", "recovery-operator", "successor", grant, now, 3, "recovery-transfer"); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", "project-a", "successor", projectaccess.ManageMembers); err != nil {
		t.Fatalf("successor not active owner: %v", err)
	}
	var payload []byte
	if err := s.projects.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT payload FROM project_membership_event WHERE tenant_id=$1 AND project_id=$2 AND event_type='membership.ownership_transferred'`, "tenant-a", "project-a").Scan(&payload)
	}); err != nil {
		t.Fatal(err)
	}
	for _, evidence := range []string{`"purpose": "owner unavailable; approved succession"`, `"approver": "security-approver"`, `"recovery": true`} {
		if !strings.Contains(string(payload), evidence) {
			t.Errorf("recovery audit missing %s in %s", evidence, payload)
		}
	}
	if err := s.TransferOwnership(ctx, "tenant-a", "project-a", "unapproved", "owner", &projectaccess.RecoveryGrant{Tenant: "tenant-a", Project: "project-a", Actor: "unapproved", Purpose: "", Approver: "approver", StartsAt: now.Add(-time.Minute), EndsAt: now.Add(time.Minute)}, now, 4, "recovery-transfer-bad"); !errors.Is(err, projectaccess.ErrRecoveryNotAuthorized) {
		t.Fatalf("unapproved recovery transfer err=%v", err)
	}
}

func createRLSRole(t *testing.T, s *projectstore.Store, schema string) string {
	t.Helper()
	role := "projectmember_rls_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	qrole := `"` + role + `"`
	qschema := `"` + strings.ReplaceAll(schema, `"`, `""`) + `"`
	if err := s.RunTx(context.Background(), func(tx dbport.Tx) error {
		if _, err := tx.Exec(context.Background(), `CREATE ROLE `+qrole+` NOLOGIN NOSUPERUSER NOBYPASSRLS`); err != nil {
			return err
		}
		if _, err := tx.Exec(context.Background(), `GRANT USAGE ON SCHEMA `+qschema+` TO `+qrole); err != nil {
			return err
		}
		_, err := tx.Exec(context.Background(), `GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA `+qschema+` TO `+qrole)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = s.RunTx(context.Background(), func(tx dbport.Tx) error {
			if _, err := tx.Exec(context.Background(), `DROP OWNED BY `+qrole); err != nil {
				return err
			}
			_, err := tx.Exec(context.Background(), `DROP ROLE `+qrole)
			return err
		})
	})
	return role
}

func runAsTenantRole(ctx context.Context, s *projectstore.Store, role, tenant string, fn func(dbport.Tx) error) error {
	return s.RunTx(ctx, func(tx dbport.Tx) error {
		qrole := `"` + role + `"`
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE `+qrole); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id',$1,true)`, tenant); err != nil {
			return err
		}
		return fn(tx)
	})
}

func TestTodo_PM_024_MentionResolutionUsesCurrentVisibleMembership(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	if err := s.Invite(ctx, "tenant-a", "project-a", "owner", "visible-user", projectaccess.RoleViewer, 0, 1, "invite-visible"); err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptInvitation(ctx, "tenant-a", "project-a", "visible-user", 2, "accept-visible"); err != nil {
		t.Fatal(err)
	}
	access := projectactivity.AccessRequest{Principal: projectactivity.Principal{TenantID: "tenant-a", SubjectID: "owner"}, ProjectID: "project-a", TaskID: "task-a", Action: projectactivity.ActionRead}
	got, err := s.ResolveVisibleMentions(ctx, access, []string{"visible-user", "missing-user"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SubjectID != "visible-user" || got[0].DisplayName != "visible-user" {
		t.Fatalf("visible mention resolution = %#v", got)
	}
	if err := s.Revoke(ctx, "tenant-a", "project-a", "owner", "visible-user", 3, "revoke-visible"); err != nil {
		t.Fatal(err)
	}
	got, err = s.ResolveVisibleMentions(ctx, access, []string{"visible-user", "missing-user"})
	if err != nil || len(got) != 0 {
		t.Fatalf("revoked and unknown handles differ from empty result: %#v err=%v", got, err)
	}
	access.Principal.SubjectID = "outsider"
	got, err = s.ResolveVisibleMentions(ctx, access, []string{"visible-user", "missing-user"})
	if !errors.Is(err, projectaccess.ErrUnauthorized) || len(got) != 0 {
		t.Fatalf("nonmember resolution = %#v err=%v", got, err)
	}
}
