package projectservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/projectconfigstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type fakeAuth struct {
	denied bool
	calls  int
	denyAt int
}

func (a *fakeAuth) Authorize(_ context.Context, _ *trust.Principal, _ string, _ projectaccess.Capability) error {
	a.calls++
	if a.denied || (a.denyAt > 0 && a.calls >= a.denyAt) {
		return errors.New("denied")
	}
	return nil
}
func (a *fakeAuth) AuthorizeCreate(_ context.Context, _ *trust.Principal) error {
	a.calls++
	if a.denied {
		return errors.New("denied")
	}
	return nil
}
func (a *fakeAuth) AuthorizeListProjects(_ context.Context, _ *trust.Principal) error {
	a.calls++
	if a.denied || (a.denyAt > 0 && a.calls >= a.denyAt) {
		return errors.New("denied")
	}
	return nil
}

type fakeStore struct {
	projects        map[string]ProjectRecord
	tasks           map[string]TaskRecord
	views           map[string]projectboard.BoardView
	lastActor       string
	lastTenant      string
	lastEdits       []project.TaskFieldEdit
	pageTasks       []projectboard.Task
	pageLimit       int
	projectReceipts map[string]ProjectRecord
}

type replayingMoveStore struct {
	*fakeStore
	replayed TaskRecord
	called   bool
}

type replayingCreateStore struct {
	*fakeStore
	replayed TaskRecord
	called   bool
}

func (r *replayingCreateStore) CreateTask(_ context.Context, _ TaskRecord, _ uint64, _, _ string) (TaskRecord, error) {
	r.called = true
	return r.replayed, nil
}

func (r *replayingMoveStore) MoveTask(_ context.Context, _, _, _, _ string, _, _ uint64, _ []project.TaskFieldEdit, _, _ string) (TaskRecord, error) {
	r.called = true
	return r.replayed, nil
}

