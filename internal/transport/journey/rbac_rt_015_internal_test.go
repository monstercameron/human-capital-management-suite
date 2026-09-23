package journey

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// RBAC-RT-015 fixtures. The registry (internal/humanwork/productui) is the
// only authority for which page/feature pairs exist: rows for an undeclared
// pair must grant nothing, so a page is judged at feature level exactly when
// its registry entry declares the feature, never based on which rows a
// tenant happens to hold.
type rt15Store struct {
	snapshot roleaccess.Snapshot
	err      error
}

func (s *rt15Store) Bootstrap(context.Context, values.TenantId, string) error { return nil }

func (s *rt15Store) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return s.snapshot, s.err
}

func (s *rt15Store) SaveRole(_ context.Context, _ values.TenantId, _ string, value roleaccess.Role) (roleaccess.Role, error) {
	return value, nil
}

func (s *rt15Store) SaveAssignment(_ context.Context, _ values.TenantId, _ string, value roleaccess.Assignment) (roleaccess.Assignment, error) {
	return value, nil
}

func (s *rt15Store) SaveVisibility(_ context.Context, _ values.TenantId, _, _ string, value roleaccess.VisibilityPolicy) (roleaccess.VisibilityPolicy, error) {
	return value, nil
}

func (s *rt15Store) SavePagePermission(_ context.Context, _ values.TenantId, _ string, value roleaccess.PagePermission) (roleaccess.PagePermission, error) {
	return value, nil
}

func (s *rt15Store) SaveFeaturePermission(_ context.Context, _ values.TenantId, _ string, value roleaccess.FeaturePermission) (roleaccess.FeaturePermission, error) {
	return value, nil
}

