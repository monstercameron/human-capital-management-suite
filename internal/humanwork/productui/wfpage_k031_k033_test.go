package productui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/documents"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/testprofile"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

func wfpageK030PlanForProductUI() *workflow.CompiledWorkflow {
	return &workflow.CompiledWorkflow{WorkflowID: "new-hire", Version: 1, Inputs: []workflow.Field{{
		Path: "legal_name", Type: workflow.ValueType{Kind: workflow.KindString}, Label: "Legal name",
	}}}
}

func wfpageK031Page() pagedef.WorkflowPageDefinition {
	return pagedef.WorkflowPageDefinition{
		Schema: pagedef.WorkflowPageSchema, SchemaVersion: pagedef.WorkflowPageSchemaVersion,
		WorkflowKey: "new-hire", WorkflowVersion: 1, PageVersion: 1, PageID: "new-hire",
		Sections:      []pagedef.WorkflowPageSection{{ID: "identity", Label: "Identity", Columns: 1}},
		Widgets:       []pagedef.WorkflowPageWidget{{ID: "name", SectionID: "identity", Column: 1, Kind: pagedef.WorkflowWidgetText, Binding: "legal_name", Type: "STRING", Label: "Legal name"}},
		ContentBlocks: []pagedef.WorkflowPageContentBlock{{ID: "welcome", SectionID: "identity", Kind: "note", TextKey: "new_hire.welcome"}},
		Accessibility: pagedef.Accessibility{Landmarks: []string{"main"}, LiveRegion: pagedef.LiveRegionOff},
		BrandTokens:   []string{"brand.color.primary"},
	}
}

