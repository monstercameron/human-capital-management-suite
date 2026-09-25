// Package project exposes the project application service through generated
// gRPC methods. It adapts wire messages only; authorization and business rules
// belong to the application service.
package project

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	applicationactivity "github.com/monstercameron/human-capital-management-suite/internal/application/projectactivity"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectconfigstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectlinkstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectmemberstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	domainproject "github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	domainactivity "github.com/monstercameron/human-capital-management-suite/internal/domains/projectactivity"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const defaultPageSize = 50

// Service is the application-facing port used by this transport. The
// application service receives a verified principal separately from request
// data, so request fields cannot choose an actor or tenant.
type Service interface {
	CreateProject(context.Context, *trust.Principal, projectservice.CreateProjectRequest) (projectservice.ProjectRecord, error)
	UpdateProjectSettings(context.Context, *trust.Principal, projectservice.UpdateProjectSettingsRequest) (projectservice.ProjectRecord, error)
	ArchiveProject(context.Context, *trust.Principal, projectservice.SetProjectLifecycleRequest) (projectservice.ProjectRecord, error)
	RestoreProject(context.Context, *trust.Principal, projectservice.SetProjectLifecycleRequest) (projectservice.ProjectRecord, error)
	CreateTask(context.Context, *trust.Principal, projectservice.CreateTaskRequest) (projectservice.TaskRecord, error)
	MoveTask(context.Context, *trust.Principal, projectservice.MoveTaskRequest) (projectservice.TaskRecord, error)
	GetProject(context.Context, *trust.Principal, string) (projectservice.ProjectRecord, error)
	ListProjects(context.Context, *trust.Principal, string, int) (projectservice.ProjectListPage, error)
	GetTask(context.Context, *trust.Principal, string, string) (projectservice.TaskRecord, error)
	ListTasks(context.Context, *trust.Principal, string, string, int, ...projectservice.TaskListFilter) (projectservice.TaskListPage, error)
	SaveWorkflowDraft(context.Context, *trust.Principal, projectservice.SaveWorkflowDraftRequest) (uint64, error)
	GetWorkflowDraft(context.Context, *trust.Principal, string, ...string) (projectworkflow.Config, uint64, error)
	PreviewWorkflowDraftWithMappings(context.Context, *trust.Principal, projectservice.PreviewWorkflowDraftRequest) (projectservice.WorkflowPreview, error)
	PublishWorkflowDraft(context.Context, *trust.Principal, projectservice.PublishWorkflowDraftRequest) (projectservice.WorkflowConfiguration, error)
	GetWorkflowConfiguration(context.Context, *trust.Principal, string) (projectservice.WorkflowConfiguration, error)
	SaveView(context.Context, *trust.Principal, projectservice.SaveViewRequest) (projectboard.BoardView, error)
	ReadView(context.Context, *trust.Principal, string, string) (projectboard.BoardView, error)
	ListBoardViews(context.Context, *trust.Principal, string, string, int) (projectservice.BoardViewList, error)
	BoardPage(context.Context, *trust.Principal, string, string, int, *projectboard.Cursor) (projectservice.BoardPageResult, error)
	AddTaskLink(context.Context, *trust.Principal, projectservice.AddTaskLinkRequest) (string, uint64, error)
	RemoveTaskLink(context.Context, *trust.Principal, projectservice.RemoveTaskLinkRequest) (uint64, error)
	ListTaskLinks(context.Context, *trust.Principal, string, string, string, int) ([]projectservice.TaskLink, *projectservice.IDCursor, error)
	ProjectMembership(context.Context, *trust.Principal, string, string) (projectservice.MembershipRecord, uint64, error)
	ProjectMemberships(context.Context, *trust.Principal, string) (projectservice.MembershipSnapshot, error)
	InviteMember(context.Context, *trust.Principal, string, string, projectaccess.Role, uint64, string) error
	AcceptMemberInvitation(context.Context, *trust.Principal, string, uint64, string) error
	ChangeMemberRole(context.Context, *trust.Principal, string, string, projectaccess.Role, uint64, string) error
	RevokeMember(context.Context, *trust.Principal, string, string, uint64, string) error
	TransferMemberOwnership(context.Context, *trust.Principal, string, string, uint64, string) error
}

// ActivityService is a narrow application port for task comments and activity.
// It is injected independently so project CRUD does not depend on comment setup.
type ActivityService interface {
	AddComment(context.Context, *trust.Principal, applicationactivity.AddCommentRequest) (domainactivity.Comment, error)
	EditComment(context.Context, *trust.Principal, applicationactivity.ReviseCommentRequest) (domainactivity.Comment, error)
	DeleteComment(context.Context, *trust.Principal, applicationactivity.ReviseCommentRequest) (domainactivity.Comment, error)
	ListComments(context.Context, *trust.Principal, applicationactivity.ListRequest) (applicationactivity.CommentPage, error)
	ListActivity(context.Context, *trust.Principal, applicationactivity.ListRequest) (applicationactivity.ActivityPage, error)
}

// TaskSearchService is injected independently so deployments without the
// search fallback can fail closed without widening the ProjectService port.
type TaskSearchService interface {
	Search(context.Context, *trust.Principal, projectsearch.Request) (projectsearch.Page, error)
}

type Dependencies struct {
	Service  Service
	Activity ActivityService
	Search   TaskSearchService
}

type server struct {
	projectv1.UnimplementedProjectServiceServer
	service  Service
	activity ActivityService
	search   TaskSearchService
}

// Register registers the generated project service on srv.
func Register(srv *grpc.Server, deps Dependencies) {
	projectv1.RegisterProjectServiceServer(srv, &server{service: deps.Service, activity: deps.Activity, search: deps.Search})
}

