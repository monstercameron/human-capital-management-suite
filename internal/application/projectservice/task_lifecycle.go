package projectservice

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TaskLifecycleRepository keeps ordinary task edits under the store's revision,
// active workflow, idempotency, and event transaction fences.
type TaskLifecycleRepository interface {
	PatchTask(context.Context, string, string, string, uint64, uint64, project.TaskPatch, string, string) (TaskRecord, error)
	SetTaskArchived(context.Context, string, string, string, uint64, uint64, bool, string, string) (TaskRecord, error)
}

type PatchTaskRequest struct {
	ProjectID, TaskID, IdempotencyKey              string
	ExpectedTaskRevision, ExpectedWorkflowRevision uint64
	Patch                                          project.TaskPatch
}

type SetTaskArchivedRequest struct {
	ProjectID, TaskID, IdempotencyKey              string
	ExpectedTaskRevision, ExpectedWorkflowRevision uint64
}

func (s Service) PatchTask(ctx context.Context, principal *trust.Principal, req PatchTaskRequest) (TaskRecord, error) {
	p, t, lifecycle, err := s.taskLifecycleContext(ctx, principal, req.ProjectID, req.TaskID, req.IdempotencyKey, req.ExpectedTaskRevision, req.ExpectedWorkflowRevision)
	if err != nil {
		return TaskRecord{}, err
	}
	if req.Patch.TypeID != nil || (req.Patch.Title == nil && req.Patch.Description == nil && req.Patch.AssigneeID == nil && req.Patch.DueDate == nil && req.Patch.Priority == nil && req.Patch.StartDate == nil && req.Patch.StoryPoints == nil && req.Patch.Labels == nil) {
		return TaskRecord{}, ErrInvalidRequest
	}
	if _, err := t.domain().PatchTask(p.domain(), req.Patch, req.ExpectedTaskRevision); err != nil && !errors.Is(err, project.ErrRevisionConflict) {
		return TaskRecord{}, err
	}
	result, err := lifecycle.PatchTask(ctx, tenant(principal), req.ProjectID, req.TaskID, req.ExpectedTaskRevision, req.ExpectedWorkflowRevision, req.Patch, principal.Subject(), req.IdempotencyKey)
	result.WorkflowRevision = req.ExpectedWorkflowRevision
	return result, err
}

func (s Service) ArchiveTask(ctx context.Context, principal *trust.Principal, req SetTaskArchivedRequest) (TaskRecord, error) {
	return s.setTaskArchived(ctx, principal, req, true)
}

func (s Service) RestoreTask(ctx context.Context, principal *trust.Principal, req SetTaskArchivedRequest) (TaskRecord, error) {
	return s.setTaskArchived(ctx, principal, req, false)
}

func (s Service) setTaskArchived(ctx context.Context, principal *trust.Principal, req SetTaskArchivedRequest, archived bool) (TaskRecord, error) {
	p, t, lifecycle, err := s.taskLifecycleContext(ctx, principal, req.ProjectID, req.TaskID, req.IdempotencyKey, req.ExpectedTaskRevision, req.ExpectedWorkflowRevision)
	if err != nil {
		return TaskRecord{}, err
	}
	if archived {
		if _, err = t.domain().Archive(p.domain(), req.ExpectedTaskRevision); err != nil && !errors.Is(err, project.ErrRevisionConflict) {
			return TaskRecord{}, err
		}
	} else {
		if _, err = t.domain().Restore(p.domain(), req.ExpectedTaskRevision); err != nil && !errors.Is(err, project.ErrRevisionConflict) {
			return TaskRecord{}, err
		}
	}
	result, err := lifecycle.SetTaskArchived(ctx, tenant(principal), req.ProjectID, req.TaskID, req.ExpectedTaskRevision, req.ExpectedWorkflowRevision, archived, principal.Subject(), req.IdempotencyKey)
	result.WorkflowRevision = req.ExpectedWorkflowRevision
	return result, err
}

func (s Service) taskLifecycleContext(ctx context.Context, principal *trust.Principal, projectID, taskID, key string, taskRevision, workflowRevision uint64) (ProjectRecord, TaskRecord, TaskLifecycleRepository, error) {
	if err := validPrincipal(principal); err != nil {
		return ProjectRecord{}, TaskRecord{}, nil, err
	}
	if s.Auth == nil || s.Commands == nil || s.Reads == nil {
		return ProjectRecord{}, TaskRecord{}, nil, ErrUnavailable
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(taskID) == "" || strings.TrimSpace(key) == "" || taskRevision == 0 || workflowRevision == 0 {
		return ProjectRecord{}, TaskRecord{}, nil, ErrInvalidRequest
	}
	lifecycle, ok := s.Commands.(TaskLifecycleRepository)
	if !ok {
		return ProjectRecord{}, TaskRecord{}, nil, ErrUnavailable
	}
	if err := s.Auth.Authorize(ctx, principal, projectID, projectaccess.EditTask); err != nil {
		return ProjectRecord{}, TaskRecord{}, nil, err
	}
	p, err := s.Reads.GetProject(ctx, tenant(principal), projectID)
	if err != nil {
		return ProjectRecord{}, TaskRecord{}, nil, err
	}
	t, err := s.Reads.GetTask(ctx, tenant(principal), projectID, taskID)
	if err != nil {
		return ProjectRecord{}, TaskRecord{}, nil, err
	}
	if p.TenantID != tenant(principal) || t.TenantID != tenant(principal) || t.ProjectID != projectID {
		return ProjectRecord{}, TaskRecord{}, nil, projectaccess.ErrTenantMismatch
	}
	return p, t, lifecycle, nil
}
