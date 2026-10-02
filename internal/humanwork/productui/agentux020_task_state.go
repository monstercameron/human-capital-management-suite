package productui

import "strings"

// AgentTaskStateFromStored is the one mapping from a stored or transported task
// state to the state the Agents page knows. The page files a task under Active,
// Completed or Failed from this state alone, so every spelling a store or an
// older build has written must land on the state it means: a completed answer
// whose state arrived as "COMPLETE" or "TASK_STATE_SUCCEEDED" was otherwise
// listed under Failed. A value with no known meaning is AgentTaskUnknown.
func AgentTaskStateFromStored(value string) AgentTaskState {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer("-", "_", " ", "_").Replace(normalized)
	for _, prefix := range []string{"agent_task_state_", "task_state_", "state_"} {
		normalized = strings.TrimPrefix(normalized, prefix)
	}
	switch normalized {
	case "running", "in_progress", "executing", "reconciling":
		return AgentTaskRunning
	case "awaiting_approval", "waiting_for_approval":
		return AgentTaskAwaitingApproval
	case "awaiting_input", "waiting_for_input":
		return AgentTaskAwaitingInput
	case "drafting", "planning":
		return AgentTaskDrafting
	case "awaiting_plan_confirmation":
		return AgentTaskAwaitingPlanConfirmation
	case "waiting", "ready", "queued", "pending", "admitted":
		return AgentTaskWaiting
	case "paused":
		return AgentTaskPaused
	case "completed", "complete", "succeeded", "success", "done":
		return AgentTaskCompleted
	case "failed", "failure", "error", "errored", "needs_repair":
		return AgentTaskFailed
	case "cancelled", "canceled":
		return AgentTaskCancelled
	case "expired", "timed_out":
		return AgentTaskExpired
	default:
		return AgentTaskUnknown
	}
}

// AgentTaskSettledState is the state a task is shown in. A task stored before
// the projection carried a state the page knows, but which holds an answer, is
// a completed task: its answer is on the page, so it is not listed as failed.
func AgentTaskSettledState(task AgentTask) AgentTaskState {
	state := AgentTaskStateFromStored(string(task.State))
	if state == AgentTaskUnknown && strings.TrimSpace(task.FailureReason) == "" &&
		(strings.TrimSpace(task.AnswerText) != "" || strings.TrimSpace(task.ResultPreview) != "") {
		return AgentTaskCompleted
	}
	return state
}

// AgentTaskGroup is the one tab a task is listed under: "active", "completed"
// or "failed". Every state belongs to exactly one.
func AgentTaskGroup(task AgentTask) string {
	return agentTaskCategory(AgentTaskSettledState(task))
}

// agentUX020SettledTasks gives every task its settled state before the page
// groups, counts and draws them, so the tab, its count and the row agree.
func agentUX020SettledTasks(snapshot AgentSnapshot) AgentSnapshot {
	tasks := make([]AgentTask, len(snapshot.Tasks))
	for index, task := range snapshot.Tasks {
		task.State = AgentTaskSettledState(task)
		if task.State == AgentTaskCompleted && strings.TrimSpace(task.AnswerText) == "" {
			task.AnswerText = task.ResultPreview
		}
		tasks[index] = task
	}
	snapshot.Tasks = tasks
	if snapshot.SelectedTask != nil {
		selected := *snapshot.SelectedTask
		selected.State = AgentTaskSettledState(selected)
		if selected.State == AgentTaskCompleted && strings.TrimSpace(selected.AnswerText) == "" {
			selected.AnswerText = selected.ResultPreview
		}
		snapshot.SelectedTask = &selected
	}
	return snapshot
}
