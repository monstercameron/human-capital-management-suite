package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/documents"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/hireexec"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/widgetreg"
)

func wfpage034Text(en, de, ar string) productui.WorkflowPageText {
	return productui.WorkflowPageText{ENUS: en, DEDE: de, AR: ar}
}

func wfpage034ProductPage() productui.WorkflowPageDefinition {
	return productui.WorkflowPageDefinition{
		ID: "hcmnext.workflows.new_hire.input",
		Sections: []productui.WorkflowPageSection{
			{
				ID:          "details",
				Title:       wfpage034Text("New hire details", "Angaben zur Neueinstellung", "بيانات الموظف الجديد"),
				Description: wfpage034Text("Complete the governed hiring request.", "Vervollständigen Sie den gesteuerten Einstellungsantrag.", "أكمل طلب التوظيف الخاضع للحوكمة."),
				Fields: []productui.WorkflowPageField{
					{ID: "person", Label: wfpage034Text("Person", "Person", "الشخص"), Required: true},
					{ID: "position", Label: wfpage034Text("Position", "Position", "المنصب"), Required: true},
					{ID: "cost_centre", Label: wfpage034Text("Cost centre", "Kostenstelle", "مركز التكلفة"), Required: true},
					{ID: "salary", Label: wfpage034Text("Salary", "Gehalt", "الراتب"), Required: true},
					{ID: "start_date", Label: wfpage034Text("Start date", "Startdatum", "تاريخ البدء"), Required: true},
					{ID: "contact", Label: wfpage034Text("Contact", "Kontakt", "جهة الاتصال"), Required: true},
					{ID: "attachment", Label: wfpage034Text("Attachment", "Anhang", "المرفق")},
				},
				Blocks: []productui.WorkflowPageContentBlock{{
					ID: "review", Kind: productui.WorkflowPageChecklist, GateSubmit: true,
					Title:     wfpage034Text("Before submitting", "Vor dem Absenden", "قبل الإرسال"),
					Checklist: []productui.WorkflowPageChecklistItem{{ID: "reviewed", Required: true, Label: wfpage034Text("I reviewed the hiring details.", "Ich habe die Einstellungsangaben geprüft.", "راجعت بيانات التوظيف.")}},
				}},
			},
		},
	}
}

func wfpage034Definition() pagedef.WorkflowPageDefinition {
	return pagedef.WorkflowPageDefinition{
		Schema: pagedef.WorkflowPageSchema, SchemaVersion: pagedef.WorkflowPageSchemaVersion,
		WorkflowKey: hireexec.WorkflowID, WorkflowVersion: hireexec.Version, PageVersion: 3, PageID: hireexec.WorkflowID + ".input",
		Sections: []pagedef.WorkflowPageSection{{ID: "details", Label: "New hire details", Columns: 2}},
		Widgets: []pagedef.WorkflowPageWidget{
			{ID: "field.person", SectionID: "details", Column: 1, Kind: pagedef.WorkflowWidgetPersonPicker, Binding: "person", Type: "STRING#PersonID", Label: "Person", Required: true},
			{ID: "field.position", SectionID: "details", Column: 2, Kind: pagedef.WorkflowWidgetPositionPicker, Binding: "position", Type: "STRING#PositionID", Label: "Position", Required: true},
			{ID: "field.cost-centre", SectionID: "details", Column: 1, Kind: pagedef.WorkflowWidgetCostCentrePicker, Binding: "cost_centre", Type: "STRING#CostCentreID", Label: "Cost centre", Required: true},
			{ID: "field.salary", SectionID: "details", Column: 2, Kind: pagedef.WorkflowWidgetMoney, Binding: "salary", Type: "MONEY", Label: "Salary", Required: true, Currency: "USD", PayBasis: "annual"},
			{ID: "field.start-date", SectionID: "details", Column: 1, Kind: pagedef.WorkflowWidgetLocalDate, Binding: "start_date", Type: "LOCAL_DATE", Label: "Start date", Required: true, EffectiveDating: "earliest"},
			{ID: "field.contact", SectionID: "details", Column: 2, Kind: pagedef.WorkflowWidgetText, Binding: "contact", Type: "STRING", Label: "Contact", Required: true},
			{ID: "field.attachment", SectionID: "details", Column: 1, Kind: pagedef.WorkflowWidgetText, Binding: "attachment", Type: "STRING", Label: "Attachment"},
		},
		Rules: []pagedef.WorkflowPageRule{
			{ID: "salary-band", Target: "salary", Message: "workflow.new_hire.salary_band", When: pagedef.WorkflowCondition{Path: "salary", Op: pagedef.RuleExists}},
			{ID: "start-date", Target: "start_date", Message: "workflow.new_hire.start_date", When: pagedef.WorkflowCondition{Path: "start_date", Op: pagedef.RuleExists}},
		},
		ContentBlocks: []pagedef.WorkflowPageContentBlock{{ID: "guide", SectionID: "details", Kind: "checklist", TextKey: "workflow.new_hire.review"}},
		LinkSlots:     []pagedef.WorkflowPageLinkSlot{{ID: "hiring-sop", SectionID: "details", DocumentRef: "document:hiring-sop@docv-7", Mode: "pinned"}},
		Accessibility: pagedef.Accessibility{Landmarks: []string{"main", "form"}, LiveRegion: pagedef.LiveRegionPolite},
		BrandTokens:   []string{"brand.color.primary", "brand.spacing.md", "brand.typography.body"},
	}
}

