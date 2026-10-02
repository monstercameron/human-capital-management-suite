package main

import (
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func agentTaskFinal(task productui.AgentTask) bool {
	switch task.State {
	case productui.AgentTaskCompleted, productui.AgentTaskFailed, productui.AgentTaskCancelled, productui.AgentTaskExpired, productui.AgentTaskUnknown:
		return true
	default:
		return false
	}
}

func agentTaskTerminalAnnouncement(task productui.AgentTask) string {
	if task.State == productui.AgentTaskCompleted {
		return "answered"
	}
	return "failed"
}

// agentTaskNavigationTarget keeps a task-row click inside the current page.
// It preserves locale and other supported query values while selecting the row.
func agentTaskNavigationTarget(path, rawQuery, taskID string) (string, bool) {
	path, taskID = strings.TrimSpace(path), strings.TrimSpace(taskID)
	if path == "" || taskID == "" {
		return "", false
	}
	query, err := url.ParseQuery(strings.TrimPrefix(rawQuery, "?"))
	if err != nil {
		return "", false
	}
	query.Set("task", taskID)
	return path + "?" + query.Encode() + "#agents-task-title", true
}
