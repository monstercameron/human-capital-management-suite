package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_AGENTUX_077: the Agents pages' first impression. The two links under
// the title are one spaced switch in the same family as the Ask / Setup /
// Operations one; breadcrumbs name the page; each start button carries its own
// one-line explanation; an announcement is one flat record with a labelled
// schedule row, a danger Delete and the shared disclosure.
func TestTodo_AGENTUX_077(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		asked := renderAgentUXPage5(t, localeID, AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true}, false)
		// AGENTUX-080: My agent access is one link at the end of the intro line,
		// not a second tab strip that repeats Ask.
		if strings.Contains(asked, "agents-view-tabs") || strings.Contains(asked, "agents-view-tab\"") {
			t.Fatalf("%s a second switch is still drawn under the title: %s", localeID, asked)
		}
		if lineAt := strings.Index(asked, `class="agents-view-line"`); lineAt < 0 || !strings.Contains(asked[lineAt:], `class="agents-access-link"`) || !strings.Contains(asked[lineAt:], ">"+agentAccessText(locale, "tab_access")+"<") {
			t.Fatalf("%s the access link is missing from the intro line: %s", localeID, asked)
		}
		if strings.Contains(asked, "agents-composer-actions-help") {
			t.Fatalf("%s one paragraph still explains both buttons: %s", localeID, asked)
		}
		// Each button is followed by its own explanation, in order.
		ask, askHint := strings.Index(asked, `data-agent-action="quick-answer"`), strings.Index(asked, agentUX077Text(locale, "ask_hint"))
		plan, planHint := strings.Index(asked, `data-agent-action="long-task"`), strings.Index(asked, agentUX077Text(locale, "plan_hint"))
		if !(ask >= 0 && ask < askHint && askHint < plan && plan < planHint) {
			t.Fatalf("%s button explanations out of place: ask %d/%d plan %d/%d", localeID, ask, askHint, plan, planHint)
		}
		for _, id := range []string{`id="agents-ask-hint"`, `id="agents-plan-hint"`, `aria-describedby="agents-ask-hint"`, `aria-describedby="agents-plan-hint"`} {
			if !strings.Contains(asked, id) {
				t.Fatalf("%s a button is not tied to its explanation: missing %s", localeID, id)
			}
		}
		// Breadcrumbs name the page they are on.
		if crumbs := agentUXR7Render(t, agentUXR7Breadcrumb(locale)); !strings.Contains(crumbs, locale.Text("page.chat.label")) || !strings.Contains(crumbs, locale.Text("agents.page_title")) || !strings.Contains(crumbs, `aria-hidden="true">/`) {
			t.Fatalf("%s the Agents breadcrumb is not a trail: %s", localeID, crumbs)
		}
		setup := agentUXR7Render(t, personaAdminBreadcrumb(locale))
		if !strings.Contains(setup, personaAdminText(locale, "breadcrumb_admin")) || !strings.Contains(setup, ">"+personaAdminText(locale, "title")+"<") {
			t.Fatalf("%s the Agent setup breadcrumb does not name Agent setup: %s", localeID, setup)
		}
		view := ApplyLocale(NewView(PageAgentOperations, "tenant", "owner", ""), locale)
		view.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
		ops := agentUXR7Render(t, BuildAgentOperationsPage(view))
		if !strings.Contains(ops, `aria-current="page">`+agentOperationsText(locale, "title")+`</span>`) {
			t.Fatalf("%s the Agent operations breadcrumb does not name Agent operations: %s", localeID, ops)
		}
	}

	// One announcement: flat, labelled, danger Delete, shared disclosure.
	locale := ResolveProductLocale("en-US")
	row := AgentAnnouncementRow{
		ID: "holiday", AgentName: "Assistant", ConversationName: "general", OwnerName: "Walt Brennan", State: "ACTIVE",
		Instruction: "Tell employees which company holidays are coming up.", ResultCode: "POSTED", LastRun: "Oct 1, 2026, 4:30 PM", LastRunAt: "2026-10-01T20:30:00Z",
		Editor:   AgentAnnouncementEditorValue{Cadence: "NOW"},
		Attempts: []AgentAnnouncementAttempt{{At: "2026-10-01T20:30:00Z", TimeLabel: "Oct 1, 2026, 4:30 PM", ResultCode: "POSTED"}},
	}
	card := agentUXR7Render(t, RenderAgentAnnouncements(locale, AgentAnnouncementsSnapshot{Available: true, CanCreate: true, Rows: []AgentAnnouncementRow{row}}))
	for _, want := range []string{"Posts once · already posted", `class="button danger compact agent-announcement-delete-action"`, `class="agent-announcement-history agent-operations-technical"`, "data-chevron="} {
		if !strings.Contains(card, want) {
			t.Fatalf("announcement is missing %q: %s", want, card)
		}
	}
	if strings.Contains(card, "No future post") || strings.Contains(card, "Once ·") {
		t.Fatalf("a one-time announcement reads as a fault: %s", card)
	}
	row.ResultCode = "FAILED"
	if failed := agentUXR7Render(t, RenderAgentAnnouncements(locale, AgentAnnouncementsSnapshot{Available: true, Rows: []AgentAnnouncementRow{row}})); !strings.Contains(failed, "Posts once · Not posted yet") {
		t.Fatalf("a one-time announcement that failed says it was posted: %s", failed)
	}
	// A recurring one keeps its cadence and its next post.
	row.Editor, row.NextRun, row.ResultCode = AgentAnnouncementEditorValue{Cadence: "DAILY", Time: "09:00"}, "Oct 8, 2026, 9:00 AM", "POSTED"
	if daily := agentUXR7Render(t, RenderAgentAnnouncements(locale, AgentAnnouncementsSnapshot{Available: true, Rows: []AgentAnnouncementRow{row}})); strings.Contains(daily, "Posts once") || !strings.Contains(daily, "Next post: Oct 8, 2026, 9:00 AM") {
		t.Fatalf("a recurring announcement lost its schedule: %s", daily)
	}
}