func admitted(ctx context.Context) (*trust.Principal, error) {
	invocation, ok := transport.InvocationFromContext(ctx)
	if !ok || invocation == nil {
		return nil, envelope.New(envelope.CodeUnauthenticated, "project.no_trusted_context", "request has no trusted context")
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.Subject() == "" || p.Tenant().String() == "" {
		return nil, envelope.New(envelope.CodeUnauthenticated, "project.no_principal", "request has no authenticated principal")
	}
	if bound := invocation.Principal(); bound != nil && bound != p {
		return nil, envelope.New(envelope.CodeUnauthenticated, "project.principal_mismatch", "request has no authenticated principal")
	}
	return p, nil
}

func (s *server) unavailable() error {
	return envelope.New(envelope.CodeUnavailable, "project.service.unavailable", "project operation is unavailable")
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := envelope.As(err); ok {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}
	switch {
	case errors.Is(err, projectservice.ErrInvalidPrincipal):
		return envelope.New(envelope.CodeUnauthenticated, "project.invalid_principal", "request has no authenticated principal")
	case errors.Is(err, applicationactivity.ErrInvalidPrincipal):
		return envelope.New(envelope.CodeUnauthenticated, "project.invalid_principal", "request has no authenticated principal")
	case errors.Is(err, applicationactivity.ErrUnavailable), errors.Is(err, domainactivity.ErrPortMissing):
		return envelope.New(envelope.CodeUnavailable, "project.activity.unavailable", "task comments and activity are unavailable")
	case errors.Is(err, applicationactivity.ErrInvalidRequest), errors.Is(err, domainactivity.ErrInvalidRequest), errors.Is(err, domainactivity.ErrCursor):
		return envelope.New(envelope.CodeInvalidArgument, "project.activity.invalid_argument", "task comment or activity request is invalid")
	case errors.Is(err, domainactivity.ErrDenied):
		return envelope.New(envelope.CodePermissionDenied, "project.activity.permission_denied", "task comment or activity operation is not authorized")
	case errors.Is(err, domainactivity.ErrNotFound):
		return envelope.New(envelope.CodeNotFound, "project.activity.not_found", "task comment was not found")
	case errors.Is(err, domainactivity.ErrConflict):
		return envelope.New(envelope.CodeAborted, "project.activity.revision_conflict", "task comment revision has changed")
	case errors.Is(err, projectservice.ErrInvalidRequest), errors.Is(err, domainproject.ErrInvalidIdentity), errors.Is(err, domainproject.ErrInvalidState), errors.Is(err, projectboard.ErrInvalidView), errors.Is(err, projectboard.ErrInvalidPage):
		return envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	case errors.Is(err, projectsearch.ErrInvalidRequest):
		return envelope.New(envelope.CodeInvalidArgument, "project.search.invalid_argument", "task search request is invalid")
	case errors.Is(err, projectsearch.ErrInvalidCursor):
		return envelope.New(envelope.CodeAborted, "project.search.cursor_stale", "task search cursor is stale")
	case errors.Is(err, projectsearch.ErrTextUnavailable):
		return envelope.New(envelope.CodeUnavailable, "project.search.text_unavailable", "free-text task search is unavailable")
	case errors.Is(err, projectsearch.ErrUnavailable):
		return envelope.New(envelope.CodeUnavailable, "project.search.unavailable", "task search is unavailable")
	case errors.Is(err, projectaccess.ErrInvalidProject), errors.Is(err, projectaccess.ErrInvalidRole):
		return envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	case errors.Is(err, projectaccess.ErrTenantMismatch), errors.Is(err, projectaccess.ErrInvitationNotFound), errors.Is(err, projectaccess.ErrMemberNotFound):
		return envelope.New(envelope.CodeNotFound, "project.not_found", "project resource was not found")
	case errors.Is(err, projectaccess.ErrOwnerRequired):
		return envelope.New(envelope.CodeFailedPrecondition, "project.owner_required", "project must retain an active owner")
	case errors.Is(err, projectstore.ErrNotFound):
		return envelope.New(envelope.CodeNotFound, "project.not_found", "project resource was not found")
	case errors.Is(err, projectlinkstore.ErrInvalidRecord):
		return envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	case errors.Is(err, projectlinkstore.ErrConflict):
		return envelope.New(envelope.CodeAlreadyExists, "project.links.conflict", "task link id is already in use")
	case errors.Is(err, domainproject.ErrNotAuthorized), errors.Is(err, projectaccess.ErrUnauthorized), errors.Is(err, projectaccess.ErrRecoveryNotAuthorized):
		return envelope.New(envelope.CodePermissionDenied, "project.permission_denied", "project operation is not authorized")
	case errors.Is(err, domainproject.ErrRevisionConflict):
		return envelope.New(envelope.CodeAborted, "project.revision_conflict", "project revision has changed")
	case errors.Is(err, projectstore.ErrRevisionConflict), errors.Is(err, projectconfigstore.ErrRevisionConflict), errors.Is(err, projectconfigstore.ErrMigrationPlanDigestConflict):
		return envelope.New(envelope.CodeAborted, "project.revision_conflict", "project revision has changed")
	case errors.Is(err, projectmemberstore.ErrRevisionConflict):
		return envelope.New(envelope.CodeAborted, "project.membership.revision_conflict", "project membership revision has changed")
	case errors.Is(err, projectmemberstore.ErrInvalidRequest):
		return envelope.New(envelope.CodeInvalidArgument, "project.membership.invalid_argument", "project membership request is invalid")
	case errors.Is(err, projectmemberstore.ErrNotFound):
		return envelope.New(envelope.CodeNotFound, "project.not_found", "project resource was not found")
	case errors.Is(err, projectstore.ErrIdempotencyConflict), errors.Is(err, projectconfigstore.ErrIdempotencyReuse):
		return envelope.New(envelope.CodeAlreadyExists, "project.idempotency_conflict", "idempotency key was already used for a different request")
	case errors.Is(err, projectservice.ErrStaleView):
		return envelope.New(envelope.CodeAborted, "project.view.revision_conflict", "board view revision has changed")
	case errors.Is(err, projectservice.ErrUnavailable), errors.Is(err, domainproject.ErrWriteUnavailable):
		return envelope.New(envelope.CodeUnavailable, "project.service.unavailable", "project operation is unavailable")
	case errors.Is(err, projectlink.ErrResolverUnavailable):
		return envelope.New(envelope.CodeUnavailable, "project.links.unavailable", "task links are unavailable")
	case errors.Is(err, projectlink.ErrInvalidReference), errors.Is(err, projectworkflow.ErrInvalidConfig):
		return envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	case errors.Is(err, projectworkflow.ErrInvalidVersion):
		return envelope.New(envelope.CodeAborted, "project.workflow.revision_conflict", "project workflow revision has changed")
	case errors.Is(err, projectworkflow.ErrSnapshotLimitExceeded):
		return envelope.New(envelope.CodeResourceExhausted, "project.workflow.snapshot_limit", "workflow migration snapshot exceeds its bounded limit")
	case errors.Is(err, projectworkflow.ErrMigrationLimitExceeded):
		return envelope.New(envelope.CodeResourceExhausted, "project.workflow.limit_exceeded", "workflow operation exceeds its bounded limit")
	case errors.Is(err, projectstore.ErrRestoreWorkflowMismatch), errors.Is(err, projectservice.ErrUnsafeWorkflowPublish), errors.Is(err, projectconfigstore.ErrMigrationRequired), errors.Is(err, projectworkflow.ErrUnsafeMigration), errors.Is(err, projectworkflow.ErrTransitionRejected), errors.Is(err, projectconfigstore.ErrDigestConflict), errors.Is(err, projectconfigstore.ErrReviewRequired):
		return envelope.New(envelope.CodeFailedPrecondition, "project.workflow.rejected", "project workflow operation was rejected")
	case errors.Is(err, projectservice.ErrUnsupportedLink):
		return envelope.New(envelope.CodeInvalidArgument, "project.links.unsupported", "task link type is not supported")
	default:
		return envelope.New(envelope.CodeUnspecified, "project.internal_error", "project operation failed")
	}
}

func requireService(s *server) error {
	if s == nil || s.service == nil {
		return envelope.New(envelope.CodeUnavailable, "project.service.unavailable", "project operation is unavailable")
	}
	return nil
}

func requireActivity(s *server) error {
	if s == nil || s.activity == nil {
		return envelope.New(envelope.CodeUnavailable, "project.activity.unavailable", "task comments and activity are unavailable")
	}
	return nil
}

func pageSize(page interface{ GetPageSize() int32 }) (int, error) {
	if page == nil || page.GetPageSize() == 0 {
		return defaultPageSize, nil
	}
	n := int(page.GetPageSize())
	if n < 1 || n > projectboard.MaxPageSize {
		return 0, envelope.New(envelope.CodeInvalidArgument, "project.page_size.invalid", "page size must be between 1 and 100")
	}
	return n, nil
}

func projectMessage(record projectservice.ProjectRecord) *projectv1.Project {
	state := projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_UNSPECIFIED
	switch record.State {
	case domainproject.LifecycleActive:
		state = projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ACTIVE
	case domainproject.LifecycleSuspended:
		state = projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_SUSPENDED
	case domainproject.LifecycleArchived:
		state = projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ARCHIVED
	}
	return &projectv1.Project{ProjectId: record.ID, Name: record.Name, ProjectTimezone: record.Timezone, Lifecycle: state, Revision: record.Revision, WorkflowRevision: record.WorkflowRevision, OwnerId: record.OwnerID}
}

func taskMessage(record projectservice.TaskRecord) *projectv1.ProjectTask {
	priority := projectv1.TaskPriority_TASK_PRIORITY_NORMAL
	switch domainproject.Priority(record.Priority) {
	case domainproject.PriorityLow:
		priority = projectv1.TaskPriority_TASK_PRIORITY_LOW
	case domainproject.PriorityHigh:
		priority = projectv1.TaskPriority_TASK_PRIORITY_HIGH
	case domainproject.PriorityUrgent:
		priority = projectv1.TaskPriority_TASK_PRIORITY_URGENT
	}
	out := &projectv1.ProjectTask{TaskId: record.ID, ProjectId: record.ProjectID, Title: record.Title, Description: record.Description, TaskTypeId: record.TypeID, StatusId: record.StatusID, AssigneeId: record.AssigneeID, DueDate: record.DueDate, Revision: record.Revision, WorkflowRevision: record.WorkflowRevision, Priority: priority, Archived: record.Archived, CreatedBy: record.CreatedBy, StartDate: record.StartDate, StoryPoints: record.StoryPoints, Labels: append([]string(nil), record.Labels...)}
	if !record.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(record.CreatedAt)
	}
	if !record.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(record.UpdatedAt)
	}
	for field, edit := range record.Fields {
		if value := typedFieldMessage(field, edit); value != nil {
			out.CustomFields = append(out.CustomFields, value)
		}
	}
	if len(record.Fields) == 0 {
		for field, value := range record.EnumFields {
			out.CustomFields = append(out.CustomFields, &projectv1.TypedFieldValue{FieldId: field, Value: &projectv1.TypedFieldValue_EnumValue{EnumValue: value}})
		}
	}
	return out
}

