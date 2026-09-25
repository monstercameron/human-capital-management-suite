// Package projectservice coordinates project domain operations at the
// application boundary. Persistence, authorization, and workflow policy are
// explicit ports; this package contains no SQL or transport types.
package projectservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrInvalidPrincipal = errors.New("projectservice: trusted principal is required")
	ErrInvalidRequest   = errors.New("projectservice: invalid request")
	ErrUnavailable      = errors.New("projectservice: required port unavailable")
	ErrViewOwner        = errors.New("projectservice: personal view belongs to another user")
)

// ProjectRecord and TaskRecord are small application projections. Data adapters
// convert these to their persistence records; they intentionally omit storage
// timestamps and database-specific types.
type ProjectRecord struct {
	ID, TenantID, OwnerID, Name, Timezone string
	State                                 project.Lifecycle
	Revision                              uint64
	WorkflowRevision                      uint64
}

type TaskRecord struct {
	ID, TenantID, ProjectID              string
	Title, Description, StatusID, TypeID string
	Priority, AssigneeID, DueDate        string
	Revision                             uint64
	WorkflowRevision                     uint64
	Archived                             bool
	EnumFields                           map[string]string
	Fields                               map[string]project.TaskFieldEdit
	// CreatedBy is the reporter. StartDate is a civil date; Labels and
	// StoryPoints are planning fields. The times are server-maintained.
	CreatedBy            string
	StartDate            string
	StoryPoints          uint32
	Labels               []string
	CreatedAt, UpdatedAt time.Time
}

func (r ProjectRecord) domain() project.Project {
	state := r.State
	if state == "" {
		state = project.LifecycleActive
	}
	return project.Project{ID: project.ProjectID(r.ID), TenantID: r.TenantID, OwnerID: r.OwnerID, Name: r.Name, Timezone: r.Timezone, State: state, Revision: r.Revision}
}

func (r TaskRecord) domain() project.Task {
	priority := project.Priority(r.Priority)
	if priority == "" {
		priority = project.PriorityNormal
	}
	typeID := project.TypeID(r.TypeID)
	if typeID == "" {
		typeID = project.TypeID("task_default")
	}
	fields := make(map[string]project.TaskFieldEdit, len(r.Fields))
	for id, field := range r.Fields {
		fields[id] = field
	}
	return project.Task{ID: project.TaskID(r.ID), ProjectID: project.ProjectID(r.ProjectID), TenantID: r.TenantID, Title: r.Title, Description: r.Description, Status: r.StatusID, TypeID: typeID, Priority: priority, AssigneeID: r.AssigneeID, DueDate: r.DueDate, Revision: r.Revision, Archived: r.Archived, Fields: fields}
}

func projectRecord(p project.Project) ProjectRecord {
	return ProjectRecord{ID: string(p.ID), TenantID: p.TenantID, OwnerID: p.OwnerID, Name: p.Name, Timezone: p.Timezone, State: p.State, Revision: p.Revision, WorkflowRevision: 1}
}

func taskRecord(t project.Task) TaskRecord {
	fields := make(map[string]project.TaskFieldEdit, len(t.Fields))
	for id, field := range t.Fields {
		fields[id] = field
	}
	return TaskRecord{ID: string(t.ID), TenantID: t.TenantID, ProjectID: string(t.ProjectID), Title: t.Title, Description: t.Description, StatusID: t.Status, TypeID: string(t.TypeID), Priority: string(t.Priority), AssigneeID: t.AssigneeID, DueDate: t.DueDate, Revision: t.Revision, Archived: t.Archived, Fields: fields}
}

// Authorizer checks current project membership and grants for the supplied
// authenticated identity. Implementations must read trusted policy state; the
// service derives tenant and actor only from principal.
type Authorizer interface {
	Authorize(context.Context, *trust.Principal, string, projectaccess.Capability) error
	AuthorizeCreate(context.Context, *trust.Principal) error
	AuthorizeListProjects(context.Context, *trust.Principal) error
}

