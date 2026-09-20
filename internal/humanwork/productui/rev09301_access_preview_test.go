package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func rev09301PreviewView() View {
	view := testView(PageOrganizationVisibility)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "Manager", Active: true}, {ID: "comp_admin", Name: "Compensation administrator", Active: true}}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{{RoleID: "manager", Mode: "ALLOWLIST", OrganizationUnits: []string{"Engineering"}}}
	view.People = []Person{{ID: "worker-1", Name: "Worker One", Team: "Engineering"}, {ID: "worker-2", Name: "Worker Two", Team: "Sales"}}
	return view
}

func rev09301Panel(locale string) roleAccessPreviewPanelProps {
	return roleAccessPreviewPanelProps{
		Page:  OrganizationVisibilityPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale(locale)}, OnPreview: func(OrganizationVisibilityPolicy, func(RoleAccessPreview, error)) {}},
		Role:  AccessRole{ID: "manager", Name: "Manager", Active: true},
		Draft: OrganizationVisibilityPolicy{RoleID: "manager", Mode: "ALLOWLIST", OrganizationUnits: []string{"Engineering", "Sales"}},
	}
}

func rev09301Ready(props roleAccessPreviewPanelProps, preview RoleAccessPreview) roleAccessPreviewState {
	return roleAccessPreviewState{Status: roleAccessPreviewReady, Seq: 1, Requested: organizationVisibilityPolicySnapshot(props.Draft), Preview: preview}
}