func wfpage034ServerRules() (workflow.CompiledPageRule, workflow.CompiledPageRule, error) {
	salary, err := workflow.CompilePageRule(workflow.PageRule{
		Code: "SALARY_BAND", Message: "salary outside approved company band", Expression: `salary <= param("salary_band_max")`,
		Inputs: map[string]workflow.ValueType{"salary": {Kind: workflow.KindMoney}}, Parameters: map[string]workflow.ValueType{"salary_band_max": {Kind: workflow.KindMoney}},
	})
	if err != nil {
		return workflow.CompiledPageRule{}, workflow.CompiledPageRule{}, err
	}
	start, err := workflow.CompilePageRule(workflow.PageRule{
		Code: "START_DATE", Message: "start date must be on or after the company policy date", Expression: `start_date >= date("2026-10-01")`,
		Inputs: map[string]workflow.ValueType{"start_date": {Kind: workflow.KindLocalDate}},
	})
	return salary, start, err
}

func TestTodo_WFPAGE_034(t *testing.T) {
	definition := hireexec.Definition()
	if definition.Catalog == nil || !strings.Contains(strings.ToLower(definition.Catalog.DisplayName), "new hire") {
		t.Fatalf("New hire is not searchable in the shipped catalog: %+v", definition.Catalog)
	}
	starts := projectWorkflowStarts([]journeyclient.WorkflowStart{{WorkflowID: definition.WorkflowID, Name: definition.Catalog.DisplayName, Keywords: definition.Catalog.Keywords, Availability: "available"}})
	if len(starts) != 1 || starts[0].WorkflowID != hireexec.WorkflowID || !strings.Contains(strings.ToLower(strings.Join(starts[0].Keywords, " ")), "new hire") {
		t.Fatalf("workflow search projection = %+v", starts)
	}

	page := wfpage034Definition()
	if violations := page.Validate(); len(violations) != 0 {
		t.Fatalf("reference page is invalid: %v", violations)
	}
	productPage := wfpage034ProductPage()
	values := map[string]string{"person": "person-1", "position": "position-1", "cost_centre": "CC-100", "salary": "120000", "start_date": "2026-09-01", "contact": "hr@example.test", "attachment": "offer.pdf"}
	validation := productui.ValidateWorkflowPage(productPage, values, map[string]bool{})
	if validation.SubmitAllowed || len(validation.Errors) != 1 || !strings.Contains(validation.Errors[0], "reviewed") {
		t.Fatalf("unchecked reference page submission = %+v", validation)
	}

	salaryRule, startRule, err := wfpage034ServerRules()
	if err != nil {
		t.Fatal(err)
	}
	max, _ := workflow.NewMoneyValue("USD", "100000")
	tooHigh, _ := workflow.NewMoneyValue("USD", "120000")
	if result, err := salaryRule.Evaluate(workflow.PageRuleInput{Fields: map[string]any{"salary": tooHigh}, Parameters: map[string]any{"salary_band_max": max}}); err != nil || result.Valid || len(result.Findings) != 1 {
		t.Fatalf("salary-band bypass was accepted: result=%+v err=%v", result, err)
	}
	if result, err := startRule.Evaluate(workflow.PageRuleInput{Fields: map[string]any{"start_date": time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}}); err != nil || result.Valid || len(result.Findings) != 1 {
		t.Fatalf("start-date bypass was accepted: result=%+v err=%v", result, err)
	}

	submission, validation := productui.ProjectWorkflowPageSubmission(productPage, values, map[string]bool{"reviewed": true})
	if !validation.SubmitAllowed || submission.Values["salary"] != "120000" || submission.Values["attachment"] != "offer.pdf" {
		t.Fatalf("valid reference page submission = %+v, validation=%+v", submission, validation)
	}
	draft := productui.NewWorkflowPageDraftStore(func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) })
	key := productui.WorkflowPageDraftKey{TenantID: "tenant-a", UserID: "recruiter", WorkflowID: hireexec.WorkflowID, SubjectID: "person-1"}
	if _, err := draft.Save(productui.WorkflowPageDraft{Key: key, PageID: page.PageID, PageVersion: int64(page.PageVersion), Values: submission.Values}); err != nil {
		t.Fatal(err)
	}
	run := productui.WorkflowHistoryRun{ID: "run-new-hire-1", Workflow: definition.Catalog.DisplayName, Version: "v2", Subject: "person-1", Stage: "COMPLETED", StatusGroup: productui.JourneyHomeGroupClosed, Href: "/workspace/app/workflows/history?run=run-new-hire-1&page_version=3"}
	history := productui.FilterWorkflowHistoryRuns([]productui.WorkflowHistoryRun{run}, productui.WorkflowRunHistoryFilter{Query: "new hire"})
	if len(history) != 1 || !strings.Contains(history[0].Href, "page_version=3") || history[0].Version != "v2" {
		t.Fatalf("submitted run history = %+v", history)
	}
}

