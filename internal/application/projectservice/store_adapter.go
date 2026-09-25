package projectservice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectconfigstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectlinkstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectmemberstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectviewstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

var ErrInvalidCursor = errors.New("projectservice: invalid or stale board cursor")
var ErrUnsafeWorkflowPublish = errors.New("projectservice: workflow publish would change existing tasks without atomic migration")

// MigrationPreviewer supplies an authorized, bounded snapshot of project
// tasks to the pure workflow migration planner. A nil previewer fails closed.
type MigrationPreviewer interface {
	Preview(context.Context, string, string, projectworkflow.Config, uint64, projectworkflow.MigrationMappings) (WorkflowPreview, error)
}

// StoreAdapter translates application ports to the isolated project data
// stores. Board cursors are HMAC sealed to tenant, principal, project, view,
// project revision, and workflow revision.
type StoreAdapter struct {
	Projects     *projectstore.Store
	Members      *projectmemberstore.Store
	Configs      *projectconfigstore.Store
	Views        *projectviewstore.Store
	Links        *projectlinkstore.Store
	LinkResolver LinkResolver
	Previewer    MigrationPreviewer
	CursorKey    []byte
}

var (
	_ CommandRepository          = (*StoreAdapter)(nil)
	_ SourceLinkedTaskRepository = (*StoreAdapter)(nil)
	_ ReadRepository             = (*StoreAdapter)(nil)
	_ ViewRepository             = (*StoreAdapter)(nil)
	_ BoardPageSource            = (*StoreAdapter)(nil)
	_ WorkflowPort               = (*StoreAdapter)(nil)
	_ LinkRepository             = (*StoreAdapter)(nil)
	_ LinkResolver               = (*StoreAdapter)(nil)
	_ MembershipPolicy           = (*StoreAdapter)(nil)
	_ MembershipRepository       = (*StoreAdapter)(nil)
	_ ScopedListRepository       = (*StoreAdapter)(nil)
)

func (a *StoreAdapter) CreateProject(ctx context.Context, p ProjectRecord, actor, key string) (ProjectRecord, error) {
	if a == nil || a.Projects == nil || a.Configs == nil || a.Views == nil {
		return ProjectRecord{}, ErrUnavailable
	}
	record := projectstore.ProjectRecord{ID: p.ID, TenantID: p.TenantID, OwnerID: p.OwnerID, Name: p.Name, Timezone: p.Timezone, Lifecycle: string(p.State), Revision: int64(p.Revision)}
	err := a.Projects.CreateProjectWith(ctx, record, actor, "HUMAN", key, func(ctx context.Context, tx dbport.Tx) error {
		if err := projectconfigstore.InitializeProjectTx(ctx, tx, p.TenantID, p.ID, actor); err != nil {
			return err
		}
		return projectviewstore.InitializeProjectTx(ctx, tx, p.TenantID, p.ID, actor)
	})
	if err != nil {
		return ProjectRecord{}, err
	}
	return p, nil
}

func (a *StoreAdapter) CreateTask(ctx context.Context, t TaskRecord, configVersion uint64, actor, key string) (TaskRecord, error) {
	if a == nil || a.Projects == nil {
		return TaskRecord{}, ErrUnavailable
	}
	record, err := toStoreTask(t)
	if err != nil {
		return TaskRecord{}, err
	}
	err = a.Projects.CreateTaskWithConfig(ctx, record, int64(configVersion), actor, "HUMAN", key)
	if err != nil {
		return TaskRecord{}, err
	}
	t.WorkflowRevision = configVersion
	return t, nil
}

