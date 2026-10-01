package pagedef

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
)

func testWorkflowPage() WorkflowPageDefinition {
	return WorkflowPageDefinition{
		Schema: WorkflowPageSchema, SchemaVersion: WorkflowPageSchemaVersion,
		WorkflowKey: "workflow.new_hire", WorkflowVersion: 3, PageVersion: 2, PageID: "workflow.new_hire.input",
		Sections:      []WorkflowPageSection{{ID: "section.person", Label: "Person", Columns: 2, StepID: "details"}},
		Steps:         []WorkflowPageStep{{ID: "details", Label: "Details"}},
		Widgets:       []WorkflowPageWidget{{ID: "field.person", SectionID: "section.person", Column: 1, StepID: "details", Kind: WorkflowWidgetText, Binding: "person.name", Type: "STRING", Label: "Name", Required: true}},
		Rules:         []WorkflowPageRule{{ID: "name-required", Target: "person.name", Message: "workflow.name.required", When: WorkflowCondition{Path: "person.name", Op: RuleExists}}},
		ContentBlocks: []WorkflowPageContentBlock{{ID: "guide", SectionID: "section.person", Kind: "guide", TextKey: "workflow.new_hire.guide"}},
		LinkSlots:     []WorkflowPageLinkSlot{{ID: "sop", SectionID: "section.person", DocumentRef: "document:hiring-sop", Mode: "latest"}},
		Visibility:    []WorkflowPageVisibility{{Target: "field.person", When: WorkflowCondition{Path: "person.name", Op: RuleNotEquals, Value: "hidden"}}},
		Accessibility: Accessibility{Landmarks: []string{"main", "form"}, LiveRegion: LiveRegionPolite},
		BrandTokens:   []string{"brand.color.primary", "brand.spacing.md"},
	}
}

// TestTodo_WFPAGE_010 proves the workflow page contract is closed, versioned,
// data-only, and bound to the compiled workflow rather than an invented path.
func TestTodo_WFPAGE_010(t *testing.T) {
	page := testWorkflowPage()
	plan := &workflow.CompiledWorkflow{WorkflowID: page.WorkflowKey, Version: page.WorkflowVersion, Inputs: []workflow.Field{{Path: "person.name", Type: workflow.ValueType{Kind: workflow.KindString}}}}
	if got := page.ValidateAgainst(plan); len(got) != 0 {
		t.Fatalf("valid workflow page rejected: %v", got)
	}

	bad := page
	bad.Widgets = append([]WorkflowPageWidget(nil), page.Widgets...)
	bad.Widgets[0].Binding = "person.unknown"
	if got := bad.ValidateAgainst(plan); len(got) == 0 || !strings.Contains(got[len(got)-1].Error(), "compiled workflow input") {
		t.Fatalf("undeclared binding accepted: %v", got)
	}

	bad = page
	bad.ContentBlocks = []WorkflowPageContentBlock{{ID: "unsafe", SectionID: "section.person", Kind: "guide", TextKey: "<b>raw</b>"}}
	if got := bad.Validate(); len(got) == 0 {
		t.Fatal("free markup accepted in a content reference")
	}

	bad = page
	bad.LinkSlots = []WorkflowPageLinkSlot{{ID: "unsafe", SectionID: "section.person", DocumentRef: "https://example.invalid", Mode: "latest"}}
	if got := bad.Validate(); len(got) == 0 {
		t.Fatal("arbitrary URL accepted as a link slot")
	}
}