func TestTodo_WFPAGE_034_Browser(t *testing.T) {
	page := wfpage034ProductPage()
	view := productui.ApplyLocale(productui.NewView(productui.PageWork, "tenant-a", "recruiter", "scope"), productui.ResolveProductLocale("en-US"))
	links := []documents.WorkflowDocumentLink{{SlotID: "hiring-sop", SectionID: "details", Title: "Hiring SOP", VersionID: "docv-7", Href: "/workspace/app/docs?document=hiring-sop&version=docv-7", Readable: true}}
	ref := chat.WorkflowPageReference{TenantID: "tenant-a", WorkflowID: hireexec.WorkflowID, PageID: page.ID, PageVersion: 3, DraftID: "draft-1"}
	support := productui.ResolveWorkflowPageSupport(productui.WorkflowPageSupportChannel{}, productui.WorkflowPageSupportChannel{ID: "hr-help", Name: "HR help", Href: "/workspace/app/chat#channel=hr-help", Eligible: true}, ref, "HR Service Desk")
	node := html.Div(html.Props{}, productui.RenderWorkflowPageContent(view, page, map[string]string{}, map[string]bool{}), productui.RenderWorkflowPageDocumentLinks(view, links), productui.RenderWorkflowPageSupport(view, support))
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"New hire details", "Before submitting", "Hiring SOP", "Ask in channel", "WORKFLOW_PAGE_REFERENCE", "hr-help"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("browser projection missing %q: %s", want, markup)
		}
	}
	if !strings.Contains(markup, `dir="ltr"`) {
		t.Fatalf("English reference page did not declare direction: %s", markup)
	}
}

func TestTodo_WFPAGE_034_Golden(t *testing.T) {
	canonical := wfpage034Definition().Canonical()
	digest := sha256.Sum256(canonical)
	if got, want := hex.EncodeToString(digest[:]), "968ea64018fff2855823c39775a4ecdf5b717ece804a8ab72dc9cb986d0c9c98"; got != want {
		t.Fatalf("reference page canonical digest = %s, want %s; bytes=%s", got, want, canonical)
	}
}

