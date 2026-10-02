package main

import (
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var initialAgentTasks struct {
	sync.RWMutex
	tasks []productui.AgentTask
}

// validAgentTaskStates bounds the island's task state vocabulary; an unknown
// state is shown as ended, never as progressing.
var validAgentTaskStates = map[productui.AgentTaskState]bool{
	productui.AgentTaskRunning: true, productui.AgentTaskAwaitingApproval: true, productui.AgentTaskAwaitingInput: true,
	productui.AgentTaskAwaitingPlanConfirmation: true, productui.AgentTaskDrafting: true, productui.AgentTaskWaiting: true,
	productui.AgentTaskPaused: true, productui.AgentTaskCompleted: true, productui.AgentTaskFailed: true,
	productui.AgentTaskCancelled: true, productui.AgentTaskExpired: true, productui.AgentTaskUnknown: true,
}

func normalizeAgentTaskState(value string) productui.AgentTaskState {
	// The page owns the one mapping from a stored state to the state it shows,
	// so the island, the task RPCs and the server's first paint agree.
	state := productui.AgentTaskStateFromStored(value)
	if !validAgentTaskStates[state] {
		return productui.AgentTaskUnknown
	}
	return state
}

func nextAgentTaskFilterIndex(current, length int, key string) (int, bool) {
	if length <= 0 || current < 0 || current >= length {
		return current, false
	}
	switch key {
	case "ArrowLeft":
		return (current - 1 + length) % length, true
	case "ArrowRight":
		return (current + 1) % length, true
	case "Home":
		return 0, true
	case "End":
		return length - 1, true
	default:
		return current, false
	}
}

func agentTaskTitlePrefix(request string) string {
	request = strings.Join(strings.Fields(request), " ")
	const limit = 48
	runes := []rune(request)
	if len(runes) <= limit {
		return request
	}
	prefix := strings.TrimSpace(string(runes[:limit-1]))
	if boundary := strings.LastIndex(prefix, " "); boundary >= limit/2 {
		prefix = prefix[:boundary]
	}
	return prefix + "…"
}

func agentFollowUpContext(request string) string {
	request = strings.TrimSpace(request)
	if request == "" {
		return ""
	}
	return "> " + strings.ReplaceAll(request, "\n", "\n> ") + "\n\n"
}

// agentPageAgentSummaries is the list of agents the viewer may ask, as the
// Agents page draws them. Both the page and the task list it re-renders from
// the task RPCs take the agents from here: a task row finds the icon of the
// agent that answered in this list, so a list drawn without it showed a
// different picture from the choice card above it.
func agentPageAgentSummaries(value *journeyclient.Agents) []productui.AgentSummary {
	if value == nil || !value.Enabled {
		return nil
	}
	summaries := make([]productui.AgentSummary, 0, len(value.Agents))
	seen := make(map[string]bool, len(value.Agents))
	for _, agent := range value.Agents {
		id := strings.TrimSpace(agent.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		// The stored icon rides with the agent; without it the page drew a made-up
		// glyph for an agent that Chat draws with its own.
		summary := productui.AgentSummary{ID: id, Name: strings.TrimSpace(agent.Name), Description: strings.TrimSpace(agent.Description), Status: strings.TrimSpace(agent.Status)}
		if agent.Icon.Valid() {
			summary.Icon, summary.IconRevision = agent.Icon, agent.IconRevision
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

// agentTasksRegionSnapshot is what the task list is re-rendered from after the
// task RPCs answer: the tasks, their loading state, and the agents the page
// lists, so each row wears the icon of the agent that answered it.
func agentTasksRegionSnapshot(agents *journeyclient.Agents, tasks []productui.AgentTask, loading, failed bool) productui.AgentSnapshot {
	return productui.AgentSnapshot{Agents: agentPageAgentSummaries(agents), Tasks: tasks, TasksLoading: loading, TasksLoadFailed: failed}
}

// projectAgents converts the island's agents projection into the page
// contract. A missing projection fails closed (agents hidden); tasks are kept
// only when agents are on, and productui.NormalizeAgentsAvailability bounds
// the reason and the settings route to the canonical values.
func projectAgents(value *journeyclient.Agents) *productui.AgentsAvailabilityProjection {
	projection := productui.AgentsAvailabilityProjection{}
	if value != nil {
		projection.Enabled, projection.ViewerIsAdmin = value.Enabled, value.ViewerIsAdmin
		projection.ReasonKey, projection.SettingsHref = value.Reason, value.SettingsHref
	}
	if projection.Enabled {
		projection.Snapshot.StartAvailable, projection.Snapshot.StartUnavailableReason = value.StartAvailable, value.StartUnavailableReason
		projection.Snapshot.Availability = productui.AgentsUnavailable
		projection.Snapshot.TasksLoading = true
		if value.Service == "available" {
			projection.Snapshot.Availability = productui.AgentsAvailable
		}
		projection.Snapshot.Agents = agentPageAgentSummaries(value)
		seen := make(map[string]bool, len(value.Tasks))
		for _, task := range value.Tasks {
			id := strings.TrimSpace(task.ID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			state := normalizeAgentTaskState(task.State)
			item := productui.AgentTask{
				ID: id, Version: task.Version, Title: task.Title, Goal: task.Goal, State: state, LiveStep: task.LiveStep,
				BudgetUsed: task.BudgetUsed, BudgetLimit: task.BudgetLimit, PlanRevision: task.PlanRevision,
				Actions: productui.AgentTaskActionPolicy{ConfirmPlan: task.Actions.ConfirmPlan, Pause: task.Actions.Pause, Resume: task.Actions.Resume, Cancel: task.Actions.Cancel, ExtendBudget: task.Actions.ExtendBudget},
			}
			item.CreatedAt, _ = time.Parse(time.RFC3339Nano, task.CreatedAt)
			item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, task.UpdatedAt)
			item.ResultPreview, item.FailureReason, item.Retryable = strings.TrimSpace(task.ResultPreview), strings.TrimSpace(task.FailureReason), task.Retryable
			item.AnsweringAgentID, item.AnsweringAgentDisplayName, item.AnsweringAgentVersion = strings.TrimSpace(task.AnsweringAgentID), strings.TrimSpace(task.AnsweringAgentDisplayName), strings.TrimSpace(task.AnsweringAgentVersion)
			item.DocumentUsageState = productui.AgentDocumentUsageState(strings.TrimSpace(task.DocumentUsageState))
			for _, reference := range task.DocumentReferences {
				if id := strings.TrimSpace(reference.DocumentID); id != "" {
					item.Documents = append(item.Documents, productui.AgentTaskDocumentReference{DocumentID: id, Label: strings.TrimSpace(reference.Label), SectionAnchor: strings.TrimSpace(reference.SectionAnchor)})
				}
			}
			for _, reference := range task.UsedDocumentReferences {
				if id := strings.TrimSpace(reference.DocumentID); id != "" {
					item.UsedDocuments = append(item.UsedDocuments, productui.AgentTaskDocumentReference{DocumentID: id, Label: strings.TrimSpace(reference.Label), SectionAnchor: strings.TrimSpace(reference.SectionAnchor)})
				}
			}
			for _, omission := range task.DocumentOmissions {
				item.DocumentOmissions = append(item.DocumentOmissions, productui.AgentTaskDocumentOmission{Label: strings.TrimSpace(omission.Label), Reason: strings.TrimSpace(omission.Reason)})
			}
			if state == productui.AgentTaskCompleted {
				item.AnswerText = task.AnswerText
			}
			for _, step := range task.Steps {
				projected := productui.AgentTaskStep{Name: step.Name, State: step.State, Tier: step.Tier, FailureReason: strings.TrimSpace(step.FailureReason)}
				projected.StartedAt, _ = time.Parse(time.RFC3339Nano, step.StartedAt)
				projected.FinishedAt, _ = time.Parse(time.RFC3339Nano, step.FinishedAt)
				item.Steps = append(item.Steps, projected)
			}
			for _, approval := range task.Approvals {
				item.Approvals = append(item.Approvals, productui.AgentApproval{ID: approval.ID, Digest: approval.Digest, Summary: approval.Summary, Sources: append([]string(nil), approval.Sources...), Taint: approval.Taint})
			}
			applyAgentTaskDetail(&item, task.Detail)
			projection.Snapshot.Tasks = append(projection.Snapshot.Tasks, item)
		}
	}
	initialAgentTasks.Lock()
	initialAgentTasks.tasks = append([]productui.AgentTask(nil), projection.Snapshot.Tasks...)
	initialAgentTasks.Unlock()
	normalized := productui.NormalizeAgentsAvailability(projection)
	return &normalized
}

// applyAgentTaskDetail puts the server's task detail sections on the page
// contract, the same conversion the server makes for its own first render.
func applyAgentTaskDetail(item *productui.AgentTask, detail *journeyclient.AgentTaskDetail) {
	if detail == nil {
		return
	}
	for _, checkpoint := range detail.Checkpoints {
		item.Checkpoints = append(item.Checkpoints, productui.AgentCheckpoint{Kind: checkpoint.Kind, Step: checkpoint.Step, Label: checkpoint.Label, At: checkpoint.At})
	}
	for _, artifact := range detail.Artifacts {
		item.Artifacts = append(item.Artifacts, productui.AgentArtifact{Kind: artifact.Kind, Name: artifact.Name, Href: artifact.Href})
	}
	for _, intent := range detail.SubmittedIntents {
		item.SubmittedIntents = append(item.SubmittedIntents, productui.AgentIntentStatus{Name: intent.Name, Status: intent.Status})
	}
	for _, change := range detail.PlanChanges {
		item.PlanChanges = append(item.PlanChanges, productui.AgentPlanChange{Change: change.Change, Step: productui.AgentTaskStep{Name: change.Step, Tier: change.Tier}})
	}
}
