package productui

import (
	"strings"
	"testing"
)

func TestOrganizationVisibilityAdminPageExposesEveryPolicyModeAndUnit(t *testing.T) {
	view := testView(PageOrganizationVisibility)
	view.AccessRoles = []AccessRole{{ID: "hr_partner", Name: "HR partner", Active: true}}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{{Version: 3, RoleID: "hr_partner", Mode: "ALLOWLIST", OrganizationUnits: []string{"People Operations"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Configured visibility by role", "HR partner", "Everyone", "Their own organization unit", "Only selected units", "All except selected units", "People Operations", `id="organization-visibility-status-hr_partner"`, `name="organization-visibility-mode-hr_partner"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("organization visibility page missing %q", want)
		}
	}
}

func TestOrganizationVisibilityDiffReportsEffectiveUnitChanges(t *testing.T) {
	current := OrganizationVisibilityPolicy{Mode: "ALLOWLIST", OrganizationUnits: []string{"People"}}
	proposed := OrganizationVisibilityPolicy{Mode: "ALLOWLIST", OrganizationUnits: []string{"People", "Product"}}
	added, removed, modeChanged := organizationVisibilityDiff(current, proposed, []string{"People", "Product", "Finance"})
	if modeChanged || len(removed) != 0 || len(added) != 1 || added[0] != "Product" {
		t.Fatalf("diff = added %v removed %v modeChanged %v", added, removed, modeChanged)
	}

	view := testView(PageOrganizationVisibility)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "People manager", Active: true}}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{{RoleID: "manager", Mode: "ALLOWLIST", OrganizationUnits: []string{"People"}}}
	view.People = []Person{{ID: "worker-1", Name: "Priya", Team: "People"}, {ID: "worker-2", Name: "Sam", Team: "Product"}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "Preview changes") || strings.Contains(doc, `data-diff-state="unchanged"`) || !strings.Contains(doc, `data-unsaved="false"`) {
		t.Fatal("unchanged editor should omit redundant diff and retain sticky save state")
	}
}
