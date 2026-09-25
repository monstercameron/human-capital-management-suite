package projectservice

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/projectconfigstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrUnsupportedLink = errors.New("projectservice: unsupported task link")
	ErrStaleView       = errors.New("projectservice: saved view version conflict")
)

const MaxListPageSize = 100

type IDCursor struct{ AfterID string }
type ProjectListPage struct {
	Projects []ProjectRecord
	Next     *IDCursor
}
type TaskListPage struct {
	Tasks []TaskRecord
	Next  *IDCursor
}
type BoardViewList struct {
	Views []projectboard.BoardView
	Next  *IDCursor
}
type TaskListFilter struct {
	StatusIDs  []string
	AssigneeID string
	Priorities []string
	TypeIDs    []string
	EnumFields map[string][]string
}

// ScopedListRepository must filter by current membership before applying the
// stable ID cursor, so neither results nor page continuation reveal hidden IDs.
type ScopedListRepository interface {
	ListAuthorizedProjects(context.Context, string, string, string, int) ([]ProjectRecord, error)
	ListTaskRecordsAuthorized(context.Context, string, string, string, string, TaskListFilter, int) ([]TaskRecord, error)
	ListViews(context.Context, string, string, string, string, int) ([]projectboard.BoardView, error)
}

func (s Service) GetProject(ctx context.Context, p *trust.Principal, id string) (ProjectRecord, error) {
	if err := validPrincipal(p); err != nil {
		return ProjectRecord{}, err
	}
	if s.Auth == nil || s.Reads == nil || s.Workflows == nil {
		return ProjectRecord{}, ErrUnavailable
	}
	if strings.TrimSpace(id) == "" {
		return ProjectRecord{}, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, p, id, projectaccess.ReadProject); err != nil {
		return ProjectRecord{}, err
	}
	r, err := s.Reads.GetProject(ctx, tenant(p), id)
	if err == nil && r.TenantID != tenant(p) {
		return ProjectRecord{}, projectaccess.ErrTenantMismatch
	}
	if err == nil {
		cfg, cfgErr := s.Workflows.GetPublished(ctx, tenant(p), id)
		if cfgErr != nil {
			return ProjectRecord{}, cfgErr
		}
		r.WorkflowRevision = cfg.Version
	}
	return r, err
}

func (s Service) GetTask(ctx context.Context, p *trust.Principal, projectID, taskID string) (TaskRecord, error) {
	if err := validPrincipal(p); err != nil {
		return TaskRecord{}, err
	}
	if s.Auth == nil || s.Reads == nil || s.Workflows == nil {
		return TaskRecord{}, ErrUnavailable
	}
	if projectID == "" || taskID == "" {
		return TaskRecord{}, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, p, projectID, projectaccess.ReadTask); err != nil {
		return TaskRecord{}, err
	}
	r, err := s.Reads.GetTask(ctx, tenant(p), projectID, taskID)
	if err == nil && (r.TenantID != tenant(p) || r.ProjectID != projectID) {
		return TaskRecord{}, projectaccess.ErrTenantMismatch
	}
	if err == nil {
		cfg, cfgErr := s.Workflows.GetPublished(ctx, tenant(p), projectID)
		if cfgErr != nil {
			return TaskRecord{}, cfgErr
		}
		r.WorkflowRevision = cfg.Version
		if authErr := s.Auth.Authorize(ctx, p, projectID, projectaccess.ReadTask); authErr != nil {
			return TaskRecord{}, authErr
		}
	}
	return r, err
}

func (s Service) ListProjects(ctx context.Context, p *trust.Principal, after string, limit int) (ProjectListPage, error) {
	if err := validPrincipal(p); err != nil {
		return ProjectListPage{}, err
	}
	if s.Auth == nil || s.Reads == nil || s.Workflows == nil {
		return ProjectListPage{}, ErrUnavailable
	}
	if limit < 1 || limit > MaxListPageSize {
		return ProjectListPage{}, projectboard.ErrInvalidPage
	}
	// A tenant-level listing grant is checked before asking for membership-filtered rows.
	if err := s.Auth.AuthorizeListProjects(ctx, p); err != nil {
		return ProjectListPage{}, err
	}
	repo, ok := s.Reads.(ScopedListRepository)
	if !ok {
		return ProjectListPage{}, ErrUnavailable
	}
	rows, err := repo.ListAuthorizedProjects(ctx, tenant(p), p.Subject(), after, limit+1)
	if err != nil {
		return ProjectListPage{}, err
	}
	page := ProjectListPage{Projects: rows}
	if len(rows) > limit {
		page.Projects = rows[:limit]
		page.Next = &IDCursor{AfterID: rows[limit-1].ID}
	}
	for i := range page.Projects {
		cfg, e := s.Workflows.GetPublished(ctx, tenant(p), page.Projects[i].ID)
		if e != nil {
			return ProjectListPage{}, e
		}
		page.Projects[i].WorkflowRevision = cfg.Version
	}
	if err := s.Auth.AuthorizeListProjects(ctx, p); err != nil {
		return ProjectListPage{}, err
	}
	current, err := repo.ListAuthorizedProjects(ctx, tenant(p), p.Subject(), after, limit+1)
	if err != nil {
		return ProjectListPage{}, err
	}
	if len(current) != len(rows) {
		return ProjectListPage{}, ErrInvalidCursor
	}
	for i := range rows {
		if current[i].ID != rows[i].ID {
			return ProjectListPage{}, ErrInvalidCursor
		}
	}
	return page, nil
}

