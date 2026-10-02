package workspace

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// agentUX041FeaturePolicy is a tenant's stored role policy with feature-level
// grants, as roleaccessstore.Bootstrap writes it: the default page grants, and
// each page's features expanded through them. It has no row for Agent
// operations, whose default audience is nobody.
func agentUX041FeaturePolicy(t *testing.T) roleaccess.Snapshot {
	t.Helper()
	pages := roleaccess.DefaultPagePermissions()
	var catalog []roleaccess.FeatureDefinition
	for _, item := range productui.FlattenFeatureDefinitions() {
		catalog = append(catalog, roleaccess.FeatureDefinition{PageID: string(item.Page), FeatureID: string(item.Feature.ID), View: item.Feature.View, Create: item.Feature.Create, Update: item.Feature.Update, Delete: item.Feature.Delete})
	}
	snapshot := roleaccess.Snapshot{PagePermissions: pages, FeaturePermissions: roleaccess.DefaultFeaturePermissions(catalog, pages)}
	if len(snapshot.FeaturePermissions) == 0 {
		t.Fatal("the fixture has no feature-level grants")
	}
	for _, page := range pages {
		if page.PageID == string(productui.PageAgentOperations) {
			t.Fatalf("the stored policy already grants Agent operations to %s; the fixture proves nothing", page.RoleID)
		}
	}
	return snapshot
}

func agentUX041Access(snapshot roleaccess.Snapshot, roles ...string) productAccess {
	return productAccess{
		configured: true, featuresConfigured: len(snapshot.FeaturePermissions) > 0, roles: roles,
		permissions: roleaccess.EffectivePagePermissions(snapshot, roles), features: roleaccess.EffectiveFeaturePermissions(snapshot, roles),
	}
}

// TestTodo_AGENTUX_041: on a tenant with feature-level policy the person who
// owns the agents is given Agent operations as a page and as its content
// feature, to view and nothing more.
func TestTodo_AGENTUX_041(t *testing.T) {
	policy := agentUX041FeaturePolicy(t)
	owner := agentUX041Access(policy, productui.RoleHCMAdmin)
	if !agentsAdmin(owner) || owner.can(productui.PageAgentOperations, roleaccess.ActionView) {
		t.Fatalf("fixture: owner=%v, already admitted=%v", agentsAdmin(owner), owner.can(productui.PageAgentOperations, roleaccess.ActionView))
	}
	projected := projectAgentOperationsAccess(owner)
	if !projected.can(productui.PageAgentOperations, roleaccess.ActionView) {
		t.Fatal("the agent owner is refused Agent operations on a tenant with feature-level policy")
	}
	for _, action := range []string{roleaccess.ActionCreate, roleaccess.ActionUpdate, roleaccess.ActionDelete} {
		if projected.can(productui.PageAgentOperations, action) {
			t.Errorf("the derived grant allows %s; it is view only", action)
		}
	}
	// Exactly one page row and one content-feature row were added, both view only.
	if got := len(projected.permissions) - len(owner.permissions); got != 1 {
		t.Fatalf("page rows added = %d, want 1", got)
	}
	if got := len(projected.features) - len(owner.features); got != 1 {
		t.Fatalf("feature rows added = %d, want 1", got)
	}
	page, feature := projected.permissions[len(projected.permissions)-1], projected.features[len(projected.features)-1]
	if page.PageID != string(productui.PageAgentOperations) || !page.View || page.Create || page.Update || page.Delete {
		t.Errorf("derived page grant = %+v", page)
	}
	if feature.PageID != string(productui.PageAgentOperations) || feature.FeatureID != string(productui.FeatureContent) || !feature.View || feature.Create || feature.Update || feature.Delete {
		t.Errorf("derived feature grant = %+v", feature)
	}
	// Every other page is admitted exactly as before.
	for _, definition := range productui.PageDefinitions() {
		if definition.ID == productui.PageAgentOperations {
			continue
		}
		for _, action := range []string{roleaccess.ActionView, roleaccess.ActionUpdate} {
			if owner.can(definition.ID, action) != projected.can(definition.ID, action) {
				t.Errorf("the derived grant changed %s on %s", action, definition.ID)
			}
		}
	}
	// Applying it again adds nothing.
	again := projectAgentOperationsAccess(projected)
	if len(again.permissions) != len(projected.permissions) || len(again.features) != len(projected.features) {
		t.Errorf("a second projection added rows: %d pages, %d features", len(again.permissions)-len(projected.permissions), len(again.features)-len(projected.features))
	}
	// A tenant without feature-level policy gets the page row alone.
	plain := agentUX041Access(roleaccess.Snapshot{PagePermissions: policy.PagePermissions}, productui.RoleHCMAdmin)
	plainProjected := projectAgentOperationsAccess(plain)
	if !plainProjected.can(productui.PageAgentOperations, roleaccess.ActionView) || len(plainProjected.features) != len(plain.features) {
		t.Errorf("tenant without feature-level policy: admitted=%v, feature rows added=%d", plainProjected.can(productui.PageAgentOperations, roleaccess.ActionView), len(plainProjected.features)-len(plain.features))
	}
}