func (a *StoreAdapter) CreateTaskWithLink(ctx context.Context, t TaskRecord, configVersion uint64, actor, key string, ref projectlink.Reference) (TaskRecord, error) {
	if a == nil || a.Projects == nil || ref.Validate() != nil {
		return TaskRecord{}, ErrUnavailable
	}
	record, err := toStoreTask(t)
	if err != nil {
		return TaskRecord{}, err
	}
	link := projectlinkstore.LinkRecord{
		ID:       stableID(t.TenantID, actor, "task.source-link:"+t.ProjectID+":"+t.ID, key),
		TenantID: t.TenantID, ProjectID: t.ProjectID, TaskID: t.ID, Reference: ref,
	}
	refFingerprint, err := json.Marshal(ref)
	if err != nil {
		return TaskRecord{}, err
	}
	err = a.Projects.CreateTaskWith(ctx, record, int64(configVersion), actor, "HUMAN", key, string(refFingerprint), func(ctx context.Context, tx dbport.Tx) error {
		return projectlinkstore.AddTx(ctx, tx, link)
	})
	if err != nil {
		return TaskRecord{}, err
	}
	t.WorkflowRevision = configVersion
	return t, nil
}

func (a *StoreAdapter) MoveTask(ctx context.Context, tenantID, projectID, taskID, target string, expected, config uint64, edits []project.TaskFieldEdit, actor, key string) (TaskRecord, error) {
	if a == nil || a.Projects == nil {
		return TaskRecord{}, ErrUnavailable
	}
	r, err := a.Projects.MoveTask(ctx, tenantID, projectID, taskID, target, int64(expected), int64(config), edits, actor, "HUMAN", key)
	if err != nil {
		return TaskRecord{}, err
	}
	out, err := fromStoreTask(r)
	out.WorkflowRevision = config
	return out, err
}

func (a *StoreAdapter) GetProject(ctx context.Context, tenantID, id string) (ProjectRecord, error) {
	if a == nil || a.Projects == nil {
		return ProjectRecord{}, ErrUnavailable
	}
	r, err := a.Projects.GetProject(ctx, tenantID, id)
	if err != nil {
		return ProjectRecord{}, err
	}
	return ProjectRecord{ID: r.ID, TenantID: r.TenantID, OwnerID: r.OwnerID, Name: r.Name, Timezone: r.Timezone, State: project.Lifecycle(r.Lifecycle), Revision: uint64(r.Revision)}, nil
}
func (a *StoreAdapter) GetTask(ctx context.Context, tenantID, projectID, id string) (TaskRecord, error) {
	if a == nil || a.Projects == nil {
		return TaskRecord{}, ErrUnavailable
	}
	r, err := a.Projects.GetTask(ctx, tenantID, projectID, id)
	if err != nil {
		return TaskRecord{}, err
	}
	return fromStoreTask(r)
}
func (a *StoreAdapter) GetView(ctx context.Context, tenantID, projectID, userID, id string) (projectboard.BoardView, error) {
	if a == nil || a.Views == nil {
		return projectboard.BoardView{}, ErrUnavailable
	}
	return a.Views.GetView(ctx, tenantID, projectID, userID, id)
}
func (a *StoreAdapter) SaveView(ctx context.Context, tenantID, projectID, ownerID, actor string, v projectboard.BoardView, expected uint64, key string) (projectboard.BoardView, error) {
	if a == nil || a.Views == nil {
		return projectboard.BoardView{}, ErrUnavailable
	}
	return a.Views.SaveView(ctx, tenantID, projectID, ownerID, actor, v, expected, key)
}

