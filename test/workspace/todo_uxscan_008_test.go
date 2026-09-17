package workspace_test

// UXSCAN-008 INTEGRATION + BROWSER: explain effective access before an
// administrator changes role visibility.
//
// The positive server preview (explicit versus inherited roles, effective
// scope, withheld facts, and a representative-worker current-versus-proposed
// comparison) is qualified by the AccessPreview contract in
// tools/uxqual/productclient, which carries only server-resolved role, unit
// and scope names and can never return a worker record. That contract stays
// green while the role store learns credential inheritance and governed
// scope simulation (see the UXSCAN-008 progress note in planning/todos.md).
//
// What this file proves against the composed cell cmd/hcmnext serves is the
// half of GREEN that must already hold: the Roles and visibility-editor
// routes never return forbidden records to an unauthorized viewer, the
// editor renders its role-scoped editing surface (no per-worker preview
// decided in the browser), and the served editor document passes the
// repository's DOM qualification checks.

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
)

const (
	uxscan008RolesPath      = workspace.PathProductPrefix + "admin/roles"
	uxscan008VisibilityPath = workspace.PathProductPrefix + "admin/organization-visibility"
)

func uxscan008Get(t *testing.T, client *http.Client, serverURL, path string) (int, string) {
	t.Helper()
	res, err := client.Get(serverURL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(body)
}

// TestTodo_UXSCAN_008_Integration proves the authorization discipline the
// effective-access preview depends on: administrators reach the Roles and
// visibility-editor routes, a viewer without that grant is refused before
// any record is rendered, and the editor is scoped to roles rather than to
// per-worker records decided in the browser.
func TestTodo_UXSCAN_008_Integration(t *testing.T) {
	serverURL := uxaudit014Cell(t)
	admin := uxaudit014Login(t, serverURL, "admin")
	viewer := uxaudit014Login(t, serverURL, "individual-contributor")

	rolesStatus, rolesBody := uxscan008Get(t, admin, serverURL, uxscan008RolesPath)
	if rolesStatus != http.StatusOK {
		t.Fatalf("GET %s as admin = %d, want 200", uxscan008RolesPath, rolesStatus)
	}
	editorStatus, editorBody := uxscan008Get(t, admin, serverURL, uxscan008VisibilityPath)
	if editorStatus != http.StatusOK {
		t.Fatalf("GET %s as admin = %d, want 200", uxscan008VisibilityPath, editorStatus)
	}

	for _, path := range []string{uxscan008RolesPath, uxscan008VisibilityPath} {
		if status, _ := uxscan008Get(t, viewer, serverURL, path); status != http.StatusForbidden {
			t.Errorf("GET %s as individual-contributor = %d, want 403 (no forbidden records)", path, status)
		}
	}

	// The editor renders its role-scoped editing surface for the
	// administrator (role sections appear once durable roles exist; this
	// cell seeds none, so the test pins the surface rather than a row).
	if !strings.Contains(editorBody, "role-visibility-editor") {
		t.Error("visibility editor lost its editing surface: no role-visibility-editor in served page")
	}
	if !strings.Contains(editorBody, "Organization visibility") {
		t.Error("visibility editor lost its page identity: no Organization visibility title in served page")
	}
	_ = rolesBody
}

// TestTodo_UXSCAN_008_Browser runs the served visibility-editor document
// through the repository's DOM qualification checks: the page that will
// host the effective-access preview must already meet the keyboard,
// screen-reader, reflow and masking bar before the preview lands.
func TestTodo_UXSCAN_008_Browser(t *testing.T) {
	serverURL := uxaudit014Cell(t)
	admin := uxaudit014Login(t, serverURL, "admin")
	status, body := uxscan008Get(t, admin, serverURL, uxscan008VisibilityPath)
	if status != http.StatusOK {
		t.Fatalf("GET %s as admin = %d, want 200", uxscan008VisibilityPath, status)
	}
	// RunDocumentChecks leaves Buildable unset by design (it is computed
	// separately via BuildNativePackage/BuildWasmPackage), so the empty-named
	// slot is skipped exactly as the workspace locale suite does.
	result := qual.RunDocumentChecks("uxscan-008-visibility-editor", body, workspace.MaskedActionNeedles())
	for _, criterion := range result.All() {
		if criterion.Name != "" && !criterion.Pass {
			t.Errorf("%s: FAIL %s", criterion.Name, criterion.Detail)
		}
	}
}