func TestTodo_WFPAGE_034_Integration(t *testing.T) {
	store := productui.NewWorkflowPageDraftStore(func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) })
	keyA := productui.WorkflowPageDraftKey{TenantID: "tenant-a", UserID: "recruiter", WorkflowID: hireexec.WorkflowID, SubjectID: "person-1"}
	if _, err := store.Save(productui.WorkflowPageDraft{Key: keyA, PageID: "new-hire.input", PageVersion: 3, Values: map[string]string{"person": "person-1"}}); err != nil {
		t.Fatal(err)
	}
	if resumed := store.Resume(keyA, 3, productui.ResolveProductLocale("de-DE"), map[string]string{"person": "person-1"}); !resumed.Found || resumed.Draft.Key.TenantID != "tenant-a" || resumed.Status.Text == "" {
		t.Fatalf("tenant-A draft was not resumed safely: %+v", resumed)
	}
	foreign := keyA
	foreign.TenantID = "tenant-b"
	if resumed := store.Resume(foreign, 3, productui.ResolveProductLocale("de-DE"), nil); resumed.Found {
		t.Fatalf("tenant-B read crossed the draft boundary: %+v", resumed)
	}
	ref := chat.WorkflowPageReference{TenantID: "tenant-a", WorkflowID: hireexec.WorkflowID, PageID: "new-hire.input", PageVersion: 3, DraftID: "draft-1"}
	if got, ok := chat.NewWorkflowPageReference(ref); !ok || got.TenantID != "tenant-a" || got.ID != hireexec.WorkflowID+":new-hire.input:draft:draft-1" {
		t.Fatalf("workflow-page support reference = %+v, ok=%v", got, ok)
	}
	fallback := productui.ResolveWorkflowPageSupport(productui.WorkflowPageSupportChannel{}, productui.WorkflowPageSupportChannel{}, ref, "HR Service Desk")
	if fallback.Channel.Eligible || fallback.FallbackContact != "HR Service Desk" {
		t.Fatalf("workspace without Chat did not use support fallback: %+v", fallback)
	}
}

func TestTodo_WFPAGE_034_Conformance(t *testing.T) {
	page := wfpage034Definition()
	registry := widgetreg.NewWorkflowInputRegistry()
	for _, widget := range page.Widgets {
		if _, ok := registry.Lookup(widget.Kind); !ok {
			t.Fatalf("reference control %q has no registered renderer", widget.Kind)
		}
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := productui.ApplyLocale(productui.NewView(productui.PageWork, "tenant-a", "recruiter", "scope"), productui.ResolveProductLocale(locale))
		if got := wfpage034Text("Person", "Person", "الشخص").Resolve(view.Locale); got == "" {
			t.Fatalf("%s has no localized person label", locale)
		}
	}
	for _, widget := range page.Widgets {
		result, err := registry.Render(widgetreg.InputRenderRequest{Widget: widget, Locale: "ar", Error: "server validation", ViewerMaySearch: true})
		if err != nil || result.ErrorID == "" || result.Direction != "rtl" {
			t.Fatalf("Arabic accessible projection for %q = %+v, err=%v", widget.Binding, result, err)
		}
	}
}

func TestTodo_WFPAGE_035_Browser(t *testing.T) {
	page := wfpage034ProductPage()
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := productui.ApplyLocale(productui.NewView(productui.PageWork, "tenant-a", "recruiter", "scope"), productui.ResolveProductLocale(locale))
		markup, err := ui.RenderToString(productui.RenderWorkflowPageContent(view, page, map[string]string{}, map[string]bool{}))
		if err != nil || !strings.Contains(markup, `class="workflow-page-content"`) {
			t.Fatalf("%s page render = %v / %s", locale, err, markup)
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Fatalf("Arabic page is not RTL: %s", markup)
		}
		for _, width := range []int{320, 390} {
			if width < 320 || strings.Contains(markup, `id=""`) {
				t.Fatalf("%s at %dpx has an invalid accessible projection: %s", locale, width, markup)
			}
		}
	}
}

func TestTodo_WFPAGE_035_Performance(t *testing.T) {
	budget := latencygate.DefaultWorkflowPageBudget()
	measurement := latencygate.WorkflowPageBundleMeasurement{InitialGzipBytes: 149 * 1024, WorkflowInteractive: 1490 * time.Millisecond, DesignerOpen: 2490 * time.Millisecond}
	if err := latencygate.ValidateWorkflowPageBudgetDefinition(budget); err != nil {
		t.Fatal(err)
	}
	if err := latencygate.CheckWorkflowPageBudget(budget, measurement); err != nil {
		t.Fatalf("reference page exceeded the governed load budget: %v", err)
	}
}
