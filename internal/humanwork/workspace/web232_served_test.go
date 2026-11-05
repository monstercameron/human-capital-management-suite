package workspace

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func web232Handler(t *testing.T, snapshot preferences.Snapshot, roles roleaccess.Snapshot) (*Handler, string) {
	t.Helper()
	h, token := newShellHandler(t, false)
	h.preferences = uxblindTPreferenceStore{snapshot: snapshot}
	h.roleAccess = launcherRoleAccessStore{snapshot: roles}
	return h, token
}

func web232Snapshot() preferences.Snapshot {
	snapshot := preferences.DefaultSnapshot()
	snapshot.Theme.Version = 3
	snapshot.OrganizationVisibility.Version = 2
	snapshot.OrganizationVisibility.Mode = preferences.OrganizationVisibilityAll
	return snapshot
}

// TestTodo_WEB_232_ServedProjectionIsLive proves the configuration center
// page is backed by live governed configuration: the served island carries
// the areas with their stored versions and admin destinations.
func TestTodo_WEB_232_ServedProjectionIsLive(t *testing.T) {
	grants := []roleaccess.PagePermission{
		{RoleID: "comp_admin", PageID: string(productui.PageConfigurationCenter), View: true},
		{RoleID: "comp_admin", PageID: string(productui.PageAppearance), View: true},
		{RoleID: "comp_admin", PageID: string(productui.PageOrganizationVisibility), View: true},
		{RoleID: "comp_admin", PageID: "home", View: true},
	}
	h, token := web232Handler(t, web232Snapshot(), roleaccess.Snapshot{PagePermissions: grants})
	recorder := uxblindTRequest(t, h, token, productui.Path(productui.PageConfigurationCenter))
	if recorder.Code != 200 {
		t.Fatalf("GET configuration center = %d: %s", recorder.Code, recorder.Body.String())
	}
	doc := recorder.Body.String()
	for _, want := range []string{
		`"configuration_center":{"Ready":true`,
		`"ID":"appearance"`, `"Version":"v3"`, `"Href":"/workspace/app/admin/appearance"`,
		`"ID":"organization-visibility"`, `"Version":"v2"`, `"Href":"/workspace/app/admin/organization-visibility"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("served island misses %q", want)
		}
	}
}

// TestTodo_WEB_232_ServedProjectionFiltersByGrant proves areas the viewer
// may not open are omitted while the projection stays an authorized,
// ready, empty result instead of collapsing to unavailable.
func TestTodo_WEB_232_ServedProjectionFiltersByGrant(t *testing.T) {
	grants := []roleaccess.PagePermission{
		{RoleID: "comp_admin", PageID: string(productui.PageConfigurationCenter), View: true},
		{RoleID: "comp_admin", PageID: "home", View: true},
	}
	h, token := web232Handler(t, web232Snapshot(), roleaccess.Snapshot{PagePermissions: grants})
	recorder := uxblindTRequest(t, h, token, productui.Path(productui.PageConfigurationCenter))
	if recorder.Code != 200 {
		t.Fatalf("GET configuration center = %d: %s", recorder.Code, recorder.Body.String())
	}
	doc := recorder.Body.String()
	if !strings.Contains(doc, `"configuration_center":{"Ready":true`) {
		t.Fatal("filtered projection is not a ready result")
	}
	for _, leaked := range []string{`"ID":"appearance"`, `"ID":"organization-visibility"`, "/workspace/app/admin/appearance"} {
		if strings.Contains(doc, leaked) {
			t.Errorf("served island leaks ungranted area %q", leaked)
		}
	}
}
