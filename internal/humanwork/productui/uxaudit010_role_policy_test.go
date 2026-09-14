package productui

import (
	"strings"
	"testing"
)

func TestTodo_UXAUDIT_010(t *testing.T) {
	view := testView(PageOrganizationVisibility)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "People manager", Active: true}, {ID: "hr", Name: "HR partner", Active: true}}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{{RoleID: "manager", Mode: "ALLOWLIST", OrganizationUnits: []string{"People"}}, {RoleID: "hr", Mode: "ALL"}}
	view.People = []Person{{ID: "one", Name: "One", Team: "People"}, {ID: "two", Name: "Two", Team: "Finance"}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="role-visibility-list"`, `id="organization-visibility-role-manager"`, `id="organization-visibility-role-hr"`, `name="organization-visibility-editors"`, "Current scope", "Proposed scope"} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestTodo_UXAUDIT_010_Integration(t *testing.T) {
	current := OrganizationVisibilityPolicy{Mode: "ALLOWLIST", OrganizationUnits: []string{"People"}}
	proposed := OrganizationVisibilityPolicy{Mode: "DENYLIST", OrganizationUnits: []string{"Finance"}}
	added, removed, modeChanged := organizationVisibilityDiff(current, proposed, []string{"People", "Finance", "Product"})
	if !modeChanged || len(added) != 1 || len(removed) != 0 || added[0] != "Product" {
		t.Fatalf("diff=%v,%v,%v", added, removed, modeChanged)
	}
}

func TestTodo_UXAUDIT_010_Browser(t *testing.T) {
	view := testView(PageOrganizationVisibility)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "People manager", Active: true}}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{{RoleID: "manager", Mode: "ALLOWLIST", OrganizationUnits: []string{"People"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `data-diff-state="unchanged"`) || !strings.Contains(doc, `data-unsaved="false"`) {
		t.Fatal("unchanged policy should omit no-op diff and keep Save disabled")
	}
}

func TestTodo_UXAUDIT_010_Accessibility(t *testing.T) {
	view := testView(PageOrganizationVisibility)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "People manager", Active: true}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `aria-label="Configured visibility by role"`) || !strings.Contains(doc, `aria-live="polite"`) {
		t.Fatal("role selector/status semantics missing")
	}
}

func TestTodo_UXAUDIT_010_Security(t *testing.T) {
	if len(organizationVisibilityPolicyValidation(OrganizationVisibilityPolicy{Mode: "ALLOWLIST", OrganizationUnits: []string{"forged"}}, []string{"People"})) == 0 {
		t.Fatal("unknown unit must not silently validate")
	}
	if len(organizationVisibilityPolicyValidation(OrganizationVisibilityPolicy{Mode: "DENYLIST", OrganizationUnits: []string{"People"}}, []string{"People"})) != 0 {
		t.Fatal("denylist excluding every unit remains a valid policy")
	}
	view := testView(PageOrganizationVisibility)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "People manager", Active: true}}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{{RoleID: "manager", Mode: "ALLOWLIST", OrganizationUnits: []string{"forged"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `disabled`) {
		t.Fatal("invalid policy must keep Save disabled")
	}
}

func TestTodo_UXAUDIT_010_Regression(t *testing.T) {
	view := testView(PageOrganizationVisibility)
	view.Locale = ResolveProductLocale("de-DE")
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "People manager", Active: true}}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{{RoleID: "manager", Mode: "ALL", DataDomains: []string{"People", "withheld-domain"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "withheld-domain") || strings.Contains(doc, "⟦organization_visibility.preview") {
		t.Fatal("withheld data or missing localized preview copy leaked")
	}
	if !strings.Contains(doc, "Beschäftigtenvorschau nicht verfügbar") {
		t.Fatal("localized unavailable preview copy missing")
	}
}