func newFakeStore() *fakeStore {
	return &fakeStore{projects: map[string]ProjectRecord{}, tasks: map[string]TaskRecord{}, views: map[string]projectboard.BoardView{}, projectReceipts: map[string]ProjectRecord{}}
}
func taskKey(projectID, taskID string) string { return projectID + "/" + taskID }
func (f *fakeStore) GetProject(_ context.Context, tenantID, id string) (ProjectRecord, error) {
	f.lastTenant = tenantID
	p, ok := f.projects[id]
	if !ok {
		return ProjectRecord{}, errors.New("missing project")
	}
	return p, nil
}
func (f *fakeStore) GetTask(_ context.Context, tenantID, projectID, id string) (TaskRecord, error) {
	f.lastTenant = tenantID
	t, ok := f.tasks[taskKey(projectID, id)]
	if !ok {
		return TaskRecord{}, errors.New("missing task")
	}
	return t, nil
}
func (f *fakeStore) CreateProject(_ context.Context, p ProjectRecord, actor, _ string) (ProjectRecord, error) {
	f.lastActor = actor
	f.lastTenant = p.TenantID
	if _, ok := f.projects[p.ID]; ok {
		return f.projects[p.ID], nil
	}
	f.projects[p.ID] = p
	return p, nil
}
func (f *fakeStore) UpdateProjectSettings(_ context.Context, tenantID, projectID, name, timezone string, expected uint64, actor, key string) (ProjectRecord, error) {
	if replay, ok := f.projectReceipts["settings/"+key]; ok {
		return replay, nil
	}
	p, ok := f.projects[projectID]
	if !ok || p.TenantID != tenantID {
		return ProjectRecord{}, errors.New("missing project")
	}
	updated, err := p.domain().UpdateSettings(name, timezone, expected)
	if err != nil {
		return ProjectRecord{}, err
	}
	p.Name, p.Timezone, p.Revision = updated.Name, updated.Timezone, updated.Revision
	f.projects[projectID] = p
	f.projectReceipts["settings/"+key] = p
	f.lastActor = actor
	return p, nil
}
func (f *fakeStore) TransitionProject(_ context.Context, tenantID, projectID string, target project.Lifecycle, expected uint64, actor, key string) (ProjectRecord, error) {
	operation := "archive/"
	if target == project.LifecycleActive {
		operation = "restore/"
	}
	if replay, ok := f.projectReceipts[operation+key]; ok {
		return replay, nil
	}
	p, ok := f.projects[projectID]
	if !ok || p.TenantID != tenantID {
		return ProjectRecord{}, errors.New("missing project")
	}
	updated, err := p.domain().TransitionProject(target, expected, actor, project.RoleOwner, "")
	if err != nil {
		return ProjectRecord{}, err
	}
	p.State, p.Revision = updated.State, updated.Revision
	f.projects[projectID] = p
	f.projectReceipts[operation+key] = p
	f.lastActor = actor
	return p, nil
}
func (f *fakeStore) CreateTask(_ context.Context, t TaskRecord, workflowRevision uint64, actor, _ string) (TaskRecord, error) {
	f.lastActor = actor
	f.lastTenant = t.TenantID
	if workflowRevision != 7 {
		return TaskRecord{}, errors.New("wrong workflow revision")
	}
	if _, ok := f.tasks[taskKey(t.ProjectID, t.ID)]; ok {
		return f.tasks[taskKey(t.ProjectID, t.ID)], nil
	}
	f.tasks[taskKey(t.ProjectID, t.ID)] = t
	return t, nil
}
func (f *fakeStore) MoveTask(_ context.Context, tenantID, projectID, taskID, target string, expected, config uint64, edits []project.TaskFieldEdit, actor, _ string) (TaskRecord, error) {
	f.lastActor = actor
	f.lastTenant = tenantID
	f.lastEdits = append([]project.TaskFieldEdit(nil), edits...)
	t, ok := f.tasks[taskKey(projectID, taskID)]
	if !ok {
		return TaskRecord{}, errors.New("missing task")
	}
	if t.Revision != expected || config != 7 {
		return TaskRecord{}, project.ErrRevisionConflict
	}
	t.StatusID = target
	t.Revision++
	f.tasks[taskKey(projectID, taskID)] = t
	return t, nil
}
func (f *fakeStore) GetView(_ context.Context, _, _, _, id string) (projectboard.BoardView, error) {
	v, ok := f.views[id]
	if !ok {
		return v, errors.New("missing view")
	}
	return v, nil
}
func (f *fakeStore) SaveView(_ context.Context, _, _, _, _ string, v projectboard.BoardView, expected uint64, _ string) (projectboard.BoardView, error) {
	v.Version = expected + 1
	f.views[v.ID] = v
	return v, nil
}
func (f *fakeStore) ListAuthorizedTasks(_ context.Context, _, _, _ string, q projectboard.AuthorizedTaskQuery) (AuthorizedBoardTasks, *projectboard.Cursor, error) {
	f.pageLimit = q.Limit
	if len(f.pageTasks) > q.Limit {
		return AuthorizedBoardTasks{Tasks: f.pageTasks[:q.Limit], TaskRevisions: map[string]uint64{"t1": 1}, WorkflowRevision: 1, Freshness: FreshnessCurrent}, &projectboard.Cursor{Token: "next"}, nil
	}
	return AuthorizedBoardTasks{Tasks: f.pageTasks, TaskRevisions: map[string]uint64{"t1": 1}, WorkflowRevision: 1, Freshness: FreshnessCurrent}, nil, nil
}

func (f *fakeStore) ListAuthorizedProjects(context.Context, string, string, string, int) ([]ProjectRecord, error) {
	return nil, nil
}
func (f *fakeStore) ListTaskRecordsAuthorized(context.Context, string, string, string, string, TaskListFilter, int) ([]TaskRecord, error) {
	return nil, nil
}
func (f *fakeStore) ListViews(context.Context, string, string, string, string, int) ([]projectboard.BoardView, error) {
	return nil, nil
}

type fakeWorkflowPort struct {
	version      uint64
	config       projectworkflow.Config
	preview      WorkflowPreview
	publishCalls int
	lastMappings projectworkflow.MigrationMappings
	lastPlan     string
}

