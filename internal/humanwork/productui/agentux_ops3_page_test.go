package productui

import (
	"html"
	"strings"
	"testing"
)

func TestAgentUXOps3_RunningStates(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	loading := renderAgentOperationsTest(t, AgentControlsMount(locale))
	if !strings.Contains(loading, `aria-busy="true"`) || !strings.Contains(loading, "Loading authorized controls") {
		t.Fatalf("loading state = %s", loading)
	}
	errorMarkup := renderAgentOperationsTest(t, RenderAgentControls(locale, AgentControlsSnapshot{}, "error"))
	for _, want := range []string{`role="alert"`, "Couldn&#39;t load running agents.", "The agent service is unavailable right now.", "To pause an agent now, open", "Setup", "Retry"} {
		if !strings.Contains(errorMarkup, want) {
			t.Errorf("error state missing %q: %s", want, errorMarkup)
		}
	}
	if strings.Contains(errorMarkup, "up to date") {
		t.Fatalf("error state also claimed success: %s", errorMarkup)
	}
	recent := AgentControlRun{ID: "opaque-failed", Name: "Policy Helper", Version: "2", State: "FAILED", Started: "2026-10-01T13:00:00Z", Duration: "12 seconds", RequestedBy: "Walt Brennan", Location: "#general", Failure: "MODEL_UNAVAILABLE"}
	empty := renderAgentOperationsTest(t, RenderAgentControls(locale, AgentControlsSnapshot{Available: true, UpdatedAt: "2026-10-01T13:01:00Z", Runs: []AgentControlRun{recent}}, "done"))
	for _, want := range []string{"No agents are running right now.", "Recent finished runs", "Walt Brennan", "#general", "12 seconds", "The agent could not answer because the service it needed was unavailable."} {
		if !strings.Contains(empty, want) {
			t.Errorf("empty/recent state missing %q: %s", want, empty)
		}
	}
	running := recent
	running.ID, running.State, running.Failure, running.Actions = "opaque-running", "RUNNING", "", []string{"pause"}
	active := renderAgentOperationsTest(t, RenderAgentControls(locale, AgentControlsSnapshot{Available: true, UpdatedAt: "now", Runs: []AgentControlRun{running}}, "done"))
	if strings.Contains(active, "No agents are running") || !strings.Contains(active, "Pause task Policy Helper, version 2") {
		t.Fatalf("running state = %s", active)
	}
}

func TestTodo_AGENTUX_036(t *testing.T) {
	codes := []string{"CONTEXT_UNAVAILABLE", "MODEL_UNAVAILABLE", "MODEL_REFUSED_OR_INCOMPLETE", "TOOL_EXECUTION_FAILED", "OUTPUT_REJECTED", "DELIVERY_FAILED", "OUT_OF_SCOPE"}
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		for _, code := range codes {
			reason, action := agentOpsFailureCopy(locale, code)
			if strings.TrimSpace(reason) == "" || strings.TrimSpace(action) == "" || reason == code || action == code {
				t.Errorf("%s %s reason=%q action=%q", localeID, code, reason, action)
			}
			if strings.Count(reason, ".") != 1 || strings.Count(action, ".") != 1 {
				t.Errorf("%s %s must map to one reason sentence and one action sentence: %q / %q", localeID, code, reason, action)
			}
			markup := html.UnescapeString(renderAgentOperationsTest(t, agentOpsRecentRun(locale, AgentControlRun{ID: "opaque", Name: "Policy Helper", State: "FAILED", Failure: code})))
			if !strings.Contains(markup, reason) || !strings.Contains(markup, action) {
				t.Errorf("%s %s rendered row omitted plain-language help: %s", localeID, code, markup)
			}
		}
	}
}