func typedFieldMessage(field string, edit domainproject.TaskFieldEdit) *projectv1.TypedFieldValue {
	switch edit.Type {
	case "TEXT":
		return &projectv1.TypedFieldValue{FieldId: field, Value: &projectv1.TypedFieldValue_TextValue{TextValue: canonicalString(edit.CanonicalValue)}}
	case "DATE":
		return &projectv1.TypedFieldValue{FieldId: field, Value: &projectv1.TypedFieldValue_DateValue{DateValue: canonicalString(edit.CanonicalValue)}}
	case "ENUM":
		return &projectv1.TypedFieldValue{FieldId: field, Value: &projectv1.TypedFieldValue_EnumValue{EnumValue: canonicalString(edit.CanonicalValue)}}
	case "PERSON":
		return &projectv1.TypedFieldValue{FieldId: field, Value: &projectv1.TypedFieldValue_PersonId{PersonId: canonicalString(edit.CanonicalValue)}}
	case "LINK":
		return &projectv1.TypedFieldValue{FieldId: field, Value: &projectv1.TypedFieldValue_LinkValue{LinkValue: canonicalString(edit.CanonicalValue)}}
	case "BOOLEAN":
		v, err := strconv.ParseBool(edit.CanonicalValue)
		if err == nil {
			return &projectv1.TypedFieldValue{FieldId: field, Value: &projectv1.TypedFieldValue_BooleanValue{BooleanValue: v}}
		}
	case "NUMBER":
		raw := edit.CanonicalValue
		sign := commonv1.DecimalSign_DECIMAL_SIGN_POSITIVE
		if strings.HasPrefix(raw, "-") {
			sign = commonv1.DecimalSign_DECIMAL_SIGN_NEGATIVE
			raw = strings.TrimPrefix(raw, "-")
		}
		parts := strings.Split(raw, ".")
		if len(parts) > 2 {
			return nil
		}
		scale := 0
		if len(parts) == 2 {
			scale = len(parts[1])
			raw = parts[0] + parts[1]
		}
		magnitude, ok := new(big.Int).SetString(raw, 10)
		if !ok || scale > 18 {
			return nil
		}
		if magnitude.Sign() == 0 {
			sign = commonv1.DecimalSign_DECIMAL_SIGN_ZERO
		}
		return &projectv1.TypedFieldValue{FieldId: field, Value: &projectv1.TypedFieldValue_NumberValue{NumberValue: &commonv1.Decimal{Sign: sign, UnscaledMagnitude: magnitude.Bytes(), Scale: int32(scale)}}}
	}
	return nil
}

func canonicalString(value string) string {
	var decoded string
	if json.Unmarshal([]byte(value), &decoded) == nil {
		return decoded
	}
	return value
}

func (s *server) CreateProject(ctx context.Context, req *projectv1.CreateProjectRequest) (*projectv1.CreateProjectResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	record, err := s.service.CreateProject(ctx, p, projectservice.CreateProjectRequest{Name: req.GetName(), Timezone: req.GetProjectTimezone(), IdempotencyKey: req.GetIdempotencyKey()})
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.CreateProjectResponse{Project: projectMessage(record)}, nil
}

func (s *server) UpdateProjectSettings(ctx context.Context, req *projectv1.UpdateProjectSettingsRequest) (*projectv1.UpdateProjectSettingsResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	record, err := s.service.UpdateProjectSettings(ctx, p, projectservice.UpdateProjectSettingsRequest{
		ProjectID: req.GetProjectId(), Name: req.GetName(), Timezone: req.GetProjectTimezone(),
		ExpectedProjectRevision: req.GetExpectedProjectRevision(), IdempotencyKey: req.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.UpdateProjectSettingsResponse{Project: projectMessage(record)}, nil
}

func (s *server) ArchiveProject(ctx context.Context, req *projectv1.SetProjectLifecycleRequest) (*projectv1.SetProjectLifecycleResponse, error) {
	return s.setProjectLifecycle(ctx, req, true)
}

func (s *server) RestoreProject(ctx context.Context, req *projectv1.SetProjectLifecycleRequest) (*projectv1.SetProjectLifecycleResponse, error) {
	return s.setProjectLifecycle(ctx, req, false)
}

func (s *server) setProjectLifecycle(ctx context.Context, req *projectv1.SetProjectLifecycleRequest, archive bool) (*projectv1.SetProjectLifecycleResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	input := projectservice.SetProjectLifecycleRequest{ProjectID: req.GetProjectId(), ExpectedProjectRevision: req.GetExpectedProjectRevision(), IdempotencyKey: req.GetIdempotencyKey()}
	var record projectservice.ProjectRecord
	if archive {
		record, err = s.service.ArchiveProject(ctx, p, input)
	} else {
		record, err = s.service.RestoreProject(ctx, p, input)
	}
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.SetProjectLifecycleResponse{Project: projectMessage(record)}, nil
}

func (s *server) CreateTask(ctx context.Context, req *projectv1.CreateTaskRequest) (*projectv1.CreateTaskResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	typeID := req.GetTaskTypeId()
	priority := "NORMAL"
	switch req.GetPriority() {
	case projectv1.TaskPriority_TASK_PRIORITY_LOW:
		priority = "LOW"
	case projectv1.TaskPriority_TASK_PRIORITY_HIGH:
		priority = "HIGH"
	case projectv1.TaskPriority_TASK_PRIORITY_URGENT:
		priority = "URGENT"
	case projectv1.TaskPriority_TASK_PRIORITY_NORMAL:
		priority = "NORMAL"
	case projectv1.TaskPriority_TASK_PRIORITY_UNSPECIFIED:
	default:
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.priority.invalid", "task priority is invalid")
	}
	fields, err := typedFieldEdits(req.GetCustomFields())
	if err != nil {
		return nil, err
	}
	var sourceReference *projectlink.Reference
	if req.GetSourceReference() != nil {
		source, sourceErr := sourceReferenceFromMessage(req.GetSourceReference())
		if sourceErr != nil {
			return nil, mapError(sourceErr)
		}
		sourceReference = &source
	}
	record, err := s.service.CreateTask(ctx, p, projectservice.CreateTaskRequest{ProjectID: req.GetProjectId(), Title: req.GetTitle(), Description: req.GetDescription(), AssigneeID: req.GetAssigneeId(), DueDate: req.GetDueDate(), InitialStatusID: req.GetInitialStatusId(), TypeID: typeID, Priority: priority, IdempotencyKey: req.GetIdempotencyKey(), ExpectedWorkflowRevision: req.GetExpectedWorkflowRevision(), CustomFields: fields, SourceReference: sourceReference})
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.CreateTaskResponse{Task: taskMessage(record)}, nil
}

func (s *server) MoveTask(ctx context.Context, req *projectv1.MoveTaskRequest) (*projectv1.MoveTaskResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	var edits []domainproject.TaskFieldEdit
	if field := req.GetLaneFieldEdit(); field != nil {
		var err error
		edits, err = typedFieldEdits([]*projectv1.TypedFieldValue{field})
		if err != nil {
			return nil, err
		}
	}
	record, err := s.service.MoveTask(ctx, p, projectservice.MoveTaskRequest{ProjectID: req.GetProjectId(), TaskID: req.GetTaskId(), TargetStatusID: req.GetTargetStatusId(), ExpectedTaskRevision: req.GetExpectedTaskRevision(), ExpectedConfigRevision: req.GetExpectedWorkflowRevision(), IdempotencyKey: req.GetIdempotencyKey(), FieldEdits: edits})
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.MoveTaskResponse{Task: taskMessage(record)}, nil
}

func (s *server) SaveBoardView(ctx context.Context, req *projectv1.SaveBoardViewRequest) (*projectv1.SaveBoardViewResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil || req.GetView() == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.view.invalid", "board view is invalid")
	}
	view := viewFromMessage(req.GetView())
	if err := validateViewWire(req.GetView()); err != nil {
		return nil, err
	}
	saved, err := s.service.SaveView(ctx, p, projectservice.SaveViewRequest{ProjectID: req.GetProjectId(), ExpectedVersion: req.GetExpectedViewRevision(), IdempotencyKey: req.GetIdempotencyKey(), View: view})
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.SaveBoardViewResponse{View: viewMessage(req.GetProjectId(), req.GetView().GetName(), saved)}, nil
}

func (s *server) GetBoard(ctx context.Context, req *projectv1.GetBoardRequest) (*projectv1.GetBoardResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	limit, err := pageSize(req.GetPage())
	if err != nil {
		return nil, err
	}
	cursor := (*projectboard.Cursor)(nil)
	if token := req.GetPage().GetCursor(); token != "" {
		cursor = &projectboard.Cursor{Token: token}
	}
	view, err := s.service.ReadView(ctx, p, req.GetProjectId(), req.GetViewId())
	if err != nil {
		return nil, mapError(err)
	}
	page, err := s.service.BoardPage(ctx, p, req.GetProjectId(), req.GetViewId(), limit, cursor)
	if err != nil {
		return nil, mapError(err)
	}
	freshness := projectv1.BoardFreshness_BOARD_FRESHNESS_CURRENT
	if page.Freshness == "BOUNDED_STALE" {
		freshness = projectv1.BoardFreshness_BOARD_FRESHNESS_BOUNDED_STALE
	}
	return &projectv1.GetBoardResponse{View: viewMessage(req.GetProjectId(), "", view), Tasks: cards(req.GetProjectId(), page.Page, page.WorkflowRevision, page.TaskRevisions), Page: &commonv1.PageResponse{NextCursor: nextCursor(page.Page)}, ProjectRevision: page.ProjectRevision, WorkflowRevision: page.WorkflowRevision, Freshness: freshness}, nil
}

func viewFromMessage(v *projectv1.BoardView) projectboard.BoardView {
	view := projectboard.BoardView{ID: v.GetViewId(), Name: strings.TrimSpace(v.GetName()), Version: v.GetRevision(), CardFields: append([]string(nil), v.GetCardFieldIds()...), SwimlaneValueOrder: append([]string(nil), v.GetSwimlaneValueOrder()...)}
	if v.GetAudience() == projectv1.BoardViewAudience_BOARD_VIEW_AUDIENCE_PROJECT {
		view.Audience = projectboard.AudienceProject
	} else {
		view.Audience = projectboard.AudiencePersonal
	}
	for _, c := range v.GetColumns() {
		if c != nil {
			view.Columns = append(view.Columns, projectboard.Column{ID: c.GetColumnId(), Label: c.GetName(), StatusIDs: append([]string(nil), c.GetStatusIds()...)})
		}
	}
	if f := v.GetFilter(); f != nil {
		view.Filter.StatusIDs = append([]string(nil), f.GetStatusIds()...)
		if len(f.GetAssigneeIds()) == 1 {
			view.Filter.AssigneeID = f.GetAssigneeIds()[0]
		}
		view.Filter.TypeIDs = append([]string(nil), f.GetTaskTypeIds()...)
		for _, priority := range f.GetPriorities() {
			if text, ok := priorityString(priority); ok {
				view.Filter.Priorities = append(view.Filter.Priorities, text)
			}
		}
		view.Filter.EnumFields = make(map[string][]string, len(f.GetEnumFilters()))
		for _, ef := range f.GetEnumFilters() {
			if ef != nil {
				view.Filter.EnumFields[ef.GetFieldId()] = append([]string(nil), ef.GetValues()...)
			}
		}
	}
	switch v.GetSwimlaneGrouping() {
	case projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_ASSIGNEE:
		view.Grouping.Kind = projectboard.GroupAssignee
	case projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_PRIORITY:
		view.Grouping.Kind = projectboard.GroupPriority
	case projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_ENUM_FIELD:
		view.Grouping = projectboard.Grouping{Kind: projectboard.GroupEnum, FieldID: v.GetSwimlaneFieldId()}
	default:
		view.Grouping.Kind = projectboard.GroupNone
	}
	switch v.GetOrder() {
	case projectv1.BoardOrder_BOARD_ORDER_TITLE:
		view.OrderBy = projectboard.OrderTitle
	case projectv1.BoardOrder_BOARD_ORDER_PRIORITY:
		view.OrderBy = projectboard.OrderPriority
	case projectv1.BoardOrder_BOARD_ORDER_DUE_DATE:
		view.OrderBy = projectboard.OrderDueDate
	default:
		view.OrderBy = projectboard.OrderTaskID
	}
	return view
}

func validateViewWire(v *projectv1.BoardView) error {
	if v == nil {
		return envelope.New(envelope.CodeInvalidArgument, "project.view.invalid", "board view is invalid")
	}
	if v.GetAudience() == projectv1.BoardViewAudience_BOARD_VIEW_AUDIENCE_UNSPECIFIED || len(v.GetFilter().GetAssigneeIds()) > 1 || v.GetOrder() == projectv1.BoardOrder_BOARD_ORDER_UPDATED {
		return envelope.New(envelope.CodeInvalidArgument, "project.view.unsupported_shape", "board view contains an unsupported configuration")
	}
	if v.GetOrder() < projectv1.BoardOrder_BOARD_ORDER_UNSPECIFIED || v.GetOrder() > projectv1.BoardOrder_BOARD_ORDER_TITLE || v.GetSwimlaneGrouping() < projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_UNSPECIFIED || v.GetSwimlaneGrouping() > projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_ENUM_FIELD {
		return envelope.New(envelope.CodeInvalidArgument, "project.view.invalid_enum", "board view contains an invalid enum")
	}
	return nil
}

func typedFieldEdits(fields []*projectv1.TypedFieldValue) ([]domainproject.TaskFieldEdit, error) {
	out := make([]domainproject.TaskFieldEdit, 0, len(fields))
	for _, field := range fields {
		if field == nil || strings.TrimSpace(field.GetFieldId()) == "" {
			return nil, envelope.New(envelope.CodeInvalidArgument, "project.typed_field.invalid", "typed field value is invalid")
		}
		value, kind := "", ""
		switch v := field.GetValue().(type) {
		case *projectv1.TypedFieldValue_TextValue:
			value, kind = v.TextValue, "TEXT"
		case *projectv1.TypedFieldValue_NumberValue:
			if v.NumberValue == nil {
				return nil, envelope.New(envelope.CodeInvalidArgument, "project.typed_field.invalid", "typed field value is invalid")
			}
			d := v.NumberValue
			mag := new(big.Int).SetBytes(d.GetUnscaledMagnitude())
			negative := d.GetSign() == commonv1.DecimalSign_DECIMAL_SIGN_NEGATIVE
			if negative {
				mag.Neg(mag)
			} else if d.GetSign() != commonv1.DecimalSign_DECIMAL_SIGN_POSITIVE && mag.Sign() != 0 {
				return nil, envelope.New(envelope.CodeInvalidArgument, "project.typed_field.invalid", "typed field value is invalid")
			}
			scale := int(d.GetScale())
			if scale < 0 || scale > 18 {
				return nil, envelope.New(envelope.CodeInvalidArgument, "project.typed_field.invalid", "typed field value is invalid")
			}
			raw := mag.String()
			digits := strings.TrimPrefix(raw, "-")
			for len(digits) <= scale {
				digits = "0" + digits
			}
			if scale > 0 {
				raw = digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
			} else {
				raw = digits
			}
			if negative {
				raw = "-" + raw
			}
			value, kind = raw, "NUMBER"
		case *projectv1.TypedFieldValue_DateValue:
			value, kind = v.DateValue, "DATE"
		case *projectv1.TypedFieldValue_EnumValue:
			value, kind = v.EnumValue, "ENUM"
		case *projectv1.TypedFieldValue_PersonId:
			value, kind = v.PersonId, "PERSON"
		case *projectv1.TypedFieldValue_LinkValue:
			value, kind = v.LinkValue, "LINK"
		case *projectv1.TypedFieldValue_BooleanValue:
			value, kind = strconv.FormatBool(v.BooleanValue), "BOOLEAN"
		default:
			return nil, envelope.New(envelope.CodeInvalidArgument, "project.typed_field.value_missing", "typed field value is required")
		}
		if kind == "TEXT" || kind == "DATE" || kind == "ENUM" || kind == "PERSON" || kind == "LINK" {
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, envelope.New(envelope.CodeInvalidArgument, "project.typed_field.invalid", "typed field value is invalid")
			}
			value = string(encoded)
		}
		out = append(out, domainproject.TaskFieldEdit{FieldID: field.GetFieldId(), Type: kind, CanonicalValue: value})
	}
	return out, nil
}