func (f fakeWorkflowPort) GetPublished(context.Context, string, string) (WorkflowConfiguration, error) {
	return WorkflowConfiguration{Version: f.version, Config: f.config}, nil
}
func (f fakeWorkflowPort) GetDraft(context.Context, string, string) (projectworkflow.Config, uint64, error) {
	return f.config, 1, nil
}
func (f fakeWorkflowPort) SaveDraft(context.Context, string, string, string, projectworkflow.Config, uint64, string) (uint64, error) {
	return 2, nil
}
func (f fakeWorkflowPort) PreviewDraft(context.Context, string, string, projectworkflow.Config, uint64, projectworkflow.MigrationMappings) (WorkflowPreview, error) {
	return f.preview, nil
}
func (f *fakeWorkflowPort) PublishDraft(_ context.Context, _, _, _ string, _, _ uint64, _, plan, _ string, mappings projectworkflow.MigrationMappings, _ WorkflowReviewEvidence) (WorkflowConfiguration, error) {
	f.publishCalls++
	f.lastMappings, f.lastPlan = mappings, plan
	return WorkflowConfiguration{}, nil
}

func transitionConfig() projectworkflow.Config {
	return projectworkflow.Config{
		TaskTypes:   []projectworkflow.TaskType{{ID: "task_default", Name: "Task", InitialStatus: "todo"}},
		Statuses:    []projectworkflow.Status{{ID: "todo", Name: "To do", Category: projectworkflow.CategoryNotStarted, AllowedNextStatusIDs: []string{"doing"}}, {ID: "doing", Name: "Doing", Category: projectworkflow.CategoryActive}},
		Transitions: []projectworkflow.Transition{{From: "todo", To: "doing"}},
		Columns:     []projectworkflow.Column{{ID: "todo", Name: "To do", StatusIDs: []string{"todo"}}, {ID: "doing", Name: "Doing", StatusIDs: []string{"doing"}}},
	}
}

type allowTransitions struct{}

func (allowTransitions) ValidateTaskTransition(_ project.ProjectID, _, _ string, revision uint64, _ []project.TaskFieldEdit) error {
	if revision != 7 {
		return errors.New("stale config")
	}
	return nil
}

func testPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	at := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-a", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "credential"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestService_CreateProjectUsesTrustedIdentityAndTenant(t *testing.T) {
	store := newFakeStore()
	auth := &fakeAuth{}
	svc := Service{Auth: auth, Commands: store}
	got, err := svc.CreateProject(context.Background(), testPrincipal(t), CreateProjectRequest{ID: "p1", Name: "Launch", Timezone: "UTC", IdempotencyKey: "create-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "tenant-a" || got.OwnerID != "alice" || got.State != project.LifecycleActive || got.Revision != 1 {
		t.Fatalf("unexpected project: %+v", got)
	}
	if store.lastActor != "alice" || store.lastTenant != "tenant-a" {
		t.Fatalf("scope was not derived from principal: actor=%q tenant=%q", store.lastActor, store.lastTenant)
	}
}

func TestService_CreateTaskChecksAccessBeforeProjectRead(t *testing.T) {
	store := newFakeStore()
	store.projects["p1"] = ProjectRecord{ID: "p1", TenantID: "tenant-a", OwnerID: "alice", Name: "Launch", Timezone: "UTC", State: project.LifecycleActive, Revision: 1}
	auth := &fakeAuth{denied: true}
	svc := Service{Auth: auth, Commands: store}
	_, err := svc.CreateTask(context.Background(), testPrincipal(t), CreateTaskRequest{ProjectID: "p1", ID: "t1", Title: "Plan", InitialStatusID: "todo", IdempotencyKey: "create-1"})
	if err == nil {
		t.Fatal("expected access denial")
	}
	if len(store.tasks) != 0 || store.lastTenant != "" {
		t.Fatalf("repository was read or written before authorization: %+v", store)
	}
}

func TestService_CreateTaskAllowsDurableReplayAfterWorkflowPublication(t *testing.T) {
	store := newFakeStore()
	store.projects["p1"] = ProjectRecord{ID: "p1", TenantID: "tenant-a", OwnerID: "alice", Name: "Launch", Timezone: "UTC", State: project.LifecycleActive, Revision: 1}
	replay := TaskRecord{ID: "t1", TenantID: "tenant-a", ProjectID: "p1", Title: "Plan", StatusID: "todo", Revision: 1}
	commands := &replayingCreateStore{fakeStore: store, replayed: replay}
	svc := Service{Auth: &fakeAuth{}, Commands: commands, Reads: store, Workflows: &fakeWorkflowPort{version: 8, config: transitionConfig()}}
	got, err := svc.CreateTask(context.Background(), testPrincipal(t), CreateTaskRequest{ProjectID: "p1", ID: "t1", Title: "Plan", InitialStatusID: "todo", ExpectedWorkflowRevision: 7, IdempotencyKey: "create-1"})
	if err != nil || !commands.called || got.ID != replay.ID || got.WorkflowRevision != 7 {
		t.Fatalf("receipt was not reachable after publication: got=%+v called=%v err=%v", got, commands.called, err)
	}
}