func TestTodo_AGENTUX_036_Security(t *testing.T) {
	markup := renderAgentOperationsTest(t, agentOpsRecentRun(ResolveProductLocale("en-US"), AgentControlRun{ID: "opaque", Name: "Policy Helper", State: "FAILED", Failure: "OUTPUT_REJECTED"}))
	if strings.Contains(markup, "OUTPUT_REJECTED") {
		t.Fatalf("failure code exposed as owner-facing content: %s", markup)
	}
	for _, secret := range []string{"request body", "document body", "model output"} {
		if strings.Contains(markup, secret) {
			t.Fatalf("failure projection leaked %q: %s", secret, markup)
		}
	}
}

func TestTodo_AGENTUX_036_Browser(t *testing.T) {
	markup := renderAgentOperationsTest(t, agentOpsRecentRun(ResolveProductLocale("en-US"), AgentControlRun{ID: "opaque", Name: "Policy Helper", Version: "2", State: "FAILED", Started: "9:40 AM", Duration: "2m", RequestedBy: "Walt Brennan", Location: "#general · Public channel · 18 members", Failure: "DELIVERY_FAILED"}))
	for _, want := range []string{"Policy Helper, version 2", "Who asked", "Where", "Started", "Duration", "Failed", "What happened", "What to do", `class="agent-operations-run-details"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("finished-run row missing %q: %s", want, markup)
		}
	}
}

func TestAgentUXOps3_RolloutNamesAndCopy(t *testing.T) {
	snapshot := AgentRolloutSnapshot{Available: true, CanPreview: true,
		Personas: []AgentRolloutPersona{{ID: "policy-helper", Name: "Policy Helper"}},
		Versions: []AgentRolloutVersion{{PersonaID: "policy-helper", Version: 4, PublishedAt: "2026-09-30", Current: true}},
		Installations: []AgentRolloutInstallation{
			{ID: "general", PersonaID: "policy-helper", Name: "general", ConversationKind: "PUBLIC_CHANNEL", MemberCount: 18, Visible: true, Version: 4, CanaryEligible: true},
			{ID: "hidden", PersonaID: "policy-helper", MemberCount: 3, Version: 4, CanaryEligible: true},
		},
	}
	markup := renderAgentOperationsTest(t, AgentRolloutPortableMount(ResolveProductLocale("en-US"), snapshot, AgentPortableSnapshot{Available: true, CanExport: true, Manifests: []AgentPortableManifest{{ID: "policy-helper", Version: "2", Name: "Answer tenant-wide policy questions with cited policy documents.."}}}))
	for _, want := range []string{"Move conversations from the version they run now", "Version to roll out", "Version 4 (current) · published Sep 30", "All 2 conversations already run version 4. Nothing to roll out.", "Agent · File version 2"} {
		if !strings.Contains(markup, want) {
			t.Errorf("rollout missing %q: %s", want, markup)
		}
	}
	for _, forbidden := range []string{"Ready for an authorized preview.", "Target version", "Try first in", "Answer tenant-wide", "<legend"} {
		if strings.Contains(markup, forbidden) {
			t.Errorf("rollout retained %q: %s", forbidden, markup)
		}
	}
}

func TestAgentUXOps3_TabsAndStyles(t *testing.T) {
	view := ApplyLocale(NewView(PageAgentOperations, "tenant", "admin", ""), ResolveProductLocale("en-US"))
	view.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
	markup := renderAgentOperationsTest(t, BuildAgentOperationsPage(view))
	for _, want := range []string{`role="tablist"`, `data-agent-operations-tab="running"`, `tab=rollout`, `tab=move`, "Move between workspaces", `data-agent-operations-panel="running"`, `role="tabpanel"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("tabs missing %q: %s", want, markup)
		}
	}
	css := Stylesheet()
	for _, selector := range []string{".agent-operations-tabs", "#agent-rollout-portable .agent-rollout-choice input", "#agent-rollout-portable .agent-rollout-number input", ".agent-operations-alert"} {
		if !strings.Contains(css, selector) {
			t.Errorf("stylesheet missing %s", selector)
		}
	}
	if !strings.Contains(css, "inline-size:18px") || !strings.Contains(css, "inline-size:96px") || !strings.Contains(css, "background:transparent") {
		t.Fatalf("control sizing or dark-mode checkbox contract missing: %s", css)
	}
}
