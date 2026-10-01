package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaCatalogRoleStore struct {
	roleaccess.Store
	snapshot roleaccess.Snapshot
	err      error
	calls    int
}

func (s *personaCatalogRoleStore) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	s.calls++
	return s.snapshot, s.err
}

func TestTodo_AGENTP_018_RoleAuthorizer(t *testing.T) {
	ctx, principal := personaCatalogRoleContext(t)
	view := roleaccess.PagePermission{RoleID: "hcm_admin", PageID: "persona-admin", View: true}
	for _, tc := range []struct {
		name      string
		principal *trust.Principal
		tenant    values.TenantId
		snapshot  roleaccess.Snapshot
		loadErr   error
		wantErr   bool
	}{
		{name: "explicit grant", principal: principal, tenant: "tenant-a", snapshot: roleaccess.Snapshot{PagePermissions: []roleaccess.PagePermission{view}}},
		{name: "default admin roles have no implicit grant", principal: principal, tenant: "tenant-a", snapshot: roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}, wantErr: true},
		{name: "missing view grant", principal: principal, tenant: "tenant-a", snapshot: roleaccess.Snapshot{PagePermissions: []roleaccess.PagePermission{{RoleID: "hcm_admin", PageID: "persona-admin", Update: true}}}, wantErr: true},
		{name: "tenant mismatch", principal: principal, tenant: "tenant-b", snapshot: roleaccess.Snapshot{PagePermissions: []roleaccess.PagePermission{view}}, wantErr: true},
		{name: "load error", principal: principal, tenant: "tenant-a", loadErr: errors.New("store unavailable"), wantErr: true},
		{name: "missing principal", tenant: "tenant-a", snapshot: roleaccess.Snapshot{PagePermissions: []roleaccess.PagePermission{view}}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &personaCatalogRoleStore{snapshot: tc.snapshot, err: tc.loadErr}
			err := (PersonaCatalogRoleAuthorizer{Roles: store}).AuthorizePersonaCatalog(ctx, tc.principal, tc.tenant)
			if (err != nil) != tc.wantErr {
				t.Fatalf("AuthorizePersonaCatalog() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.principal == nil || tc.tenant != principal.Tenant() {
				if store.calls != 0 {
					t.Fatalf("role store called %d times for invalid identity", store.calls)
				}
				return
			}
			if store.calls != 1 {
				t.Fatalf("role store called %d times, want one", store.calls)
			}
		})
	}
}

func personaCatalogRoleContext(t *testing.T) (context.Context, *trust.Principal) {
	t.Helper()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "user-a", Roles: []string{"hcm_admin"}, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "cred:sha256:persona-catalog"})
	if err != nil {
		t.Fatal(err)
	}
	return context.Background(), p
}