func TestTodo_WFPAGE_031(t *testing.T) {
	page, evaluation, err := EditWorkflowPageRule(wfpageK031Page(), wfpageK030PlanForProductUI(), []WorkflowPageRuleLibraryEntry{{
		ID: "legal-name", Rule: pagedef.WorkflowPageRule{ID: "legal-name-required", Target: "legal_name", Message: "Enter the legal name", When: pagedef.WorkflowCondition{Path: "legal_name", Op: pagedef.RuleExists}},
	}}, "legal-name", pagedef.WorkflowPageRule{}, map[string]string{"legal_name": "Avery"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rules) != 1 || !evaluation.Passed || evaluation.Message == "" || len(evaluation.CoveredFields) != 1 {
		t.Fatalf("rule editor result = %+v, page=%+v", evaluation, page)
	}
	if err := ValidateWorkflowPageLocalizedMarkdown(WorkflowPageLocalizedMarkdown{ENUS: "Welcome", DEDE: "Willkommen", AR: "مرحبًا"}); err != nil {
		t.Fatalf("localized Markdown refused: %v", err)
	}
	link, err := PickWorkflowPageSOP(documents.WorkflowDocumentLinkSlot{ID: "sop", SectionID: "identity", DocumentRef: "document:onboarding@v2", Mode: documents.LinkPinned}, []documents.WorkflowDocument{{
		ID: "onboarding", Title: "Onboarding SOP", Readable: true, Versions: []documents.WorkflowDocumentVersion{{ID: "v2", Reviewed: true, Deployed: true}},
	}})
	if err != nil || !link.Readable || link.VersionID != "v2" || link.Href == "" {
		t.Fatalf("SOP picker result = %+v, %v", link, err)
	}
	if err := ValidateWorkflowPageSupportBinding(WorkflowPageSupportBinding{ChannelID: "hr-support", Label: "HR support", Eligible: true, Href: "/workspace/app/chat?channel=hr-support"}); err != nil {
		t.Fatal(err)
	}
	contentPage := wfpageK031Page()
	if err := AddWorkflowPageContentBlock(&contentPage, WorkflowPageContentEdit{ID: "checklist", SectionID: "identity", Kind: WorkflowPageChecklist, TextKey: "new_hire.checklist", Copy: WorkflowPageLocalizedMarkdown{ENUS: "Checklist", DEDE: "Checkliste", AR: "قائمة التحقق"}}); err != nil {
		t.Fatal(err)
	}
	if len(contentPage.ContentBlocks) != 2 {
		t.Fatalf("content block editor appended %d blocks", len(contentPage.ContentBlocks))
	}
	copy := WorkflowPageLocalizedMarkdown{ENUS: "Name", DEDE: "Name", AR: "اسم"}
	if err := EditWorkflowPageMarkdown(&copy, "de-DE", "## Vollständiger Name"); err != nil || copy.DEDE != "## Vollständiger Name" {
		t.Fatalf("localized Markdown edit = %+v, %v", copy, err)
	}
}

func TestTodo_WFPAGE_031_Browser(t *testing.T) {
	page := wfpageK031Page()
	page.Rules = append(page.Rules, pagedef.WorkflowPageRule{ID: "rule", Target: "legal_name", Message: "Required", When: pagedef.WorkflowCondition{Path: "legal_name", Op: pagedef.RuleExists}})
	result := EvaluateWorkflowPageRule(page.Rules[0], map[string]string{})
	if result.Passed || result.Message != "Required" || len(result.CoveredFields) != 1 {
		t.Fatalf("live rule editor projection = %+v", result)
	}
	if err := ValidateWorkflowPageLocalizedMarkdown(WorkflowPageLocalizedMarkdown{ENUS: "ok", DEDE: "", AR: "ok"}); err == nil {
		t.Fatal("incomplete locale set was accepted")
	}
}

func TestTodo_WFPAGE_032(t *testing.T) {
	profiles, err := testprofile.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	request := WorkflowPagePreviewRequest{Tenant: "harborcare-demo", Persona: WorkflowPagePreviewRecruiter, Profile: testprofile.ProfileRef{ID: "harborcare-promotion", Version: 1}, Profiles: profiles, Plan: wfpageK030PlanForProductUI(), Page: wfpageK031Page(), Copy: map[string]WorkflowPageLocalizedMarkdown{
		"new_hire.welcome": {ENUS: "Welcome", DEDE: "Willkommen", AR: "مرحبًا"},
	}}
	preview, err := PreviewWorkflowPage(request)
	if err != nil || !preview.Passed {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	if preview.RunStarted || preview.Writes != 0 || preview.FixtureDigest == "" || len(preview.ReleaseFixture()) == 0 {
		t.Fatalf("preview caused a side effect or saved no fixture: %+v", preview)
	}
	request.Persona = WorkflowPagePreviewPersona("developer")
	if _, err := PreviewWorkflowPage(request); err == nil {
		t.Fatal("unsupported persona was accepted")
	}
	request.Persona = WorkflowPagePreviewRecruiter
	request.Tenant = "other-tenant"
	if _, err := PreviewWorkflowPage(request); !errors.Is(err, testprofile.ErrTenantMismatch) {
		t.Fatalf("cross-tenant preview error = %v", err)
	}
}

func TestTodo_WFPAGE_032_Browser(t *testing.T) {
	profiles, err := testprofile.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	page := wfpageK031Page()
	page.Rules = append(page.Rules, pagedef.WorkflowPageRule{ID: "never", Target: "legal_name", Message: "Never", When: pagedef.WorkflowCondition{Path: "legal_name", Op: pagedef.RuleEquals, Value: "never"}})
	preview, err := PreviewWorkflowPage(WorkflowPagePreviewRequest{Tenant: "harborcare-demo", Persona: WorkflowPagePreviewHiringManager, Profile: testprofile.ProfileRef{ID: "harborcare-promotion", Version: 1}, Profiles: profiles, Plan: wfpageK030PlanForProductUI(), Page: page, Copy: map[string]WorkflowPageLocalizedMarkdown{"new_hire.welcome": {ENUS: "Welcome", DEDE: "Willkommen", AR: "مرحبًا"}}, ValueDomains: map[string][]string{"legal_name": {"Avery"}}})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Passed {
		t.Fatal("preview passed with a rule that has no possible domain value")
	}
	preview, err = PreviewWorkflowPage(WorkflowPagePreviewRequest{Tenant: "harborcare-demo", Persona: WorkflowPagePreviewHiringManager, Profile: testprofile.ProfileRef{ID: "harborcare-promotion", Version: 1}, Profiles: profiles, Plan: wfpageK030PlanForProductUI(), Page: wfpageK031Page(), Copy: map[string]WorkflowPageLocalizedMarkdown{"new_hire.welcome": {ENUS: "Welcome", DEDE: "Willkommen"}}})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Passed {
		t.Fatal("preview passed with a missing RTL translation")
	}
}

func TestTodo_WFPAGE_032_Golden(t *testing.T) {
	profiles, err := testprofile.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	request := WorkflowPagePreviewRequest{Tenant: "harborcare-demo", Persona: WorkflowPagePreviewHRPartner, Profile: testprofile.ProfileRef{ID: "harborcare-promotion", Version: 1}, Profiles: profiles, Plan: wfpageK030PlanForProductUI(), Page: wfpageK031Page(), Copy: map[string]WorkflowPageLocalizedMarkdown{"new_hire.welcome": {ENUS: "Welcome", DEDE: "Willkommen", AR: "مرحبًا"}}}
	first, err := PreviewWorkflowPage(request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PreviewWorkflowPage(request)
	if err != nil || string(first.ReleaseFixture()) != string(second.ReleaseFixture()) || first.FixtureDigest != second.FixtureDigest {
		t.Fatalf("preview release fixture is not deterministic: %q/%q", first.FixtureDigest, second.FixtureDigest)
	}
	if strings.Contains(string(first.ReleaseFixture()), "developer") {
		t.Fatal("fixture leaked an unregistered persona")
	}
}

func wfpageK033Principal(tenant, id string, permissions ...WorkflowPagePermission) WorkflowPagePrincipal {
	set := make(map[WorkflowPagePermission]bool, len(permissions))
	for _, permission := range permissions {
		set[permission] = true
	}
	return WorkflowPagePrincipal{Tenant: tenant, ID: id, Permissions: set}
}

func TestTodo_WFPAGE_033(t *testing.T) {
	service, err := NewWorkflowPageReleaseService("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	author := wfpageK033Principal("tenant-a", "author", WorkflowPageDesignPermission)
	reviewer := wfpageK033Principal("tenant-a", "reviewer", WorkflowPagePublishPermission)
	starter := wfpageK033Principal("tenant-a", "starter", WorkflowPageStartPermission)
	draft := WorkflowPageReleaseDraft{Tenant: "tenant-a", WorkflowOwner: "workflow-owner", Author: author, Plan: wfpageK030PlanForProductUI(), Page: wfpageK031Page(), Version: 1, FixtureDigest: "sha256:fixture-1", PreviewPassed: true}
	if _, err := service.SubmitWorkflowPageDraft(draft); err != nil {
		t.Fatal(err)
	}
	if err := service.ApproveWorkflowPageVersion(author, 1); err == nil {
		t.Fatal("author approved their own page")
	}
	if err := service.ApproveWorkflowPageVersion(reviewer, 1); err != nil {
		t.Fatal(err)
	}
	if err := service.ActivateWorkflowPageVersion(reviewer, 1); err != nil {
		t.Fatal(err)
	}
	served, ok := service.ServedPage()
	if !ok || served.Version != 1 || !served.Active || !service.AuthorizeWorkflowPageStart(starter) {
		t.Fatalf("served page / start authority = %+v, %v", served, service.AuthorizeWorkflowPageStart(starter))
	}
	if service.AuthorizeWorkflowPageStart(author) {
		t.Fatal("design authority implied start authority")
	}
}

func TestTodo_WFPAGE_033_Security(t *testing.T) {
	service, err := NewWorkflowPageReleaseService("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	foreign := wfpageK033Principal("tenant-b", "reviewer", WorkflowPagePublishPermission)
	if _, err := service.SubmitWorkflowPageDraft(WorkflowPageReleaseDraft{Tenant: "tenant-b", Author: foreign, Plan: wfpageK030PlanForProductUI(), Page: wfpageK031Page(), Version: 1, WorkflowOwner: "owner", FixtureDigest: "fixture", PreviewPassed: true}); err == nil {
		t.Fatal("foreign tenant submitted a page")
	}
	startOnly := wfpageK033Principal("tenant-a", "start-only", WorkflowPageStartPermission)
	if service.AuthorizeWorkflowPageStart(startOnly) && startOnly.Allows("tenant-a", WorkflowPageDesignPermission) {
		t.Fatal("start permission crossed into design permission")
	}
}

func TestTodo_WFPAGE_033_Integration(t *testing.T) {
	service, err := NewWorkflowPageReleaseService("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	author := wfpageK033Principal("tenant-a", "author", WorkflowPageDesignPermission)
	reviewer := wfpageK033Principal("tenant-a", "reviewer", WorkflowPagePublishPermission)
	for version := uint32(1); version <= 2; version++ {
		if _, err := service.SubmitWorkflowPageDraft(WorkflowPageReleaseDraft{Tenant: "tenant-a", WorkflowOwner: "owner", Author: author, Plan: wfpageK030PlanForProductUI(), Page: wfpageK031Page(), Version: version, FixtureDigest: fmt.Sprintf("fixture-%d", version), PreviewPassed: true}); err != nil {
			t.Fatal(err)
		}
		if err := service.ApproveWorkflowPageVersion(reviewer, version); err != nil {
			t.Fatal(err)
		}
		if err := service.ActivateWorkflowPageVersion(reviewer, version); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.RollbackWorkflowPageVersion(reviewer, 1); err != nil {
		t.Fatal(err)
	}
	served, ok := service.ServedPage()
	if !ok || served.Version != 1 {
		t.Fatalf("rollback served version = %+v", served)
	}
	events := service.Events()
	if len(events) != 3 || events[2].Kind != "rollback" || events[2].Owner != "owner" {
		t.Fatalf("release notifications = %+v", events)
	}
}