func priorityString(p projectv1.TaskPriority) (string, bool) {
	switch p {
	case projectv1.TaskPriority_TASK_PRIORITY_LOW:
		return "LOW", true
	case projectv1.TaskPriority_TASK_PRIORITY_NORMAL:
		return "NORMAL", true
	case projectv1.TaskPriority_TASK_PRIORITY_HIGH:
		return "HIGH", true
	case projectv1.TaskPriority_TASK_PRIORITY_URGENT:
		return "URGENT", true
	default:
		return "", false
	}
}

func viewMessage(projectID, name string, v projectboard.BoardView) *projectv1.BoardView {
	if strings.TrimSpace(name) == "" {
		name = v.Name
	}
	audience := projectv1.BoardViewAudience_BOARD_VIEW_AUDIENCE_PERSONAL
	if v.Audience == projectboard.AudienceProject {
		audience = projectv1.BoardViewAudience_BOARD_VIEW_AUDIENCE_PROJECT
	}
	out := &projectv1.BoardView{ViewId: v.ID, ProjectId: projectID, Name: name, Audience: audience, Revision: v.Version, CardFieldIds: append([]string(nil), v.CardFields...), SwimlaneValueOrder: append([]string(nil), v.SwimlaneValueOrder...)}
	filter := &projectv1.BoardFilter{StatusIds: append([]string(nil), v.Filter.StatusIDs...), TaskTypeIds: append([]string(nil), v.Filter.TypeIDs...)}
	if v.Filter.AssigneeID != "" {
		filter.AssigneeIds = []string{v.Filter.AssigneeID}
	}
	for _, priority := range v.Filter.Priorities {
		if p, ok := priorityEnum(priority); ok {
			filter.Priorities = append(filter.Priorities, p)
		}
	}
	for field, values := range v.Filter.EnumFields {
		filter.EnumFilters = append(filter.EnumFilters, &projectv1.EnumBoardFilter{FieldId: field, Values: append([]string(nil), values...)})
	}
	out.Filter = filter
	switch v.Grouping.Kind {
	case projectboard.GroupAssignee:
		out.SwimlaneGrouping = projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_ASSIGNEE
	case projectboard.GroupPriority:
		out.SwimlaneGrouping = projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_PRIORITY
	case projectboard.GroupEnum:
		out.SwimlaneGrouping = projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_ENUM_FIELD
		out.SwimlaneFieldId = v.Grouping.FieldID
	default:
		out.SwimlaneGrouping = projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_NONE
	}
	switch v.OrderBy {
	case projectboard.OrderTitle:
		out.Order = projectv1.BoardOrder_BOARD_ORDER_TITLE
	case projectboard.OrderPriority:
		out.Order = projectv1.BoardOrder_BOARD_ORDER_PRIORITY
	case projectboard.OrderDueDate:
		out.Order = projectv1.BoardOrder_BOARD_ORDER_DUE_DATE
	default:
		out.Order = projectv1.BoardOrder_BOARD_ORDER_MANUAL
	}
	for i, c := range v.Columns {
		out.Columns = append(out.Columns, &projectv1.BoardColumn{ColumnId: c.ID, Name: c.Label, StatusIds: append([]string(nil), c.StatusIDs...), Position: int32(i)})
	}
	return out
}

