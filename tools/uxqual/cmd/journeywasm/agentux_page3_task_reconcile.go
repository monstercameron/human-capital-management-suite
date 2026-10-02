package main

import (
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func mergeAgentTask(previous, projection productui.AgentTask) productui.AgentTask {
	result := previous
	result.ID, result.Version, result.State = projection.ID, projection.Version, projection.State
	if value := strings.TrimSpace(projection.Title); value != "" {
		result.Title, result.Goal = value, value
	}
	if !projection.CreatedAt.IsZero() {
		result.CreatedAt = projection.CreatedAt
	}
	if !projection.UpdatedAt.IsZero() {
		result.UpdatedAt = projection.UpdatedAt
	}
	result.ResultPreview, result.FailureReason = projection.ResultPreview, projection.FailureReason
	result.Retryable = projection.Retryable
	result.AnsweringAgentID, result.AnsweringAgentDisplayName, result.AnsweringAgentVersion = projection.AnsweringAgentID, projection.AnsweringAgentDisplayName, projection.AnsweringAgentVersion
	result.Documents = append([]productui.AgentTaskDocumentReference(nil), projection.Documents...)
	result.UsedDocuments = append([]productui.AgentTaskDocumentReference(nil), projection.UsedDocuments...)
	result.DocumentUsageState = projection.DocumentUsageState
	result.DocumentOmissions = append([]productui.AgentTaskDocumentOmission(nil), projection.DocumentOmissions...)
	result.Steps = append([]productui.AgentTaskStep(nil), projection.Steps...)
	if projection.AnswerText != "" {
		result.AnswerText = projection.AnswerText
	}
	return result
}

func reconcileAgentTasks(previous, projections []productui.AgentTask) []productui.AgentTask {
	byID := make(map[string]productui.AgentTask, len(previous))
	for _, task := range previous {
		byID[task.ID] = task
	}
	result := make([]productui.AgentTask, 0, len(projections))
	seen := make(map[string]bool, len(projections))
	for _, task := range projections {
		if strings.TrimSpace(task.ID) == "" || seen[task.ID] {
			continue
		}
		seen[task.ID] = true
		result = append(result, mergeAgentTask(byID[task.ID], task))
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := result[i].UpdatedAt, result[j].UpdatedAt
		if left.IsZero() {
			left = result[i].CreatedAt
		}
		if right.IsZero() {
			right = result[j].CreatedAt
		}
		return left.After(right)
	})
	return result
}