// TestTodo_AGENTUX_041_Security: someone who cannot change the agent settings
// receives neither the page nor its feature, whatever else they may do.
func TestTodo_AGENTUX_041_Security(t *testing.T) {
	policy := agentUX041FeaturePolicy(t)
	for _, role := range []string{"worker_self", "manager", "comp_admin", "intent_author", "executive"} {
		access := agentUX041Access(policy, role)
		if agentsAdmin(access) {
			// A role the stored policy lets change agent settings is an owner, and
			// is covered by the primary test.
			continue
		}
		projected := projectAgentOperationsAccess(access)
		if projected.can(productui.PageAgentOperations, roleaccess.ActionView) {
			t.Errorf("%s was admitted to Agent operations", role)
		}
		if len(projected.permissions) != len(access.permissions) || len(projected.features) != len(access.features) {
			t.Errorf("%s received derived rows: %d pages, %d features", role, len(projected.permissions)-len(access.permissions), len(projected.features)-len(access.features))
		}
	}
	// Seeing the settings page is not the authority to change it: a role that
	// may only view Chat settings is not an agent owner.
	viewer := productAccess{
		configured: true, featuresConfigured: true, roles: []string{"auditor"},
		permissions: []roleaccess.PagePermission{{RoleID: "auditor", PageID: string(productui.PageChatSettings), View: true}},
		features: []roleaccess.FeaturePermission{
			{RoleID: "auditor", PageID: string(productui.PageChatSettings), FeatureID: string(productui.FeatureContent), View: true},
			{RoleID: "auditor", PageID: string(productui.PageChatSettings), FeatureID: string(productui.FeatureActions), View: true},
		},
	}
	if agentsAdmin(viewer) || projectAgentOperationsAccess(viewer).can(productui.PageAgentOperations, roleaccess.ActionView) {
		t.Error("a person who can only view Chat settings was admitted to Agent operations")
	}
	// The page-level update without the actions feature is not that authority
	// either on a feature-level tenant.
	pageOnly := viewer
	pageOnly.permissions = []roleaccess.PagePermission{{RoleID: "auditor", PageID: string(productui.PageChatSettings), View: true, Update: true}}
	if agentsAdmin(pageOnly) || projectAgentOperationsAccess(pageOnly).can(productui.PageAgentOperations, roleaccess.ActionView) {
		t.Error("a page-level update with no actions feature was treated as agent ownership")
	}
	// No stored policy at all: nothing is derived.
	unconfigured := productAccess{roles: []string{productui.RoleHCMAdmin}}
	if got := projectAgentOperationsAccess(unconfigured); len(got.permissions) != 0 || len(got.features) != 0 {
		t.Errorf("an unconfigured tenant received derived rows: %+v %+v", got.permissions, got.features)
	}
}

