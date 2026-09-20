package productui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func rev09301OverrideDoc(t *testing.T, locale string) (string, *xhtml.Node) {
	t.Helper()
	view := testView(PageOrganizationVisibility)
	if locale != "" {
		view.Locale = ResolveProductLocale(locale)
	}
	view.AccessRoles = []AccessRole{
		{ID: "hcm_admin", Name: "HCM administrator", Active: true},
		{ID: "comp_admin", Name: "Compensation administrator", Active: true},
		{ID: "manager", Name: "People manager", Active: true},
	}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{{RoleID: "comp_admin", Mode: "OWN_UNIT"}}
	view.People = []Person{{ID: "worker-1", Name: "Worker One", Team: "Engineering"}, {ID: "worker-2", Name: "Worker Two", Team: "Sales"}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	return doc, root
}

func rev09301Summary(editor *xhtml.Node) string {
	var summary *xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if summary == nil && n.Type == xhtml.ElementNode && n.Data == "summary" {
			summary = n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(editor)
	if summary == nil {
		return ""
	}
	return textContent(summary)
}

// TestTodo_REV_093_01_AdministratorEditor: administrator roles show the
// scope the directory enforces (everyone) and the override explanation, and
// nothing else: no inert choices, scope panel, preview action or save row
// for a policy that has no effect (UXLIVE-014/017).
func TestTodo_REV_093_01_AdministratorEditor(t *testing.T) {
	_, root := rev09301OverrideDoc(t, "en-US")
	for _, roleID := range []string{"hcm_admin", "comp_admin"} {
		editor := findVisibilityEditor(root, roleID)
		if editor == nil {
			t.Fatalf("missing editor for %s", roleID)
		}
		text := textContent(editor)
		if summary := rev09301Summary(editor); !strings.Contains(summary, "Everyone (administrator override)") || strings.Contains(summary, "Their own organization unit") {
			t.Errorf("%s summary = %q", roleID, summary)
		}
		for _, want := range []string{"Administrator override", "does not narrow administrators"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s editor missing %q", roleID, want)
			}
		}
		for _, inert := range []string{"Their own organization unit", "Current scope", "Proposed scope", "Preview access", "Worker preview unavailable", "Save"} {
			if strings.Contains(text, inert) {
				t.Errorf("%s editor still renders inert %q:\n%s", roleID, inert, text)
			}
		}
		for _, class := range []string{"organization-visibility-modes", "organization-visibility-scope", "organization-visibility-actions", "role-access-preview", "organization-visibility-preview-unavailable"} {
			if findClassToken(editor, class) != nil {
				t.Errorf("%s editor still renders .%s", roleID, class)
			}
		}
		var walk func(*xhtml.Node)
		walk = func(n *xhtml.Node) {
			if n.Type == xhtml.ElementNode && (n.Data == "input" || n.Data == "button") {
				t.Errorf("%s editor renders a <%s> control", roleID, n.Data)
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(editor)
	}
	// A non-administrator role keeps its ordinary, editable policy.
	manager := findVisibilityEditor(root, "manager")
	if summary := rev09301Summary(manager); strings.Contains(summary, "administrator override") {
		t.Errorf("manager summary claims the override: %q", summary)
	}
	if findClassToken(manager, "organization-visibility-actions") == nil || findClassToken(manager, "organization-visibility-modes") == nil {
		t.Error("manager editor lost its choices or save row")
	}
	if !IsAdministratorVisibilityRole(" HCM_Admin ") || IsAdministratorVisibilityRole("manager") {
		t.Error("administrator role predicate drifted from roleaccess.IsAdministratorRole")
	}
}

// TestTodo_REV_093_01_AdministratorEditorI18N keeps the override copy in
// German and Arabic.
func TestTodo_REV_093_01_AdministratorEditorI18N(t *testing.T) {
	for locale, want := range map[string]string{"de-DE": "Alle (Administratorzugriff)", "ar": "الجميع (تجاوز المسؤول)"} {
		doc, _ := rev09301OverrideDoc(t, locale)
		if !strings.Contains(doc, want) {
			t.Errorf("%s document missing %q", locale, want)
		}
	}
	if css := roleAccessPreviewStylesheet(); !strings.Contains(css, ".organization-visibility-override") {
		t.Error("override notice styles missing")
	}
}

// TestTodo_REV_093_01_RolesDefinitionOverride: the Roles page definition
// describes administrator scope as the override too.
func TestTodo_REV_093_01_RolesDefinitionOverride(t *testing.T) {
	props := RolesPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}}
	for roleID, want := range map[string]string{"comp_admin": "Everyone (administrator override)", "manager": "Their own organization unit"} {
		markup := rev09301Render(t, roleDefinitionDisclosure(props, AccessRole{ID: roleID, Name: roleID, Active: true}, OrganizationVisibilityPolicy{RoleID: roleID, Mode: "OWN_UNIT"}, 1))
		if !strings.Contains(markup, want) {
			t.Errorf("%s definition missing %q:\n%s", roleID, want, markup)
		}
	}
}