// TestTodo_WFPAGE_010_Golden pins the exact canonical bytes, not just a
// digest, so field additions and ordering changes remain review-visible.
func TestTodo_WFPAGE_010_Golden(t *testing.T) {
	got := string(testWorkflowPage().Canonical())
	want := `{"schema":"hcmnext.uxqual.pagedef.WorkflowPageDefinition","schema_version":1,"workflow_key":"workflow.new_hire","workflow_version":3,"page_version":2,"page_id":"workflow.new_hire.input","sections":[{"id":"section.person","label":"Person","columns":2,"step_id":"details"}],"steps":[{"id":"details","label":"Details"}],"widgets":[{"id":"field.person","section_id":"section.person","column":1,"step_id":"details","kind":"text","binding":"person.name","type":"STRING","label":"Name","required":true}],"rules":[{"id":"name-required","target":"person.name","message":"workflow.name.required","when":{"path":"person.name","op":"exists"}}],"content_blocks":[{"id":"guide","section_id":"section.person","kind":"guide","text_key":"workflow.new_hire.guide"}],"link_slots":[{"id":"sop","section_id":"section.person","document_ref":"document:hiring-sop","mode":"latest"}],"visibility":[{"target":"field.person","when":{"path":"person.name","op":"not_equals","value":"hidden"}}],"accessibility":{"landmarks":["main","form"],"live_region":"polite"},"brand_tokens":["brand.color.primary","brand.spacing.md"]}`
	if got != want {
		t.Fatalf("canonical bytes changed\n got: %s\nwant: %s", got, want)
	}
}

// TestTodo_WFPAGE_012 proves the default generator covers every supported
// workflow input kind once and keeps regeneration byte-identical.
func TestTodo_WFPAGE_012(t *testing.T) {
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatalf("compile prototype: %v", err)
	}
	page, err := DefaultWorkflowPageDefinition(plan)
	if err != nil {
		t.Fatalf("generate default page: %v", err)
	}
	if got := page.ValidateAgainst(plan); len(got) != 0 {
		t.Fatalf("generated page invalid: %v", got)
	}
	if len(page.Widgets) != len(plan.Inputs) {
		t.Fatalf("controls=%d inputs=%d", len(page.Widgets), len(plan.Inputs))
	}
	seen := map[string]bool{}
	for _, widget := range page.Widgets {
		if seen[widget.Binding] {
			t.Fatalf("duplicate control for %s", widget.Binding)
		}
		seen[widget.Binding] = true
	}
	second, err := DefaultWorkflowPageDefinition(plan)
	if err != nil || string(page.Canonical()) != string(second.Canonical()) {
		t.Fatalf("default regeneration is not byte-identical: %v", err)
	}
}

// TestTodo_WFPAGE_012_Browser pins the browser-facing semantic projection:
// each generated control has a label, one section mount, and a kind that a
// registered widget can render. A real browser is intentionally not started
// in this native contract package.
func TestTodo_WFPAGE_012_Browser(t *testing.T) {
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatal(err)
	}
	page, err := DefaultWorkflowPageDefinition(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, widget := range page.Widgets {
		if widget.Label == "" || widget.SectionID == "" || widget.Column < 1 || !workflowPageKindKnown(widget.Kind) {
			t.Fatalf("browser projection has an incomplete control: %+v", widget)
		}
	}
}

func TestTodo_WFPAGE_012_Golden(t *testing.T) {
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatal(err)
	}
	page, err := DefaultWorkflowPageDefinition(plan)
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:6dd6ac1eda3d783f6164a927e2ccabaa3fb1fceb94f0bbad331dbbe7117e00de"
	if got := page.Digest(); got != want {
		t.Fatalf("prototype default digest = %q, want pinned golden %q", got, want)
	}
}

func TestTodo_WFPAGE_012_Property(t *testing.T) {
	kinds := []struct {
		kind workflow.Kind
		want WorkflowWidgetKind
	}{
		{workflow.KindString, WorkflowWidgetText}, {workflow.KindInteger, WorkflowWidgetInteger}, {workflow.KindDecimal, WorkflowWidgetDecimal},
		{workflow.KindBool, WorkflowWidgetCheckbox}, {workflow.KindInstant, WorkflowWidgetInstant}, {workflow.KindLocalDate, WorkflowWidgetLocalDate},
		{workflow.KindMoney, WorkflowWidgetMoney}, {workflow.KindEnum, WorkflowWidgetChoice}, {workflow.KindList, WorkflowWidgetList},
	}
	for _, tc := range kinds {
		if got := defaultWidgetKind(tc.kind); got != tc.want {
			t.Fatalf("kind %s mapped to %s, want %s", tc.kind, got, tc.want)
		}
	}
}