func priorityEnum(priority string) (projectv1.TaskPriority, bool) {
	switch domainproject.Priority(priority) {
	case domainproject.PriorityLow:
		return projectv1.TaskPriority_TASK_PRIORITY_LOW, true
	case domainproject.PriorityNormal:
		return projectv1.TaskPriority_TASK_PRIORITY_NORMAL, true
	case domainproject.PriorityHigh:
		return projectv1.TaskPriority_TASK_PRIORITY_HIGH, true
	case domainproject.PriorityUrgent:
		return projectv1.TaskPriority_TASK_PRIORITY_URGENT, true
	default:
		return projectv1.TaskPriority_TASK_PRIORITY_UNSPECIFIED, false
	}
}

func cards(projectID string, page projectboard.Page, workflowRevision uint64, revisions map[string]uint64) []*projectv1.ProjectTask {
	var out []*projectv1.ProjectTask
	for _, col := range page.Columns {
		for _, lane := range col.Lanes {
			for _, card := range lane.Cards {
				t := card.Task
				item := &projectv1.ProjectTask{TaskId: t.ID, ProjectId: projectID, Title: t.Title, StatusId: t.StatusID, AssigneeId: t.AssigneeID, DueDate: t.DueDate, TaskTypeId: t.TypeID, WorkflowRevision: workflowRevision, Revision: revisions[t.ID], Labels: append([]string(nil), t.Labels...), StoryPoints: t.StoryPoints, StartDate: t.StartDate}
				switch domainproject.Priority(t.Priority) {
				case domainproject.PriorityLow:
					item.Priority = projectv1.TaskPriority_TASK_PRIORITY_LOW
				case domainproject.PriorityHigh:
					item.Priority = projectv1.TaskPriority_TASK_PRIORITY_HIGH
				case domainproject.PriorityUrgent:
					item.Priority = projectv1.TaskPriority_TASK_PRIORITY_URGENT
				default:
					item.Priority = projectv1.TaskPriority_TASK_PRIORITY_NORMAL
				}
				for field, value := range t.EnumFields {
					item.CustomFields = append(item.CustomFields, &projectv1.TypedFieldValue{FieldId: field, Value: &projectv1.TypedFieldValue_EnumValue{EnumValue: value}})
				}
				out = append(out, item)
			}
		}
	}
	return out
}

func nextCursor(page projectboard.Page) string {
	if page.Next == nil {
		return ""
	}
	return page.Next.Token
}

func listPageSize(page interface{ GetPageSize() int32 }) (int, error) {
	if page == nil || page.GetPageSize() == 0 {
		return defaultPageSize, nil
	}
	n := int(page.GetPageSize())
	if n < 1 || n > projectservice.MaxListPageSize {
		return 0, envelope.New(envelope.CodeInvalidArgument, "project.page_size.invalid", "page size must be between 1 and 100")
	}
	return n, nil
}

func taskListFilter(f *projectv1.BoardFilter) (projectservice.TaskListFilter, error) {
	var out projectservice.TaskListFilter
	if f == nil {
		return out, nil
	}
	out.StatusIDs = append([]string(nil), f.GetStatusIds()...)
	out.TypeIDs = append([]string(nil), f.GetTaskTypeIds()...)
	if len(f.GetAssigneeIds()) > 1 {
		return out, envelope.New(envelope.CodeInvalidArgument, "project.task_filter.invalid", "task filter is invalid")
	}
	if len(f.GetAssigneeIds()) == 1 {
		out.AssigneeID = f.GetAssigneeIds()[0]
	}
	out.EnumFields = map[string][]string{}
	for _, ef := range f.GetEnumFilters() {
		if ef == nil || strings.TrimSpace(ef.GetFieldId()) == "" || len(ef.GetValues()) == 0 {
			return out, envelope.New(envelope.CodeInvalidArgument, "project.task_filter.invalid", "task filter is invalid")
		}
		out.EnumFields[ef.GetFieldId()] = append([]string(nil), ef.GetValues()...)
	}
	for _, p := range f.GetPriorities() {
		text, ok := priorityString(p)
		if !ok {
			return out, envelope.New(envelope.CodeInvalidArgument, "project.task_filter.invalid", "task filter is invalid")
		}
		out.Priorities = append(out.Priorities, text)
	}
	return out, nil
}

func workflowFromSpec(spec *projectv1.WorkflowConfigurationSpec) projectworkflow.Config {
	var cfg projectworkflow.Config
	for _, status := range spec.GetStatuses() {
		if status != nil {
			cfg.Statuses = append(cfg.Statuses, projectworkflow.Status{ID: status.GetStatusId(), Name: status.GetName(), Category: workflowCategory(status.GetCategory()), AllowedNextStatusIDs: append([]string(nil), status.GetAllowedNextStatusIds()...), RequiredFieldIDs: append([]string(nil), status.GetRequiredFieldIds()...), Retired: status.GetRetired()})
		}
	}
	for _, typ := range spec.GetTaskTypes() {
		if typ != nil {
			cfg.TaskTypes = append(cfg.TaskTypes, projectworkflow.TaskType{ID: typ.GetTaskTypeId(), Name: typ.GetName(), FieldIDs: append([]string(nil), typ.GetFieldIds()...), InitialStatus: typ.GetInitialStatusId(), RequiredFields: append([]string(nil), typ.GetRequiredFieldIds()...)})
		}
	}
	for _, field := range spec.GetFields() {
		if field != nil {
			item := projectworkflow.Field{ID: field.GetFieldId(), Name: field.GetName(), Type: workflowFieldType(field.GetType()), Required: field.GetRequired(), Validation: projectworkflow.FieldValidation{Options: append([]string(nil), field.GetEnumValues()...)}, Classification: field.GetClassification(), Indexed: field.GetIndexed(), Searchable: field.GetSearchable(), Retired: field.GetRetired()}
			if field.MinLength != nil {
				value := int(field.GetMinLength())
				item.Validation.MinLength = &value
			}
			if field.MaxLength != nil {
				value := int(field.GetMaxLength())
				item.Validation.MaxLength = &value
			}
			if field.MinNumber != nil {
				value := field.GetMinNumber()
				item.Validation.MinNumber = &value
			}
			if field.MaxNumber != nil {
				value := field.GetMaxNumber()
				item.Validation.MaxNumber = &value
			}
			if field.GetDefaultJson() != "" {
				item.Default = json.RawMessage(field.GetDefaultJson())
			}
			cfg.Fields = append(cfg.Fields, item)
		}
	}
	for _, transition := range spec.GetTransitions() {
		if transition != nil {
			cfg.Transitions = append(cfg.Transitions, projectworkflow.Transition{From: transition.GetFromStatusId(), To: transition.GetToStatusId(), TaskTypeID: transition.GetTaskTypeId(), RequiredFields: append([]string(nil), transition.GetRequiredFieldIds()...)})
		}
	}
	for _, column := range spec.GetColumns() {
		if column != nil {
			cfg.Columns = append(cfg.Columns, projectworkflow.Column{ID: column.GetColumnId(), Name: column.GetName(), StatusIDs: append([]string(nil), column.GetStatusIds()...)})
		}
	}
	return cfg
}

func workflowCategory(category projectv1.ProjectStatusCategory) projectworkflow.StatusCategory {
	switch category {
	case projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_NOT_STARTED:
		return projectworkflow.CategoryNotStarted
	case projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_ACTIVE:
		return projectworkflow.CategoryActive
	case projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_BLOCKED:
		return projectworkflow.CategoryBlocked
	case projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_DONE:
		return projectworkflow.CategoryDone
	case projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_CANCELLED:
		return projectworkflow.CategoryCancelled
	default:
		return ""
	}
}

func workflowFieldType(typ projectv1.ProjectFieldType) projectworkflow.FieldType {
	switch typ {
	case projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_TEXT:
		return projectworkflow.FieldText
	case projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_NUMBER:
		return projectworkflow.FieldNumber
	case projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_DATE:
		return projectworkflow.FieldDate
	case projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_ENUM:
		return projectworkflow.FieldEnum
	case projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_PERSON:
		return projectworkflow.FieldPerson
	case projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_LINK:
		return projectworkflow.FieldLink
	case projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_BOOLEAN:
		return projectworkflow.FieldBoolean
	default:
		return ""
	}
}