func (s Service) ListTasks(ctx context.Context, p *trust.Principal, projectID, after string, limit int, filters ...TaskListFilter) (TaskListPage, error) {
	var filter TaskListFilter
	if len(filters) > 0 {
		filter = filters[0]
	}
	return s.ListTasksFiltered(ctx, p, projectID, filter, after, limit)
}

func (s Service) ListTasksFiltered(ctx context.Context, p *trust.Principal, projectID string, filter TaskListFilter, after string, limit int) (TaskListPage, error) {
	if err := validPrincipal(p); err != nil {
		return TaskListPage{}, err
	}
	if s.Auth == nil || s.Reads == nil || s.Workflows == nil {
		return TaskListPage{}, ErrUnavailable
	}
	if projectID == "" || limit < 1 || limit > MaxListPageSize {
		return TaskListPage{}, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, p, projectID, projectaccess.ReadTask); err != nil {
		return TaskListPage{}, err
	}
	repo, ok := s.Reads.(ScopedListRepository)
	if !ok {
		return TaskListPage{}, ErrUnavailable
	}
	rows, err := repo.ListTaskRecordsAuthorized(ctx, tenant(p), projectID, p.Subject(), after, filter, limit+1)
	if err != nil {
		return TaskListPage{}, err
	}
	page := TaskListPage{Tasks: rows}
	if len(rows) > limit {
		page.Tasks = rows[:limit]
		page.Next = &IDCursor{AfterID: rows[limit-1].ID}
	}
	workflow, e := s.Workflows.GetPublished(ctx, tenant(p), projectID)
	if e != nil {
		return TaskListPage{}, e
	}
	for i := range page.Tasks {
		page.Tasks[i].WorkflowRevision = workflow.Version
	}
	if err := s.Auth.Authorize(ctx, p, projectID, projectaccess.ReadTask); err != nil {
		return TaskListPage{}, err
	}
	return page, nil
}

type WorkflowPreview struct {
	Config            projectworkflow.Config
	DraftRevision     uint64
	Errors            projectworkflow.ValidationErrors
	Digest            string
	PlanDigest        string
	AffectedTaskCount int
	AffectedTaskIDs   []string
	Safe              bool
}
type WorkflowReviewEvidence struct {
	Required                                                   bool
	ReviewerID, DecisionID, ReviewedDigest, ReviewedPlanDigest string
}
type WorkflowConfiguration struct {
	Version uint64
	Digest  string
	Config  projectworkflow.Config
}
type WorkflowPort interface {
	GetDraft(context.Context, string, string) (projectworkflow.Config, uint64, error)
	SaveDraft(context.Context, string, string, string, projectworkflow.Config, uint64, string) (uint64, error)
	PreviewDraft(context.Context, string, string, projectworkflow.Config, uint64, projectworkflow.MigrationMappings) (WorkflowPreview, error)
	PublishDraft(context.Context, string, string, string, uint64, uint64, string, string, string, projectworkflow.MigrationMappings, WorkflowReviewEvidence) (WorkflowConfiguration, error)
	GetPublished(context.Context, string, string) (WorkflowConfiguration, error)
}

const CurrentWorkflowDraftID = "current"