// CommandRepository owns durable idempotency, revisions and activity/outbox
// transactions for mutations. CreateProject also installs the starter workflow
// and default view in its project creation transaction.
type CommandRepository interface {
	CreateProject(context.Context, ProjectRecord, string, string) (ProjectRecord, error)
	CreateTask(context.Context, TaskRecord, uint64, string, string) (TaskRecord, error)
	MoveTask(context.Context, string, string, string, string, uint64, uint64, []project.TaskFieldEdit, string, string) (TaskRecord, error)
}

// SourceLinkedTaskRepository commits a task and its validated typed source
// reference in one tenant transaction. It is required whenever CreateTask
// receives SourceReference; the service never falls back to a partial write.
type SourceLinkedTaskRepository interface {
	CreateTaskWithLink(context.Context, TaskRecord, uint64, string, string, projectlink.Reference) (TaskRecord, error)
}

// ReadRepository is used only after membership authorization. BoardPageSource
// must recheck current access as part of its query before filters, counts, or
// cursor advancement are computed.
type ReadRepository interface {
	GetProject(context.Context, string, string) (ProjectRecord, error)
	GetTask(context.Context, string, string, string) (TaskRecord, error)
	GetView(context.Context, string, string, string, string) (projectboard.BoardView, error)
}

type ViewRepository interface {
	SaveView(context.Context, string, string, string, string, projectboard.BoardView, uint64, string) (projectboard.BoardView, error)
}

// MembershipRepository owns membership revisions and idempotent transitions.
type MembershipRepository interface {
	GetMemberships(context.Context, string, string) (MembershipSnapshot, error)
	ValidateInviteeClassification(context.Context, string, string, uint8) error
	InviteMember(context.Context, string, string, string, string, projectaccess.Role, uint8, uint64, string) error
	AcceptMemberInvitation(context.Context, string, string, string, uint64, string) error
	ChangeMemberRole(context.Context, string, string, string, string, projectaccess.Role, uint64, string) error
	RevokeMember(context.Context, string, string, string, string, uint64, string) error
	TransferMemberOwnership(context.Context, string, string, string, string, uint64, string) error
}

// InviteeEligibility resolves a target against trusted same-tenant identity
// state and supplies the target's classification. User supplied claims are
// never accepted as eligibility evidence.
type InviteeEligibility interface {
	CheckInvitee(context.Context, *trust.Principal, string) (uint8, error)
}

type MembershipRecord struct {
	UserID   string
	Role     projectaccess.Role
	State    projectaccess.MembershipState
	Revision uint64
}
type MembershipSnapshot struct {
	Revision uint64
	Members  []MembershipRecord
}

type BoardPageSource interface {
	ListAuthorizedTasks(context.Context, string, string, string, projectboard.AuthorizedTaskQuery) (AuthorizedBoardTasks, *projectboard.Cursor, error)
}

type AuthorizedBoardTasks struct {
	Tasks            []projectboard.Task
	TaskRevisions    map[string]uint64
	WorkflowRevision uint64
	Freshness        string
}

type Service struct {
	Auth         Authorizer
	Commands     CommandRepository
	Reads        ReadRepository
	Views        ViewRepository
	Members      MembershipRepository
	Invitees     InviteeEligibility
	Pages        BoardPageSource
	Workflow     project.TransitionPolicy
	Workflows    WorkflowPort
	Links        LinkRepository
	LinkResolver LinkResolver
}

const (
	FreshnessCurrent      = "CURRENT"
	FreshnessBoundedStale = "BOUNDED_STALE"
)

type CreateProjectRequest struct{ ID, Name, Timezone, IdempotencyKey string }
type CreateTaskRequest struct {
	ProjectID, ID, Title, InitialStatusID, TypeID, Priority, IdempotencyKey string
	Description, AssigneeID, DueDate                                        string
	ExpectedWorkflowRevision                                                uint64
	CustomFields                                                            []project.TaskFieldEdit
	SourceReference                                                         *projectlink.Reference
}
type MoveTaskRequest struct {
	ProjectID, TaskID, TargetStatusID            string
	ExpectedTaskRevision, ExpectedConfigRevision uint64
	IdempotencyKey                               string
	FieldEdits                                   []project.TaskFieldEdit
}

