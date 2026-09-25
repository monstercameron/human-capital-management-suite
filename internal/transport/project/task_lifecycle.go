package project

import (
	"context"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type TaskLifecycleService interface {
	PatchTask(context.Context, *trust.Principal, projectservice.PatchTaskRequest) (projectservice.TaskRecord, error)
	ArchiveTask(context.Context, *trust.Principal, projectservice.SetTaskArchivedRequest) (projectservice.TaskRecord, error)
	RestoreTask(context.Context, *trust.Principal, projectservice.SetTaskArchivedRequest) (projectservice.TaskRecord, error)
}

func (s *server) PatchTask(ctx context.Context, req *projectv1.PatchTaskRequest) (*projectv1.PatchTaskResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	lifecycle, ok := s.service.(TaskLifecycleService)
	if !ok {
		return nil, s.unavailable()
	}
	patch := project.TaskPatch{Title: req.Title, Description: req.Description, AssigneeID: req.AssigneeId, DueDate: req.DueDate, StartDate: req.StartDate, StoryPoints: req.StoryPoints}
	if req.Labels != nil {
		labels := append([]string{}, req.Labels.GetValues()...)
		patch.Labels = &labels
	}
	if req.Priority != nil {
		priority, valid := priorityString(*req.Priority)
		if !valid || *req.Priority == projectv1.TaskPriority_TASK_PRIORITY_UNSPECIFIED {
			return nil, envelope.New(envelope.CodeInvalidArgument, "project.priority.invalid", "task priority is invalid")
		}
		value := project.Priority(priority)
		patch.Priority = &value
	}
	record, err := lifecycle.PatchTask(ctx, p, projectservice.PatchTaskRequest{ProjectID: req.GetProjectId(), TaskID: req.GetTaskId(), IdempotencyKey: req.GetIdempotencyKey(), ExpectedTaskRevision: req.GetExpectedTaskRevision(), ExpectedWorkflowRevision: req.GetExpectedWorkflowRevision(), Patch: patch})
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.PatchTaskResponse{Task: taskMessage(record)}, nil
}

func (s *server) ArchiveTask(ctx context.Context, req *projectv1.ArchiveTaskRequest) (*projectv1.ArchiveTaskResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	lifecycle, ok := s.service.(TaskLifecycleService)
	if !ok {
		return nil, s.unavailable()
	}
	record, err := lifecycle.ArchiveTask(ctx, p, projectservice.SetTaskArchivedRequest{ProjectID: req.GetProjectId(), TaskID: req.GetTaskId(), IdempotencyKey: req.GetIdempotencyKey(), ExpectedTaskRevision: req.GetExpectedTaskRevision(), ExpectedWorkflowRevision: req.GetExpectedWorkflowRevision()})
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.ArchiveTaskResponse{Task: taskMessage(record)}, nil
}

func (s *server) RestoreTask(ctx context.Context, req *projectv1.RestoreTaskRequest) (*projectv1.RestoreTaskResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	lifecycle, ok := s.service.(TaskLifecycleService)
	if !ok {
		return nil, s.unavailable()
	}
	record, err := lifecycle.RestoreTask(ctx, p, projectservice.SetTaskArchivedRequest{ProjectID: req.GetProjectId(), TaskID: req.GetTaskId(), IdempotencyKey: req.GetIdempotencyKey(), ExpectedTaskRevision: req.GetExpectedTaskRevision(), ExpectedWorkflowRevision: req.GetExpectedWorkflowRevision()})
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.RestoreTaskResponse{Task: taskMessage(record)}, nil
}

// TaskLinkTargetService is the reverse link read, implemented by the
// project service when its link store supports it.
type TaskLinkTargetService interface {
	ListTasksLinkingTo(context.Context, *trust.Principal, projectlink.Kind, string, int) ([]projectservice.LinkedTaskRecord, error)
}

func (s *server) ListTaskLinksByTarget(ctx context.Context, req *projectv1.ListTaskLinksByTargetRequest) (*projectv1.ListTaskLinksByTargetResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireService(s); err != nil {
		return nil, err
	}
	if req == nil || req.GetTarget() == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	reverse, ok := s.service.(TaskLinkTargetService)
	if !ok {
		return nil, s.unavailable()
	}
	ref, err := referenceFromMessage(req.GetTarget())
	if err != nil || (ref.Kind != projectlink.Journey && ref.Kind != projectlink.WorkItem && ref.Kind != projectlink.WorkOrder) {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.link_target.invalid", "only workflow, work item, and work order targets can be looked up")
	}
	limit, err := listPageSize(req.GetPage())
	if err != nil {
		return nil, err
	}
	records, err := reverse.ListTasksLinkingTo(ctx, p, ref.Kind, ref.ID, limit)
	if err != nil {
		return nil, mapError(err)
	}
	out := &projectv1.ListTaskLinksByTargetResponse{Tasks: make([]*projectv1.LinkedTask, 0, len(records))}
	for _, r := range records {
		out.Tasks = append(out.Tasks, &projectv1.LinkedTask{ProjectId: r.ProjectID, TaskId: r.TaskID, Title: r.Title, StatusId: r.StatusID})
	}
	return out, nil
}