type SaveWorkflowDraftRequest struct {
	ProjectID             string
	Config                projectworkflow.Config
	ExpectedDraftRevision uint64
	IdempotencyKey        string
}
type PublishWorkflowDraftRequest struct {
	ProjectID, DraftID                           string
	ExpectedDraftRevision, ExpectedConfigVersion uint64
	ReviewedDigest, ReviewedPlanDigest           string
	IdempotencyKey                               string
	Mappings                                     projectworkflow.MigrationMappings
	ReviewEvidence                               WorkflowReviewEvidence
}
type PreviewWorkflowDraftRequest struct {
	ProjectID, DraftID    string
	ExpectedDraftRevision uint64
	Mappings              projectworkflow.MigrationMappings
}

func (s Service) SaveWorkflowDraft(ctx context.Context, p *trust.Principal, req SaveWorkflowDraftRequest) (uint64, error) {
	if err := validPrincipal(p); err != nil {
		return 0, err
	}
	if s.Auth == nil || s.Workflows == nil {
		return 0, ErrUnavailable
	}
	if req.ProjectID == "" || req.IdempotencyKey == "" {
		return 0, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, p, req.ProjectID, projectaccess.ConfigureWorkflow); err != nil {
		return 0, err
	}
	return s.Workflows.SaveDraft(ctx, tenant(p), req.ProjectID, p.Subject(), req.Config, req.ExpectedDraftRevision, req.IdempotencyKey)
}

func (s Service) GetWorkflowDraft(ctx context.Context, p *trust.Principal, projectID string, draftIDs ...string) (projectworkflow.Config, uint64, error) {
	if err := validPrincipal(p); err != nil {
		return projectworkflow.Config{}, 0, err
	}
	if s.Auth == nil || s.Workflows == nil {
		return projectworkflow.Config{}, 0, ErrUnavailable
	}
	if projectID == "" {
		return projectworkflow.Config{}, 0, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, p, projectID, projectaccess.ReadProject); err != nil {
		return projectworkflow.Config{}, 0, err
	}
	if len(draftIDs) > 0 && draftIDs[0] != CurrentWorkflowDraftID {
		return projectworkflow.Config{}, 0, ErrInvalidRequest
	}
	return s.Workflows.GetDraft(ctx, tenant(p), projectID)
}

func (s Service) PreviewWorkflowDraft(ctx context.Context, p *trust.Principal, projectID string, draftIDs ...string) (WorkflowPreview, error) {
	draftID := ""
	if len(draftIDs) > 0 {
		draftID = draftIDs[0]
	}
	cfg, revision, err := s.GetWorkflowDraft(ctx, p, projectID, draftIDs...)
	if err != nil {
		return WorkflowPreview{}, err
	}
	_ = cfg
	return s.PreviewWorkflowDraftWithMappings(ctx, p, PreviewWorkflowDraftRequest{ProjectID: projectID, DraftID: draftID, ExpectedDraftRevision: revision})
}

func (s Service) PreviewWorkflowDraftWithMappings(ctx context.Context, p *trust.Principal, req PreviewWorkflowDraftRequest) (WorkflowPreview, error) {
	var cfg projectworkflow.Config
	var rev uint64
	var err error
	if req.DraftID == "" {
		cfg, rev, err = s.GetWorkflowDraft(ctx, p, req.ProjectID)
	} else {
		cfg, rev, err = s.GetWorkflowDraft(ctx, p, req.ProjectID, req.DraftID)
	}
	if err != nil {
		return WorkflowPreview{}, err
	}
	if req.ExpectedDraftRevision != 0 && rev != req.ExpectedDraftRevision {
		return WorkflowPreview{}, projectconfigstore.ErrRevisionConflict
	}
	if errs := projectworkflow.Validate(cfg); len(errs) > 0 {
		return WorkflowPreview{Config: cfg, DraftRevision: rev, Errors: errs}, nil
	}
	return s.Workflows.PreviewDraft(ctx, tenant(p), req.ProjectID, cfg, rev, req.Mappings)
}

func (s Service) PublishWorkflowDraft(ctx context.Context, p *trust.Principal, req PublishWorkflowDraftRequest) (WorkflowConfiguration, error) {
	if err := validPrincipal(p); err != nil {
		return WorkflowConfiguration{}, err
	}
	if s.Auth == nil || s.Workflows == nil {
		return WorkflowConfiguration{}, ErrUnavailable
	}
	if req.ProjectID == "" || req.ReviewedDigest == "" || req.ReviewedPlanDigest == "" || req.IdempotencyKey == "" || req.ExpectedDraftRevision == 0 || req.ExpectedConfigVersion == 0 || (req.DraftID != "" && req.DraftID != CurrentWorkflowDraftID) {
		return WorkflowConfiguration{}, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, p, req.ProjectID, projectaccess.PublishConfiguration); err != nil {
		return WorkflowConfiguration{}, err
	}
	return s.Workflows.PublishDraft(ctx, tenant(p), req.ProjectID, p.Subject(), req.ExpectedDraftRevision, req.ExpectedConfigVersion, req.ReviewedDigest, req.ReviewedPlanDigest, req.IdempotencyKey, req.Mappings, req.ReviewEvidence)
}