func (a *StoreAdapter) GetDraft(ctx context.Context, tenantID, projectID string) (projectworkflow.Config, uint64, error) {
	if a == nil || a.Configs == nil {
		return projectworkflow.Config{}, 0, ErrUnavailable
	}
	return a.Configs.GetDraft(ctx, tenantID, projectID)
}
func (a *StoreAdapter) SaveDraft(ctx context.Context, tenantID, projectID, actor string, c projectworkflow.Config, expected uint64, key string) (uint64, error) {
	if a == nil || a.Configs == nil {
		return 0, ErrUnavailable
	}
	draft, err := a.Configs.SaveDraft(ctx, tenantID, projectID, actor, int64(expected), c, key)
	return uint64(draft.Revision), err
}
func (a *StoreAdapter) PreviewDraft(ctx context.Context, tenantID, projectID string, c projectworkflow.Config, revision uint64, mappings projectworkflow.MigrationMappings) (WorkflowPreview, error) {
	if a == nil {
		return WorkflowPreview{}, ErrUnavailable
	}
	previewer := a.Previewer
	if previewer == nil && a.Projects != nil && a.Configs != nil {
		previewer = StoreMigrationPreviewer{Projects: a.Projects, Configs: a.Configs}
	}
	if previewer == nil {
		return WorkflowPreview{}, ErrUnavailable
	}
	return previewer.Preview(ctx, tenantID, projectID, c, revision, mappings)
}
func (a *StoreAdapter) PublishDraft(ctx context.Context, tenantID, projectID, actor string, draftRevision, configVersion uint64, digest, planDigest, key string, mappings projectworkflow.MigrationMappings, e WorkflowReviewEvidence) (WorkflowConfiguration, error) {
	if a == nil || a.Configs == nil {
		return WorkflowConfiguration{}, ErrUnavailable
	}
	evidence := projectconfigstore.ReviewEvidence{Required: e.Required, ReviewerID: e.ReviewerID, DecisionID: e.DecisionID, ReviewedDigest: e.ReviewedDigest, ReviewedPlanDigest: e.ReviewedPlanDigest}
	p, err := a.Configs.PublishWithMigration(ctx, tenantID, projectID, actor, int64(draftRevision), int64(configVersion), digest, planDigest, key, evidence, mappings)
	return WorkflowConfiguration{Version: uint64(p.Version), Digest: p.Digest, Config: p.Config}, err
}
func (a *StoreAdapter) GetPublished(ctx context.Context, tenantID, projectID string) (WorkflowConfiguration, error) {
	if a == nil || a.Configs == nil {
		return WorkflowConfiguration{}, ErrUnavailable
	}
	p, err := a.Configs.GetPublished(ctx, tenantID, projectID)
	if err != nil {
		return WorkflowConfiguration{}, err
	}
	return WorkflowConfiguration{Version: uint64(p.Version), Digest: p.Digest, Config: p.Config}, nil
}

func (a *StoreAdapter) AddTaskLink(ctx context.Context, tenantID, projectID, taskID, actor, key string, ref projectlink.Reference, expectedTaskRevision uint64) (string, uint64, error) {
	if a == nil || a.Links == nil {
		return "", 0, ErrUnavailable
	}
	if expectedTaskRevision > uint64(^uint64(0)>>1) {
		return "", 0, ErrInvalidRequest
	}
	id := stableID(tenantID, actor, "task.link:"+projectID+":"+taskID, key)
	revision, err := a.Links.AddRevisioned(ctx, projectlinkstore.LinkRecord{ID: id, TenantID: tenantID, ProjectID: projectID, TaskID: taskID, Reference: ref}, actor, key, int64(expectedTaskRevision))
	return id, uint64(revision), err
}
func (a *StoreAdapter) RemoveTaskLink(ctx context.Context, tenantID, projectID, taskID, linkID, actor string, expectedTaskRevision uint64, key string) (uint64, error) {
	if a == nil || a.Links == nil || expectedTaskRevision > uint64(^uint64(0)>>1) {
		return 0, ErrUnavailable
	}
	revision, err := a.Links.Remove(ctx, tenantID, projectID, taskID, linkID, actor, key, int64(expectedTaskRevision))
	return uint64(revision), err
}

// ListTasksLinkingTo implements TaskLinkTargetRepository.
func (a *StoreAdapter) ListTasksLinkingTo(ctx context.Context, tenantID string, kind projectlink.Kind, targetID string, limit int) ([]LinkedTaskRecord, error) {
	if a == nil || a.Links == nil {
		return nil, ErrUnavailable
	}
	rows, err := a.Links.ListByTarget(ctx, tenantID, kind, targetID, int32(limit))
	if err != nil {
		return nil, err
	}
	out := make([]LinkedTaskRecord, len(rows))
	for i, r := range rows {
		out[i] = LinkedTaskRecord{ProjectID: r.ProjectID, TaskID: r.TaskID, Title: r.Title, StatusID: r.StatusID}
	}
	return out, nil
}