// TestTodo_AGENTUX_077_Browser: a loading Agents page shows its own title,
// breadcrumb and switches at once, with the shared loading rows below, in each
// language and direction; the sheet carries the rules that give the new pieces
// their layout.
func TestTodo_AGENTUX_077_Browser(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		for _, test := range []struct {
			page  PageID
			title string
			wants []string
		}{
			{PageAgents, locale.Text("agents.page_title"), []string{`agents-view-line`, locale.Text("page.chat.label")}},
			{PageAgentOperations, agentOperationsText(locale, "title"), []string{`id="agent-operations-tab-running"`, agentOperationsText(locale, "tab_running"), locale.Text("agents.nav.setup")}},
			{PagePersonaAdmin, personaAdminText(locale, "title"), []string{locale.Text("agents.nav.operations"), personaAdminText(locale, "breadcrumb_admin")}},
		} {
			markup, err := ui.RenderToString(loadingProxyBody(test.page, locale))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(markup, ">"+test.title+"</h1>") {
				t.Fatalf("%s %s: the loading page has no title: %s", localeID, test.page, markup)
			}
			wants := append([]string{`chatux037-loading`, `loading-block`, `dir="` + string(locale.Direction) + `"`, `aria-busy="true"`}, test.wants...)
			for _, want := range wants {
				if !strings.Contains(markup, want) {
					t.Fatalf("%s %s: the loading page is missing %q: %s", localeID, test.page, want, markup)
				}
			}
		}
	}
	css := agentUX073Stylesheet()
	for _, want := range []string{
		".persona-admin-breadcrumb{display:flex", ".agents-agent-choice:has(input:checked)", ".agents-composer-action{", ".agent-announcement-row{border:0",
		".agent-announcement-row .button.danger{", ".chatux037-bar{background:color-mix", "scroll-snap-type:y proximity",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("the agent stylesheet is missing %q", want)
		}
	}
}