func workflowSpec(cfg projectworkflow.Config) *projectv1.WorkflowConfigurationSpec {
	out := &projectv1.WorkflowConfigurationSpec{}
	for _, status := range cfg.Statuses {
		item := &projectv1.ProjectStatus{StatusId: status.ID, Name: status.Name}
		item.Category = workflowCategoryMessage(status.Category)
		item.AllowedNextStatusIds = append([]string(nil), status.AllowedNextStatusIDs...)
		item.RequiredFieldIds = append([]string(nil), status.RequiredFieldIDs...)
		item.Retired = status.Retired
		out.Statuses = append(out.Statuses, item)
	}
	for _, typ := range cfg.TaskTypes {
		out.TaskTypes = append(out.TaskTypes, &projectv1.ProjectTaskType{TaskTypeId: typ.ID, Name: typ.Name, FieldIds: append([]string(nil), typ.FieldIDs...), InitialStatusId: typ.InitialStatus, RequiredFieldIds: append([]string(nil), typ.RequiredFields...)})
	}
	for _, field := range cfg.Fields {
		item := &projectv1.ProjectFieldDefinition{FieldId: field.ID, Name: field.Name, Type: workflowFieldTypeMessage(field.Type), Required: field.Required, EnumValues: append([]string(nil), field.Validation.Options...), Classification: field.Classification, DefaultJson: string(field.Default), Indexed: field.Indexed, Searchable: field.Searchable, Retired: field.Retired}
		if field.Validation.MinLength != nil {
			value := int64(*field.Validation.MinLength)
			item.MinLength = &value
		}
		if field.Validation.MaxLength != nil {
			value := int64(*field.Validation.MaxLength)
			item.MaxLength = &value
		}
		if field.Validation.MinNumber != nil {
			value := *field.Validation.MinNumber
			item.MinNumber = &value
		}
		if field.Validation.MaxNumber != nil {
			value := *field.Validation.MaxNumber
			item.MaxNumber = &value
		}
		out.Fields = append(out.Fields, item)
	}
	for _, transition := range cfg.Transitions {
		out.Transitions = append(out.Transitions, &projectv1.ProjectWorkflowTransition{FromStatusId: transition.From, ToStatusId: transition.To, TaskTypeId: transition.TaskTypeID, RequiredFieldIds: append([]string(nil), transition.RequiredFields...)})
	}
	for _, column := range cfg.Columns {
		out.Columns = append(out.Columns, &projectv1.ProjectWorkflowColumn{ColumnId: column.ID, Name: column.Name, StatusIds: append([]string(nil), column.StatusIDs...)})
	}
	return out
}

func workflowCategoryMessage(category projectworkflow.StatusCategory) projectv1.ProjectStatusCategory {
	switch category {
	case projectworkflow.CategoryNotStarted:
		return projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_NOT_STARTED
	case projectworkflow.CategoryActive:
		return projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_ACTIVE
	case projectworkflow.CategoryBlocked:
		return projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_BLOCKED
	case projectworkflow.CategoryDone:
		return projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_DONE
	case projectworkflow.CategoryCancelled:
		return projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_CANCELLED
	default:
		return projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_UNSPECIFIED
	}
}

func workflowFieldTypeMessage(typ projectworkflow.FieldType) projectv1.ProjectFieldType {
	switch typ {
	case projectworkflow.FieldText:
		return projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_TEXT
	case projectworkflow.FieldNumber:
		return projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_NUMBER
	case projectworkflow.FieldDate:
		return projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_DATE
	case projectworkflow.FieldEnum:
		return projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_ENUM
	case projectworkflow.FieldPerson:
		return projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_PERSON
	case projectworkflow.FieldLink:
		return projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_LINK
	case projectworkflow.FieldBoolean:
		return projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_BOOLEAN
	default:
		return projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_UNSPECIFIED
	}
}

func workflowConfigurationMessage(projectID string, c projectservice.WorkflowConfiguration) *projectv1.WorkflowConfiguration {
	spec := workflowSpec(c.Config)
	return &projectv1.WorkflowConfiguration{ProjectId: projectID, Revision: c.Version, Digest: c.Digest, Statuses: spec.GetStatuses(), TaskTypes: spec.GetTaskTypes(), Fields: spec.GetFields(), Transitions: spec.GetTransitions(), Columns: spec.GetColumns()}
}

func referenceFromMessage(ref *projectv1.TaskLinkReference) (projectlink.Reference, error) {
	if ref == nil {
		return projectlink.Reference{}, projectlink.ErrInvalidReference
	}
	switch target := ref.GetTarget().(type) {
	case *projectv1.TaskLinkReference_ChatConversation:
		if target.ChatConversation == nil {
			return projectlink.Reference{}, projectlink.ErrInvalidReference
		}
		return projectlink.Reference{Kind: projectlink.ChatConversation, ID: target.ChatConversation.GetConversationId()}, nil
	case *projectv1.TaskLinkReference_ChatPost:
		if target.ChatPost == nil {
			return projectlink.Reference{}, projectlink.ErrInvalidReference
		}
		return projectlink.Reference{Kind: projectlink.ChatPost, ID: target.ChatPost.GetPostId(), ConversationID: target.ChatPost.GetConversationId()}, nil
	case *projectv1.TaskLinkReference_DeployedDocument:
		if target.DeployedDocument == nil {
			return projectlink.Reference{}, projectlink.ErrInvalidReference
		}
		return projectlink.Reference{Kind: projectlink.DeployedDocument, ID: target.DeployedDocument.GetDocumentId(), Version: target.DeployedDocument.GetDeployedVersionId(), ScopeID: target.DeployedDocument.GetScopeId()}, nil
	case *projectv1.TaskLinkReference_WorkItem:
		if target.WorkItem == nil {
			return projectlink.Reference{}, projectlink.ErrInvalidReference
		}
		return projectlink.Reference{Kind: projectlink.WorkItem, ID: target.WorkItem.GetWorkItemId()}, nil
	case *projectv1.TaskLinkReference_Journey:
		if target.Journey == nil {
			return projectlink.Reference{}, projectlink.ErrInvalidReference
		}
		return projectlink.Reference{Kind: projectlink.Journey, ID: target.Journey.GetIntentId()}, nil
	default:
		return projectlink.Reference{}, projectlink.ErrInvalidReference
	}
}

func sourceReferenceFromMessage(ref *projectv1.ProjectSourceReference) (projectlink.Reference, error) {
	if ref == nil {
		return projectlink.Reference{}, projectlink.ErrInvalidReference
	}
	switch source := ref.GetSource().(type) {
	case *projectv1.ProjectSourceReference_ChatConversation:
		if source.ChatConversation == nil {
			return projectlink.Reference{}, projectlink.ErrInvalidReference
		}
		return projectlink.Reference{Kind: projectlink.ChatConversation, ID: source.ChatConversation.GetConversationId()}, nil
	case *projectv1.ProjectSourceReference_ChatPost:
		if source.ChatPost == nil {
			return projectlink.Reference{}, projectlink.ErrInvalidReference
		}
		return projectlink.Reference{Kind: projectlink.ChatPost, ID: source.ChatPost.GetPostId(), ConversationID: source.ChatPost.GetConversationId()}, nil
	case *projectv1.ProjectSourceReference_DeployedDocument:
		if source.DeployedDocument == nil {
			return projectlink.Reference{}, projectlink.ErrInvalidReference
		}
		return projectlink.Reference{Kind: projectlink.DeployedDocument, ID: source.DeployedDocument.GetDocumentId(), Version: source.DeployedDocument.GetDeployedVersionId(), ScopeID: source.DeployedDocument.GetScopeId()}, nil
	default:
		return projectlink.Reference{}, projectlink.ErrInvalidReference
	}
}

func referenceMessage(ref projectlink.Reference) *projectv1.TaskLinkReference {
	switch ref.Kind {
	case projectlink.ChatConversation:
		return &projectv1.TaskLinkReference{Target: &projectv1.TaskLinkReference_ChatConversation{ChatConversation: &projectv1.ChatConversationLink{ConversationId: ref.ID}}}
	case projectlink.ChatPost:
		return &projectv1.TaskLinkReference{Target: &projectv1.TaskLinkReference_ChatPost{ChatPost: &projectv1.ChatPostLink{ConversationId: ref.ConversationID, PostId: ref.ID}}}
	case projectlink.DeployedDocument:
		return &projectv1.TaskLinkReference{Target: &projectv1.TaskLinkReference_DeployedDocument{DeployedDocument: &projectv1.DeployedDocumentLink{DocumentId: ref.ID, DeployedVersionId: ref.Version, ScopeId: ref.ScopeID}}}
	case projectlink.WorkItem:
		return &projectv1.TaskLinkReference{Target: &projectv1.TaskLinkReference_WorkItem{WorkItem: &projectv1.WorkItemLink{WorkItemId: ref.ID}}}
	case projectlink.Journey:
		return &projectv1.TaskLinkReference{Target: &projectv1.TaskLinkReference_Journey{Journey: &projectv1.JourneyLink{IntentId: ref.ID}}}
	default:
		return nil
	}
}

