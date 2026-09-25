package projectservice

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
)

var _ TaskLifecycleRepository = (*StoreAdapter)(nil)
var _ ProjectLifecycleRepository = (*StoreAdapter)(nil)

func (a *StoreAdapter) UpdateProjectSettings(ctx context.Context, tenantID, projectID, name, timezone string, expected uint64, actor, key string) (ProjectRecord, error) {
	if a == nil || a.Projects == nil {
		return ProjectRecord{}, ErrUnavailable
	}
	record, err := a.Projects.UpdateProjectSettings(ctx, tenantID, projectID, name, timezone, int64(expected), actor, "HUMAN", key)
	if err != nil {
		return ProjectRecord{}, err
	}
	return ProjectRecord{ID: record.ID, TenantID: record.TenantID, OwnerID: record.OwnerID, Name: record.Name, Timezone: record.Timezone, State: project.Lifecycle(record.Lifecycle), Revision: uint64(record.Revision)}, nil
}

func (a *StoreAdapter) TransitionProject(ctx context.Context, tenantID, projectID string, target project.Lifecycle, expected uint64, actor, key string) (ProjectRecord, error) {
	if a == nil || a.Projects == nil {
		return ProjectRecord{}, ErrUnavailable
	}
	record, err := a.Projects.TransitionProject(ctx, tenantID, projectID, target, int64(expected), actor, "HUMAN", key)
	if err != nil {
		return ProjectRecord{}, err
	}
	return ProjectRecord{ID: record.ID, TenantID: record.TenantID, OwnerID: record.OwnerID, Name: record.Name, Timezone: record.Timezone, State: project.Lifecycle(record.Lifecycle), Revision: uint64(record.Revision)}, nil
}

func (a *StoreAdapter) PatchTask(ctx context.Context, tenantID, projectID, taskID string, expectedRevision, workflowRevision uint64, patch project.TaskPatch, actor, key string) (TaskRecord, error) {
	if a == nil || a.Projects == nil {
		return TaskRecord{}, ErrUnavailable
	}
	stored := projectstore.TaskPatch{Title: patch.Title, Description: patch.Description, AssigneeID: patch.AssigneeID, DueDate: patch.DueDate}
	if patch.Priority != nil {
		priority := string(*patch.Priority)
		stored.Priority = &priority
	}
	stored.StartDate, stored.Labels = patch.StartDate, patch.Labels
	if patch.StoryPoints != nil {
		points := int32(*patch.StoryPoints)
		stored.StoryPoints = &points
	}
	record, err := a.Projects.PatchTask(ctx, tenantID, projectID, taskID, int64(expectedRevision), int64(workflowRevision), stored, actor, "HUMAN", key)
	if err != nil {
		return TaskRecord{}, err
	}
	out, err := fromStoreTask(record)
	out.WorkflowRevision = workflowRevision
	return out, err
}

func (a *StoreAdapter) SetTaskArchived(ctx context.Context, tenantID, projectID, taskID string, expectedRevision, workflowRevision uint64, archived bool, actor, key string) (TaskRecord, error) {
	if a == nil || a.Projects == nil {
		return TaskRecord{}, ErrUnavailable
	}
	record, err := a.Projects.SetTaskArchived(ctx, tenantID, projectID, taskID, int64(expectedRevision), int64(workflowRevision), archived, actor, "HUMAN", key)
	if err != nil {
		return TaskRecord{}, err
	}
	out, err := fromStoreTask(record)
	out.WorkflowRevision = workflowRevision
	return out, err
}