func TestService_MoveTaskValidatesWorkflowAndPersistsLaneEdit(t *testing.T) {
	store := newFakeStore()
	store.projects["p1"] = ProjectRecord{ID: "p1", TenantID: "tenant-a", OwnerID: "alice", Name: "Launch", Timezone: "UTC", State: project.LifecycleActive, Revision: 1}
	store.tasks[taskKey("p1", "t1")] = TaskRecord{ID: "t1", TenantID: "tenant-a", ProjectID: "p1", Title: "Plan", StatusID: "todo", TypeID: "task_default", Priority: "NORMAL", Revision: 3}
	svc := Service{Auth: &fakeAuth{}, Commands: store, Reads: store, Workflows: &fakeWorkflowPort{version: 7, config: transitionConfig()}, Workflow: allowTransitions{}}
	edits := []project.TaskFieldEdit{{FieldID: "team", Type: "ENUM", CanonicalValue: "design"}}
	got, err := svc.MoveTask(context.Background(), testPrincipal(t), MoveTaskRequest{ProjectID: "p1", TaskID: "t1", TargetStatusID: "doing", ExpectedTaskRevision: 3, ExpectedConfigRevision: 7, IdempotencyKey: "move-1", FieldEdits: edits})
	if err != nil {
		t.Fatal(err)
	}
	if got.StatusID != "doing" || got.Revision != 4 {
		t.Fatalf("move result not returned after commit: %+v", got)
	}
	if len(store.lastEdits) != 1 || store.lastEdits[0] != edits[0] {
		t.Fatalf("lane field edit was dropped: %+v", store.lastEdits)
	}
}

func TestService_MoveTaskRejectsStaleWorkflowBeforeWrite(t *testing.T) {
	store := newFakeStore()
	store.projects["p1"] = ProjectRecord{ID: "p1", TenantID: "tenant-a", OwnerID: "alice", Name: "Launch", Timezone: "UTC", State: project.LifecycleActive, Revision: 1}
	store.tasks[taskKey("p1", "t1")] = TaskRecord{ID: "t1", TenantID: "tenant-a", ProjectID: "p1", Title: "Plan", StatusID: "todo", TypeID: "task_default", Priority: "NORMAL", Revision: 3}
	svc := Service{Auth: &fakeAuth{}, Commands: store, Reads: store, Workflows: &fakeWorkflowPort{version: 7, config: transitionConfig()}, Workflow: allowTransitions{}}
	_, err := svc.MoveTask(context.Background(), testPrincipal(t), MoveTaskRequest{ProjectID: "p1", TaskID: "t1", TargetStatusID: "doing", ExpectedTaskRevision: 3, ExpectedConfigRevision: 6, IdempotencyKey: "move-1"})
	if err == nil {
		t.Fatal("expected stale workflow rejection")
	}
	if got := store.tasks[taskKey("p1", "t1")]; got.StatusID != "todo" || got.Revision != 3 {
		t.Fatalf("stale move changed task: %+v", got)
	}
}