func (s Service) CreateProject(ctx context.Context, principal *trust.Principal, req CreateProjectRequest) (ProjectRecord, error) {
	if err := validPrincipal(principal); err != nil {
		return ProjectRecord{}, err
	}
	if s.Auth == nil || s.Commands == nil {
		return ProjectRecord{}, ErrUnavailable
	}
	if err := s.Auth.AuthorizeCreate(ctx, principal); err != nil {
		return ProjectRecord{}, err
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Timezone) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return ProjectRecord{}, ErrInvalidRequest
	}
	if req.ID == "" {
		req.ID = stableID(tenant(principal), principal.Subject(), "project.create", req.IdempotencyKey)
	}
	p, err := project.NewProject(project.ProjectID(req.ID), tenant(principal), principal.Subject(), req.Name, req.Timezone)
	if err != nil {
		return ProjectRecord{}, err
	}
	result, err := s.Commands.CreateProject(ctx, projectRecord(p), principal.Subject(), req.IdempotencyKey)
	return result, err
}

func (s Service) CreateTask(ctx context.Context, principal *trust.Principal, req CreateTaskRequest) (TaskRecord, error) {
	if err := validPrincipal(principal); err != nil {
		return TaskRecord{}, err
	}
	if s.Auth == nil || s.Commands == nil || s.Reads == nil {
		return TaskRecord{}, ErrUnavailable
	}
	if req.ProjectID == "" || req.InitialStatusID == "" || req.IdempotencyKey == "" || req.ExpectedWorkflowRevision == 0 {
		return TaskRecord{}, ErrInvalidRequest
	}
	if req.ID == "" {
		req.ID = stableID(tenant(principal), principal.Subject(), "task.create:"+req.ProjectID, req.IdempotencyKey)
	}
	if err := s.Auth.Authorize(ctx, principal, req.ProjectID, projectaccess.CreateTask); err != nil {
		return TaskRecord{}, err
	}
	if req.SourceReference != nil {
		if req.SourceReference.Validate() != nil {
			return TaskRecord{}, ErrInvalidRequest
		}
		if s.LinkResolver == nil || s.Links == nil {
			return TaskRecord{}, ErrUnavailable
		}
		preview, resolveErr := s.LinkResolver.Resolve(ctx, principal.Subject(), *req.SourceReference)
		if resolveErr != nil {
			return TaskRecord{}, resolveErr
		}
		if preview.State != projectlink.Available {
			return TaskRecord{}, ErrUnsupportedLink
		}
	}
	if s.Workflows == nil {
		return TaskRecord{}, ErrUnavailable
	}
	published, err := s.Workflows.GetPublished(ctx, tenant(principal), req.ProjectID)
	if err != nil {
		return TaskRecord{}, err
	}
	// A later publication must not block an exact create replay. The durable
	// command checks its receipt before rejecting a new stale-version create.
	staleWorkflow := published.Version != req.ExpectedWorkflowRevision
	typeID := req.TypeID
	if typeID == "" {
		typeID = "task_default"
	}
	fieldValues := make(map[string]json.RawMessage, len(req.CustomFields))
	for _, edit := range req.CustomFields {
		fieldValues[edit.FieldID] = json.RawMessage(edit.CanonicalValue)
	}
	if !staleWorkflow {
		if err := projectworkflow.ValidateTaskCreation(published.Config, req.ExpectedWorkflowRevision, published.Version, typeID, req.InitialStatusID, fieldValues); err != nil {
			return TaskRecord{}, err
		}
	}
	p, err := s.Reads.GetProject(ctx, tenant(principal), req.ProjectID)
	if err != nil {
		return TaskRecord{}, err
	}
	if p.TenantID != tenant(principal) {
		return TaskRecord{}, projectaccess.ErrTenantMismatch
	}
	domainTypeID := project.TypeID(typeID)
	priority := project.Priority(req.Priority)
	if priority == "" {
		priority = project.PriorityNormal
	}
	task, err := project.NewTaskWithDetails(project.TaskID(req.ID), p.domain(), req.Title, req.InitialStatusID, domainTypeID, priority)
	if err != nil {
		return TaskRecord{}, err
	}
	task.Description, task.AssigneeID, task.DueDate = req.Description, req.AssigneeID, req.DueDate
	task.Fields = make(map[string]project.TaskFieldEdit, len(req.CustomFields))
	for _, edit := range req.CustomFields {
		task.Fields[edit.FieldID] = edit
	}
	var result TaskRecord
	if req.SourceReference != nil {
		linked, ok := s.Commands.(SourceLinkedTaskRepository)
		if !ok {
			return TaskRecord{}, ErrUnavailable
		}
		result, err = linked.CreateTaskWithLink(ctx, taskRecord(task), req.ExpectedWorkflowRevision, principal.Subject(), req.IdempotencyKey, *req.SourceReference)
	} else {
		result, err = s.Commands.CreateTask(ctx, taskRecord(task), req.ExpectedWorkflowRevision, principal.Subject(), req.IdempotencyKey)
	}
	if err != nil {
		return TaskRecord{}, err
	}
	result.WorkflowRevision = req.ExpectedWorkflowRevision
	return result, err
}

