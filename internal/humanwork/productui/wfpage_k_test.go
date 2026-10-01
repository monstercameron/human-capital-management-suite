package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func workflowPageFixture() WorkflowPageDefinition {
	return WorkflowPageDefinition{ID: "hire", Sections: []WorkflowPageSection{
		{ID: "basics", Title: WorkflowPageText{ENUS: "Basics", DEDE: "Grunddaten", AR: "البيانات الأساسية"}, Fields: []WorkflowPageField{{ID: "name", Required: true}}, Blocks: []WorkflowPageContentBlock{
			{ID: "note", Kind: WorkflowPageNote, Title: WorkflowPageText{ENUS: "Hiring note", DEDE: "Hinweis", AR: "ملاحظة التوظيف"}, Markdown: WorkflowPageText{ENUS: "**Use the company way.** <script>alert(1)</script> [unsafe](javascript:alert(1))"}},
			{ID: "guide", Kind: WorkflowPageGuide, Title: WorkflowPageText{ENUS: "Guide", DEDE: "Leitfaden", AR: "إرشاد"}, Text: WorkflowPageText{ENUS: "Ask the hiring manager before you submit.", DEDE: "Fragen Sie vor dem Absenden die einstellende Führungskraft.", AR: "اسأل مدير التوظيف قبل الإرسال."}},
			{ID: "check", Kind: WorkflowPageChecklist, GateSubmit: true, Checklist: []WorkflowPageChecklistItem{{ID: "review", Required: true, Label: WorkflowPageText{ENUS: "I reviewed the details.", DEDE: "Ich habe die Angaben geprüft.", AR: "راجعت التفاصيل."}}}},
		}},
		{ID: "salary", Title: WorkflowPageText{ENUS: "Salary", DEDE: "Gehalt", AR: "الراتب"}, Rule: WorkflowPageRule{Field: "offer", Equals: "yes"}, Fields: []WorkflowPageField{{ID: "salary", Required: true}}},
	}}
}

func TestTodo_WFPAGE_018(t *testing.T) {
	page := workflowPageFixture()
	validation := ValidateWorkflowPage(page, map[string]string{"name": "Ada", "salary": "secret", "offer": "no"}, map[string]bool{})
	if validation.SubmitAllowed || len(validation.Errors) != 1 || !strings.Contains(validation.Errors[0], "review") {
		t.Fatalf("validation = %+v; hidden salary must not validate and checklist must gate submit", validation)
	}
	submission, validation := ProjectWorkflowPageSubmission(page, map[string]string{"name": "Ada", "salary": "secret", "offer": "no"}, map[string]bool{"review": true})
	if !validation.SubmitAllowed || submission.Values["salary"] != "" || submission.Values["name"] != "Ada" {
		t.Fatalf("submission = %+v, validation = %+v; hidden values leaked", submission, validation)
	}
	markup, err := ui.RenderToString(RenderWorkflowPageContent(ApplyLocale(NewView(PageWork, "tenant", "user", "scope"), ResolveProductLocale("en-US")), page, map[string]string{"name": "Ada", "offer": "no"}, map[string]bool{"review": true}))
	if err != nil || strings.Contains(markup, "<script") || strings.Contains(markup, `href="javascript:`) || !strings.Contains(markup, "Use the company way.") || strings.Contains(markup, "Salary") {
		t.Fatalf("content render err=%v markup=%s", err, markup)
	}
}