func (s Service) GetWorkflowConfiguration(ctx context.Context, p *trust.Principal, projectID string) (WorkflowConfiguration, error) {
	if err := validPrincipal(p); err != nil {
		return WorkflowConfiguration{}, err
	}
	if s.Auth == nil || s.Workflows == nil {
		return WorkflowConfiguration{}, ErrUnavailable
	}
	if projectID == "" {
		return WorkflowConfiguration{}, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, p, projectID, projectaccess.ReadProject); err != nil {
		return WorkflowConfiguration{}, err
	}
	return s.Workflows.GetPublished(ctx, tenant(p), projectID)
}

func (s Service) ListBoardViews(ctx context.Context, p *trust.Principal, projectID, after string, limit int) (BoardViewList, error) {
	if err := validPrincipal(p); err != nil {
		return BoardViewList{}, err
	}
	if s.Auth == nil || s.Reads == nil {
		return BoardViewList{}, ErrUnavailable
	}
	if projectID == "" || limit < 1 || limit > MaxListPageSize {
		return BoardViewList{}, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, p, projectID, projectaccess.ReadProject); err != nil {
		return BoardViewList{}, err
	}
	repo, ok := s.Reads.(ScopedListRepository)
	if !ok {
		return BoardViewList{}, ErrUnavailable
	}
	rows, err := repo.ListViews(ctx, tenant(p), projectID, p.Subject(), after, limit+1)
	if err != nil {
		return BoardViewList{}, err
	}
	page := BoardViewList{Views: rows}
	if len(rows) > limit {
		page.Views = rows[:limit]
		page.Next = &IDCursor{AfterID: rows[limit-1].ID}
	}
	return page, nil
}

type TaskLink struct {
	ID         string
	Reference  projectlink.Reference
	Resolution projectlink.Result
}
type TaskLinkRecord struct {
	ID        string
	Reference projectlink.Reference
}
type LinkRepository interface {
	AddTaskLink(context.Context, string, string, string, string, string, projectlink.Reference, uint64) (string, uint64, error)
	RemoveTaskLink(context.Context, string, string, string, string, string, uint64, string) (uint64, error)
	ListTaskLinks(context.Context, string, string, string, string, int) ([]TaskLinkRecord, *IDCursor, error)
}
type LinkResolver interface {
	Resolve(context.Context, string, projectlink.Reference) (projectlink.Result, error)
}
type AddTaskLinkRequest struct {
	ProjectID, TaskID    string
	Reference            projectlink.Reference
	ExpectedTaskRevision uint64
	IdempotencyKey       string
}

func (s Service) AddTaskLink(ctx context.Context, p *trust.Principal, req AddTaskLinkRequest) (string, uint64, error) {
	if err := validPrincipal(p); err != nil {
		return "", 0, err
	}
	if s.Auth == nil || s.Links == nil {
		return "", 0, ErrUnavailable
	}
	if req.ProjectID == "" || req.TaskID == "" || req.ExpectedTaskRevision == 0 || req.ExpectedTaskRevision > uint64(^uint64(0)>>1) || req.IdempotencyKey == "" || req.Reference.Validate() != nil {
		return "", 0, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, p, req.ProjectID, projectaccess.EditTask); err != nil {
		return "", 0, err
	}
	if s.LinkResolver == nil {
		return "", 0, ErrUnavailable
	}
	resolved, err := s.LinkResolver.Resolve(ctx, p.Subject(), req.Reference)
	if err != nil {
		return "", 0, err
	}
	if resolved.State != projectlink.Available {
		return "", 0, ErrUnsupportedLink
	}
	if err := s.Auth.Authorize(ctx, p, req.ProjectID, projectaccess.EditTask); err != nil {
		return "", 0, err
	}
	return s.Links.AddTaskLink(ctx, tenant(p), req.ProjectID, req.TaskID, p.Subject(), req.IdempotencyKey, req.Reference, req.ExpectedTaskRevision)
}

type RemoveTaskLinkRequest struct {
	ProjectID, TaskID, LinkID string
	ExpectedTaskRevision      uint64
	IdempotencyKey            string
}