func (s Service) MoveTask(ctx context.Context, principal *trust.Principal, req MoveTaskRequest) (TaskRecord, error) {
	if err := validPrincipal(principal); err != nil {
		return TaskRecord{}, err
	}
	if s.Auth == nil || s.Commands == nil || s.Reads == nil || s.Workflows == nil {
		return TaskRecord{}, ErrUnavailable
	}
	if req.ProjectID == "" || req.TaskID == "" || req.TargetStatusID == "" || req.ExpectedTaskRevision == 0 || req.ExpectedConfigRevision == 0 || req.IdempotencyKey == "" {
		return TaskRecord{}, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, principal, req.ProjectID, projectaccess.MoveTask); err != nil {
		return TaskRecord{}, err
	}
	p, err := s.Reads.GetProject(ctx, tenant(principal), req.ProjectID)
	if err != nil {
		return TaskRecord{}, err
	}
	t, err := s.Reads.GetTask(ctx, tenant(principal), req.ProjectID, req.TaskID)
	if err != nil {
		return TaskRecord{}, err
	}
	if p.TenantID != tenant(principal) || t.TenantID != tenant(principal) || t.ProjectID != req.ProjectID {
		return TaskRecord{}, projectaccess.ErrTenantMismatch
	}
	workflow, err := s.Workflows.GetPublished(ctx, tenant(principal), req.ProjectID)
	if err != nil {
		return TaskRecord{}, err
	}
	if workflow.Version != req.ExpectedConfigRevision {
		// The store checks an exact idempotency receipt before comparing the
		// current configuration. A successful move must remain replayable after
		// a later workflow publication; a new stale move is still rejected there.
		return s.Commands.MoveTask(ctx, tenant(principal), req.ProjectID, req.TaskID, req.TargetStatusID, req.ExpectedTaskRevision, req.ExpectedConfigRevision, req.FieldEdits, principal.Subject(), req.IdempotencyKey)
	}
	published, err := projectworkflow.Publish(workflow.Config, workflow.Version)
	if err != nil {
		return TaskRecord{}, err
	}
	policy := taskTransitionPolicy{published: published, typeID: project.TypeID(t.TypeID), fields: taskFieldValues(t.Fields), delegate: s.Workflow}
	if _, err = t.domain().TransitionTask(p.domain(), policy, req.TargetStatusID, req.ExpectedTaskRevision, req.ExpectedConfigRevision, req.FieldEdits); err != nil {
		if !errors.Is(err, project.ErrRevisionConflict) {
			return TaskRecord{}, err
		}
		// A retry sees the task's newer revision. The transactional command
		// decides whether this is an exact replay or a conflicting new write.
	}
	result, err := s.Commands.MoveTask(ctx, tenant(principal), req.ProjectID, req.TaskID, req.TargetStatusID, req.ExpectedTaskRevision, req.ExpectedConfigRevision, req.FieldEdits, principal.Subject(), req.IdempotencyKey)
	result.WorkflowRevision = workflow.Version
	return result, err
}

