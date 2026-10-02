package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestAgentUXProactive_RenderedTab(t *testing.T) {
	wants := map[string][]string{
		"en-US": {"Announcements", "New announcement", "What should it post?", "Save schedule"},
		"de-DE": {"Ankündigungen", "Neue Ankündigung", "Was soll veröffentlicht werden?", "Zeitplan speichern"},
		"ar":    {"الإعلانات", "إعلان جديد", "ماذا ينبغي أن ينشر؟", "حفظ الجدول"},
	}
	for localeID, localized := range wants {
		view := ApplyLocale(NewView(PageAgentOperations, "tenant", "owner", ""), ResolveProductLocale(localeID))
		view.Query = "announcements"
		view.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
		markup, err := ui.RenderToString(BuildAgentOperationsPage(view))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range append(localized, `data-agent-operations-tab="announcements"`, `aria-selected="true"`, `data-agent-announcements-mount="true"`) {
			if !strings.Contains(markup, want) {
				t.Errorf("%s missing %q: %s", localeID, want, markup)
			}
		}
		if !strings.Contains(markup, `dir="`+string(view.Locale.Direction)+`"`) || strings.Contains(markup, `value="Tell employees`) {
			t.Errorf("%s lost direction or made the controlled textarea immutable: %s", localeID, markup)
		}
	}
}

func TestAgentUXProactive_RenderedTab_390px(t *testing.T) {
	markup, err := ui.RenderToString(RenderAgentAnnouncements(ResolveProductLocale("en-US"), AgentAnnouncementsSnapshot{Available: true, CanCreate: true, Rows: []AgentAnnouncementRow{{ID: "opaque", AgentName: "Policy Helper", ConversationName: "General", Instruction: "Tell employees which company holidays are coming up.\nDo not include internal notes.", NextRun: "Monday, 9:00 AM", LastResult: "Posted", MessageHref: "/workspace/app/chat?message=posted", State: "ACTIVE", Revision: 3}}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Policy Helper · General", "Tell employees which company holidays are coming up.", "Monday, 9:00 AM", "Posted", "Pause", "Post now", "Edit", "Delete", "Delete this announcement?"} {
		if !strings.Contains(markup, want) {
			t.Errorf("390px announcement surface missing %q: %s", want, markup)
		}
	}
	// The narrow layout is in the product stylesheet, the one sheet the page's
	// content security policy admits; the panel emits no style element.
	for _, want := range []string{"@media(max-width:800px)", "grid-template-columns:minmax(0,1fr)"} {
		if !strings.Contains(AgentAnnouncementsStyles, want) || !strings.Contains(Stylesheet(), AgentAnnouncementsStyles) {
			t.Errorf("390px announcement rules missing %q from the product stylesheet", want)
		}
	}
	if strings.Contains(markup, "<style") {
		t.Fatal("the announcements panel emits a style element the content security policy refuses")
	}
	if strings.Contains(markup, "opaque</") || strings.Contains(markup, "AGENTUX") || strings.Contains(markup, "announcement:opaque") {
		t.Fatalf("announcement row exposed an internal identifier: %s", markup)
	}
}

func TestAgentUXProactive_TabMountSurvivesNavigation(t *testing.T) {
	for _, selected := range []bool{false, true} {
		markup, err := ui.RenderToString(AgentAnnouncementsMountForTab(ResolveProductLocale("en-US"), selected))
		if err != nil {
			t.Fatal(err)
		}
		root := markup[:strings.IndexByte(markup, '>')]
		if !strings.Contains(root, `id="agent-announcements"`) || !strings.Contains(root, `data-agent-operations-panel="announcements"`) || strings.Contains(root, ` hidden`) == selected {
			t.Fatalf("tab panel cannot be selected after navigation: %s", markup)
		}
	}
}
