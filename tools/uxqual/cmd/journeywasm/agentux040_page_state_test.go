package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// A URL without a task selects nothing, so the page does not jump to the task
// list on load; the default tab is the first that has rows; and a task that has
// finished is announced, which is when the "Task started" status is cleared.
func TestTodo_AGENTUX_040(t *testing.T) {
	for query, want := range map[string]string{
		"":                             "",
		"?":                            "",
		"?locale=en-US":                "",
		"?task=":                       "",
		"?task=%20%20":                 "",
		"?task=%3Cnull%3E":             "",
		"?task=null":                   "",
		"?task=undefined":              "",
		"?locale=ar&task=task-7":       "task-7",
		"task=task-9&locale=de-DE":     "task-9",
		"?task=%zz":                    "",
		"?agent=policy-helper&tab=x":   "",
		"?task=NULL-is-not-a-real-one": "NULL-is-not-a-real-one",
	} {
		if got := selectedAgentTaskID(query); got != want {
			t.Errorf("selectedAgentTaskID(%q) = %q, want %q", query, got, want)
		}
	}

	// The default tab is the first of Active, Completed, Failed that has rows.
	for _, tc := range []struct {
		name             string
		stored, selected string
		counts           map[string]int
		want             string
	}{
		{"nothing listed", "", "", map[string]int{}, "active"},
		{"only failed", "", "", map[string]int{"failed": 3}, "failed"},
		{"completed before failed", "", "", map[string]int{"completed": 7, "failed": 4}, "completed"},
		{"active first", "", "", map[string]int{"active": 1, "completed": 7, "failed": 4}, "active"},
		{"a remembered tab with no rows gives way", "failed", "", map[string]int{"completed": 2}, "completed"},
		{"a remembered tab with rows is kept", "failed", "", map[string]int{"completed": 2, "failed": 1}, "failed"},
		{"the open task's tab wins", "failed", "completed", map[string]int{"completed": 2, "failed": 1}, "completed"},
	} {
		if got := preferredAgentTaskFilter(tc.stored, tc.selected, tc.counts); got != tc.want {
			t.Errorf("%s: tab %q, want %q", tc.name, got, tc.want)
		}
	}

	// Every state the page knows is either still running or final; a final task
	// is announced as answered or as failed, never left as "started".
	for state, final := range map[productui.AgentTaskState]bool{
		productui.AgentTaskRunning: false, productui.AgentTaskWaiting: false, productui.AgentTaskDrafting: false, productui.AgentTaskPaused: false,
		productui.AgentTaskAwaitingApproval: false, productui.AgentTaskAwaitingInput: false, productui.AgentTaskAwaitingPlanConfirmation: false,
		productui.AgentTaskCompleted: true, productui.AgentTaskFailed: true, productui.AgentTaskCancelled: true, productui.AgentTaskExpired: true, productui.AgentTaskUnknown: true,
	} {
		task := productui.AgentTask{State: state}
		if agentTaskFinal(task) != final {
			t.Errorf("state %s final = %t, want %t", state, agentTaskFinal(task), final)
		}
		if final {
			want := "failed"
			if state == productui.AgentTaskCompleted {
				want = "answered"
			}
			if got := agentTaskTerminalAnnouncement(task); got != want {
				t.Errorf("state %s is announced as %q, want %q", state, got, want)
			}
		}
	}

	// Opening a row keeps the person on the page and names the task in the URL.
	if target, ok := agentTaskNavigationTarget("/workspace/app/chat/agents", "?locale=de-DE", "task-7"); !ok || selectedAgentTaskID(target[len("/workspace/app/chat/agents"):len(target)-len("#agents-task-title")]) != "task-7" {
		t.Fatalf("a row's address does not select its task: %q", target)
	}
}