func (a *StoreAdapter) ListTaskLinks(ctx context.Context, tenantID, projectID, taskID, after string, limit int) ([]TaskLinkRecord, *IDCursor, error) {
	if a == nil || a.Links == nil {
		return nil, nil, ErrUnavailable
	}
	fetch := limit + 1
	if fetch > 100 {
		fetch = 100
	}
	rows, err := a.Links.List(ctx, tenantID, projectID, taskID, after, int32(fetch))
	if err != nil {
		return nil, nil, err
	}
	next := (*IDCursor)(nil)
	if len(rows) > limit {
		rows = rows[:limit]
		next = &IDCursor{AfterID: rows[len(rows)-1].ID}
	}
	out := make([]TaskLinkRecord, len(rows))
	for i, r := range rows {
		out[i] = TaskLinkRecord{ID: r.ID, Reference: r.Reference}
	}
	return out, next, nil
}
func (a *StoreAdapter) Resolve(ctx context.Context, principal string, ref projectlink.Reference) (projectlink.Result, error) {
	if a == nil || a.LinkResolver == nil {
		return projectlink.Result{}, ErrUnavailable
	}
	return a.LinkResolver.Resolve(ctx, principal, ref)
}

func (a *StoreAdapter) Authorize(ctx context.Context, tenantID, projectID, userID string, cap projectaccess.Capability) error {
	if a == nil || a.Members == nil {
		return ErrUnavailable
	}
	return a.Members.Authorize(ctx, tenantID, projectID, userID, cap)
}
func (a *StoreAdapter) AuthorizeList(ctx context.Context, tenantID, userID string) error {
	if a == nil || a.Members == nil {
		return ErrUnavailable
	}
	return a.Members.AuthorizeList(ctx, tenantID, userID)
}

