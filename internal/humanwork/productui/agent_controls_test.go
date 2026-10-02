package productui

import (
	"encoding/json"
	stdhtml "html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_AGENT_030_Controls(t *testing.T) {
	snapshot := AgentControlsSnapshot{Available: true, CanDraft: true, Schedules: []AgentControlSchedule{{ID: "digest", Revision: 7, State: "PAUSED", Version: "v3", Zone: "America/New_York", Calendar: "business", Destination: "inbox", Misfire: "SKIP", Overlap: "REFUSE", Budget: "100", Occurrences: []string{"2026-11-01T01:30:00-04:00"}, Actions: []string{"resume", "dry_run", "run_now", "publish"}}}}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip AgentControlsSnapshot
	if err := json.Unmarshal(encoded, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if roundtrip.Schedules[0].Revision != 7 {
		t.Fatal("revision was lost in JSON projection")
	}
	markup, err := ui.RenderToString(RenderAgentControls(ResolveProductLocale("en-US"), roundtrip, "done"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-owner-revision="7"`, `data-owner-action="resume"`, `data-owner-action="dry_run"`, "America/New_York", "2026-11-01T01:30:00-04:00", `tab=announcements`, "Manage announcements"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(markup, `data-owner-action="run_now"`) {
		t.Fatal("unsupported bypass action rendered")
	}
}

func TestTodo_AGENT_041_ControlsSecurity(t *testing.T) {
	snapshot := AgentControlsSnapshot{Available: true, Runs: []AgentControlRun{{ID: "run", State: "FAILED", Revision: 3, FailureGate: "MODEL_UNAVAILABLE", Failure: "provider_down", Incident: "incident-3", Denials: []string{"tool_denied"}}, {ID: "running", State: "RUNNING", Revision: 3, Actions: []string{"pause"}}}, Memory: []AgentControlMemory{{ID: "derived", Source: `<script>alert(1)</script>`, Purpose: "policy", Held: true, Actions: []string{"revoke"}}}}
	markup, err := ui.RenderToString(RenderAgentControls(ResolveProductLocale("en-US"), snapshot, "done"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{stdhtml.EscapeString(agentControlsText(ResolveProductLocale("en-US"), "failure_model_unavailable")), `data-owner-action="pause"`, `data-owner-action="revoke"`, "Legal hold", `role="status"`, `aria-live="polite"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(markup, "provider_down") || strings.Contains(markup, "incident-3") || strings.Contains(markup, "tool_denied") || strings.Contains(markup, "<script>") || strings.Contains(markup, `data-owner-action="quarantine"`) || strings.Contains(markup, `data-owner-action="delete"`) {
		t.Fatal("projection widened authority or inserted markup")
	}
	unavailable, err := ui.RenderToString(RenderAgentControls(ResolveProductLocale("en-US"), AgentControlsSnapshot{}, "error"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(unavailable, "data-owner-action") || !strings.Contains(unavailable, "unavailable") {
		t.Fatal("unavailable projection offered controls")
	}
}

func TestTodo_AGENT_043_ControlsConformance(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		markup, err := ui.RenderToString(RenderAgentControls(locale, AgentControlsSnapshot{Available: true, CanDraft: true, CanExport: true}, "conflict"))
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"title", "reason", "conflict"} {
			if !strings.Contains(markup, agentControlsText(locale, key)) {
				t.Fatalf("%s missing %s", localeID, key)
			}
		}
		if !strings.Contains(markup, `dir="`+string(locale.Direction)+`"`) || !strings.Contains(markup, agentUXR7Text(locale, "manage_announcements")) || !strings.Contains(markup, `for="agent-controls-reason"`) {
			t.Fatalf("%s missing direction, announcement navigation or label association", localeID)
		}
		mount, err := ui.RenderToString(AgentControlsMount(locale))
		if err != nil || !strings.Contains(mount, `id="agent-controls"`) || !strings.Contains(mount, agentControlsText(locale, "loading")) {
			t.Fatal("mount missing loading state")
		}
	}
}
