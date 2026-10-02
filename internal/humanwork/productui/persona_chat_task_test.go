package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"strings"
	"testing"
)

func TestTodo_AGENTP_020_SharedTaskSummary(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(RenderAgentTaskSummary(ResolveProductLocale(locale), AgentTask{ID: "task", Title: "Review policy", State: AgentTaskAwaitingApproval}, "/workspace/app/agents?task=task"))
		if err != nil || !strings.Contains(markup, `aria-live="polite"`) || !strings.Contains(markup, `data-state="awaiting_approval"`) || !strings.Contains(markup, `href="/workspace/app/agents?task=task"`) || strings.Contains(markup, "agents.state.") {
			t.Fatalf("%s summary: %s %v", locale, markup, err)
		}
	}
}

func TestTodo_AGENTP_020_WaitingTaskList(t *testing.T) {
	snapshot := AgentSnapshot{Tasks: []AgentTask{{ID: "wait-task", Title: "Waiting for review", State: AgentTaskWaiting}}}
	markup, err := ui.RenderToString(tagAgentTasks(View{}, ResolveProductLocale("en-US"), snapshot))
	if err != nil || !strings.Contains(markup, `data-task-id="wait-task"`) || !strings.Contains(markup, `General agent · Waiting`) {
		t.Fatalf("waiting task disappeared: %s %v", markup, err)
	}
}

func TestTodo_AGENTUX_011_SharedChatTaskSteps(t *testing.T) {
	task := AgentTask{ID: "failed", Title: "Prepare an answer", Goal: "Explain the policy", State: AgentTaskFailed, Steps: []AgentTaskStep{{Name: "Summarize request", State: "FAILED", Tier: "T1"}}}
	markup, err := ui.RenderToString(RenderAgentTaskSummary(ResolveProductLocale("en-US"), task, "/workspace/app/chat/agents?task=failed"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"What the agent did", "Wrote a draft only you can see", "The agent could not finish this step. You can try again.", `data-tone="failed"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("shared chat task steps missing %q: %s", want, markup)
		}
	}
}
