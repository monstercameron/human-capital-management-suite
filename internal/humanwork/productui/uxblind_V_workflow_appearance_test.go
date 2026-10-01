package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func uxblindVWorkflowProps(locale string) WorkflowDesignerPageProps {
	return WorkflowDesignerPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale(locale)},
		BaseHref:  Path(PageWorkflowDesigner),
		Catalog: []WorkflowCatalogItem{
			{WorkflowID: "published.workflow", Name: "Published workflow", SemanticVersion: "1.0.0", Status: "ACTIVE"},
			{WorkflowID: "draft.workflow", DraftID: "draft-42", Name: "Field work order", SemanticVersion: "1.0.0", Status: "DRAFT"},
		},
		Palette:   []WorkflowPaletteItem{{ID: "template.hire", Name: "New employee hire", Kind: "TEMPLATE", Version: 1, Status: "ACTIVE"}},
		CanCreate: true,
		OnCreate:  func(WorkflowDraftCreateRequest) {},
	}
}

func TestTodo_UXBLIND_070(t *testing.T) {
	markup, err := ui.RenderToString(WorkflowDesignerPage(uxblindVWorkflowProps("en-US")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `id="workflow-list-published-heading"`) || !strings.Contains(markup, `id="workflow-list-drafts-heading"`) {
		t.Fatalf("workflow catalog is not grouped by publication state:\n%s", markup)
	}
	if !strings.Contains(markup, "Field work order") {
		t.Fatalf("draft workflow is missing from the catalog:\n%s", markup)
	}
	if !strings.Contains(markup, `id="workflow-list-published-heading">Published workflows<span aria-label="1"`) || !strings.Contains(markup, `id="workflow-list-drafts-heading">Draft workflows<span aria-label="1"`) {
		t.Fatalf("publication group counts are incorrect:\n%s", markup)
	}
	if !strings.Contains(markup, "Use New employee hire template") {
		t.Fatalf("template start action missing from the catalog:\n%s", markup)
	}
	if strings.Contains(markup, "New promotions run on") {
		t.Fatalf("catalog claims an unverified promotion workflow:\n%s", markup)
	}
	if !strings.Contains(markup, "Select a workflow to inspect its latest published path.") {
		t.Fatalf("catalog help does not describe the published projection:\n%s", markup)
	}
}

func TestTodo_UXBLIND_070_Browser(t *testing.T) {
	props := uxblindVWorkflowProps("de-DE")
	selected := workflowViewerFixture()
	selected.WorkflowID = "draft.workflow"
	selected.Name = "Field work order"
	selected.PublicationStatus = "DRAFT"
	props.Selected = &selected
	markup, err := ui.RenderToString(WorkflowDesignerPage(props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Workflow-Entwürfe", "Bearbeiten aus dieser Ansicht nicht verfügbar", "Vorlage New employee hire verwenden"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("German workflow catalog missing %q:\n%s", want, markup)
		}
	}
}

func TestUXBLIND070_DraftWorkspaceExplainsEditUnavailableWithoutInventingRoute(t *testing.T) {
	props := uxblindVWorkflowProps("en-US")
	selected := workflowViewerFixture()
	selected.WorkflowID = "draft.workflow"
	selected.Name = "Field work order"
	selected.PublicationStatus = "DRAFT"
	markup, err := ui.RenderToString(uxblindWorkflowDraftWorkspace(props, selected))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"workflow-draft-selection", "Field work order", "Draft", "Editing is unavailable from this view."} {
		if !strings.Contains(markup, want) {
			t.Fatalf("draft workspace missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "Edit draft") || strings.Contains(markup, `href="`) || strings.Contains(markup, `<button`) {
		t.Fatalf("draft workspace advertises an action or fabricated route:\n%s", markup)
	}
	css := workflowDesignerStylesheet()
	for _, want := range []string{".workflow-draft-selection{", "flex-wrap:wrap", ".workflow-catalog-unavailable", "@media (max-width:640px)"} {
		if !strings.Contains(css, want) {
			t.Errorf("draft workspace styling missing %q", want)
		}
	}
}

func TestUXBLIND070_CatalogDraftWithoutAuthorizedIDHasNoEditControl(t *testing.T) {
	props := uxblindVWorkflowProps("en-US")
	item := WorkflowCatalogItem{WorkflowID: "draft.workflow", Name: "Field work order", Status: "DRAFT"}
	markup, err := ui.RenderToString(uxblindWorkflowCatalogEntry(props, item, item.Name, "Draft", "v1.0.0", "workflow-catalog-link", ""))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Edit draft") || strings.Contains(markup, `href="/workspace/app/admin/workflows?draft=`) || strings.Contains(markup, `<button`) {
		t.Fatalf("draft without an authorized ID received an edit control:\n%s", markup)
	}
	if !strings.Contains(markup, "Editing is unavailable from this view.") {
		t.Fatalf("draft without an authorized ID has no explanation:\n%s", markup)
	}
}

func uxblindVAppearanceProps(theme, published CustomerTheme) AppearancePageProps {
	return AppearancePageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Theme:     theme, PublishedTheme: published, ColorModes: ColorModeOptions(), Palettes: PaletteOptions(), Shapes: ShapeOptions(),
		Densities: DensityOptions(), Glyphs: GlyphOptions(), Typefaces: TypefaceOptions(), Navigation: NavigationOptions(), Motions: MotionOptions(), Editable: true,
	}
}

func TestTodo_UXBLIND_071(t *testing.T) {
	published := DefaultCustomerTheme()
	markup, err := ui.RenderToString(AppearancePage(uxblindVAppearanceProps(published, published)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-appearance-toc="true"`, `class="appearance-section-link is-selected"`, `aria-current="page"`,
		`data-unsaved-protection="true"`, `data-unsaved-form="appearance"`, `data-unsaved="false"`, `data-hcm-edit-dirty="false"`,
		`class="appearance-choice appearance-choice-palette-evergreen is-selected"`, `aria-label="Preview workspace"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("appearance editor missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, `class="appearance-choice appearance-choice-palette-custom is-selected"`) || !strings.Contains(markup, `data-hcm-action="save-appearance" data-hcm-editable="true" disabled`) {
		t.Fatalf("unchanged appearance rendered the wrong selection or save state:\n%s", markup)
	}
}

func TestTodo_UXBLIND_071_Browser(t *testing.T) {
	published := DefaultCustomerTheme()
	published.BrandName = "Browser appearance"
	draft := published
	draft.Palette = "ocean"
	markup, err := ui.RenderToString(AppearancePage(uxblindVAppearanceProps(draft, published)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-unsaved="true"`, `data-hcm-edit-dirty="true"`, "Previewing unsaved appearance changes", `data-hcm-palette="ocean"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("unsaved appearance preview missing %q:\n%s", want, markup)
		}
	}
}

func TestTodo_UXBLIND_071_Accessibility(t *testing.T) {
	markup, err := ui.RenderToString(AppearancePage(uxblindVAppearanceProps(DefaultCustomerTheme(), DefaultCustomerTheme())))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, `data-hcm-action="save-appearance" disabled`) && !strings.Contains(markup, `aria-describedby="appearance-status"`) {
		t.Fatal("disabled save control lacks an associated live status")
	}
	for _, label := range []string{"Preview workspace", "Color palette", "Brand signature", "Save appearance", "Preview defaults"} {
		if !strings.Contains(markup, label) {
			t.Fatalf("appearance control label missing %q", label)
		}
	}
}
