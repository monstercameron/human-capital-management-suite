package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/documents"
)

func renderWorkflowPageK4(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestTodo_WFPAGE_022(t *testing.T) {
	catalog := []documents.WorkflowDocument{
		{ID: "hiring-sop", Title: "Hiring SOP", Readable: true, Versions: []documents.WorkflowDocumentVersion{
			{ID: "docv-7", Reviewed: true, Deployed: true, Status: "deployed"},
			{ID: "docv-6", Reviewed: true, Status: "retired"},
		}},
		{ID: "private-sop", Title: "Private SOP", Readable: false, Versions: []documents.WorkflowDocumentVersion{{ID: "docv-1", Deployed: true}}},
	}
	latest, err := documents.ResolveWorkflowDocumentLink(documents.WorkflowDocumentLinkSlot{ID: "sop", SectionID: "person", DocumentRef: "document:hiring-sop#steps", Mode: documents.LinkLatest}, catalog)
	if err != nil || !latest.Readable || latest.Title != "Hiring SOP" || latest.VersionID != "docv-7" || !strings.Contains(latest.Href, "document=hiring-sop") {
		t.Fatalf("latest link = %+v, err=%v", latest, err)
	}
	pinned, err := documents.ResolveWorkflowDocumentLink(documents.WorkflowDocumentLinkSlot{ID: "reviewed", SectionID: "person", DocumentRef: "doc:hiring-sop@docv-6", Mode: documents.LinkPinned}, catalog)
	if err != nil || !pinned.Readable || !pinned.Stale || pinned.VersionID != "docv-6" {
		t.Fatalf("pinned link = %+v, err=%v", pinned, err)
	}
	denied, err := documents.ResolveWorkflowDocumentLink(documents.WorkflowDocumentLinkSlot{ID: "secret", SectionID: "person", DocumentRef: "doc:private-sop", Mode: documents.LinkLatest}, catalog)
	if err != nil || !denied.RequestAccess || denied.Title != "" || denied.DocumentID != "" || denied.Href != "" {
		t.Fatalf("denied link disclosed target: %+v, err=%v", denied, err)
	}
	picker := documents.WorkflowDocumentPicker(catalog)
	if len(picker) != 1 || picker[0].ID != "hiring-sop" {
		t.Fatalf("picker = %+v", picker)
	}
	index := documents.WorkflowBacklinkIndex{}
	index.Record(documents.WorkflowBacklink{TargetDocumentID: "hiring-sop", WorkflowID: "new-hire", PageID: "new-hire.input", SectionID: "person", SlotID: "sop", PageVersion: 3})
	if got := index.ForDocument("hiring-sop"); len(got) != 1 || got[0].PageVersion != 3 {
		t.Fatalf("backlinks = %+v", got)
	}
}

func TestTodo_WFPAGE_022_Security(t *testing.T) {
	denied, err := documents.ResolveWorkflowDocumentLink(documents.WorkflowDocumentLinkSlot{ID: "secret", SectionID: "s", DocumentRef: "doc:known-secret", Mode: documents.LinkLatest}, []documents.WorkflowDocument{{ID: "known-secret", Title: "Confidential hiring plan", Readable: false}})
	if err != nil || !denied.RequestAccess || strings.Contains(denied.Title+denied.Href+denied.DocumentID, "secret") {
		t.Fatalf("unauthorized document reference leaked existence: %+v, err=%v", denied, err)
	}
	if _, err := documents.ParseWorkflowDocumentReference("https://docs.example/secret"); err == nil {
		t.Fatal("arbitrary document URL accepted")
	}
}

func TestTodo_WFPAGE_022_Browser(t *testing.T) {
	view := ApplyLocale(NewView(PageWork, "tenant", "viewer", "scope"), ResolveProductLocale("de-DE"))
	markup := renderWorkflowPageK4(t, RenderWorkflowPageDocumentLinks(view, []documents.WorkflowDocumentLink{
		{SlotID: "sop", SectionID: "details", Title: "Einstellungsleitfaden", Href: "/workspace/app/docs?document=hiring-sop", Readable: true, Stale: true},
		{SlotID: "private", SectionID: "details", RequestAccess: true},
	}))
	if !strings.Contains(markup, "Einstellungsleitfaden") || !strings.Contains(markup, "möglicherweise") || !strings.Contains(markup, "nicht verfügbar") {
		t.Fatalf("localized document links missing: %s", markup)
	}
}

func TestTodo_WFPAGE_023(t *testing.T) {
	view := ApplyLocale(NewView(PageWork, "tenant", "viewer", "scope"), ResolveProductLocale("en-US"))
	reference := chat.WorkflowPageReference{TenantID: "tenant", WorkflowID: "new-hire", PageID: "new-hire.input", PageVersion: 3, DraftID: "draft-1"}
	support := ResolveWorkflowPageSupport(WorkflowPageSupportChannel{}, WorkflowPageSupportChannel{ID: "hr-help", Name: "HR help", Href: "/workspace/app/chat#channel=hr-help", Eligible: true}, reference, "HR Service Desk")
	markup := renderWorkflowPageK4(t, RenderWorkflowPageSupport(view, support))
	if !strings.Contains(markup, "Ask in channel") || !strings.Contains(markup, "WORKFLOW_PAGE_REFERENCE") || strings.Contains(markup, "salary") {
		t.Fatalf("support projection = %s", markup)
	}
	if got, ok := chat.NewWorkflowPageReference(reference); !ok || got.Display != "Workflow page" || strings.Contains(got.ID, "salary") {
		t.Fatalf("chat reference = %+v, ok=%v", got, ok)
	}
}

func TestTodo_WFPAGE_023_Security(t *testing.T) {
	ref := chat.WorkflowPageReference{TenantID: "tenant", WorkflowID: "new-hire", PageID: "new-hire.input", PageVersion: 3, RunID: "run-1"}
	chip := chat.ResolveWorkflowPageReferenceChip(ref, false, "New hire", "/workspace/app/workflow")
	if chip.Readable || chip.Href != "" || chip.WorkflowID != "" || !strings.Contains(chip.Label, "Restricted") {
		t.Fatalf("unauthorized chip disclosed context: %+v", chip)
	}
}

func TestTodo_WFPAGE_023_Browser(t *testing.T) {
	view := ApplyLocale(NewView(PageWork, "tenant", "viewer", "scope"), ResolveProductLocale("ar"))
	markup := renderWorkflowPageK4(t, RenderWorkflowPageSupport(view, WorkflowPageSupportProjection{FallbackContact: "HR", Reference: chat.WorkflowPageReference{}}))
	if !strings.Contains(markup, "دعم") || strings.Contains(markup, "WORKFLOW_PAGE_REFERENCE") {
		t.Fatalf("fallback support browser projection = %s", markup)
	}
}

func TestTodo_WFPAGE_024(t *testing.T) {
	view := ApplyLocale(NewView(PageWork, "tenant", "viewer", "scope"), ResolveProductLocale("en-US"))
	context := ProjectWorkflowPageContext(WorkflowPageContextProjection{
		WorkflowName: "New hire", PageID: "new-hire.input", PageVersion: 4,
		Subject: WorkflowPagePersonLink("worker-1", "Ada"), Position: WorkflowPagePositionLink("position-1", "Engineer"),
		Organization: WorkflowPageOrganizationLink("org-1", "People Operations"),
		FollowUpTask: WorkflowPageFollowUpLink("project-1", "task-1", "Prepare onboarding"),
		Approval:     WorkflowPageApprovalNotification{WorkflowName: "New hire", SubjectName: "Ada", PageVersionHref: "/workspace/app/workflows/new-hire?page_version=4", Readable: true},
	})
	markup := renderWorkflowPageK4(t, RenderWorkflowPageContext(view, context))
	for _, want := range []string{"Ada", "Engineer", "People Operations", "Create follow-up task", "New hire: Ada", "page-version", "/workspace/app/workflows/new-hire?page_version=4"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("context missing %q: %s", want, markup)
		}
	}
}

