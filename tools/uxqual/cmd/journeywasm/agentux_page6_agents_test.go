package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestAgentUXPage6_ComposerFinalState(t *testing.T) {
	for _, tc := range []struct {
		state productui.AgentTaskState
		final bool
		want  string
	}{
		{productui.AgentTaskRunning, false, ""},
		{productui.AgentTaskAwaitingApproval, false, ""},
		{productui.AgentTaskCompleted, true, "answered"},
		{productui.AgentTaskFailed, true, "failed"},
		{productui.AgentTaskCancelled, true, "failed"},
	} {
		task := productui.AgentTask{State: tc.state}
		if got := agentTaskFinal(task); got != tc.final {
			t.Errorf("%s final = %t, want %t", tc.state, got, tc.final)
		}
		if tc.final && agentTaskTerminalAnnouncement(task) != tc.want {
			t.Errorf("%s announcement = %q, want %q", tc.state, agentTaskTerminalAnnouncement(task), tc.want)
		}
	}
}

func TestAgentUXPage6_TaskRowNavigation(t *testing.T) {
	got, ok := agentTaskNavigationTarget("/workspace/app/chat/agents", "?locale=de-DE", "task-7")
	if !ok || got != "/workspace/app/chat/agents?locale=de-DE&task=task-7#agents-task-title" {
		t.Fatalf("task row target = %q, %t", got, ok)
	}
	if _, ok := agentTaskNavigationTarget("/workspace/app/chat/agents", "%zz", "task-7"); ok {
		t.Fatal("malformed query was accepted")
	}
}