func (a *StoreAdapter) GetMemberships(ctx context.Context, tenantID, projectID string) (MembershipSnapshot, error) {
	if a == nil || a.Members == nil {
		return MembershipSnapshot{}, ErrUnavailable
	}
	s, err := a.Members.GetSnapshot(ctx, tenantID, projectID)
	if err != nil {
		return MembershipSnapshot{}, err
	}
	out := MembershipSnapshot{Revision: uint64(s.Revision), Members: make([]MembershipRecord, 0, len(s.Members))}
	for _, m := range s.Members {
		out.Members = append(out.Members, MembershipRecord{UserID: m.UserID, Role: m.Role, State: m.State, Revision: uint64(m.Revision)})
	}
	return out, nil
}
func (a *StoreAdapter) ValidateInviteeClassification(ctx context.Context, tenantID, projectID string, inviteeClass uint8) error {
	if a == nil || a.Projects == nil {
		return ErrUnavailable
	}
	// journey_worker has no per-worker project classification. The workforce
	// adapter therefore assigns baseline class 0, which this release permits
	// only on baseline-class projects. Higher-class invitations need an
	// authoritative classification source.
	if inviteeClass != workforceInviteeBaselineClass {
		return nil
	}
	var projectClass int
	err := a.Projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT classification_level FROM project WHERE tenant_id=$1 AND id=$2`, tenantID, projectID).Scan(&projectClass)
	})
	if err != nil {
		return err
	}
	if projectClass != int(workforceInviteeBaselineClass) {
		return projectaccess.ErrUnauthorized
	}
	return nil
}
func (a *StoreAdapter) InviteMember(ctx context.Context, tenantID, projectID, actorID, userID string, role projectaccess.Role, inviteClass uint8, expected uint64, key string) error {
	if a == nil || a.Members == nil {
		return ErrUnavailable
	}
	return a.Members.Invite(ctx, tenantID, projectID, actorID, userID, role, inviteClass, int64(expected), key)
}
func (a *StoreAdapter) AcceptMemberInvitation(ctx context.Context, tenantID, projectID, userID string, expected uint64, key string) error {
	if a == nil || a.Members == nil {
		return ErrUnavailable
	}
	return a.Members.AcceptInvitation(ctx, tenantID, projectID, userID, int64(expected), key)
}
func (a *StoreAdapter) ChangeMemberRole(ctx context.Context, tenantID, projectID, actorID, userID string, role projectaccess.Role, expected uint64, key string) error {
	if a == nil || a.Members == nil {
		return ErrUnavailable
	}
	return a.Members.ChangeRole(ctx, tenantID, projectID, actorID, userID, role, int64(expected), key)
}
func (a *StoreAdapter) RevokeMember(ctx context.Context, tenantID, projectID, actorID, userID string, expected uint64, key string) error {
	if a == nil || a.Members == nil {
		return ErrUnavailable
	}
	return a.Members.Revoke(ctx, tenantID, projectID, actorID, userID, int64(expected), key)
}
func (a *StoreAdapter) TransferMemberOwnership(ctx context.Context, tenantID, projectID, actorID, userID string, expected uint64, key string) error {
	if a == nil || a.Members == nil {
		return ErrUnavailable
	}
	return a.Members.TransferOwnership(ctx, tenantID, projectID, actorID, userID, nil, time.Time{}, int64(expected), key)
}

func (a *StoreAdapter) ListAuthorizedProjects(ctx context.Context, tenantID, userID, after string, limit int) ([]ProjectRecord, error) {
	if a == nil || a.Projects == nil || a.Members == nil {
		return nil, ErrUnavailable
	}
	if limit < 1 || limit > MaxListPageSize+1 {
		return nil, ErrInvalidRequest
	}
	raw, err := a.Members.ListAuthorizedProjects(ctx, tenantID, userID, after, int32(limit))
	if err != nil {
		return nil, err
	}
	out := make([]ProjectRecord, len(raw))
	for i, r := range raw {
		out[i] = ProjectRecord{ID: r.ID, TenantID: r.TenantID, OwnerID: r.OwnerID, Name: r.Name, Timezone: r.Timezone, State: project.Lifecycle(r.Lifecycle), Revision: uint64(r.Revision)}
	}
	return out, nil
}

func (a *StoreAdapter) ListTaskRecordsAuthorized(ctx context.Context, tenantID, projectID, userID, after string, f TaskListFilter, limit int) ([]TaskRecord, error) {
	if a == nil || a.Projects == nil || a.Members == nil {
		return nil, ErrUnavailable
	}
	if err := a.Members.Authorize(ctx, tenantID, projectID, userID, projectaccess.ReadTask); err != nil {
		return nil, err
	}
	if limit < 1 || limit > MaxListPageSize+1 {
		return nil, ErrInvalidRequest
	}
	raw, err := a.Projects.ListTasksFiltered(ctx, tenantID, projectID, after, int32(limit), projectstore.TaskQuery{StatusIDs: f.StatusIDs, Filter: projectboard.Filter{AssigneeID: f.AssigneeID, Priorities: f.Priorities, TypeIDs: f.TypeIDs, EnumFields: f.EnumFields}})
	if err != nil {
		return nil, err
	}
	out := make([]TaskRecord, 0, len(raw))
	for _, r := range raw {
		t, err := fromStoreTask(r)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := a.Members.Authorize(ctx, tenantID, projectID, userID, projectaccess.ReadTask); err != nil {
		return nil, err
	}
	return out, nil
}
func (a *StoreAdapter) ListViews(ctx context.Context, tenantID, projectID, userID, after string, limit int) ([]projectboard.BoardView, error) {
	if a == nil || a.Views == nil {
		return nil, ErrUnavailable
	}
	if limit > projectviewstore.MaxViewsPerProject {
		limit = projectviewstore.MaxViewsPerProject
	}
	return a.Views.ListViews(ctx, tenantID, projectID, userID, after, int32(limit))
}

func (a *StoreAdapter) ListAuthorizedTasks(ctx context.Context, tenantID, projectID, userID string, q projectboard.AuthorizedTaskQuery) (AuthorizedBoardTasks, *projectboard.Cursor, error) {
	if a == nil || a.Projects == nil || a.Members == nil || a.Configs == nil || len(a.CursorKey) < 32 {
		return AuthorizedBoardTasks{}, nil, ErrUnavailable
	}
	if err := a.Members.Authorize(ctx, tenantID, projectID, userID, projectaccess.ReadTask); err != nil {
		return AuthorizedBoardTasks{}, nil, err
	}
	p, err := a.Projects.GetProject(ctx, tenantID, projectID)
	if err != nil {
		return AuthorizedBoardTasks{}, nil, err
	}
	eventSequence, err := a.projectEventSequence(ctx, tenantID, projectID)
	if err != nil {
		return AuthorizedBoardTasks{}, nil, err
	}
	published, err := a.Configs.GetPublished(ctx, tenantID, projectID)
	if err != nil {
		return AuthorizedBoardTasks{}, nil, err
	}
	filterDigest := queryDigest(q)
	afterValue, afterID := "", ""
	if q.Cursor.Token != "" {
		c, e := a.decodeCursor(q.Cursor.Token)
		if e != nil {
			return AuthorizedBoardTasks{}, nil, e
		}
		if !cursorMatches(c, tenantID, projectID, userID, q, uint64(p.Revision), uint64(published.Version), eventSequence, filterDigest) {
			return AuthorizedBoardTasks{}, nil, ErrInvalidCursor
		}
		afterValue, afterID = c.AfterValue, c.AfterID
	}
	limit := q.Limit
	if limit < 1 || limit > projectboard.MaxPageSize {
		return AuthorizedBoardTasks{}, nil, projectboard.ErrInvalidPage
	}
	rows, err := a.Projects.ListBoardTasksFiltered(ctx, tenantID, projectID, afterValue, afterID, normalizedOrder(q.OrderBy), q.Descending, int32(limit+1), projectstore.TaskQuery{StatusIDs: q.StatusIDs, Filter: q.Filter, RequireStatusScope: true, ExcludeArchived: true})
	if err != nil {
		return AuthorizedBoardTasks{}, nil, err
	}
	matched := make([]projectboard.Task, 0, len(rows))
	revisions := map[string]uint64{}
	for _, r := range rows {
		t, e := fromStoreTask(r)
		if e != nil {
			return AuthorizedBoardTasks{}, nil, e
		}
		if !boardTaskMatches(q, t) {
			return AuthorizedBoardTasks{}, nil, ErrUnavailable
		}
		matched = append(matched, boardTask(t))
		revisions[t.ID] = t.Revision
	}
	if err := a.Members.Authorize(ctx, tenantID, projectID, userID, projectaccess.ReadTask); err != nil {
		return AuthorizedBoardTasks{}, nil, err
	}
	currentSequence, err := a.projectEventSequence(ctx, tenantID, projectID)
	if err != nil {
		return AuthorizedBoardTasks{}, nil, err
	}
	if currentSequence != eventSequence {
		return AuthorizedBoardTasks{}, nil, ErrInvalidCursor
	}
	var next *projectboard.Cursor
	if len(matched) > limit {
		matched = matched[:limit]
		last := matched[len(matched)-1].ID
		lastTask := matched[len(matched)-1]
		token, e := a.encodeCursor(boardCursor{TenantID: tenantID, ProjectID: projectID, UserID: userID, ViewID: q.ViewID, ViewVersion: q.ViewVersion, ProjectRevision: uint64(p.Revision), WorkflowRevision: uint64(published.Version), EventSequence: eventSequence, FilterDigest: filterDigest, OrderBy: normalizedOrder(q.OrderBy), Descending: q.Descending, AfterValue: boardOrderValue(lastTask, normalizedOrder(q.OrderBy)), AfterID: last})
		if e != nil {
			return AuthorizedBoardTasks{}, nil, e
		}
		next = &projectboard.Cursor{Token: token}
	}
	for id := range revisions {
		found := false
		for _, t := range matched {
			if t.ID == id {
				found = true
				break
			}
		}
		if !found {
			delete(revisions, id)
		}
	}
	return AuthorizedBoardTasks{Tasks: matched, TaskRevisions: revisions, WorkflowRevision: uint64(published.Version), Freshness: FreshnessCurrent}, next, nil
}

func (a *StoreAdapter) projectEventSequence(ctx context.Context, tenantID, projectID string) (uint64, error) {
	var sequence int64
	err := a.Projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT event_sequence FROM project WHERE tenant_id=$1 AND id=$2`, tenantID, projectID).Scan(&sequence)
	})
	if err != nil {
		return 0, err
	}
	if sequence < 0 {
		return 0, ErrInvalidCursor
	}
	return uint64(sequence), nil
}