func rev09301Render(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// TestTodo_REV_093_01_Editor proves the visibility editor consumes the
// server preview: with the preview call connected, each role editor offers
// it inline instead of the old "unavailable" notice, and a resolved answer
// is laid out as current versus proposed with its role sources.
func TestTodo_REV_093_01_Editor(t *testing.T) {
	view := rev09301PreviewView()
	var asked []OrganizationVisibilityPolicy
	view.PreviewRoleVisibility = func(policy OrganizationVisibilityPolicy, done func(RoleAccessPreview, error)) {
		asked = append(asked, policy)
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "Worker preview unavailable") {
		t.Fatal("editor still shows the unavailable notice with a connected preview")
	}
	for _, want := range []string{`class="role-access-preview"`, "Access preview", "Preview access", `data-preview-state="idle"`, `aria-labelledby="role-access-preview-title-manager"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("editor document missing %q", want)
		}
	}
	if strings.Contains(doc, `role-access-preview-title-comp_admin`) {
		t.Error("administrator role offers a preview for a policy that has no effect")
	}
	if len(asked) != 0 {
		t.Fatalf("preview requested before the administrator asked: %v", asked)
	}

	props := rev09301Panel("en-US")
	preview := RoleAccessPreview{
		RoleID: "manager", RoleName: "Manager", ExplicitRoles: []string{"manager"}, InheritedRoles: []string{"hr_partner"},
		CurrentMode: "ALLOWLIST", CurrentUnits: []string{"Engineering"}, ProposedMode: "ALLOWLIST", ProposedUnits: []string{"Engineering", "Sales"},
		AddedUnits: []string{"Sales"}, HolderCount: 3, OverriddenHolderCount: 1,
	}
	ready := rev09301Render(t, roleAccessPreviewNode(props, rev09301Ready(props, preview), ui.Handler{}))
	for _, want := range []string{"Current (saved)", "Proposed (this draft)", "Newly visible:", "Sales", "hr_partner", "Other roles its people also hold", "People with this role saved", ">3<", "Of those, also administrators (see every unit)", "Preview again", `data-preview-state="ready"`, `role="status"`} {
		if !strings.Contains(ready, want) {
			t.Errorf("ready preview missing %q:\n%s", want, ready)
		}
	}
	if strings.Contains(ready, "You changed this draft") {
		t.Error("fresh preview marked stale")
	}
	// Editing the draft after the answer marks the answer stale.
	changed := props
	changed.Draft.Mode = "ALL"
	if stale := rev09301Render(t, roleAccessPreviewNode(changed, rev09301Ready(props, preview), ui.Handler{})); !strings.Contains(stale, "You changed this draft after the preview.") {
		t.Errorf("stale preview not flagged:\n%s", stale)
	}
	// A removed unit and a relative scope are both named, not implied.
	narrowing := preview
	narrowing.AddedUnits, narrowing.RemovedUnits, narrowing.ProposedUnits, narrowing.ProposedRelative, narrowing.ProposedMode = nil, []string{"Engineering"}, nil, true, "OWN_UNIT"
	narrowed := rev09301Render(t, roleAccessPreviewNode(props, rev09301Ready(props, narrowing), ui.Handler{}))
	for _, want := range []string{"No longer visible:", "Each person sees their own unit. Nobody has a saved assignment to this role yet."} {
		if !strings.Contains(narrowed, want) {
			t.Errorf("narrowing preview missing %q", want)
		}
	}
}

// TestTodo_REV_093_01_EditorStates covers the loading, failure and
// unavailable states and the invalid-draft guard.
func TestTodo_REV_093_01_EditorStates(t *testing.T) {
	props := rev09301Panel("en-US")
	loading := rev09301Render(t, roleAccessPreviewNode(props, roleAccessPreviewState{Status: roleAccessPreviewLoading}, ui.Handler{}))
	if !strings.Contains(loading, "Checking what this role would reveal") || !strings.Contains(loading, `aria-busy="true"`) || !strings.Contains(loading, "disabled") {
		t.Errorf("loading state:\n%s", loading)
	}
	failed := rev09301Render(t, roleAccessPreviewNode(props, roleAccessPreviewState{Status: roleAccessPreviewFailed}, ui.Handler{}))
	if !strings.Contains(failed, "load the access preview. Try again.") || !strings.Contains(failed, `role="alert"`) || strings.Contains(failed, `role="status"`) || !strings.Contains(failed, "Preview again") {
		t.Errorf("failed state:\n%s", failed)
	}
	invalid := props
	invalid.Invalid = true
	if idle := rev09301Render(t, roleAccessPreviewNode(invalid, roleAccessPreviewState{Status: roleAccessPreviewIdle}, ui.Handler{})); !strings.Contains(idle, "disabled") {
		t.Errorf("invalid draft can still request a preview:\n%s", idle)
	}
	unavailable := props
	unavailable.Page.OnPreview = nil
	if markup := rev09301Render(t, ui.CreateElement(RoleAccessPreviewPanel, unavailable)); !strings.Contains(markup, "Worker preview unavailable") {
		t.Errorf("disconnected preview lost its unavailable notice:\n%s", markup)
	}
	if markup := rev09301Render(t, ui.CreateElement(RoleAccessPreviewPanel, props)); !strings.Contains(markup, `data-preview-state="idle"`) {
		t.Errorf("connected panel did not start idle:\n%s", markup)
	}
	if css := roleAccessPreviewStylesheet(); !strings.Contains(css, ".role-access-preview-compare") || !strings.Contains(css, "minmax(min(100%, 14rem), 1fr)") {
		t.Errorf("preview stylesheet lost its phone-safe compare grid:\n%s", css)
	}
	if !strings.Contains(Stylesheet(), ".role-access-preview-compare") {
		t.Error("preview styles are not part of the product stylesheet")
	}
}

// TestTodo_REV_093_01_Security proves the administrator override is shown
// as unrestricted on both sides and that the preview renders no worker.
func TestTodo_REV_093_01_Security(t *testing.T) {
	props := rev09301Panel("en-US")
	props.Role = AccessRole{ID: "comp_admin", Name: "Compensation administrator", Active: true}
	props.Draft = OrganizationVisibilityPolicy{RoleID: "comp_admin", Mode: "ALLOWLIST", OrganizationUnits: []string{"Sales"}}
	all := []string{"Engineering", "Sales"}
	preview := RoleAccessPreview{RoleID: "comp_admin", ExplicitRoles: []string{"comp_admin"}, AdministratorOverride: true,
		CurrentMode: "OWN_UNIT", CurrentUnits: all, ProposedMode: "ALLOWLIST", ProposedUnits: all, HolderCount: 1, OverriddenHolderCount: 1}
	markup := rev09301Render(t, roleAccessPreviewNode(props, rev09301Ready(props, preview), ui.Handler{}))
	if !strings.Contains(markup, "Administrator override") || !strings.Contains(markup, "does not narrow their access") {
		t.Fatalf("override not explained:\n%s", markup)
	}
	if strings.Count(markup, "Every organization unit") != 2 {
		t.Fatalf("both sides must read as every organization unit:\n%s", markup)
	}
	if strings.Contains(markup, "Only selected units") || strings.Contains(markup, "No longer visible") {
		t.Fatalf("override rendered as a narrowing:\n%s", markup)
	}
	if strings.Contains(markup, "Of those, also administrators") {
		t.Fatal("override role repeats its own administrator count")
	}
	for _, leak := range []string{"worker-1", "worker-2", "Worker One"} {
		if strings.Contains(markup, leak) {
			t.Fatalf("preview rendered worker record %q", leak)
		}
	}
}

// TestTodo_REV_093_01_I18N renders the preview in German and Arabic.
func TestTodo_REV_093_01_I18N(t *testing.T) {
	for locale, want := range map[string]string{"de-DE": "Zugriffsvorschau", "ar": "معاينة الوصول", "fr-FR": "Access preview"} {
		props := rev09301Panel(locale)
		markup := rev09301Render(t, roleAccessPreviewNode(props, roleAccessPreviewState{Status: roleAccessPreviewIdle}, ui.Handler{}))
		if !strings.Contains(markup, want) {
			t.Errorf("%s preview missing %q:\n%s", locale, want, markup)
		}
	}
	for language, copy := range roleAccessPreviewCopy {
		for key := range roleAccessPreviewCopy["en"] {
			if strings.TrimSpace(copy[key]) == "" {
				t.Errorf("%s copy missing %q", language, key)
			}
		}
	}
}
