package main

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// validAgentTaskStates bounds the island's task state vocabulary; an unknown
// state is shown as ended, never as progressing.
var validAgentTaskStates = map[productui.AgentTaskState]bool{
	productui.AgentTaskRunning: true, productui.AgentTaskAwaitingApproval: true, productui.AgentTaskAwaitingInput: true,
	productui.AgentTaskAwaitingPlanConfirmation: true, productui.AgentTaskDrafting: true, productui.AgentTaskWaiting: true,
	productui.AgentTaskPaused: true, productui.AgentTaskCompleted: true, productui.AgentTaskFailed: true,
	productui.AgentTaskCancelled: true, productui.AgentTaskExpired: true, productui.AgentTaskUnknown: true,
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
		if value.Service == "available" {
			projection.Snapshot.Availability = productui.AgentsAvailable
		}
		seen := make(map[string]bool, len(value.Tasks))
		for _, task := range value.Tasks {
			id := strings.TrimSpace(task.ID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			state := productui.AgentTaskState(task.State)
			if !validAgentTaskStates[state] {
				state = productui.AgentTaskUnknown
			}
			item := productui.AgentTask{
				ID: id, Version: task.Version, Title: task.Title, Goal: task.Goal, State: state, LiveStep: task.LiveStep,
				BudgetUsed: task.BudgetUsed, BudgetLimit: task.BudgetLimit, PlanRevision: task.PlanRevision,
				Actions: productui.AgentTaskActionPolicy{ConfirmPlan: task.Actions.ConfirmPlan, Pause: task.Actions.Pause, Resume: task.Actions.Resume, Cancel: task.Actions.Cancel},
			}
			if state == productui.AgentTaskCompleted {
				item.AnswerText = task.AnswerText
			}
			for _, step := range task.Steps {
				item.Steps = append(item.Steps, productui.AgentTaskStep{Name: step.Name, State: step.State, Tier: step.Tier})
			}
			for _, approval := range task.Approvals {
				item.Approvals = append(item.Approvals, productui.AgentApproval{ID: approval.ID, Digest: approval.Digest, Summary: approval.Summary})
			}
			projection.Snapshot.Tasks = append(projection.Snapshot.Tasks, item)
		}
	}
	normalized := productui.NormalizeAgentsAvailability(projection)
	return &normalized
}