func TestTodo_WFPAGE_024_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(NewView(PageWork, "tenant", "viewer", "scope"), ResolveProductLocale(locale))
		markup := renderWorkflowPageK4(t, RenderWorkflowPageContext(view, WorkflowPageContextProjection{Subject: WorkflowPagePersonLink("worker-1", "Ada")}))
		if !strings.Contains(markup, "Ada") || strings.Contains(markup, "shell.") {
			t.Fatalf("%s context = %s", locale, markup)
		}
	}
}

func TestTodo_WFPAGE_024_Integration(t *testing.T) {
	context := ProjectWorkflowPageContext(WorkflowPageContextProjection{
		Subject:      WorkflowPageContextLink{Label: "Private subject", Href: "/workspace/app/person?person=private", Readable: false},
		Position:     WorkflowPageContextLink{Label: "Private position", Href: "/workspace/app/position?position_ref=private", Readable: false},
		Organization: WorkflowPageContextLink{Label: "Private org", Href: "/workspace/app/organization?selected=private", Readable: false},
		Approval:     WorkflowPageApprovalNotification{WorkflowName: "Private", SubjectName: "Private", Readable: false},
	})
	if context.Subject.Href != "" || context.Position.Href != "" || context.Organization.Href != "" || context.Approval.Readable {
		t.Fatalf("unauthorized integration context survived projection: %+v", context)
	}
}

func TestTodo_WFPAGE_025(t *testing.T) {
	view := ApplyLocale(NewView(PageWork, "tenant", "viewer", "scope"), ResolveProductLocale("en-US"))
	markup := renderWorkflowPageK4(t, RenderWorkflowPageConnections(view, WorkflowPageCapabilityAvailability{}, nil, WorkflowPageSupportProjection{}, WorkflowPageContextProjection{}))
	if !strings.Contains(markup, "Not set up for this workspace") || strings.Contains(markup, "Try again") || strings.Contains(markup, "Create follow-up") || strings.Contains(markup, "Ask in channel") {
		t.Fatalf("unavailable connections were interactive: %s", markup)
	}
}

func TestTodo_WFPAGE_025_Browser(t *testing.T) {
	view := ApplyLocale(NewView(PageWork, "tenant", "viewer", "scope"), ResolveProductLocale("ar"))
	markup := renderWorkflowPageK4(t, RenderWorkflowPageConnections(view, WorkflowPageCapabilityAvailability{}, nil, WorkflowPageSupportProjection{}, WorkflowPageContextProjection{}))
	if !strings.Contains(markup, "غير") || strings.Contains(markup, "Try again") {
		t.Fatalf("Arabic unavailable projection = %s", markup)
	}
}

func TestTodo_WFPAGE_025_Golden(t *testing.T) {
	view := ApplyLocale(NewView(PageWork, "tenant", "viewer", "scope"), ResolveProductLocale("en-US"))
	got := renderWorkflowPageK4(t, RenderWorkflowPageConnections(view, WorkflowPageCapabilityAvailability{}, nil, WorkflowPageSupportProjection{}, WorkflowPageContextProjection{}))
	want := "<section aria-atomic=\"true\" class=\"surface empty-state capability-unavailable\" role=\"status\"><h2>Not set up for this workspace</h2><p class=\"muted\">An administrator can enable this capability for the workspace.</p></section>"
	if got != want {
		t.Fatalf("workflow-page unavailable golden changed:\n got %s\nwant %s", got, want)
	}
}