func TestService_MoveTaskAllowsDurableReplayAfterTaskOrWorkflowRevisionChanges(t *testing.T) {
	for _, tc := range []struct {
		name            string
		taskRevision    uint64
		workflowVersion uint64
	}{
		{name: "task revision", taskRevision: 4, workflowVersion: 7},
		{name: "workflow version", taskRevision: 4, workflowVersion: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore()
			store.projects["p1"] = ProjectRecord{ID: "p1", TenantID: "tenant-a", OwnerID: "alice", Name: "Launch", Timezone: "UTC", State: project.LifecycleActive, Revision: 1}
			store.tasks[taskKey("p1", "t1")] = TaskRecord{ID: "t1", TenantID: "tenant-a", ProjectID: "p1", Title: "Plan", StatusID: "doing", TypeID: "task_default", Priority: "NORMAL", Revision: tc.taskRevision}
			replay := TaskRecord{ID: "t1", TenantID: "tenant-a", ProjectID: "p1", StatusID: "doing", Revision: 4, WorkflowRevision: 7}
			commands := &replayingMoveStore{fakeStore: store, replayed: replay}
			svc := Service{Auth: &fakeAuth{}, Commands: commands, Reads: store, Workflows: &fakeWorkflowPort{version: tc.workflowVersion, config: transitionConfig()}}
			got, err := svc.MoveTask(context.Background(), testPrincipal(t), MoveTaskRequest{ProjectID: "p1", TaskID: "t1", TargetStatusID: "doing", ExpectedTaskRevision: 3, ExpectedConfigRevision: 7, IdempotencyKey: "move-1"})
			if err != nil || !commands.called || got.ID != replay.ID || got.Revision != replay.Revision || got.WorkflowRevision != replay.WorkflowRevision {
				t.Fatalf("receipt was not reachable: got=%+v called=%v err=%v", got, commands.called, err)
			}
		})
	}
}