func linkMessage(link projectservice.TaskLink) *projectv1.ResolvedTaskLink {
	result := &projectv1.ResolvedTaskLink{LinkId: link.ID, Reference: referenceMessage(link.Reference), State: projectv1.TaskLinkResolutionState_TASK_LINK_RESOLUTION_STATE_RESTRICTED}
	if link.Resolution.State == projectlink.Available {
		result.State = projectv1.TaskLinkResolutionState_TASK_LINK_RESOLUTION_STATE_AVAILABLE
		if p := link.Resolution.Preview; p != nil {
			result.Preview = &projectv1.TaskLinkPreview{Title: p.Title, Snippet: p.Snippet, SafeWorkItemStatus: p.Status, Freshness: p.Freshness}
		}
	}
	return result
}

func (s *server) GetProject(ctx context.Context, req *projectv1.GetProjectRequest) (*projectv1.GetProjectResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	record, err := s.service.GetProject(ctx, p, req.GetProjectId())
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.GetProjectResponse{Project: projectMessage(record)}, nil
}

func (s *server) ListProjects(ctx context.Context, req *projectv1.ListProjectsRequest) (*projectv1.ListProjectsResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	limit, err := listPageSize(req.GetPage())
	if err != nil {
		return nil, err
	}
	page, err := s.service.ListProjects(ctx, p, req.GetPage().GetCursor(), limit)
	if err != nil {
		return nil, mapError(err)
	}
	out := &projectv1.ListProjectsResponse{Projects: make([]*projectv1.Project, 0, len(page.Projects)), Page: &commonv1.PageResponse{}}
	for _, record := range page.Projects {
		out.Projects = append(out.Projects, projectMessage(record))
	}
	if page.Next != nil {
		out.Page.NextCursor = page.Next.AfterID
	}
	return out, nil
}

func (s *server) GetTask(ctx context.Context, req *projectv1.GetTaskRequest) (*projectv1.GetTaskResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	record, err := s.service.GetTask(ctx, p, req.GetProjectId(), req.GetTaskId())
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.GetTaskResponse{Task: taskMessage(record)}, nil
}

func (s *server) ListTasks(ctx context.Context, req *projectv1.ListTasksRequest) (*projectv1.ListTasksResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	limit, err := listPageSize(req.GetPage())
	if err != nil {
		return nil, err
	}
	filter, err := taskListFilter(req.GetFilter())
	if err != nil {
		return nil, err
	}
	page, err := s.service.ListTasks(ctx, p, req.GetProjectId(), req.GetPage().GetCursor(), limit, filter)
	if err != nil {
		return nil, mapError(err)
	}
	out := &projectv1.ListTasksResponse{Tasks: make([]*projectv1.ProjectTask, 0, len(page.Tasks)), Page: &commonv1.PageResponse{}}
	for _, record := range page.Tasks {
		out.Tasks = append(out.Tasks, taskMessage(record))
	}
	if page.Next != nil {
		out.Page.NextCursor = page.Next.AfterID
	}
	return out, nil
}

func (s *server) SearchTasks(ctx context.Context, req *projectv1.SearchTasksRequest) (*projectv1.SearchTasksResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || s.search == nil {
		return nil, s.unavailable()
	}
	limit, err := listPageSize(req.GetPage())
	if err != nil {
		return nil, err
	}
	filter := projectsearch.Filter{}
	if f := req.GetFilter(); f != nil {
		filter.StatusIDs = append([]string(nil), f.GetStatusIds()...)
		filter.AssigneeID = f.GetAssigneeId()
		filter.TypeIDs = append([]string(nil), f.GetTaskTypeIds()...)
		filter.DueDateFrom, filter.DueDateTo = f.GetDueDateFrom(), f.GetDueDateTo()
		filter.Fields = make(map[string][]string, len(f.GetFields()))
		for _, field := range f.GetFields() {
			if field == nil {
				return nil, envelope.New(envelope.CodeInvalidArgument, "project.search.invalid_argument", "task search request is invalid")
			}
			filter.Fields[field.GetFieldId()] = append(filter.Fields[field.GetFieldId()], field.GetValues()...)
		}
	}
	page, err := s.search.Search(ctx, p, projectsearch.Request{ProjectID: req.GetProjectId(), Text: req.GetText(), Filter: filter, Limit: limit, Cursor: req.GetPage().GetCursor()})
	if err != nil {
		return nil, mapError(err)
	}
	out := &projectv1.SearchTasksResponse{Tasks: make([]*projectv1.ProjectTask, 0, len(page.Tasks)), Page: &commonv1.PageResponse{NextCursor: page.Next}}
	for _, task := range page.Tasks {
		out.Tasks = append(out.Tasks, taskMessage(projectservice.TaskRecord{ID: task.ID, TenantID: task.TenantID, ProjectID: task.ProjectID, Title: task.Title, Description: task.Description, StatusID: task.StatusID, TypeID: task.TypeID, Priority: task.Priority, AssigneeID: task.AssigneeID, DueDate: task.DueDate, Revision: task.Revision, Archived: task.Archived, Fields: task.Fields}))
	}
	switch page.Freshness {
	case projectsearch.FreshnessCurrent:
		out.Freshness = projectv1.SearchFreshness_SEARCH_FRESHNESS_CURRENT
	case projectsearch.FreshnessDegraded:
		out.Freshness = projectv1.SearchFreshness_SEARCH_FRESHNESS_DEGRADED_EXACT_FILTER
	default:
		out.Freshness = projectv1.SearchFreshness_SEARCH_FRESHNESS_UNSPECIFIED
	}
	return out, nil
}

func (s *server) SaveWorkflowDraft(ctx context.Context, req *projectv1.SaveWorkflowDraftRequest) (*projectv1.SaveWorkflowDraftResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil || req.GetConfiguration() == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.workflow.invalid", "workflow configuration is invalid")
	}
	revision, err := s.service.SaveWorkflowDraft(ctx, p, projectservice.SaveWorkflowDraftRequest{ProjectID: req.GetProjectId(), ExpectedDraftRevision: req.GetExpectedDraftRevision(), IdempotencyKey: req.GetIdempotencyKey(), Config: workflowFromSpec(req.GetConfiguration())})
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.SaveWorkflowDraftResponse{DraftId: projectservice.CurrentWorkflowDraftID, DraftRevision: revision}, nil
}

func (s *server) GetWorkflowDraft(ctx context.Context, req *projectv1.GetWorkflowDraftRequest) (*projectv1.GetWorkflowDraftResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	cfg, revision, err := s.service.GetWorkflowDraft(ctx, p, req.GetProjectId(), req.GetDraftId())
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.GetWorkflowDraftResponse{DraftId: projectservice.CurrentWorkflowDraftID, DraftRevision: revision, Configuration: workflowSpec(cfg)}, nil
}

func (s *server) PreviewWorkflowDraft(ctx context.Context, req *projectv1.PreviewWorkflowDraftRequest) (*projectv1.PreviewWorkflowDraftResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	if req.GetExpectedDraftRevision() == 0 {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.draft.revision_required", "expected workflow draft revision is required")
	}
	mappings, err := workflowMappingsFromProto(req.GetStatusMappings(), req.GetFieldMappings())
	if err != nil {
		return nil, err
	}
	preview, err := s.service.PreviewWorkflowDraftWithMappings(ctx, p, projectservice.PreviewWorkflowDraftRequest{ProjectID: req.GetProjectId(), DraftID: req.GetDraftId(), ExpectedDraftRevision: req.GetExpectedDraftRevision(), Mappings: mappings})
	if err != nil {
		return nil, mapError(err)
	}
	resp := &projectv1.PreviewWorkflowDraftResponse{DraftId: projectservice.CurrentWorkflowDraftID, DraftRevision: preview.DraftRevision, ReviewedDigest: preview.Digest, MigrationPlanDigest: preview.PlanDigest, Valid: len(preview.Errors) == 0, AffectedTaskCount: uint64(preview.AffectedTaskCount), AffectedTaskIds: append([]string(nil), preview.AffectedTaskIDs...)}
	for _, issue := range preview.Errors {
		resp.Diagnostics = append(resp.Diagnostics, &projectv1.WorkflowDiagnostic{Code: issue.Code, FieldPath: issue.Path, SafeDetail: issue.Message})
	}
	return resp, nil
}

func (s *server) PublishWorkflowDraft(ctx context.Context, req *projectv1.PublishWorkflowDraftRequest) (*projectv1.PublishWorkflowDraftResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	mappings, err := workflowMappingsFromProto(req.GetStatusMappings(), req.GetFieldMappings())
	if err != nil {
		return nil, err
	}
	configuration, err := s.service.PublishWorkflowDraft(ctx, p, projectservice.PublishWorkflowDraftRequest{ProjectID: req.GetProjectId(), DraftID: req.GetDraftId(), ExpectedDraftRevision: req.GetExpectedDraftRevision(), ExpectedConfigVersion: req.GetExpectedProjectWorkflowRevision(), ReviewedDigest: req.GetReviewedDigest(), ReviewedPlanDigest: req.GetReviewedMigrationPlanDigest(), IdempotencyKey: req.GetIdempotencyKey(), Mappings: mappings})
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.PublishWorkflowDraftResponse{Configuration: workflowConfigurationMessage(req.GetProjectId(), configuration)}, nil
}