// RemoveTaskLink applies project edit authority before asking the repository
// to atomically tombstone the link and append task activity/outbox evidence.
func (s Service) RemoveTaskLink(ctx context.Context, p *trust.Principal, req RemoveTaskLinkRequest) (uint64, error) {
	if err := validPrincipal(p); err != nil {
		return 0, err
	}
	if s.Auth == nil || s.Links == nil {
		return 0, ErrUnavailable
	}
	if req.ProjectID == "" || req.TaskID == "" || req.LinkID == "" || req.ExpectedTaskRevision == 0 || req.IdempotencyKey == "" || req.ExpectedTaskRevision > uint64(^uint64(0)>>1) {
		return 0, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, p, req.ProjectID, projectaccess.EditTask); err != nil {
		return 0, err
	}
	return s.Links.RemoveTaskLink(ctx, tenant(p), req.ProjectID, req.TaskID, req.LinkID, p.Subject(), req.ExpectedTaskRevision, req.IdempotencyKey)
}

// LinkedTaskRecord is one task that links a workflow run or work item.
type LinkedTaskRecord struct {
	ProjectID, TaskID, Title, StatusID string
}

// TaskLinkTargetRepository is the reverse link read: tasks linking a target.
type TaskLinkTargetRepository interface {
	ListTasksLinkingTo(context.Context, string, projectlink.Kind, string, int) ([]LinkedTaskRecord, error)
}

// ListTasksLinkingTo returns the tasks that link one workflow run (journey)
// or work item, limited to projects the viewer may read. Tasks in other
// projects are omitted, never reported as restricted, so the answer does not
// disclose where else a workflow is tracked.
func (s Service) ListTasksLinkingTo(ctx context.Context, p *trust.Principal, kind projectlink.Kind, targetID string, limit int) ([]LinkedTaskRecord, error) {
	if err := validPrincipal(p); err != nil {
		return nil, err
	}
	repo, ok := s.Links.(TaskLinkTargetRepository)
	if s.Auth == nil || !ok {
		return nil, ErrUnavailable
	}
	if (kind != projectlink.Journey && kind != projectlink.WorkItem) || strings.TrimSpace(targetID) == "" || limit < 1 || limit > MaxListPageSize {
		return nil, ErrInvalidRequest
	}
	records, err := repo.ListTasksLinkingTo(ctx, tenant(p), kind, targetID, 100)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	out := make([]LinkedTaskRecord, 0, len(records))
	for _, record := range records {
		ok, seen := allowed[record.ProjectID]
		if !seen {
			ok = s.Auth.Authorize(ctx, p, record.ProjectID, projectaccess.ReadTask) == nil
			allowed[record.ProjectID] = ok
		}
		if ok {
			out = append(out, record)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func (s Service) ListTaskLinks(ctx context.Context, p *trust.Principal, projectID, taskID, after string, limit int) ([]TaskLink, *IDCursor, error) {
	if err := validPrincipal(p); err != nil {
		return nil, nil, err
	}
	if s.Auth == nil || s.Links == nil || s.LinkResolver == nil {
		return nil, nil, ErrUnavailable
	}
	if projectID == "" || taskID == "" || limit < 1 || limit > MaxListPageSize {
		return nil, nil, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, p, projectID, projectaccess.ReadTask); err != nil {
		return nil, nil, err
	}
	records, next, err := s.Links.ListTaskLinks(ctx, tenant(p), projectID, taskID, after, limit)
	if err != nil {
		return nil, nil, err
	}
	results := make([]TaskLink, 0, len(records))
	for _, record := range records {
		resolved, resolveErr := s.LinkResolver.Resolve(ctx, p.Subject(), record.Reference)
		if resolveErr != nil {
			return nil, nil, resolveErr
		}
		if resolved.State != projectlink.Available {
			resolved = projectlink.Result{State: projectlink.Restricted}
			results = append(results, TaskLink{ID: record.ID, Resolution: resolved})
			continue
		}
		results = append(results, TaskLink{ID: record.ID, Reference: record.Reference, Resolution: resolved})
	}
	if err := s.Auth.Authorize(ctx, p, projectID, projectaccess.ReadTask); err != nil {
		return nil, nil, err
	}
	return results, next, nil
}

// View save and list operations share project authorization; personal view
// repositories must key rows by both tenant and owner subject.
func (s Service) ListPersonalViews(ctx context.Context, p *trust.Principal, projectID string) ([]projectboard.BoardView, error) {
	page, err := s.ListBoardViews(ctx, p, projectID, "", MaxListPageSize)
	if err != nil {
		return nil, err
	}
	return page.Views, nil
}
