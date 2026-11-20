package project

import (
	"context"
	"strings"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	domainproject "github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// MaxProjectPreviewRefs bounds one GetProjectPreviews request.
	MaxProjectPreviewRefs = 50
	// previewProgressPages bounds the pages read to count one board's tasks.
	previewProgressPages = 50
	// previewRequestPages bounds the task pages one request reads across all
	// boards, so a batch of large boards cannot fan out into thousands of reads.
	previewRequestPages = 100
)

// GetProjectPreviews is the batched unfurl read chat uses for task and board
// links. Every item goes through the same application reads a single
// GetProject, GetTask and GetWorkflowConfiguration would make, as the caller,
// so a person sees exactly what those reads would show. A refused item is the
// neutral RESTRICTED answer with no other field; a deleted record is MISSING,
// an archived one ARCHIVED and any other failed read FAILED so it can be
// retried (issue 3750 states). One bad item never fails the batch.
func (s *server) GetProjectPreviews(ctx context.Context, req *projectv1.GetProjectPreviewsRequest) (*projectv1.GetProjectPreviewsResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil || len(req.GetRefs()) > MaxProjectPreviewRefs {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	reader := &previewReader{ctx: ctx, service: s.service, principal: p, pagesLeft: previewRequestPages, projects: map[string]previewProject{}, configs: map[string]*previewConfig{}}
	out := &projectv1.GetProjectPreviewsResponse{Previews: make([]*projectv1.ProjectPreview, 0, len(req.GetRefs()))}
	seen := make(map[string]bool, len(req.GetRefs()))
	for _, ref := range req.GetRefs() {
		key := ref.GetProjectId() + "\x00" + ref.GetTaskId()
		if seen[key] {
			continue
		}
		seen[key] = true
		out.Previews = append(out.Previews, reader.preview(ref.GetProjectId(), ref.GetTaskId()))
	}
	return out, nil
}

type previewProject struct {
	state projectv1.ProjectPreviewState
	name  string
}

type previewConfig struct {
	statuses map[string]*projectv1.ProjectStatus
}

type previewReader struct {
	ctx       context.Context
	service   Service
	principal *trust.Principal
	pagesLeft int
	projects  map[string]previewProject
	configs   map[string]*previewConfig
}

func previewAnswer(projectID, taskID string, state projectv1.ProjectPreviewState) *projectv1.ProjectPreview {
	return &projectv1.ProjectPreview{ProjectId: projectID, TaskId: taskID, State: state}
}

// previewFailureState classifies a failed read the way the single reads were
// classified: a refusal stays neutral, a gone record is missing, anything else
// (a transient failure, a cancelled batch) is a retryable failure.
func previewFailureState(err error) projectv1.ProjectPreviewState {
	mapped := mapError(err)
	code := status.Code(mapped)
	if reported, ok := envelope.As(mapped); ok {
		code = reported.Code().GRPCCode()
	}
	switch code {
	case codes.PermissionDenied:
		return projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_RESTRICTED
	case codes.NotFound:
		return projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_MISSING
	}
	return projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_FAILED
}

func (r *previewReader) project(projectID string) previewProject {
	if cached, ok := r.projects[projectID]; ok {
		return cached
	}
	answer := previewProject{state: projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_RESTRICTED}
	if r.ctx.Err() != nil {
		answer.state = projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_FAILED
		return answer
	}
	record, err := r.service.GetProject(r.ctx, r.principal, projectID)
	switch {
	case err != nil:
		answer.state = previewFailureState(err)
	case record.ID != projectID:
		// A malformed answer is the neutral refusal.
	case record.State == domainproject.LifecycleArchived:
		// The caller read the project, so its name may label the archived board.
		answer.state = projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_ARCHIVED
		answer.name = strings.TrimSpace(record.Name)
	default:
		answer.state = projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY
		answer.name = strings.TrimSpace(record.Name)
	}
	r.projects[projectID] = answer
	return answer
}

// config reads a project's statuses once per request. A caller who may not
// read the workflow still gets the card, only without a status.
func (r *previewReader) config(projectID string) *previewConfig {
	if cached, ok := r.configs[projectID]; ok {
		return cached
	}
	var out *previewConfig
	if configuration, err := r.service.GetWorkflowConfiguration(r.ctx, r.principal, projectID); err == nil {
		out = &previewConfig{statuses: map[string]*projectv1.ProjectStatus{}}
		for _, item := range workflowSpec(configuration.Config).GetStatuses() {
			out.statuses[item.GetStatusId()] = item
		}
	}
	r.configs[projectID] = out
	return out
}

func (r *previewReader) preview(projectID, taskID string) *projectv1.ProjectPreview {
	if projectID == "" {
		return previewAnswer(projectID, taskID, projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_RESTRICTED)
	}
	owner := r.project(projectID)
	if owner.state != projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY {
		answer := previewAnswer(projectID, taskID, owner.state)
		if taskID == "" && owner.state == projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_ARCHIVED && owner.name != "" {
			// Issue 3750: an archived board the caller can read keeps its name.
			answer.ProjectName, answer.Title = owner.name, owner.name
		}
		return answer
	}
	if taskID == "" {
		return r.board(projectID, owner)
	}
	return r.task(projectID, taskID, owner)
}

func (r *previewReader) board(projectID string, owner previewProject) *projectv1.ProjectPreview {
	if owner.name == "" {
		return previewAnswer(projectID, "", projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_RESTRICTED)
	}
	out := &projectv1.ProjectPreview{ProjectId: projectID, State: projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY, ProjectName: owner.name, Title: owner.name}
	configuration := r.config(projectID)
	if configuration == nil {
		return out
	}
	var done, total uint32
	cursor := ""
	for page := 0; page < previewProgressPages; page++ {
		if r.ctx.Err() != nil || r.pagesLeft <= 0 {
			return out
		}
		r.pagesLeft--
		listed, err := r.service.ListTasks(r.ctx, r.principal, projectID, cursor, 100)
		if err != nil {
			return out
		}
		for _, task := range listed.Tasks {
			if task.Archived {
				continue
			}
			total++
			if configuration.statuses[task.StatusID].GetCategory() == projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_DONE {
				done++
			}
		}
		if listed.Next == nil || listed.Next.AfterID == "" {
			out.ProgressKnown, out.DoneTasks, out.TotalTasks = true, done, total
			return out
		}
		cursor = listed.Next.AfterID
	}
	return out
}

func (r *previewReader) task(projectID, taskID string, owner previewProject) *projectv1.ProjectPreview {
	if r.ctx.Err() != nil {
		return previewAnswer(projectID, taskID, projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_FAILED)
	}
	record, err := r.service.GetTask(r.ctx, r.principal, projectID, taskID)
	if err != nil {
		return previewAnswer(projectID, taskID, previewFailureState(err))
	}
	title := strings.TrimSpace(record.Title)
	if record.ProjectID != projectID || record.ID != taskID || title == "" {
		return previewAnswer(projectID, taskID, projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_RESTRICTED)
	}
	if record.Archived {
		// Issue 3750: the caller read this task, so an archived one keeps its title.
		answer := previewAnswer(projectID, taskID, projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_ARCHIVED)
		answer.Title = title
		return answer
	}
	out := &projectv1.ProjectPreview{
		ProjectId: projectID, TaskId: taskID, State: projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY,
		ProjectName: owner.name, Title: title, DueDate: record.DueDate, AssigneeId: record.AssigneeID,
		Priority: taskMessage(record).GetPriority(),
	}
	if configuration := r.config(projectID); configuration != nil {
		if item := configuration.statuses[record.StatusID]; item != nil {
			out.StatusName = strings.TrimSpace(item.GetName())
			out.StatusCategory = item.GetCategory()
		}
	}
	return out
}
