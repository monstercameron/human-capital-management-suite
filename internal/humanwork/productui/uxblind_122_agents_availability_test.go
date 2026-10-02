package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func uxblind122View(t *testing.T, locale string, projection *AgentsAvailabilityProjection) View {
	t.Helper()
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", ""), ResolveProductLocale(locale))
	view = ApplyPagePermissions(view, []RolePagePermission{
		{RoleID: "r", Page: PageHome, View: true}, {RoleID: "r", Page: PageChat, View: true},
		{RoleID: "r", Page: PageAgents, View: true}, {RoleID: "r", Page: PageAdmin, View: true},
		{RoleID: "r", Page: PageChatSettings, View: true, Update: true}, {RoleID: "r", Page: PageSettings, View: true},
	})
	if projection != nil {
		view = ApplyAgentsAvailability(view, *projection)
	}
	return ApplyLocale(view, ResolveProductLocale(locale))
}

func uxblind122Snapshot() AgentSnapshot {
	return AgentSnapshot{Availability: AgentsAvailable, Tasks: []AgentTask{{ID: "task-own", Title: "Prepare quarterly summary", Goal: "Summarise my open requests", State: AgentTaskRunning}}}
}

func navHasAgents(items []NavItem) bool {
	for _, item := range items {
		if item.Page == PageAgents || navHasAgents(item.Children) {
			return true
		}
	}
	return false
}

