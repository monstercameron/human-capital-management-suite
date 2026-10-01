package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_122_AgentsVisualLayout(t *testing.T) {
	task := AgentTask{
		ID:         "task-visual",
		Title:      "Prepare a review",
		Goal:       "Summarise the open work",
		AnswerText: "First line\nSecond line",
		State:      AgentTaskCompleted,
	}
	view := uxblind122View(t, "en-US", &AgentsAvailabilityProjection{
		Enabled: true,
		Snapshot: AgentSnapshot{
			Availability: AgentsAvailable,
			Agents:       []AgentSummary{{ID: "agent-1", Name: "Review agent", Description: "Summarises work", Skills: []string{"Reviews"}}},
			Threads:      []AgentThread{{ID: "thread-1", AgentID: "agent-1", Title: "Review thread", Posts: []AgentPost{{ID: "post-1", Author: "Review agent", Body: "Ready", ForUser: true}}}},
			Tasks:        []AgentTask{task},
			SelectedTask: &task,
		},
	})
	markup, err := ui.RenderToString(BuildAgentsSurface(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range []string{
		`class="agents-page"`, `class="agents-layout"`, `class="agents-sidebar"`,
		`class="agents-main"`, `class="agents-composer"`, `class="agents-task-view"`,
		`class="agents-task-answer"`,
	} {
		if !strings.Contains(markup, class) {
			t.Fatalf("rendered Agents page missing %s: %s", class, markup)
		}
	}
	if !strings.Contains(markup, "First line\nSecond line") {
		t.Fatalf("answer text lost its line break in rendered page: %s", markup)
	}

	css := Stylesheet()
	layout := declarationsFor(css, ".agents-layout")
	for _, declaration := range []string{
		"align-items:flex-start", "display:grid", "grid-template-columns:minmax(224px,288px) minmax(0,1fr)", "min-width:0",
	} {
		if !strings.Contains(layout, declaration) {
			t.Errorf("Agents layout missing %q: %s", declaration, layout)
		}
	}
	composer := declarationsFor(css, ".agents-composer textarea")
	for _, declaration := range []string{"width:100%", "min-width:0", "min-height:96px"} {
		if !strings.Contains(composer, declaration) {
			t.Errorf("composer textarea missing %q: %s", declaration, composer)
		}
	}
	answer := declarationsFor(css, ".agents-task-answer p")
	if !strings.Contains(answer, "white-space:pre-line") {
		t.Errorf("answer text is not configured for localized line wrapping: %s", answer)
	}
	for _, selector := range []string{".agents-page .button", ".agents-task-link"} {
		controls := declarationsFor(css, selector)
		if !strings.Contains(controls, "min-height:44px") || !strings.Contains(controls, "min-width:44px") {
			t.Errorf("Agents controls do not meet the touch target (%s): %s", selector, controls)
		}
	}
	focus := declarationsFor(css, ".agents-page :is(.button,.agents-task-link,.agents-composer textarea):focus-visible")
	if !strings.Contains(focus, "var(--hcm-color-focus,var(--accent))") {
		t.Errorf("Agents focus ring does not use theme tokens: %s", focus)
	}

	if !strings.Contains(css, "@media (max-width:800px){.agents-layout{grid-template-columns:minmax(0,1fr);") {
		t.Error("Agents layout does not stack at 800px")
	}
	if !strings.Contains(css, "@media (max-width:390px){.agents-composer-actions .button,.agents-task-actions .button{flex:1 1 100%;") {
		t.Error("Agents actions do not become full-width at 390px")
	}
	if physical := agentsStylesheet(); strings.Contains(physical, "left:") || strings.Contains(physical, "right:") {
		t.Fatalf("Agents stylesheet uses physical horizontal positioning: %s", physical)
	}
}