func cursorMatches(c boardCursor, tenantID, projectID, userID string, q projectboard.AuthorizedTaskQuery, projectRevision, workflowRevision, eventSequence uint64, filterDigest string) bool {
	return c.TenantID == tenantID && c.ProjectID == projectID && c.UserID == userID && c.ViewID == q.ViewID && c.ViewVersion == q.ViewVersion && c.ProjectRevision == projectRevision && c.WorkflowRevision == workflowRevision && c.EventSequence == eventSequence && c.FilterDigest == filterDigest && c.OrderBy == normalizedOrder(q.OrderBy) && c.Descending == q.Descending
}

type boardCursor struct {
	TenantID, ProjectID, UserID, ViewID                           string
	ViewVersion, ProjectRevision, WorkflowRevision, EventSequence uint64
	FilterDigest, AfterValue, AfterID                             string
	OrderBy                                                       projectboard.OrderField
	Descending                                                    bool
}

func normalizedOrder(order projectboard.OrderField) projectboard.OrderField {
	if order == "" {
		return projectboard.OrderTaskID
	}
	return order
}

func boardOrderValue(t projectboard.Task, order projectboard.OrderField) string {
	switch order {
	case projectboard.OrderTitle:
		return projectboard.FoldTitleForOrder(t.Title)
	case projectboard.OrderPriority:
		return t.Priority
	case projectboard.OrderDueDate:
		return t.DueDate
	default:
		return ""
	}
}

