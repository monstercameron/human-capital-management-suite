package productui

import (
	stdhtml "html"
	"strings"
	"testing"
)

func TestAgentUXR5Ops_K31RunningFailureExplainsRecovery(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		markup := stdhtml.UnescapeString(renderAgentOperationsTest(t, RenderAgentControls(locale, AgentControlsSnapshot{}, "timed_out")))
		for _, want := range []string{agentControlsText(locale, "unavailable"), agentControlsText(locale, "timed_out_cause"), agentControlsText(locale, "unavailable_fallback_before"), `href="` + agentSetupHref(locale) + `"`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s running failure missing %q: %s", localeID, want, markup)
			}
		}
	}
	running := renderAgentOperationsTest(t, RenderAgentControls(ResolveProductLocale("en-US"), AgentControlsSnapshot{Available: true, Runs: []AgentControlRun{{ID: "run-1", Name: "General agent", State: "RUNNING", Revision: 1, Actions: []string{"pause"}}}}, "done"))
	if strings.Contains(running, "general-agent") || !strings.Contains(running, "Pause task General agent") {
		t.Fatalf("fallback running-agent row = %s", running)
	}
}

func TestAgentUXR5Ops_K32ToK34PortableExportAndImportControls(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	markup := renderAgentOperationsTest(t, AgentRolloutPortableMount(locale, AgentRolloutSnapshot{}, AgentPortableSnapshot{Available: true, CanExport: true, CanImport: true, Manifests: []AgentPortableManifest{{ID: "agent.starter.policy_helper", Version: "4", PersonaVersion: "4", Name: "Agent.starter.policy Helper", Live: true}}}))
	for _, want := range []string{"agent-portable-export-actions", "Download agent file", "Copy file contents", "Download an agent file first, then you can copy its contents.", "Only reviewed versions can be exported.", "Policy Helper \u00b7 File version 4", "Choose file", `id="agent-portable-file-name"`, "No file chosen", "agent-portable-file-input"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("portable controls missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Choose an export, import, or draft review action below.") || strings.Contains(markup, "Agent.starter.policy Helper") {
		t.Fatalf("portable controls retain raw or redundant copy: %s", markup)
	}
}

func TestAgentUXR5Ops_K33ReviewedVersionsNewestFirst(t *testing.T) {
	markup := renderAgentOperationsTest(t, AgentRolloutPortableMount(ResolveProductLocale("en-US"), AgentRolloutSnapshot{}, AgentPortableSnapshot{Available: true, CanExport: true, Manifests: []AgentPortableManifest{
		{ID: "policy-helper", Version: "3", Name: "Policy Helper"},
		{ID: "policy-helper", Version: "4", PersonaVersion: "4", Name: "Policy Helper", Live: true},
	}}))
	live := strings.Index(markup, "Policy Helper \u00b7 File version 4")
	reviewed := strings.Index(markup, "Policy Helper \u00b7 File version 3")
	if live < 0 || reviewed < 0 || live > reviewed {
		t.Fatalf("reviewed export versions are not newest first: %s", markup)
	}
}

func TestAgentUXR5Ops_K35RolloutWaitingVersionLinksToSetup(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		markup := renderAgentOperationsTest(t, AgentRolloutPortableMount(locale, AgentRolloutSnapshot{Available: true, WaitingName: "Policy Helper", WaitingVersion: 6, WaitingState: "IN_REVIEW"}, AgentPortableSnapshot{}))
		for _, want := range []string{"Policy Helper", locale.FormatNumber("6", 0), agentRPText(locale, "waiting_for_review"), `href="` + agentSetupHref(locale) + `"`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s waiting-version message missing %q: %s", localeID, want, markup)
			}
		}
	}
}

func TestAgentUXR5Ops_K36NarrowMoveTabUsesShortLabel(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		view := NewView(PageAgentOperations, "tenant", "admin", "")
		view.Locale = ResolveProductLocale(localeID)
		view.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
		markup := renderAgentOperationsTest(t, BuildAgentOperationsPage(view))
		if !strings.Contains(markup, agentOperationsText(view.Locale, "tab_portable_short")) {
			t.Fatalf("%s short move-tab label missing: %s", localeID, markup)
		}
	}
	css := Stylesheet()
	for _, want := range []string{".agent-operations-tab-label-short", "max-width:390px", "data-agent-operations-tab=\"move\""} {
		if !strings.Contains(css, want) {
			t.Fatalf("narrow tab stylesheet missing %q", want)
		}
	}
}