func (s *server) GetWorkflowConfiguration(ctx context.Context, req *projectv1.GetWorkflowConfigurationRequest) (*projectv1.GetWorkflowConfigurationResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	configuration, err := s.service.GetWorkflowConfiguration(ctx, p, req.GetProjectId())
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.GetWorkflowConfigurationResponse{Configuration: workflowConfigurationMessage(req.GetProjectId(), configuration)}, nil
}

func (s *server) ListBoardViews(ctx context.Context, req *projectv1.ListBoardViewsRequest) (*projectv1.ListBoardViewsResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	limit, err := listPageSize(req.GetPage())
	if err != nil {
		return nil, err
	}
	page, err := s.service.ListBoardViews(ctx, p, req.GetProjectId(), req.GetPage().GetCursor(), limit)
	if err != nil {
		return nil, mapError(err)
	}
	out := &projectv1.ListBoardViewsResponse{Views: make([]*projectv1.BoardView, 0, len(page.Views)), Page: &commonv1.PageResponse{}}
	for _, v := range page.Views {
		out.Views = append(out.Views, viewMessage(req.GetProjectId(), "", v))
	}
	if page.Next != nil {
		out.Page.NextCursor = page.Next.AfterID
	}
	return out, nil
}

func (s *server) AddTaskLink(ctx context.Context, req *projectv1.AddTaskLinkRequest) (*projectv1.AddTaskLinkResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	ref, err := referenceFromMessage(req.GetReference())
	if err != nil {
		return nil, mapError(err)
	}
	linkID, taskRevision, callErr := s.service.AddTaskLink(ctx, p, projectservice.AddTaskLinkRequest{ProjectID: req.GetProjectId(), TaskID: req.GetTaskId(), Reference: ref, ExpectedTaskRevision: req.GetExpectedTaskRevision(), IdempotencyKey: req.GetIdempotencyKey()})
	if callErr != nil {
		return nil, mapError(callErr)
	}
	return &projectv1.AddTaskLinkResponse{LinkId: linkID, TaskRevision: taskRevision}, nil
}

func (s *server) ListTaskLinks(ctx context.Context, req *projectv1.ListTaskLinksRequest) (*projectv1.ListTaskLinksResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	limit, err := listPageSize(req.GetPage())
	if err != nil {
		return nil, err
	}
	links, next, err := s.service.ListTaskLinks(ctx, p, req.GetProjectId(), req.GetTaskId(), req.GetPage().GetCursor(), limit)
	if err != nil {
		return nil, mapError(err)
	}
	out := &projectv1.ListTaskLinksResponse{Links: make([]*projectv1.ResolvedTaskLink, 0, len(links)), Page: &commonv1.PageResponse{}}
	for _, link := range links {
		out.Links = append(out.Links, linkMessage(link))
	}
	if next != nil {
		out.Page.NextCursor = next.AfterID
	}
	return out, nil
}

func (s *server) RemoveTaskLink(ctx context.Context, req *projectv1.RemoveTaskLinkRequest) (*projectv1.RemoveTaskLinkResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireService(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.invalid_argument", "project request is invalid")
	}
	revision, err := s.service.RemoveTaskLink(ctx, p, projectservice.RemoveTaskLinkRequest{ProjectID: req.GetProjectId(), TaskID: req.GetTaskId(), LinkID: req.GetLinkId(), ExpectedTaskRevision: req.GetExpectedTaskRevision(), IdempotencyKey: req.GetIdempotencyKey()})
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.RemoveTaskLinkResponse{TaskRevision: revision}, nil
}

func (s *server) AddTaskComment(ctx context.Context, req *projectv1.AddTaskCommentRequest) (*projectv1.AddTaskCommentResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireActivity(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.activity.invalid_argument", "task comment request is invalid")
	}
	comment, err := s.activity.AddComment(ctx, p, applicationactivity.AddCommentRequest{ProjectID: req.GetProjectId(), TaskID: req.GetTaskId(), Text: req.GetBodyText(), IdempotencyKey: req.GetIdempotencyKey()})
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.AddTaskCommentResponse{Comment: taskCommentMessage(comment)}, nil
}

func (s *server) EditTaskComment(ctx context.Context, req *projectv1.EditTaskCommentRequest) (*projectv1.EditTaskCommentResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireActivity(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.activity.invalid_argument", "task comment request is invalid")
	}
	comment, err := s.activity.EditComment(ctx, p, applicationactivity.ReviseCommentRequest{ProjectID: req.GetProjectId(), TaskID: req.GetTaskId(), CommentID: req.GetCommentId(), ExpectedRevision: req.GetExpectedRevision(), Text: req.GetBodyText(), IdempotencyKey: req.GetIdempotencyKey()})
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.EditTaskCommentResponse{Comment: taskCommentMessage(comment)}, nil
}

func (s *server) DeleteTaskComment(ctx context.Context, req *projectv1.DeleteTaskCommentRequest) (*projectv1.DeleteTaskCommentResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireActivity(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.activity.invalid_argument", "task comment request is invalid")
	}
	comment, err := s.activity.DeleteComment(ctx, p, applicationactivity.ReviseCommentRequest{ProjectID: req.GetProjectId(), TaskID: req.GetTaskId(), CommentID: req.GetCommentId(), ExpectedRevision: req.GetExpectedRevision(), IdempotencyKey: req.GetIdempotencyKey()})
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.DeleteTaskCommentResponse{Comment: taskCommentMessage(comment)}, nil
}

func (s *server) ListTaskComments(ctx context.Context, req *projectv1.ListTaskCommentsRequest) (*projectv1.ListTaskCommentsResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireActivity(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.activity.invalid_argument", "task comment request is invalid")
	}
	size, err := activityPageSize(req.GetPageSize(), req.GetPageCursor())
	if err != nil {
		return nil, err
	}
	page, err := s.activity.ListComments(ctx, p, applicationactivity.ListRequest{ProjectID: req.GetProjectId(), TaskID: req.GetTaskId(), Cursor: req.GetPageCursor(), PageSize: size})
	if err != nil {
		return nil, mapError(err)
	}
	out := &projectv1.ListTaskCommentsResponse{Comments: make([]*projectv1.ProjectTaskComment, 0, len(page.Comments)), NextPageCursor: page.NextCursor}
	for _, comment := range page.Comments {
		out.Comments = append(out.Comments, taskCommentMessage(comment))
	}
	return out, nil
}

func (s *server) ListTaskActivity(ctx context.Context, req *projectv1.ListTaskActivityRequest) (*projectv1.ListTaskActivityResponse, error) {
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if err = requireActivity(s); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "project.activity.invalid_argument", "task activity request is invalid")
	}
	size, err := activityPageSize(req.GetPageSize(), req.GetPageCursor())
	if err != nil {
		return nil, err
	}
	page, err := s.activity.ListActivity(ctx, p, applicationactivity.ListRequest{ProjectID: req.GetProjectId(), TaskID: req.GetTaskId(), Cursor: req.GetPageCursor(), PageSize: size})
	if err != nil {
		return nil, mapError(err)
	}
	out := &projectv1.ListTaskActivityResponse{Entries: make([]*projectv1.ProjectTaskActivity, 0, len(page.Entries)), NextPageCursor: page.NextCursor}
	for _, entry := range page.Entries {
		out.Entries = append(out.Entries, taskActivityMessage(entry))
	}
	return out, nil
}

func activityPageSize(size int32, cursor string) (int, error) {
	if size == 0 {
		size = int32(domainactivity.DefaultPageSize)
	}
	if size < 1 || size > domainactivity.MaxPageSize || (cursor != "" && len(cursor) != 64) {
		return 0, envelope.New(envelope.CodeInvalidArgument, "project.activity.page_invalid", "task activity page request is invalid")
	}
	return int(size), nil
}

func taskCommentMessage(comment domainactivity.Comment) *projectv1.ProjectTaskComment {
	current := comment.Current()
	out := &projectv1.ProjectTaskComment{CommentId: comment.ID, CurrentRevision: current.Number, Tombstone: current.Tombstone, ActorId: current.ActorID}
	if !comment.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(comment.CreatedAt)
	}
	if !current.At.IsZero() {
		out.UpdatedAt = timestamppb.New(current.At)
	}
	if !current.Tombstone {
		out.SafeHtml = current.Text.HTML
		out.SourceText = current.Text.Source
	}
	return out
}

func taskActivityMessage(activity domainactivity.Activity) *projectv1.ProjectTaskActivity {
	out := &projectv1.ProjectTaskActivity{Sequence: activity.Sequence, CommentId: activity.CommentID, Kind: activity.Kind, Revision: activity.Revision, ActorId: activity.ActorID}
	if !activity.At.IsZero() {
		out.OccurredAt = timestamppb.New(activity.At)
	}
	return out
}

var _ projectv1.ProjectServiceServer = (*server)(nil)
var _ Service = projectservice.Service{}
