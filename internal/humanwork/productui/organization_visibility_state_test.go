package productui

import (
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The GWC reconciler preserves hook state by sibling key. Role editors must
// therefore follow role identity, not their position in the projected list.
func TestOrganizationVisibilityRoleEditorsHaveDistinctStableKeys(t *testing.T) {
	roles := []AccessRole{{ID: "self_service", Name: "Employee self-service", Active: true}, {ID: "comp_admin", Name: "Compensation administrator", Active: true}}
	policies := []OrganizationVisibilityPolicy{{RoleID: "self_service", Mode: "ALL"}, {RoleID: "comp_admin", Mode: "OWN_UNIT"}}
	keys := func(input []AccessRole) map[string]bool {
		t.Helper()
		page := OrganizationVisibilityPage(OrganizationVisibilityPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Roles: input, Policies: policies})
		if len(page.Children) != 2 {
			t.Fatalf("page has %d children, want intro and role list", len(page.Children))
		}
		list, ok := page.Children[1].(ui.Node)
		if !ok {
			t.Fatal("role list is not a component node")
		}
		out := map[string]bool{}
		for _, child := range list.Children {
			editor, ok := child.(ui.Node)
			if !ok {
				t.Fatal("role editor is not a component node")
			}
			key, _ := editor.Props["key"].(string)
			if key == "" {
				key = editor.Key
			}
			if key == "" || out[key] {
				t.Fatalf("missing or duplicate editor reconciliation key %q", key)
			}
			out[key] = true
		}
		return out
	}
	first := keys(roles)
	second := keys([]AccessRole{roles[1], roles[0]})
	for _, role := range roles {
		if !first[role.ID] || !second[role.ID] {
			t.Fatalf("role %q lost its stable reconciliation identity", role.ID)
		}
	}
}