func TestTodo_WFPAGE_018_Browser(t *testing.T) {
	view := ApplyLocale(NewView(PageWork, "tenant", "user", "scope"), ResolveProductLocale("ar"))
	markup, err := ui.RenderToString(RenderWorkflowPageContent(view, workflowPageFixture(), map[string]string{"offer": "yes"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dir=\"rtl\"", "workflow-page-checklist", "البيانات الأساسية", "الراتب", "type=\"checkbox\""} {
		if !strings.Contains(markup, want) {
			t.Fatalf("browser projection missing %q: %s", want, markup)
		}
	}
}

func TestTodo_WFPAGE_018_Golden(t *testing.T) {
	page := workflowPageFixture()
	var b strings.Builder
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(NewView(PageWork, "tenant", "user", "scope"), ResolveProductLocale(locale))
		markup, err := ui.RenderToString(RenderWorkflowPageContent(view, page, map[string]string{"offer": "no"}, map[string]bool{"review": true}))
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString(locale)
		b.WriteByte(0)
		b.WriteString(markup)
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	if got, want := hex.EncodeToString(sum[:]), "7074e0e6b5f93f8524eac73d55804492aa0f2849168e36b87f4ba8e69fbcee51"; got != want {
		t.Fatalf("content golden digest = %s, want %s", got, want)
	}
}

func TestTodo_WFPAGE_019(t *testing.T) {
	locale := ResolveProductLocale("de-DE")
	got := ResolveWorkflowPagePrefill(locale, []WorkflowPagePrefillBinding{
		{FieldID: "name", Source: PrefillPerson, SourceField: "preferred_name", SourceLabel: WorkflowPageText{ENUS: "Person", DEDE: "Person", AR: "الشخص"}, Authorized: AuthorizedField{Effect: PresentationAllow}, AllowOverride: true},
		{FieldID: "salary", Source: PrefillPerson, SourceField: "base_pay", SourceLabel: WorkflowPageText{ENUS: "Person", DEDE: "Person", AR: "الشخص"}, Authorized: AuthorizedField{Effect: PresentationDenied, Reason: "Restricted"}},
	}, map[PrefillSource]map[string]string{PrefillPerson: {"preferred_name": "Ada", "base_pay": "90000"}})
	if len(got) != 2 || got[0].Value != "Ada" || !got[0].AllowOverride || got[1].Value != "" || got[1].Reason != "Restricted" || got[1].SourceLabel != "Person" {
		t.Fatalf("prefill projection = %+v", got)
	}
}

func TestTodo_WFPAGE_019_Security(t *testing.T) {
	got := ResolveWorkflowPagePrefill(ResolveProductLocale("en-US"), []WorkflowPagePrefillBinding{{FieldID: "pay", Source: PrefillPerson, SourceField: "base_pay", Authorized: AuthorizedField{Disposition: FieldMask, StandIn: "Masked"}}}, map[PrefillSource]map[string]string{PrefillPerson: {"base_pay": "90000"}})
	if len(got) != 1 || got[0].Value != "" || strings.Contains(got[0].Reason, "90000") {
		t.Fatalf("restricted prefill reached client: %+v", got)
	}
}

func TestTodo_WFPAGE_020(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	store := NewWorkflowPageDraftStore(func() time.Time { return now })
	key := WorkflowPageDraftKey{TenantID: "tenant", UserID: "user", WorkflowID: "hire", SubjectID: "person"}
	draft, status, err := SaveWorkflowPageDraft(store, WorkflowPageDraft{Key: key, PageID: "hire-page", PageVersion: 3, Values: map[string]string{"name": "Ada"}}, map[string]string{"name": "Ada"}, ResolveProductLocale("en-US"))
	if err != nil || status.State != DraftAutosaveSaved || draft.Values["name"] != "Ada" {
		t.Fatalf("save = %+v, status = %+v, err = %v", draft, status, err)
	}
	resume := store.Resume(key, 4, ResolveProductLocale("en-US"), map[string]string{"name": "Ada"})
	if !resume.Found || !resume.Stale || resume.Draft.Values["name"] != "Ada" {
		t.Fatalf("resume = %+v", resume)
	}
}

func TestTodo_WFPAGE_020_Browser(t *testing.T) {
	locale := ResolveProductLocale("de-DE")
	store := NewWorkflowPageDraftStore(func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) })
	key := WorkflowPageDraftKey{TenantID: "t", UserID: "u", WorkflowID: "w", SubjectID: "s"}
	if _, _, err := SaveWorkflowPageDraft(store, WorkflowPageDraft{Key: key, PageID: "p", PageVersion: 1, Values: map[string]string{"x": "y"}}, map[string]string{"x": "y"}, locale); err != nil {
		t.Fatal(err)
	}
	if got := store.Resume(key, 1, locale, map[string]string{"x": "y"}); got.Status.Text != locale.Text("draft.autosave_saved") {
		t.Fatalf("localized resume status = %+v", got.Status)
	}
}

func TestTodo_WFPAGE_020_Fault(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	store := NewWorkflowPageDraftStore(func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) })
	store.SaveErr = errors.New("storage unavailable")
	key := WorkflowPageDraftKey{TenantID: "t", UserID: "u", WorkflowID: "w", SubjectID: "s"}
	local := map[string]string{"name": "Ada"}
	draft, status, err := SaveWorkflowPageDraft(store, WorkflowPageDraft{Key: key, PageID: "p", PageVersion: 1}, local, locale)
	if err == nil || status.State != DraftAutosaveFailed || draft.Values["name"] != "Ada" || !strings.Contains(status.Detail, "storage unavailable") {
		t.Fatalf("failed save lost local copy: draft=%+v status=%+v err=%v", draft, status, err)
	}
}