// TestTodo_AGENTUX_041_Browser opens Agent operations through the served
// workspace on a tenant whose stored policy has feature-level grants: the
// administrator gets the page and a navigation link to it, the employee gets
// the refusal and no link.
func TestTodo_AGENTUX_041_Browser(t *testing.T) {
	handler, _ := newShellHandler(t, true)
	handler.roleAccess = launcherRoleAccessStore{snapshot: agentUX041FeaturePolicy(t)}
	handler.now = func() time.Time { return time.Now().UTC() }
	handler.agentSettings, handler.agents = &memoryAgentSettings{enabled: map[values.TenantId]bool{shellTenant: true}}, &ownerAgentClient{}
	handler.devPersonas = map[string]DevPersona{}
	for _, persona := range []uxblind122Persona{uxblind122Admin, uxblind122Worker} {
		handler.devPersonas[persona.id] = DevPersona{ID: persona.id, Name: persona.name, WorkerRef: persona.subject, Token: frontendE2EToken(t, persona.subject, persona.roles)}
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	admin, worker := uxblind122SignIn(t, server, uxblind122Admin), uxblind122SignIn(t, server, uxblind122Worker)
	const link = `href="/workspace/app/admin/agents"`
	get := func(client *http.Client, path string) (int, string) {
		t.Helper()
		response, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, string(body)
	}

	status, page := get(admin, "/workspace/app/admin/agents")
	if status != http.StatusOK {
		t.Fatalf("administrator Agent operations = %d on a tenant with feature-level policy: %s", status, page)
	}
	for _, want := range []string{`id="workspace-navigation"`, link, "Agent operations"} {
		if !strings.Contains(page, want) {
			t.Errorf("administrator page missing %q", want)
		}
	}
	if strings.Contains(page, "You do not have access to this page.") {
		t.Error("the administrator's page carries the refusal")
	}
	// What the client receives decides the navigation it draws: the page grant
	// and its content feature, view only.
	config := island(t, page)
	pageGrant, featureGrant := false, false
	for _, permission := range config.PagePermissions {
		if permission.PageID == string(productui.PageAgentOperations) {
			pageGrant = permission.View && !permission.Create && !permission.Update && !permission.Delete
		}
	}
	for _, permission := range config.FeaturePermissions {
		if permission.PageID == string(productui.PageAgentOperations) && permission.FeatureID == string(productui.FeatureContent) {
			featureGrant = permission.View && !permission.Create && !permission.Update && !permission.Delete
		}
	}
	if !pageGrant || !featureGrant {
		t.Errorf("administrator's client grants for Agent operations: page=%v content feature=%v", pageGrant, featureGrant)
	}
	// The link is offered from another page too, not only on the page itself.
	if status, home := get(admin, PathProductHome); status != http.StatusOK || !strings.Contains(home, link) {
		t.Errorf("administrator Home = %d, link offered = %v", status, strings.Contains(home, link))
	}

	status, refusal := get(worker, "/workspace/app/admin/agents")
	if status != http.StatusForbidden || !strings.Contains(refusal, "You do not have access to this page.") {
		t.Fatalf("employee Agent operations = %d: %s", status, refusal)
	}
	if strings.Contains(refusal, `aria-label="Agent operations" class="nav-link"`) {
		t.Error("the employee's refusal page offers the Agent operations link")
	}
	status, home := get(worker, PathProductHome)
	if status != http.StatusOK || strings.Contains(home, link) {
		t.Errorf("employee Home = %d, Agent operations link offered = %v", status, strings.Contains(home, link))
	}
	// The employee's client is told nothing that would let it draw the link.
	employee := island(t, home)
	for _, permission := range employee.PagePermissions {
		if permission.PageID == string(productui.PageAgentOperations) {
			t.Errorf("the employee's client received an Agent operations page grant: %+v", permission)
		}
	}
	for _, permission := range employee.FeaturePermissions {
		if permission.PageID == string(productui.PageAgentOperations) {
			t.Errorf("the employee's client received an Agent operations feature grant: %+v", permission)
		}
	}
}
