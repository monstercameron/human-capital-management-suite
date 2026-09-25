package project

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	domainproject "github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeService struct {
	projectservice.Service
	err     error
	seen    *trust.Principal
	filter  projectservice.TaskListFilter
	created projectservice.CreateTaskRequest
	preview projectservice.PreviewWorkflowDraftRequest
	publish projectservice.PublishWorkflowDraftRequest
	addLink projectservice.AddTaskLinkRequest
}

type fakeTaskSearchService struct {
	principal *trust.Principal
	request   projectsearch.Request
	page      projectsearch.Page
	err       error
}

func (f *fakeTaskSearchService) Search(_ context.Context, p *trust.Principal, req projectsearch.Request) (projectsearch.Page, error) {
	f.principal, f.request = p, req
	return f.page, f.err
}

func (f *fakeService) record(p *trust.Principal) error { f.seen = p; return f.err }
func (f *fakeService) CreateProject(_ context.Context, p *trust.Principal, r projectservice.CreateProjectRequest) (projectservice.ProjectRecord, error) {
	if e := f.record(p); e != nil {
		return projectservice.ProjectRecord{}, e
	}
	return projectservice.ProjectRecord{ID: "p1", TenantID: "tenant-a", OwnerID: "subject-a", Name: r.Name, Timezone: r.Timezone, State: domainproject.LifecycleActive, Revision: 1}, nil
}
func (f *fakeService) UpdateProjectSettings(_ context.Context, p *trust.Principal, r projectservice.UpdateProjectSettingsRequest) (projectservice.ProjectRecord, error) {
	if e := f.record(p); e != nil {
		return projectservice.ProjectRecord{}, e
	}
	return projectservice.ProjectRecord{ID: r.ProjectID, TenantID: "tenant-a", OwnerID: "subject-a", Name: r.Name, Timezone: r.Timezone, State: domainproject.LifecycleActive, Revision: r.ExpectedProjectRevision + 1}, nil
}
func (f *fakeService) ArchiveProject(_ context.Context, p *trust.Principal, r projectservice.SetProjectLifecycleRequest) (projectservice.ProjectRecord, error) {
	if e := f.record(p); e != nil {
		return projectservice.ProjectRecord{}, e
	}
	return projectservice.ProjectRecord{ID: r.ProjectID, TenantID: "tenant-a", OwnerID: "subject-a", State: domainproject.LifecycleArchived, Revision: r.ExpectedProjectRevision + 1}, nil
}
func (f *fakeService) RestoreProject(_ context.Context, p *trust.Principal, r projectservice.SetProjectLifecycleRequest) (projectservice.ProjectRecord, error) {
	if e := f.record(p); e != nil {
		return projectservice.ProjectRecord{}, e
	}
	return projectservice.ProjectRecord{ID: r.ProjectID, TenantID: "tenant-a", OwnerID: "subject-a", State: domainproject.LifecycleActive, Revision: r.ExpectedProjectRevision + 1}, nil
}
func (f *fakeService) CreateTask(_ context.Context, p *trust.Principal, r projectservice.CreateTaskRequest) (projectservice.TaskRecord, error) {
	if e := f.record(p); e != nil {
		return projectservice.TaskRecord{}, e
	}
	f.created = r
	return projectservice.TaskRecord{ID: "t1", TenantID: "tenant-a", ProjectID: r.ProjectID, Title: r.Title, StatusID: r.InitialStatusID, TypeID: r.TypeID, Priority: r.Priority, Revision: 1}, nil
}
func (f *fakeService) MoveTask(_ context.Context, p *trust.Principal, r projectservice.MoveTaskRequest) (projectservice.TaskRecord, error) {
	if e := f.record(p); e != nil {
		return projectservice.TaskRecord{}, e
	}
	return projectservice.TaskRecord{ID: r.TaskID, ProjectID: r.ProjectID, StatusID: r.TargetStatusID, Revision: r.ExpectedTaskRevision + 1}, nil
}
func (f *fakeService) PatchTask(_ context.Context, p *trust.Principal, r projectservice.PatchTaskRequest) (projectservice.TaskRecord, error) {
	if e := f.record(p); e != nil {
		return projectservice.TaskRecord{}, e
	}
	return projectservice.TaskRecord{ID: r.TaskID, ProjectID: r.ProjectID, Title: *r.Patch.Title, Revision: r.ExpectedTaskRevision + 1, WorkflowRevision: r.ExpectedWorkflowRevision}, nil
}
func (f *fakeService) ArchiveTask(_ context.Context, p *trust.Principal, r projectservice.SetTaskArchivedRequest) (projectservice.TaskRecord, error) {
	if e := f.record(p); e != nil {
		return projectservice.TaskRecord{}, e
	}
	return projectservice.TaskRecord{ID: r.TaskID, ProjectID: r.ProjectID, Revision: r.ExpectedTaskRevision + 1, Archived: true}, nil
}
func (f *fakeService) RestoreTask(_ context.Context, p *trust.Principal, r projectservice.SetTaskArchivedRequest) (projectservice.TaskRecord, error) {
	if e := f.record(p); e != nil {
		return projectservice.TaskRecord{}, e
	}
	return projectservice.TaskRecord{ID: r.TaskID, ProjectID: r.ProjectID, Revision: r.ExpectedTaskRevision + 1}, nil
}
func (f *fakeService) GetProject(_ context.Context, p *trust.Principal, id string) (projectservice.ProjectRecord, error) {
	if e := f.record(p); e != nil {
		return projectservice.ProjectRecord{}, e
	}
	return projectservice.ProjectRecord{ID: id, TenantID: "tenant-a", Name: "Project", Timezone: "UTC", State: domainproject.LifecycleActive, Revision: 2}, nil
}
func (f *fakeService) ListProjects(_ context.Context, p *trust.Principal, _ string, _ int) (projectservice.ProjectListPage, error) {
	if e := f.record(p); e != nil {
		return projectservice.ProjectListPage{}, e
	}
	return projectservice.ProjectListPage{Projects: []projectservice.ProjectRecord{{ID: "p1", Name: "Project", Timezone: "UTC", State: domainproject.LifecycleActive, Revision: 2}}}, nil
}
func (f *fakeService) GetTask(_ context.Context, p *trust.Principal, projectID, taskID string) (projectservice.TaskRecord, error) {
	if e := f.record(p); e != nil {
		return projectservice.TaskRecord{}, e
	}
	return projectservice.TaskRecord{ID: taskID, ProjectID: projectID, Title: "Task", StatusID: "todo", Priority: "NORMAL", Revision: 3}, nil
}
func (f *fakeService) ListTasks(_ context.Context, p *trust.Principal, projectID, _ string, _ int, filters ...projectservice.TaskListFilter) (projectservice.TaskListPage, error) {
	if e := f.record(p); e != nil {
		return projectservice.TaskListPage{}, e
	}
	if len(filters) > 0 {
		f.filter = filters[0]
	}
	return projectservice.TaskListPage{Tasks: []projectservice.TaskRecord{{ID: "t1", ProjectID: projectID, Title: "Task", StatusID: "todo", Priority: "NORMAL", Revision: 3}}}, nil
}
func (f *fakeService) SaveWorkflowDraft(_ context.Context, p *trust.Principal, _ projectservice.SaveWorkflowDraftRequest) (uint64, error) {
	if e := f.record(p); e != nil {
		return 0, e
	}
	return 4, nil
}
func (f *fakeService) GetWorkflowDraft(_ context.Context, p *trust.Principal, _ string, _ ...string) (projectworkflow.Config, uint64, error) {
	if e := f.record(p); e != nil {
		return projectworkflow.Config{}, 0, e
	}
	return projectworkflow.Config{}, 4, nil
}
func (f *fakeService) PreviewWorkflowDraftWithMappings(_ context.Context, p *trust.Principal, req projectservice.PreviewWorkflowDraftRequest) (projectservice.WorkflowPreview, error) {
	if e := f.record(p); e != nil {
		return projectservice.WorkflowPreview{}, e
	}
	f.preview = req
	return projectservice.WorkflowPreview{DraftRevision: 4, Digest: "digest-v1", PlanDigest: "plan-v1", AffectedTaskIDs: []string{"task-a"}}, nil
}
func (f *fakeService) PublishWorkflowDraft(_ context.Context, p *trust.Principal, req projectservice.PublishWorkflowDraftRequest) (projectservice.WorkflowConfiguration, error) {
	if e := f.record(p); e != nil {
		return projectservice.WorkflowConfiguration{}, e
	}
	f.publish = req
	return projectservice.WorkflowConfiguration{Version: 5}, nil
}
func (f *fakeService) GetWorkflowConfiguration(_ context.Context, p *trust.Principal, _ string) (projectservice.WorkflowConfiguration, error) {
	if e := f.record(p); e != nil {
		return projectservice.WorkflowConfiguration{}, e
	}
	return projectservice.WorkflowConfiguration{Version: 5}, nil
}
func (f *fakeService) SaveView(_ context.Context, p *trust.Principal, r projectservice.SaveViewRequest) (projectboard.BoardView, error) {
	if e := f.record(p); e != nil {
		return projectboard.BoardView{}, e
	}
	r.View.Version = 7
	return r.View, nil
}
func (f *fakeService) ReadView(_ context.Context, p *trust.Principal, _, _ string) (projectboard.BoardView, error) {
	if e := f.record(p); e != nil {
		return projectboard.BoardView{}, e
	}
	return projectboard.BoardView{ID: "v1", Version: 7, Audience: projectboard.AudiencePersonal, Columns: []projectboard.Column{{ID: "c1", Label: "Todo", StatusIDs: []string{"todo"}}}}, nil
}
func (f *fakeService) ListBoardViews(_ context.Context, p *trust.Principal, _, _ string, _ int) (projectservice.BoardViewList, error) {
	if e := f.record(p); e != nil {
		return projectservice.BoardViewList{}, e
	}
	return projectservice.BoardViewList{Views: []projectboard.BoardView{{ID: "v1", Version: 7, Audience: projectboard.AudiencePersonal}}}, nil
}
func (f *fakeService) BoardPage(_ context.Context, p *trust.Principal, _, _ string, _ int, _ *projectboard.Cursor) (projectservice.BoardPageResult, error) {
	if e := f.record(p); e != nil {
		return projectservice.BoardPageResult{}, e
	}
	return projectservice.BoardPageResult{Page: projectboard.Page{ViewID: "v1", Version: 7, Columns: []projectboard.BoardColumn{{ID: "c1", Label: "Todo", Lanes: []projectboard.Lane{{Cards: []projectboard.Card{{Task: projectboard.Task{ID: "t1", Title: "Task", StatusID: "todo", Priority: "NORMAL"}}}}}}}}, ProjectRevision: 8, WorkflowRevision: 5, TaskRevisions: map[string]uint64{"t1": 3}}, nil
}
func (f *fakeService) AddTaskLink(_ context.Context, p *trust.Principal, req projectservice.AddTaskLinkRequest) (string, uint64, error) {
	if err := f.record(p); err != nil {
		return "", 0, err
	}
	f.addLink = req
	return "link-1", 4, nil
}
func (f *fakeService) RemoveTaskLink(_ context.Context, p *trust.Principal, _ projectservice.RemoveTaskLinkRequest) (uint64, error) {
	if err := f.record(p); err != nil {
		return 0, err
	}
	return 5, nil
}
func (f *fakeService) ListTaskLinks(_ context.Context, p *trust.Principal, _, _, _ string, _ int) ([]projectservice.TaskLink, *projectservice.IDCursor, error) {
	if e := f.record(p); e != nil {
		return nil, nil, e
	}
	return []projectservice.TaskLink{{ID: "link-1", Reference: projectlink.Reference{Kind: projectlink.ChatConversation, ID: "conv-1"}, Resolution: projectlink.Result{State: projectlink.Restricted}}}, nil, nil
}

