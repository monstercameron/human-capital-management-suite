package productui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestUXBLIND_023(t *testing.T) {
	view := testView(PageOrganizationVisibility)
	view.AccessRoles = []AccessRole{
		{ID: "manager", Name: "Manager", Active: true},
		{ID: "support", Name: "Support", Active: true},
	}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{
		{RoleID: "manager", Mode: "OWN_UNIT", OrganizationUnits: []string{"Engineering"}},
		{RoleID: "support", Mode: "ALL", OrganizationUnits: []string{"Platform"}},
	}
	view.People = []Person{{ID: "worker-1", Name: "Worker One", Team: "Engineering"}, {ID: "worker-2", Name: "Worker Two", Team: "Platform"}}

	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}

	for _, roleID := range []string{"manager", "support"} {
		editor := findVisibilityEditor(root, roleID)
		if editor == nil {
			t.Fatalf("missing visibility editor for %q", roleID)
		}
		fieldset := findClassToken(editor, "organization-visibility-units")
		if fieldset != nil {
			t.Errorf("irrelevant unit fieldset shown for %q", roleID)
		}
		if findClassToken(editor, "mode-only") == nil {
			t.Errorf("mode-only role %q did not use the balanced single-column form", roleID)
		}
		if !hasUXAttr(editor, "name", "organization-visibility-editors") {
			t.Errorf("%q is not in the exclusive native role editor group", roleID)
		}
		if !strings.Contains(doc, `name="organization-visibility-mode-`+roleID+`"`) {
			t.Errorf("%q mode inputs were unmounted", roleID)
		}
	}

	for _, want := range []string{
		"Resolved from each viewer's organization unit",
		"Worker preview unavailable",
	} {
		if !strings.Contains(textContent(root), want) {
			t.Errorf("visibility document missing explanation %q", want)
		}
	}
	if strings.Contains(web064RoleScope(t, root, "manager", "organization-scope-current"), "No enumerable units in scope") || findClassToken(root, "organization-visibility-role-selector") != nil || findClassToken(findVisibilityEditor(root, "manager"), "data-domain-scope") != nil {
		t.Fatal("viewer-relative scope must not be presented as empty, and unsupported role/domain selector must not render")
	}
}

func TestUXBLIND_023_AllowAndDenyModesKeepUnitSelectionAvailable(t *testing.T) {
	view := testView(PageOrganizationVisibility)
	view.AccessRoles = []AccessRole{
		{ID: "allow", Name: "Allow", Active: true},
		{ID: "deny", Name: "Deny", Active: true},
	}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{
		{RoleID: "allow", Mode: "ALLOWLIST"},
		{RoleID: "deny", Mode: "DENYLIST"},
	}
	view.People = []Person{{ID: "worker-1", Name: "Worker One", Team: "Engineering"}}

	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	for _, roleID := range []string{"allow", "deny"} {
		fieldset := findClassToken(findVisibilityEditor(root, roleID), "organization-visibility-units")
		if fieldset == nil {
			t.Fatalf("missing unit fieldset for %q", roleID)
		}
		if hasUXAttr(fieldset, "disabled", "") || hasUXAttr(fieldset, "data-selection-state", "inactive") {
			t.Errorf("unit fieldset for %q is inactive in a selectable mode", roleID)
		}
		if findClassToken(findVisibilityEditor(root, roleID), "mode-only") != nil {
			t.Errorf("selected-unit role %q lost the two-column editor", roleID)
		}
	}
}

func TestUXAUDIT010_ModeOnlyFormUsesBalancedResponsiveGrid(t *testing.T) {
	css := Stylesheet()
	for _, selector := range []string{
		".organization-visibility-form.mode-only",
		".organization-visibility-form.mode-only .organization-visibility-modes",
		"@media (max-width:620px)",
	} {
		if !strings.Contains(css, selector) {
			t.Fatalf("mode-only editor stylesheet misses %q", selector)
		}
	}
}

func findVisibilityEditor(root *xhtml.Node, roleID string) *xhtml.Node {
	var found *xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found != nil {
			return
		}
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "data-role-id" && attr.Val == roleID {
					found = node
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func hasUXAttr(node *xhtml.Node, key, value string) bool {
	if node == nil {
		return false
	}
	for _, attr := range node.Attr {
		if attr.Key == key && (value == "" || attr.Val == value) {
			return true
		}
	}
	return false
}