func (a *StoreAdapter) encodeCursor(c boardCursor) (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, a.CursorKey)
	_, _ = mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (a *StoreAdapter) decodeCursor(token string) (boardCursor, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return boardCursor{}, ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return boardCursor{}, ErrInvalidCursor
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return boardCursor{}, ErrInvalidCursor
	}
	mac := hmac.New(sha256.New, a.CursorKey)
	_, _ = mac.Write(raw)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return boardCursor{}, ErrInvalidCursor
	}
	var c boardCursor
	if json.Unmarshal(raw, &c) != nil {
		return boardCursor{}, ErrInvalidCursor
	}
	return c, nil
}
func queryDigest(q projectboard.AuthorizedTaskQuery) string {
	raw, _ := json.Marshal(struct {
		Statuses   []string
		Filter     projectboard.Filter
		OrderBy    projectboard.OrderField
		Descending bool
	}{q.StatusIDs, q.Filter, normalizedOrder(q.OrderBy), q.Descending})
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:])
}

func toStoreTask(t TaskRecord) (projectstore.TaskRecord, error) {
	fields, err := json.Marshal(t.Fields)
	if err != nil {
		return projectstore.TaskRecord{}, err
	}
	if len(t.Fields) == 0 {
		fields = []byte("{}")
	}
	var due *time.Time
	if t.DueDate != "" {
		parsed, e := time.Parse("2006-01-02", t.DueDate)
		if e != nil {
			return projectstore.TaskRecord{}, fmt.Errorf("invalid civil due date: %w", e)
		}
		due = &parsed
	}
	var start *time.Time
	if t.StartDate != "" {
		parsed, e := time.Parse("2006-01-02", t.StartDate)
		if e != nil {
			return projectstore.TaskRecord{}, fmt.Errorf("invalid civil start date: %w", e)
		}
		start = &parsed
	}
	return projectstore.TaskRecord{ID: t.ID, TenantID: t.TenantID, ProjectID: t.ProjectID, Title: t.Title, Description: t.Description, StatusID: t.StatusID, TypeID: t.TypeID, Priority: t.Priority, AssigneeID: t.AssigneeID, DueDate: due, Revision: int64(t.Revision), Archived: t.Archived, Fields: fields, CreatedBy: t.CreatedBy, Labels: t.Labels, StartDate: start, StoryPoints: int32(t.StoryPoints)}, nil
}
func fromStoreTask(r projectstore.TaskRecord) (TaskRecord, error) {
	fields := map[string]project.TaskFieldEdit{}
	if len(r.Fields) > 0 {
		if err := json.Unmarshal(r.Fields, &fields); err != nil {
			return TaskRecord{}, err
		}
	}
	due := ""
	if r.DueDate != nil {
		due = r.DueDate.Format("2006-01-02")
	}
	start := ""
	if r.StartDate != nil {
		start = r.StartDate.Format("2006-01-02")
	}
	points := uint32(0)
	if r.StoryPoints > 0 {
		points = uint32(r.StoryPoints)
	}
	return TaskRecord{ID: r.ID, TenantID: r.TenantID, ProjectID: r.ProjectID, Title: r.Title, Description: r.Description, StatusID: r.StatusID, TypeID: r.TypeID, Priority: r.Priority, AssigneeID: r.AssigneeID, DueDate: due, Revision: uint64(r.Revision), Archived: r.Archived, Fields: fields, CreatedBy: r.CreatedBy, StartDate: start, StoryPoints: points, Labels: r.Labels, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}, nil
}
func boardTask(t TaskRecord) projectboard.Task {
	enums := make(map[string]string)
	for id, f := range t.Fields {
		if f.Type == "ENUM" {
			var value string
			if json.Unmarshal([]byte(f.CanonicalValue), &value) == nil {
				enums[id] = value
			}
		}
	}
	return projectboard.Task{ID: t.ID, Title: t.Title, StatusID: t.StatusID, AssigneeID: t.AssigneeID, Priority: t.Priority, TypeID: t.TypeID, DueDate: t.DueDate, EnumFields: enums, Labels: append([]string(nil), t.Labels...), StoryPoints: t.StoryPoints, StartDate: t.StartDate}
}
func boardTaskMatches(q projectboard.AuthorizedTaskQuery, t TaskRecord) bool {
	if t.Archived {
		return false
	}
	if len(q.StatusIDs) > 0 && !hasString(q.StatusIDs, t.StatusID) {
		return false
	}
	f := q.Filter
	if f.AssigneeID != "" && t.AssigneeID != f.AssigneeID {
		return false
	}
	if len(f.Priorities) > 0 && !hasString(f.Priorities, t.Priority) {
		return false
	}
	if len(f.TypeIDs) > 0 && !hasString(f.TypeIDs, t.TypeID) {
		return false
	}
	for id, values := range f.EnumFields {
		if len(values) == 0 {
			continue
		}
		field, ok := t.Fields[id]
		if !ok {
			return false
		}
		var value string
		if json.Unmarshal([]byte(field.CanonicalValue), &value) != nil || !hasString(values, value) {
			return false
		}
	}
	return true
}
func hasString(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}