type taskTransitionPolicy struct {
	published projectworkflow.PublishedVersion
	typeID    project.TypeID
	fields    map[string]json.RawMessage
	delegate  project.TransitionPolicy
}

func (p taskTransitionPolicy) ValidateTaskTransition(projectID project.ProjectID, from, to string, revision uint64, edits []project.TaskFieldEdit) error {
	if p.delegate != nil {
		return p.delegate.ValidateTaskTransition(projectID, from, to, revision, edits)
	}
	editValues := make(map[string]json.RawMessage, len(edits))
	for _, edit := range edits {
		editValues[edit.FieldID] = json.RawMessage(edit.CanonicalValue)
	}
	if errs := projectworkflow.ValidateTransition(p.published, projectworkflow.TransitionInput{ExpectedConfigVersion: revision, TaskTypeID: string(p.typeID), FromStatusID: from, ToStatusID: to, CurrentFields: p.fields, FieldEdits: editValues}); len(errs) > 0 {
		return errs
	}
	return nil
}

func taskFieldValues(fields map[string]project.TaskFieldEdit) map[string]json.RawMessage {
	values := make(map[string]json.RawMessage, len(fields))
	for id, field := range fields {
		values[id] = json.RawMessage(field.CanonicalValue)
	}
	return values
}

type SaveViewRequest struct {
	ProjectID, UserID string
	View              projectboard.BoardView
	ExpectedVersion   uint64
	IdempotencyKey    string
}

func (s Service) SaveView(ctx context.Context, principal *trust.Principal, req SaveViewRequest) (projectboard.BoardView, error) {
	if err := validPrincipal(principal); err != nil {
		return projectboard.BoardView{}, err
	}
	if s.Auth == nil || s.Views == nil {
		return projectboard.BoardView{}, ErrUnavailable
	}
	if req.ProjectID == "" || req.View.ID == "" {
		return projectboard.BoardView{}, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, principal, req.ProjectID, projectaccess.ReadProject); err != nil {
		return projectboard.BoardView{}, err
	}
	if req.View.Audience == projectboard.AudienceProject {
		if err := s.Auth.Authorize(ctx, principal, req.ProjectID, projectaccess.ManageViews); err != nil {
			return projectboard.BoardView{}, err
		}
		req.UserID = ""
	} else if req.View.Audience == projectboard.AudiencePersonal {
		req.UserID = principal.Subject()
	} else {
		return projectboard.BoardView{}, projectboard.ErrInvalidView
	}
	if err := projectboard.Validate(req.View); err != nil {
		return projectboard.BoardView{}, err
	}
	if req.IdempotencyKey == "" {
		return projectboard.BoardView{}, ErrInvalidRequest
	}
	return s.Views.SaveView(ctx, tenant(principal), req.ProjectID, req.UserID, principal.Subject(), req.View, req.ExpectedVersion, req.IdempotencyKey)
}

func (s Service) ReadView(ctx context.Context, principal *trust.Principal, projectID, viewID string) (projectboard.BoardView, error) {
	if err := validPrincipal(principal); err != nil {
		return projectboard.BoardView{}, err
	}
	if s.Auth == nil || s.Reads == nil {
		return projectboard.BoardView{}, ErrUnavailable
	}
	if projectID == "" || viewID == "" {
		return projectboard.BoardView{}, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, principal, projectID, projectaccess.ReadProject); err != nil {
		return projectboard.BoardView{}, err
	}
	view, err := s.Reads.GetView(ctx, tenant(principal), projectID, principal.Subject(), viewID)
	if err != nil {
		return projectboard.BoardView{}, err
	}
	if view.Audience == projectboard.AudienceProject {
		if err := s.Auth.Authorize(ctx, principal, projectID, projectaccess.ReadProject); err != nil {
			return projectboard.BoardView{}, err
		}
	} else if view.Audience != projectboard.AudiencePersonal {
		return projectboard.BoardView{}, projectboard.ErrInvalidView
	}
	return view, nil
}

