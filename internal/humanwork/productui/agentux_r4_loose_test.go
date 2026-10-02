package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestAgentUXR4_AgentOperationsNavigationFollowsProjectedFeatureAccess(t *testing.T) {
	base := ApplyPagePermissions(testView(PageHome), []RolePagePermission{
		{Page: PageHome, View: true},
		{Page: PageAdmin, View: true},
		{Page: PagePersonaAdmin, View: true},
		{Page: PageAgentOperations, View: true},
	})
	owner := ApplyFeaturePermissions(base, []RoleFeaturePermission{{Page: PageAdmin, Feature: FeatureContent, View: true}, {Page: PageAgentOperations, Feature: FeatureContent, View: true}})
	_, ownerItems := projectNavigation(owner)
	admin, ok := projectedNavigationItem(ownerItems, PageAdmin)
	if !ok {
		t.Fatal("owner navigation lost Admin")
	}
	if _, ok := projectedNavigationItem(admin.Children, PageAgentOperations); !ok {
		t.Fatalf("owner navigation omitted Agent operations despite matching route access: %+v", admin.Children)
	}

	employee := ApplyFeaturePermissions(base, []RoleFeaturePermission{{Page: PageAdmin, Feature: FeatureContent, View: true}, {Page: PagePersonaAdmin, Feature: FeatureContent, View: true}})
	_, employeeItems := projectNavigation(employee)
	admin, ok = projectedNavigationItem(employeeItems, PageAdmin)
	if !ok {
		t.Fatal("employee navigation lost Admin")
	}
	if _, ok := projectedNavigationItem(admin.Children, PageAgentOperations); ok {
		t.Fatalf("employee navigation exposed Agent operations without feature access: %+v", admin.Children)
	}
}

func TestAgentUXR4_FavoritesAndChatChildLabelsAreLocalized(t *testing.T) {
	for _, code := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(code)
		view := ApplyLocale(testView(PageChat), locale)
		view.FavoritePages = []PageID{PageChat}
		favorites, items := projectNavigation(view)
		if len(favorites) != 1 || favorites[0].Label != locale.Text("page.chat.label") {
			t.Fatalf("%s favorite = %+v, want section label", code, favorites)
		}
		chat, ok := projectedNavigationItem(items, PageChat)
		if !ok || len(chat.Children) == 0 {
			t.Fatalf("%s Chat navigation missing children: %+v", code, items)
		}
		if chat.Children[0].Label != locale.Text("nav.conversations") {
			t.Fatalf("%s first Chat child = %q, want %q", code, chat.Children[0].Label, locale.Text("nav.conversations"))
		}
	}
}

func TestAgentUXR4_AudienceAndProblemCopyIsLocalized(t *testing.T) {
	wantAudience := map[string]string{
		"en-US": "People in these {count} roles who are members of a conversation where this agent is added",
		"de-DE": "Personen in diesen {count} Rollen, die Mitglieder einer Unterhaltung sind, der dieser Agent hinzugefügt wurde",
		"ar":    "الأشخاص في هذه الأدوار الـ {count} الذين هم أعضاء في محادثة أضيف إليها هذا الوكيل",
	}
	for code, want := range wantAudience {
		if got := personaAdminText(ResolveProductLocale(code), "everyone_roles"); got != want {
			t.Fatalf("%s everyone_roles = %q, want %q", code, got, want)
		}
	}
}

func TestAgentUXR4_OwnerAndDocumentRowsHideAmbiguousIdentifiers(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person, err := ui.RenderToString(personaAdminPersonDefinition(locale, "Business owner", "ir-001-walt-brennan", "", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(person, "Unknown person") || strings.Contains(person, ">ir-001-walt-brennan<") || !strings.Contains(person, ">UP<") {
		t.Fatalf("unknown owner chip leaked an identifier or lost fallback initials: %s", person)
	}

	picker, err := ui.RenderToString(AgentDocumentReferencePicker(locale, AgentDocumentReferencePickerModel{ID: "docs", Available: true, State: AgentDocumentPickerReady, References: []PersonaAdminDocumentReference{{DocumentID: "doc-a", Title: "Leave policy", Label: "Leave policy", Location: "People Operations", Readable: true}}}))
	if err != nil {
		t.Fatal(err)
	}
	summary, err := ui.RenderToString(personaAdminDocumentReferences(locale, "persona-a", []PersonaAdminDocumentReference{{DocumentID: "doc-a", Title: "Leave policy", Label: "Leave policy", Location: "People Operations", Readable: true}}))
	if err != nil {
		t.Fatal(err)
	}
	for name, markup := range map[string]string{"picker": picker, "summary": summary} {
		if !strings.Contains(markup, "Leave policy") || !strings.Contains(markup, "People Operations") {
			t.Fatalf("%s row missing document location: %s", name, markup)
		}
	}
}

func TestAgentUXR4_AnsweredTasksShowUsedDocumentState(t *testing.T) {
	for _, code := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(code)
		view := ApplyLocale(NewView(PageAgents, "tenant", "owner", ""), locale)
		task := AgentTask{ID: "task-used", Title: "Review leave", State: AgentTaskCompleted, UpdatedAt: time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC), DocumentUsageState: AgentDocumentUsageUsed, UsedDocuments: []AgentTaskDocumentReference{{DocumentID: "doc-leave", Label: "Leave policy"}}}
		markup, err := ui.RenderToString(RenderAgentTaskDetail(view, locale, task, true))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, locale.Text("agents.used_documents")) || !strings.Contains(markup, "Leave policy") {
			t.Fatalf("%s used document state missing: %s", code, markup)
		}
		none := task
		none.ID = "task-none"
		none.DocumentUsageState = AgentDocumentUsageNone
		none.UsedDocuments = nil
		markup, err = ui.RenderToString(RenderAgentTaskDetail(view, locale, none, true))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, locale.Text("agents.no_documents_used")) {
			t.Fatalf("%s no-documents state missing: %s", code, markup)
		}
		unknown := task
		unknown.ID = "task-unknown"
		unknown.DocumentUsageState = AgentDocumentUsageUnknown
		unknown.UsedDocuments = nil
		markup, err = ui.RenderToString(RenderAgentTaskDetail(view, locale, unknown, true))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(markup, locale.Text("agents.used_documents")) || strings.Contains(markup, locale.Text("agents.no_documents_used")) {
			t.Fatalf("%s unknown state rendered document usage: %s", code, markup)
		}
	}
}