func TestService_BoardPageUsesBoundedAuthorizedSource(t *testing.T) {
	store := newFakeStore()
	store.views["v1"] = projectboard.BoardView{ID: "v1", Version: 1, Audience: projectboard.AudienceProject, Columns: []projectboard.Column{{ID: "todo", Label: "To do", StatusIDs: []string{"todo"}}}, Grouping: projectboard.Grouping{Kind: projectboard.GroupNone}, CardFields: []string{projectboard.CardTitle}}
	store.pageTasks = []projectboard.Task{{ID: "t1", Title: "Plan", StatusID: "todo"}}
	store.projects["p1"] = ProjectRecord{ID: "p1", TenantID: "tenant-a", Revision: 4}
	svc := Service{Auth: &fakeAuth{}, Reads: store, Pages: store, Workflows: &fakeWorkflowPort{version: 1}}
	page, err := svc.BoardPage(context.Background(), testPrincipal(t), "p1", "v1", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if store.pageLimit != 1 || len(page.Columns) != 1 || page.Columns[0].Count != 1 || len(page.Columns[0].Lanes) != 1 || page.Columns[0].Lanes[0].Cards[0].Task.Title != "Plan" || page.WorkflowRevision != 1 || page.ProjectRevision != 4 {
		t.Fatalf("unexpected bounded projection: %+v", page)
	}
}

func TestService_BoardPageDoesNotReadViewWhenMembershipFails(t *testing.T) {
	store := newFakeStore()
	auth := &fakeAuth{denied: true}
	svc := Service{Auth: auth, Reads: store, Pages: store, Workflows: &fakeWorkflowPort{version: 1}}
	_, err := svc.BoardPage(context.Background(), testPrincipal(t), "p1", "v1", 10, nil)
	if err == nil {
		t.Fatal("expected authorization failure")
	}
	if auth.calls != 1 || store.pageLimit != 0 {
		t.Fatalf("authorization was not checked before board data: calls=%d pageLimit=%d", auth.calls, store.pageLimit)
	}
}

func TestService_BoardPageDropsRowsWhenMembershipRevokedDuringRead(t *testing.T) {
	store := newFakeStore()
	store.views["v1"] = projectboard.BoardView{ID: "v1", Version: 1, Audience: projectboard.AudienceProject, Columns: []projectboard.Column{{ID: "todo", Label: "To do", StatusIDs: []string{"todo"}}}, Grouping: projectboard.Grouping{Kind: projectboard.GroupNone}, CardFields: []string{projectboard.CardTitle}}
	store.pageTasks = []projectboard.Task{{ID: "t1", Title: "Plan", StatusID: "todo"}}
	store.projects["p1"] = ProjectRecord{ID: "p1", TenantID: "tenant-a", Revision: 4}
	auth := &fakeAuth{denyAt: 4}
	svc := Service{Auth: auth, Reads: store, Pages: store, Workflows: &fakeWorkflowPort{version: 1}}
	page, err := svc.BoardPage(context.Background(), testPrincipal(t), "p1", "v1", 1, nil)
	if err == nil || len(page.Columns) != 0 || auth.calls != 4 {
		t.Fatalf("revoked board result leaked: page=%+v err=%v calls=%d", page, err, auth.calls)
	}
}

func TestService_GetTaskDropsRowWhenMembershipRevokedDuringRead(t *testing.T) {
	store := newFakeStore()
	store.tasks[taskKey("p1", "t1")] = TaskRecord{ID: "t1", TenantID: "tenant-a", ProjectID: "p1", Title: "Plan"}
	auth := &fakeAuth{denyAt: 2}
	svc := Service{Auth: auth, Reads: store, Workflows: &fakeWorkflowPort{version: 1}}
	task, err := svc.GetTask(context.Background(), testPrincipal(t), "p1", "t1")
	if err == nil || task.ID != "" || auth.calls != 2 {
		t.Fatalf("revoked task result leaked: task=%+v err=%v calls=%d", task, err, auth.calls)
	}
}

func TestService_ListReadsDropRowsWhenMembershipRevokedDuringRead(t *testing.T) {
	t.Run("projects", func(t *testing.T) {
		auth := &fakeAuth{denyAt: 2}
		svc := Service{Auth: auth, Reads: newFakeStore(), Workflows: &fakeWorkflowPort{version: 1}}
		page, err := svc.ListProjects(context.Background(), testPrincipal(t), "", 1)
		if err == nil || len(page.Projects) != 0 || auth.calls != 2 {
			t.Fatalf("revoked project list leaked: page=%+v err=%v calls=%d", page, err, auth.calls)
		}
	})
	t.Run("tasks", func(t *testing.T) {
		auth := &fakeAuth{denyAt: 2}
		svc := Service{Auth: auth, Reads: newFakeStore(), Workflows: &fakeWorkflowPort{version: 1}}
		page, err := svc.ListTasks(context.Background(), testPrincipal(t), "p1", "", 1)
		if err == nil || len(page.Tasks) != 0 || auth.calls != 2 {
			t.Fatalf("revoked task list leaked: page=%+v err=%v calls=%d", page, err, auth.calls)
		}
	})
}

func TestService_SaveProjectViewRequiresViewManagement(t *testing.T) {
	store := newFakeStore()
	auth := &fakeAuth{denied: true}
	svc := Service{Auth: auth, Views: store}
	v := projectboard.BoardView{ID: "v1", Version: 1, Audience: projectboard.AudienceProject, Columns: []projectboard.Column{{ID: "todo", Label: "To do", StatusIDs: []string{"todo"}}}, Grouping: projectboard.Grouping{Kind: projectboard.GroupNone}}
	_, err := svc.SaveView(context.Background(), testPrincipal(t), SaveViewRequest{ProjectID: "p1", View: v})
	if err == nil {
		t.Fatal("expected project view management denial")
	}
	if len(store.views) != 0 {
		t.Fatal("unauthorized view was persisted")
	}
}

func TestService_ProjectSettingsLifecycleUsesRevisionsAndCurrentAccess(t *testing.T) {
	store := newFakeStore()
	store.projects["p1"] = ProjectRecord{ID: "p1", TenantID: "tenant-a", OwnerID: "alice", Name: "Launch", Timezone: "UTC", State: project.LifecycleActive, Revision: 1}
	svc := Service{Auth: &fakeAuth{}, Commands: store, Reads: store}
	updated, err := svc.UpdateProjectSettings(context.Background(), testPrincipal(t), UpdateProjectSettingsRequest{ProjectID: "p1", Name: "Launch 2", Timezone: "Europe/Paris", ExpectedProjectRevision: 1, IdempotencyKey: "settings-1"})
	if err != nil || updated.Name != "Launch 2" || updated.Timezone != "Europe/Paris" || updated.Revision != 2 {
		t.Fatalf("settings update = %+v, err=%v", updated, err)
	}
	replayedSettings, err := svc.UpdateProjectSettings(context.Background(), testPrincipal(t), UpdateProjectSettingsRequest{ProjectID: "p1", Name: "Launch 2", Timezone: "Europe/Paris", ExpectedProjectRevision: 1, IdempotencyKey: "settings-1"})
	if err != nil || replayedSettings.Revision != 2 {
		t.Fatalf("settings retry = %+v, err=%v", replayedSettings, err)
	}
	if _, err := svc.ArchiveProject(context.Background(), testPrincipal(t), SetProjectLifecycleRequest{ProjectID: "p1", ExpectedProjectRevision: 2, IdempotencyKey: "archive-1"}); err != nil {
		t.Fatal(err)
	}
	archivedReplay, err := svc.ArchiveProject(context.Background(), testPrincipal(t), SetProjectLifecycleRequest{ProjectID: "p1", ExpectedProjectRevision: 2, IdempotencyKey: "archive-1"})
	if err != nil || archivedReplay.Revision != 3 {
		t.Fatalf("archive retry = %+v, err=%v", archivedReplay, err)
	}
	restored, err := svc.RestoreProject(context.Background(), testPrincipal(t), SetProjectLifecycleRequest{ProjectID: "p1", ExpectedProjectRevision: 3, IdempotencyKey: "restore-1"})
	if err != nil || restored.State != project.LifecycleActive || restored.Revision != 4 {
		t.Fatalf("restore = %+v, err=%v", restored, err)
	}
	if _, err := svc.ArchiveProject(context.Background(), testPrincipal(t), SetProjectLifecycleRequest{ProjectID: "p1", ExpectedProjectRevision: 3, IdempotencyKey: "stale-archive"}); !errors.Is(err, project.ErrRevisionConflict) {
		t.Fatalf("stale archive error = %v", err)
	}
}

func TestService_ProjectMutationChecksMembershipBeforeRead(t *testing.T) {
	store := newFakeStore()
	store.projects["p1"] = ProjectRecord{ID: "p1", TenantID: "tenant-a", OwnerID: "alice", Name: "Launch", Timezone: "UTC", State: project.LifecycleActive, Revision: 1}
	svc := Service{Auth: &fakeAuth{denied: true}, Commands: store, Reads: store}
	_, err := svc.UpdateProjectSettings(context.Background(), testPrincipal(t), UpdateProjectSettingsRequest{ProjectID: "p1", Name: "Changed", Timezone: "UTC", ExpectedProjectRevision: 1, IdempotencyKey: "settings-1"})
	if err == nil || store.lastTenant != "" || store.projects["p1"].Name != "Launch" {
		t.Fatalf("denied settings mutation reached project read/write: err=%v store=%+v", err, store)
	}
}

func TestService_PublishWorkflowAdmitsBoundedSafeTaskMigrations(t *testing.T) {
	workflow := &fakeWorkflowPort{
		version: 7,
		config:  transitionConfig(),
		preview: WorkflowPreview{Digest: "reviewed", AffectedTaskCount: 1, Safe: true},
	}
	svc := Service{Auth: &fakeAuth{}, Workflows: workflow}
	req := PublishWorkflowDraftRequest{
		ProjectID: "p1", DraftID: CurrentWorkflowDraftID, ExpectedDraftRevision: 2,
		ExpectedConfigVersion: 7, ReviewedDigest: "reviewed", ReviewedPlanDigest: "plan-reviewed", IdempotencyKey: "publish-1",
		Mappings: projectworkflow.MigrationMappings{Statuses: map[string]string{"todo": "doing"}},
	}
	if _, err := svc.PublishWorkflowDraft(context.Background(), testPrincipal(t), req); err != nil || workflow.publishCalls != 1 {
		t.Fatalf("reviewed migration did not reach atomic store: err=%v calls=%d", err, workflow.publishCalls)
	}
	if workflow.lastPlan != req.ReviewedPlanDigest || workflow.lastMappings.Statuses["todo"] != "doing" {
		t.Fatalf("reviewed mapping plan was not forwarded: digest=%q mappings=%+v", workflow.lastPlan, workflow.lastMappings)
	}
}

func TestService_PreviewWorkflowChecksExpectedDraftRevision(t *testing.T) {
	workflow := &fakeWorkflowPort{config: transitionConfig(), preview: WorkflowPreview{Safe: true, Digest: "digest", PlanDigest: "plan"}}
	svc := Service{Auth: &fakeAuth{}, Workflows: workflow}
	_, err := svc.PreviewWorkflowDraftWithMappings(context.Background(), testPrincipal(t), PreviewWorkflowDraftRequest{ProjectID: "p1", ExpectedDraftRevision: 2})
	if !errors.Is(err, projectconfigstore.ErrRevisionConflict) {
		t.Fatalf("stale expected draft revision error=%v", err)
	}
}