type BoardPageResult struct {
	projectboard.Page
	ProjectRevision  uint64
	WorkflowRevision uint64
	TaskRevisions    map[string]uint64
	Freshness        string
}

func (s Service) BoardPage(ctx context.Context, principal *trust.Principal, projectID, viewID string, limit int, cursor *projectboard.Cursor) (BoardPageResult, error) {
	if err := validPrincipal(principal); err != nil {
		return BoardPageResult{}, err
	}
	if s.Auth == nil || s.Reads == nil || s.Pages == nil || s.Workflows == nil {
		return BoardPageResult{}, ErrUnavailable
	}
	if projectID == "" || viewID == "" {
		return BoardPageResult{}, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, principal, projectID, projectaccess.ReadTask); err != nil {
		return BoardPageResult{}, err
	}
	view, err := s.Reads.GetView(ctx, tenant(principal), projectID, principal.Subject(), viewID)
	if err != nil {
		return BoardPageResult{}, err
	}
	if view.Audience == projectboard.AudiencePersonal && view.ID == "" {
		return BoardPageResult{}, projectboard.ErrInvalidView
	}
	if view.Audience != projectboard.AudienceProject && view.Audience != projectboard.AudiencePersonal {
		return BoardPageResult{}, projectboard.ErrInvalidView
	}
	if view.Audience == projectboard.AudienceProject {
		if err := s.Auth.Authorize(ctx, principal, projectID, projectaccess.ReadProject); err != nil {
			return BoardPageResult{}, err
		}
	}
	projectRecord, err := s.Reads.GetProject(ctx, tenant(principal), projectID)
	if err != nil {
		return BoardPageResult{}, err
	}
	workflow, err := s.Workflows.GetPublished(ctx, tenant(principal), projectID)
	if err != nil {
		return BoardPageResult{}, err
	}
	source := &authorizedSource{service: s, principal: principal, projectID: projectID}
	page, err := projectboard.BuildPage(ctx, source, view, limit, cursor)
	if err != nil {
		return BoardPageResult{}, err
	}
	if projectRecord.Revision == 0 || workflow.Version == 0 || source.details.WorkflowRevision != workflow.Version ||
		(source.details.Freshness != FreshnessCurrent && source.details.Freshness != FreshnessBoundedStale) {
		return BoardPageResult{}, ErrUnavailable
	}
	for _, column := range page.Columns {
		for _, lane := range column.Lanes {
			for _, card := range lane.Cards {
				if source.details.TaskRevisions[card.Task.ID] == 0 {
					return BoardPageResult{}, ErrUnavailable
				}
			}
		}
	}
	if err := s.Auth.Authorize(ctx, principal, projectID, projectaccess.ReadTask); err != nil {
		return BoardPageResult{}, err
	}
	return BoardPageResult{Page: page, ProjectRevision: projectRecord.Revision, WorkflowRevision: workflow.Version, TaskRevisions: source.details.TaskRevisions, Freshness: source.details.Freshness}, nil
}

type authorizedSource struct {
	service   Service
	principal *trust.Principal
	projectID string
	details   AuthorizedBoardTasks
}

func (a *authorizedSource) ListAuthorizedTasks(ctx context.Context, q projectboard.AuthorizedTaskQuery) ([]projectboard.Task, *projectboard.Cursor, error) {
	if err := a.service.Auth.Authorize(ctx, a.principal, a.projectID, projectaccess.ReadTask); err != nil {
		return nil, nil, err
	}
	page, cursor, err := a.service.Pages.ListAuthorizedTasks(ctx, tenant(a.principal), a.projectID, a.principal.Subject(), q)
	a.details = page
	return page.Tasks, cursor, err
}

func validPrincipal(p *trust.Principal) error {
	if p == nil || p.Subject() == "" || tenant(p) == "" {
		return ErrInvalidPrincipal
	}
	return nil
}
func tenant(p *trust.Principal) string { return string(p.Tenant()) }
func stableID(tenantID, actorID, operation, key string) string {
	name := strings.Join([]string{tenantID, actorID, operation, key}, "\x00")
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(name)).String()
}