func rt15Principal(t *testing.T) *trust.Principal {
	t.Helper()
	now := time.Now()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               values.TenantId("tenant-rt15"),
		OrganizationScopeID:  "org-rt15",
		Subject:              "worker-rt15",
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                []string{"worker_self"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session-rt15",
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "digest:rt15",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return principal
}

// rt15ForgedSnapshot grants worker_self full rights on an undeclared page
// and an undeclared feature of a real page. Before the fix the gate honors
// these rows; after it the registry overrules them.
func rt15ForgedSnapshot() roleaccess.Snapshot {
	return roleaccess.Snapshot{
		Roles: roleaccess.DefaultRoles(),
		PagePermissions: []roleaccess.PagePermission{
			{Version: 1, RoleID: "worker_self", PageID: "shadowconsole", View: true, Create: true, Update: true, Delete: true},
			{Version: 1, RoleID: "worker_self", PageID: "journeys", View: true, Create: true, Update: true},
		},
		FeaturePermissions: []roleaccess.FeaturePermission{
			{Version: 1, RoleID: "worker_self", PageID: "shadowconsole", FeatureID: "shadow_feature", View: true, Create: true, Update: true, Delete: true},
			{Version: 1, RoleID: "worker_self", PageID: "journeys", FeatureID: "shadow_feature", View: true, Create: true, Update: true},
		},
	}
}

func rt15SeededSnapshot() roleaccess.Snapshot {
	return roleaccess.Snapshot{
		Roles: roleaccess.DefaultRoles(),
		PagePermissions: []roleaccess.PagePermission{
			{Version: 1, RoleID: "worker_self", PageID: "journeys", View: true, Create: true, Update: true},
		},
		FeaturePermissions: []roleaccess.FeaturePermission{
			{Version: 1, RoleID: "worker_self", PageID: "journeys", FeatureID: "content", View: true},
			{Version: 1, RoleID: "worker_self", PageID: "journeys", FeatureID: "promotion_request", View: true, Create: true, Update: true},
		},
	}
}

// TestTodo_RBAC_RT_015 is the PRIMARY matrix entry: whether a call is judged
// at feature level comes from the page's registry features, never from the
// tenant's rows. Rows for an undeclared page or an undeclared feature deny
// even when they grant the caller's roles every operation; declared pairs
// keep working exactly as before.
func TestTodo_RBAC_RT_015(t *testing.T) {
	ctx := context.Background()
	principal := rt15Principal(t)
	inv := &transport.Invocation{}

	t.Run("rows for an undeclared page grant nothing", func(t *testing.T) {
		srv := &server{deps: Dependencies{RoleAccess: &rt15Store{snapshot: rt15ForgedSnapshot()}}}
		if err := srv.requireFeatureAction(ctx, principal, inv, "shadowconsole", "shadow_feature", roleaccess.ActionView); err == nil {
			t.Fatal("gate allowed an undeclared page backed by full-grant rows")
		}
		if err := srv.requirePageAction(ctx, principal, inv, "shadowconsole", roleaccess.ActionView); err == nil {
			t.Fatal("page gate allowed an undeclared page backed by full-grant rows")
		}
	})

	t.Run("rows for an undeclared feature of a declared page grant nothing", func(t *testing.T) {
		srv := &server{deps: Dependencies{RoleAccess: &rt15Store{snapshot: rt15ForgedSnapshot()}}}
		if err := srv.requireFeatureAction(ctx, principal, inv, "journeys", "shadow_feature", roleaccess.ActionView); err == nil {
			t.Fatal("gate allowed an undeclared feature backed by full-grant rows")
		}
	})

	t.Run("declared pairs keep their grants", func(t *testing.T) {
		srv := &server{deps: Dependencies{RoleAccess: &rt15Store{snapshot: rt15SeededSnapshot()}}}
		if err := srv.requirePageAction(ctx, principal, inv, "journeys", roleaccess.ActionView); err != nil {
			t.Fatalf("declared journeys content view refused: %v", err)
		}
		if err := srv.requireFeatureAction(ctx, principal, inv, "journeys", "promotion_request", roleaccess.ActionCreate); err != nil {
			t.Fatalf("declared journeys promotion_request create refused: %v", err)
		}
		if err := srv.requirePageAction(ctx, principal, inv, "people", roleaccess.ActionView); err == nil {
			t.Fatal("ungranted people view allowed")
		}
	})
}

// TestTodo_RBAC_RT_015_Security proves forged authority cannot widen the
// gate: invented pages and features deny for every action including deletes,
// case disguises of declared pairs still resolve through the registry, and a
// missing or failing store denies rather than admitting.
func TestTodo_RBAC_RT_015_Security(t *testing.T) {
	ctx := context.Background()
	principal := rt15Principal(t)
	inv := &transport.Invocation{}

	t.Run("forged rows deny every action", func(t *testing.T) {
		srv := &server{deps: Dependencies{RoleAccess: &rt15Store{snapshot: rt15ForgedSnapshot()}}}
		for _, action := range []string{roleaccess.ActionView, roleaccess.ActionCreate, roleaccess.ActionUpdate, roleaccess.ActionDelete} {
			if err := srv.requireFeatureAction(ctx, principal, inv, "shadowconsole", "shadow_feature", action); err == nil {
				t.Fatalf("forged pair allowed action %q", action)
			}
		}
	})

	t.Run("registry matching is case-insensitive but not inventive", func(t *testing.T) {
		srv := &server{deps: Dependencies{RoleAccess: &rt15Store{snapshot: rt15SeededSnapshot()}}}
		if err := srv.requireFeatureAction(ctx, principal, inv, "Journeys", "Content", roleaccess.ActionView); err != nil {
			t.Fatalf("declared pair refused under different case: %v", err)
		}
		if err := srv.requireFeatureAction(ctx, principal, inv, "journeys ", " content", roleaccess.ActionView); err != nil {
			t.Fatalf("declared pair refused with padding: %v", err)
		}
		if err := srv.requireFeatureAction(ctx, principal, inv, "journeyscontent", "content", roleaccess.ActionView); err == nil {
			t.Fatal("invented page allowed by gluing declared names")
		}
	})

	t.Run("missing store denies at the gate", func(t *testing.T) {
		srv := &server{deps: Dependencies{}}
		if err := srv.requireFeatureAction(ctx, principal, inv, "journeys", "content", roleaccess.ActionView); err == nil {
			t.Fatal("gate allowed with no role-access store")
		}
	})
}