func renderUXBLIND122(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestTodo_UXBLIND_122(t *testing.T) {
	cases := []struct {
		name                  string
		enabled, admin        bool
		wantState             AgentsSurfaceState
		wantNav               bool
		wantText, forbidTexts []string
	}{
		{name: "enabled admin", enabled: true, admin: true, wantState: AgentsSurfaceEnabled, wantNav: true, wantText: []string{"Prepare quarterly summary", "Agents"}, forbidTexts: []string{"agents-disabled"}},
		{name: "enabled regular", enabled: true, wantState: AgentsSurfaceEnabled, wantNav: true, wantText: []string{"Prepare quarterly summary"}, forbidTexts: []string{"agents-disabled"}},
		{name: "disabled admin", admin: true, wantState: AgentsSurfaceDisabledAdmin, wantNav: true, wantText: []string{"Agents are turned off for your organization", "Turn agents on in Chat settings", `href="/workspace/app/admin/chat-settings"`, "Open agent settings"}, forbidTexts: []string{"Prepare quarterly summary"}},
		{name: "disabled regular", wantState: AgentsSurfaceDisabledHidden, wantNav: false, wantText: []string{"Agents are not available", "Ask an administrator", `href="/workspace/app/chat"`}, forbidTexts: []string{"Prepare quarterly summary", "chat-settings", "Open agent settings"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projection := AgentsAvailabilityProjection{Enabled: tc.enabled, ViewerIsAdmin: tc.admin, Snapshot: uxblind122Snapshot()}
			view := uxblind122View(t, "en-US", &projection)
			surface := ResolveAgentsSurface(view.AgentsProjection)
			if surface.State != tc.wantState || surface.NavVisible != tc.wantNav {
				t.Fatalf("surface = %+v, want state %s nav %v", surface, tc.wantState, tc.wantNav)
			}
			// The same projection drives the menu, global search and the page.
			if got := navHasAgents(view.Navigation); got != tc.wantNav {
				t.Fatalf("navigation lists Agents = %v, want %v", got, tc.wantNav)
			}
			if got := authorizedNavigationPages(view)[PageAgents]; got != tc.wantNav {
				t.Fatalf("authorized destinations list Agents = %v, want %v", got, tc.wantNav)
			}
			if !tc.wantNav {
				for _, item := range view.Navigation {
					if item.Page == PageChat && len(item.Children) != 0 {
						t.Fatalf("Chat kept a lone overview child after Agents was hidden: %+v", item.Children)
					}
				}
			}
			markup, err := renderPage(view)
			if err != nil {
				t.Fatal(err)
			}
			page := renderUXBLIND122(t, markup)
			if tc.wantState != AgentsSurfaceEnabled && !strings.Contains(page, `data-agents-surface="`+string(tc.wantState)+`"`) {
				t.Fatalf("page did not render state %s: %s", tc.wantState, page)
			}
			for _, want := range tc.wantText {
				if !strings.Contains(page, want) {
					t.Fatalf("page missing %q: %s", want, page)
				}
			}
			for _, forbidden := range tc.forbidTexts {
				if strings.Contains(page, forbidden) {
					t.Fatalf("page must not contain %q: %s", forbidden, page)
				}
			}
			if strings.Count(page, "<h1") != 1 {
				t.Fatalf("agents page must have exactly one h1: %s", page)
			}
			// The administrator's setting sits on Chat settings and nowhere
			// else; regular viewers do not get the control.
			view.Page = PageChatSettings
			settings := renderUXBLIND122(t, chatSettingsWithAgents(view))
			if got := strings.Contains(settings, `id="agents-setting-toggle"`); got != tc.admin {
				t.Fatalf("agents setting control present = %v for admin=%v: %s", got, tc.admin, settings)
			}
		})
	}
	// A component preview with no server answer keeps registry navigation and
	// the truthful unavailable page.
	preview := uxblind122View(t, "en-US", nil)
	if !navHasAgents(preview.Navigation) || ResolveAgentsSurface(nil).State != AgentsSurfaceUnresolved {
		t.Fatal("a preview without a projection changed the registry navigation")
	}
	if page := renderUXBLIND122(t, BuildAgentsSurface(preview)); !strings.Contains(page, "Agents could not be loaded") {
		t.Fatalf("preview page = %s", page)
	}
}

func TestTodo_UXBLIND_122_Localized(t *testing.T) {
	for locale, want := range map[string][]string{
		"de-DE": {"Agenten sind für Ihre Organisation ausgeschaltet", "Agenteneinstellungen öffnen"},
		"ar":    {"الوكلاء متوقفون لمؤسستك", "فتح إعدادات الوكلاء"},
	} {
		view := uxblind122View(t, locale, &AgentsAvailabilityProjection{ViewerIsAdmin: true})
		page := renderUXBLIND122(t, BuildAgentsSurface(view))
		for _, text := range want {
			if !strings.Contains(page, text) {
				t.Fatalf("%s page missing %q: %s", locale, text, page)
			}
		}
		view.Page = PageChatSettings
		settings := renderUXBLIND122(t, chatSettingsWithAgents(view))
		if !strings.Contains(settings, ResolveProductLocale(locale).Text("agents.setting_turn_on")) || strings.Contains(settings, "Turn on agents") {
			t.Fatalf("%s setting is not localized: %s", locale, settings)
		}
	}
	for _, locale := range []string{"de-DE", "ar"} {
		for key, english := range agentsAvailabilityCopy[DefaultProductLocale] {
			if got := ResolveProductLocale(locale).Text(key); got == "" || got == english || got == key {
				t.Fatalf("%s: %s is untranslated (%q)", locale, key, got)
			}
		}
	}
}

func TestTodo_UXBLIND_122_Security(t *testing.T) {
	// A forged disabled payload carrying tasks, a foreign settings link and
	// an arbitrary reason renders none of them.
	forged := AgentsAvailabilityProjection{ViewerIsAdmin: true, ReasonKey: "shell.page_recovery", SettingsHref: "https://evil.example/steal", Snapshot: uxblind122Snapshot()}
	normalized := NormalizeAgentsAvailability(forged)
	if len(normalized.Snapshot.Tasks) != 0 || normalized.SettingsHref != AgentsSettingsHref() || normalized.ReasonKey != AgentsReasonTenantDisabled {
		t.Fatalf("normalization kept forged fields: %+v", normalized)
	}
	view := uxblind122View(t, "en-US", &forged)
	page := renderUXBLIND122(t, BuildAgentsSurface(view))
	if strings.Contains(page, "Prepare quarterly summary") || strings.Contains(page, "evil.example") {
		t.Fatalf("a disabled projection disclosed forged data: %s", page)
	}
	// A regular viewer's forged settings link is dropped entirely.
	if got := NormalizeAgentsAvailability(AgentsAvailabilityProjection{SettingsHref: "/workspace/app/admin/chat-settings", ReasonKey: AgentsReasonTenantDisabled}); got.SettingsHref != "" || got.ReasonKey != "" {
		t.Fatalf("regular viewer kept an admin link: %+v", got)
	}
	// Enabled but the agent service failed: truthful unavailable state, no data.
	failed := AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsUnavailable, Tasks: uxblind122Snapshot().Tasks}}
	page = renderUXBLIND122(t, BuildAgentsSurface(uxblind122View(t, "en-US", &failed)))
	if !strings.Contains(page, "Agents could not be loaded") || strings.Contains(page, "Prepare quarterly summary") {
		t.Fatalf("a failed agent service rendered data: %s", page)
	}
	// The zero projection (what a composed client installs when the server
	// sent none) hides the entry and shows no data.
	zero := uxblind122View(t, "en-US", &AgentsAvailabilityProjection{})
	if navHasAgents(zero.Navigation) || ResolveAgentsSurface(zero.AgentsProjection).State != AgentsSurfaceDisabledHidden {
		t.Fatal("a missing server answer did not fail closed")
	}
	// Threads are never invented: an enabled snapshot without threads is an
	// empty non-nil list.
	if enabled := NormalizeAgentsAvailability(AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable}}); enabled.Snapshot.Threads == nil || len(enabled.Snapshot.Threads) != 0 {
		t.Fatalf("threads = %#v", enabled.Snapshot.Threads)
	}
}