func testContext(t *testing.T) (context.Context, *trust.Principal) {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "subject-a", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-a", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "credential-a"})
	if err != nil {
		t.Fatal(err)
	}
	request := &projectv1.GetProjectRequest{}
	ctx, _, admitErr := transport.Admit(context.Background(), transport.Config{Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return p, nil }), NewRequestID: func() string { return "request-a" }}, transport.AdmissionRequest{Metadata: transport.MapMetadata{transport.AuthorizationMetadataKey: {"Bearer test-token"}}, Method: projectv1.ProjectService_GetProject_FullMethodName, Kind: transport.KindGRPC, Message: request})
	if admitErr != nil {
		t.Fatal(admitErr)
	}
	return ctx, p
}

func TestTypedProjectFieldWireRoundTrip(t *testing.T) {
	expected := map[string]string{"ticket": `"123"`, "phase": `"ready"`}
	for _, value := range []*projectv1.TypedFieldValue{
		{FieldId: "ticket", Value: &projectv1.TypedFieldValue_TextValue{TextValue: "123"}},
		{FieldId: "phase", Value: &projectv1.TypedFieldValue_EnumValue{EnumValue: "ready"}},
	} {
		edits, err := typedFieldEdits([]*projectv1.TypedFieldValue{value})
		if err != nil || len(edits) != 1 {
			t.Fatalf("decode %v: edits=%v err=%v", value, edits, err)
		}
		if edits[0].CanonicalValue != expected[value.GetFieldId()] {
			t.Fatalf("canonical string was not JSON encoded: %+v", edits[0])
		}
		got := typedFieldMessage(value.GetFieldId(), edits[0])
		if got == nil || got.String() != value.String() {
			t.Fatalf("wire round trip: want=%v got=%v", value, got)
		}
	}
}

func TestAddTaskLinkAcceptsWorkOrderReference(t *testing.T) {
	ctx, principal := testContext(t)
	fake := &fakeService{}
	s := &server{service: fake}
	response, err := s.AddTaskLink(ctx, &projectv1.AddTaskLinkRequest{
		ProjectId: "project-1", TaskId: "task-1", ExpectedTaskRevision: 9, IdempotencyKey: "link-wo-1",
		Reference: &projectv1.TaskLinkReference{Target: &projectv1.TaskLinkReference_WorkOrder{WorkOrder: &projectv1.WorkOrderLink{WorkOrderId: "wo-1"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetLinkId() != "link-1" || fake.seen != principal || fake.addLink.Reference.Kind != projectlink.WorkOrder || fake.addLink.Reference.ID != "wo-1" {
		t.Fatalf("work order link was not delegated intact: response=%+v request=%+v principal=%p", response, fake.addLink, fake.seen)
	}
}

func TestProjectRPCsDelegateWithVerifiedPrincipal(t *testing.T) {
	ctx, principal := testContext(t)
	fake := &fakeService{}
	s := &server{service: fake}
	calls := []func() error{
		func() error {
			r, e := s.GetProject(ctx, &projectv1.GetProjectRequest{ProjectId: "p1"})
			if e == nil && r.GetProject().GetProjectId() != "p1" {
				t.Errorf("project response=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.ListProjects(ctx, &projectv1.ListProjectsRequest{Page: &commonv1.PageRequest{PageSize: 10}})
			if e == nil && len(r.GetProjects()) != 1 {
				t.Errorf("list projects=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.CreateProject(ctx, &projectv1.CreateProjectRequest{Name: "N", ProjectTimezone: "UTC", IdempotencyKey: "i1"})
			if e == nil && r.GetProject().GetProjectId() != "p1" {
				t.Errorf("create project=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.UpdateProjectSettings(ctx, &projectv1.UpdateProjectSettingsRequest{ProjectId: "p1", Name: "N2", ProjectTimezone: "Europe/Paris", ExpectedProjectRevision: 1, IdempotencyKey: "i2-settings"})
			if e == nil && (r.GetProject().GetName() != "N2" || r.GetProject().GetRevision() != 2) {
				t.Errorf("update project settings=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.ArchiveProject(ctx, &projectv1.SetProjectLifecycleRequest{ProjectId: "p1", ExpectedProjectRevision: 2, IdempotencyKey: "i2-archive"})
			if e == nil && r.GetProject().GetLifecycle() != projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ARCHIVED {
				t.Errorf("archive project=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.RestoreProject(ctx, &projectv1.SetProjectLifecycleRequest{ProjectId: "p1", ExpectedProjectRevision: 3, IdempotencyKey: "i2-restore"})
			if e == nil && r.GetProject().GetLifecycle() != projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ACTIVE {
				t.Errorf("restore project=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.CreateTask(ctx, &projectv1.CreateTaskRequest{ProjectId: "p1", Title: "Task", TaskTypeId: "type", InitialStatusId: "todo", ExpectedWorkflowRevision: 2, IdempotencyKey: "i2", SourceReference: &projectv1.ProjectSourceReference{Source: &projectv1.ProjectSourceReference_ChatPost{ChatPost: &projectv1.ChatPostLink{ConversationId: "conv-1", PostId: "post-1"}}}})
			if e == nil && r.GetTask().GetTaskId() != "t1" {
				t.Errorf("create task=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.GetTask(ctx, &projectv1.GetTaskRequest{ProjectId: "p1", TaskId: "t1"})
			if e == nil && r.GetTask().GetTaskId() != "t1" {
				t.Errorf("get task=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.ListTasks(ctx, &projectv1.ListTasksRequest{ProjectId: "p1", Page: &commonv1.PageRequest{PageSize: 20}})
			if e == nil && len(r.GetTasks()) != 1 {
				t.Errorf("list tasks=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.MoveTask(ctx, &projectv1.MoveTaskRequest{ProjectId: "p1", TaskId: "t1", TargetStatusId: "doing", ExpectedTaskRevision: 3, ExpectedWorkflowRevision: 2, IdempotencyKey: "i3", LaneFieldEdit: &projectv1.TypedFieldValue{FieldId: "lane", Value: &projectv1.TypedFieldValue_EnumValue{EnumValue: "x"}}})
			if e == nil && r.GetTask().GetRevision() != 4 {
				t.Errorf("move task=%+v", r)
			}
			return e
		},
		func() error {
			title := "Revised"
			r, e := s.PatchTask(ctx, &projectv1.PatchTaskRequest{ProjectId: "p1", TaskId: "t1", ExpectedTaskRevision: 4, ExpectedWorkflowRevision: 2, IdempotencyKey: "patch-1", Title: &title})
			if e == nil && (r.GetTask().GetTitle() != title || r.GetTask().GetRevision() != 5) {
				t.Errorf("patch task=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.ArchiveTask(ctx, &projectv1.ArchiveTaskRequest{ProjectId: "p1", TaskId: "t1", ExpectedTaskRevision: 5, ExpectedWorkflowRevision: 2, IdempotencyKey: "archive-1"})
			if e == nil && !r.GetTask().GetArchived() {
				t.Errorf("archive task=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.RestoreTask(ctx, &projectv1.RestoreTaskRequest{ProjectId: "p1", TaskId: "t1", ExpectedTaskRevision: 6, ExpectedWorkflowRevision: 2, IdempotencyKey: "restore-1"})
			if e == nil && r.GetTask().GetArchived() {
				t.Errorf("restore task=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.SaveWorkflowDraft(ctx, &projectv1.SaveWorkflowDraftRequest{ProjectId: "p1", ExpectedDraftRevision: 3, IdempotencyKey: "i4", Configuration: &projectv1.WorkflowConfigurationSpec{}})
			if e == nil && r.GetDraftRevision() != 4 {
				t.Errorf("save draft=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.GetWorkflowDraft(ctx, &projectv1.GetWorkflowDraftRequest{ProjectId: "p1", DraftId: "draft"})
			if e == nil && r.GetDraftRevision() != 4 {
				t.Errorf("get draft=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.PreviewWorkflowDraft(ctx, &projectv1.PreviewWorkflowDraftRequest{ProjectId: "p1", DraftId: "draft", ExpectedDraftRevision: 4, StatusMappings: []*projectv1.WorkflowStatusMigrationMapping{{SourceStatusId: "todo", TargetStatusId: "doing"}}, FieldMappings: []*projectv1.WorkflowFieldMigrationMapping{{SourceFieldId: "owner", TargetFieldId: "owner_v2"}}})
			if e == nil && (!r.GetValid() || r.GetMigrationPlanDigest() != "plan-v1" || len(r.GetAffectedTaskIds()) != 1 || r.GetAffectedTaskIds()[0] != "task-a") {
				t.Errorf("preview=%+v", r)
			}
			if e == nil && (fake.preview.ExpectedDraftRevision != 4 || fake.preview.Mappings.Statuses["todo"] != "doing" || fake.preview.Mappings.Fields["owner"] != "owner_v2") {
				t.Errorf("preview revision or mappings not forwarded: %+v", fake.preview)
			}
			return e
		},
		func() error {
			r, e := s.PublishWorkflowDraft(ctx, &projectv1.PublishWorkflowDraftRequest{ProjectId: "p1", DraftId: "draft", ExpectedProjectWorkflowRevision: 4, ExpectedDraftRevision: 4, ReviewedDigest: "digest-v1", ReviewedMigrationPlanDigest: "plan-v1", IdempotencyKey: "i5", StatusMappings: []*projectv1.WorkflowStatusMigrationMapping{{SourceStatusId: "todo", TargetStatusId: "doing"}}, FieldMappings: []*projectv1.WorkflowFieldMigrationMapping{{SourceFieldId: "owner", TargetFieldId: "owner_v2"}}})
			if e == nil && r.GetConfiguration().GetRevision() != 5 {
				t.Errorf("publish=%+v", r)
			}
			if e == nil && (fake.publish.ReviewedPlanDigest != "plan-v1" || fake.publish.Mappings.Statuses["todo"] != "doing" || fake.publish.Mappings.Fields["owner"] != "owner_v2") {
				t.Errorf("publish mappings were not forwarded: %+v", fake.publish)
			}
			return e
		},
		func() error {
			r, e := s.GetWorkflowConfiguration(ctx, &projectv1.GetWorkflowConfigurationRequest{ProjectId: "p1"})
			if e == nil && r.GetConfiguration().GetRevision() != 5 {
				t.Errorf("workflow=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.SaveBoardView(ctx, &projectv1.SaveBoardViewRequest{ProjectId: "p1", ExpectedViewRevision: 6, View: &projectv1.BoardView{ViewId: "v1", Audience: projectv1.BoardViewAudience_BOARD_VIEW_AUDIENCE_PERSONAL, Columns: []*projectv1.BoardColumn{{ColumnId: "c1", Name: "Todo", StatusIds: []string{"todo"}}}}})
			if e == nil && r.GetView().GetRevision() != 7 {
				t.Errorf("save view=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.ListBoardViews(ctx, &projectv1.ListBoardViewsRequest{ProjectId: "p1", Page: &commonv1.PageRequest{PageSize: 10}})
			if e == nil && len(r.GetViews()) != 1 {
				t.Errorf("list views=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.GetBoard(ctx, &projectv1.GetBoardRequest{ProjectId: "p1", ViewId: "v1", Page: &commonv1.PageRequest{PageSize: 10}})
			if e == nil && len(r.GetTasks()) != 1 {
				t.Errorf("board=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.AddTaskLink(ctx, &projectv1.AddTaskLinkRequest{ProjectId: "p1", TaskId: "t1", ExpectedTaskRevision: 3, IdempotencyKey: "i6", Reference: &projectv1.TaskLinkReference{Target: &projectv1.TaskLinkReference_ChatConversation{ChatConversation: &projectv1.ChatConversationLink{ConversationId: "conv-1"}}}})
			if e == nil && (r == nil || r.GetLinkId() != "link-1" || r.GetTaskRevision() != 4) {
				t.Errorf("add link response=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.RemoveTaskLink(ctx, &projectv1.RemoveTaskLinkRequest{ProjectId: "p1", TaskId: "t1", LinkId: "link-1", ExpectedTaskRevision: 4, IdempotencyKey: "remove-1"})
			if e == nil && (r == nil || r.GetTaskRevision() != 5) {
				t.Errorf("remove link response=%+v", r)
			}
			return e
		},
		func() error {
			r, e := s.ListTaskLinks(ctx, &projectv1.ListTaskLinksRequest{ProjectId: "p1", TaskId: "t1", Page: &commonv1.PageRequest{PageSize: 10}})
			if e == nil && (len(r.GetLinks()) != 1 || r.GetLinks()[0].GetState() != projectv1.TaskLinkResolutionState_TASK_LINK_RESOLUTION_STATE_RESTRICTED || r.GetLinks()[0].GetPreview() != nil) {
				t.Errorf("links=%+v", r)
			}
			return e
		},
	}
	for i, call := range calls {
		if err := call(); err != nil {
			t.Errorf("RPC %d returned %v", i, err)
		}
		if fake.seen != principal {
			t.Errorf("RPC %d did not pass verified principal", i)
		}
	}
	if fake.created.SourceReference == nil || fake.created.SourceReference.Kind != projectlink.ChatPost || fake.created.SourceReference.ConversationID != "conv-1" || fake.created.SourceReference.ID != "post-1" {
		t.Fatalf("source reference mapping=%+v", fake.created.SourceReference)
	}
}

func TestPreviewWorkflowRequiresRevisionAndRejectsDuplicateMappings(t *testing.T) {
	ctx, principal := testContext(t)
	fake := &fakeService{}
	s := &server{service: fake}
	if _, err := s.PreviewWorkflowDraft(ctx, &projectv1.PreviewWorkflowDraftRequest{ProjectId: "p1"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("preview without expected draft revision: %v", err)
	}
	if fake.seen != nil {
		t.Fatal("request without a revision reached the application service")
	}
	ctx = trust.WithPrincipal(ctx, principal)
	_, err := s.PreviewWorkflowDraft(ctx, &projectv1.PreviewWorkflowDraftRequest{
		ProjectId: "p1", ExpectedDraftRevision: 4,
		StatusMappings: []*projectv1.WorkflowStatusMigrationMapping{
			{SourceStatusId: "todo", TargetStatusId: "doing"},
			{SourceStatusId: "todo", TargetStatusId: "queue"},
		},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("duplicate status source mapping was accepted: %v", err)
	}
}

func TestProjectTransportErrorsAndBounds(t *testing.T) {
	ctx, _ := testContext(t)
	fake := &fakeService{}
	s := &server{service: fake}
	if _, err := s.GetProject(context.Background(), &projectv1.GetProjectRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated=%v", err)
	}
	other, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-b"), Subject: "subject-b", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-b", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "credential-b"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetProject(trust.WithPrincipal(ctx, other), &projectv1.GetProjectRequest{ProjectId: "p1"}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("mismatched context principal=%v", err)
	}
	if fake.seen != nil {
		t.Fatalf("mismatched principal reached service: %v", fake.seen.Subject())
	}
	fake.err = projectaccess.ErrUnauthorized
	if _, err := s.GetProject(ctx, &projectv1.GetProjectRequest{ProjectId: "p1"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("denied=%v", err)
	}
	fake.err = projectservice.ErrUnavailable
	if _, err := s.GetProject(ctx, &projectv1.GetProjectRequest{ProjectId: "p1"}); status.Code(err) != codes.Unavailable {
		t.Fatalf("unavailable=%v", err)
	}
	fake.err = nil
	if _, err := s.GetBoard(ctx, &projectv1.GetBoardRequest{ProjectId: "p1", ViewId: "v1", Page: &commonv1.PageRequest{PageSize: 101}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("page bound=%v", err)
	}
	if _, err := s.MoveTask(ctx, &projectv1.MoveTaskRequest{ProjectId: "p1", TaskId: "t1", LaneFieldEdit: &projectv1.TypedFieldValue{FieldId: "x"}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("untyped lane edit=%v", err)
	}
	if _, err := s.ListTasks(ctx, &projectv1.ListTasksRequest{ProjectId: "p1", Filter: &projectv1.BoardFilter{StatusIds: []string{"todo"}, Priorities: []projectv1.TaskPriority{projectv1.TaskPriority_TASK_PRIORITY_HIGH}}}); err != nil || len(fake.filter.StatusIDs) != 1 || len(fake.filter.Priorities) != 1 {
		t.Fatalf("filtered list = %v, filter=%+v", err, fake.filter)
	}
}

func TestTodo_PM_018_SearchTasksMapsExactFiltersAndDegradedFreshness(t *testing.T) {
	ctx, principal := testContext(t)
	searcher := &fakeTaskSearchService{page: projectsearch.Page{
		Tasks: []projectsearch.Task{{ID: "task-1", TenantID: "tenant-a", ProjectID: "p1", Title: "Hire designer", StatusID: "todo", TypeID: "task", Priority: "HIGH", DueDate: "2026-03-02", Revision: 7, Fields: map[string]domainproject.TaskFieldEdit{"team": {FieldID: "team", Type: "ENUM", CanonicalValue: `"design"`}}}},
		Next:  "signed-cursor", Freshness: projectsearch.FreshnessDegraded,
	}}
	s := &server{search: searcher}
	response, err := s.SearchTasks(ctx, &projectv1.SearchTasksRequest{
		ProjectId: "p1",
		Filter:    &projectv1.SearchTaskFilter{StatusIds: []string{"todo"}, AssigneeId: "alice", TaskTypeIds: []string{"task"}, DueDateFrom: "2026-03-01", DueDateTo: "2026-03-10", Fields: []*projectv1.SearchTaskFieldFilter{{FieldId: "team", Values: []string{"design"}}}},
		Page:      &commonv1.PageRequest{PageSize: 25, Cursor: "prior-cursor"},
	})
	if err != nil || searcher.principal != principal || searcher.request.ProjectID != "p1" || searcher.request.Limit != 25 || searcher.request.Cursor != "prior-cursor" || searcher.request.Filter.AssigneeID != "alice" || searcher.request.Filter.DueDateFrom != "2026-03-01" || len(response.GetTasks()) != 1 || response.GetTasks()[0].GetCustomFields()[0].GetEnumValue() != "design" || response.GetPage().GetNextCursor() != "signed-cursor" || response.GetFreshness() != projectv1.SearchFreshness_SEARCH_FRESHNESS_DEGRADED_EXACT_FILTER {
		t.Fatalf("search response=%+v request=%+v err=%v", response, searcher.request, err)
	}
	searcher.err = projectsearch.ErrTextUnavailable
	_, err = s.SearchTasks(ctx, &projectv1.SearchTasksRequest{ProjectId: "p1", Text: "designer", Page: &commonv1.PageRequest{PageSize: 10}})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("free-text unavailable status=%v err=%v", status.Code(err), err)
	}
}

func TestProjectAdmissionRejectsCallerSelectedTenant(t *testing.T) {
	_, principal := testContext(t)
	_, _, err := transport.Admit(context.Background(), transport.Config{Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return principal, nil }), NewRequestID: func() string { return "request-forged" }}, transport.AdmissionRequest{Metadata: transport.MapMetadata{transport.AuthorizationMetadataKey: {"Bearer test-token"}}, Method: projectv1.ProjectService_GetProject_FullMethodName, Kind: transport.KindGRPC, Message: &projectv1.GetProjectRequest{Scope: &commonv1.ScopeContext{TenantId: "forged-tenant"}}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("caller-selected tenant admission=%v", err)
	}
}
