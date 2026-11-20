package project

import (
	"context"
	"fmt"
	"testing"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	domainproject "github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// previewFake answers per project and task, and counts every read so a test
// can prove the batch does not repeat project or workflow reads.
type previewFake struct {
	projectservice.Service
	projects     map[string]projectservice.ProjectRecord
	projectErr   map[string]error
	tasks        map[string]projectservice.TaskRecord
	taskErr      map[string]error
	workflowErr  map[string]error
	pages        map[string][][]projectservice.TaskRecord
	principals   []*trust.Principal
	projectReads map[string]int
	configReads  map[string]int
}

func newPreviewFake() *previewFake {
	return &previewFake{
		projects: map[string]projectservice.ProjectRecord{}, projectErr: map[string]error{},
		tasks: map[string]projectservice.TaskRecord{}, taskErr: map[string]error{},
		workflowErr: map[string]error{}, pages: map[string][][]projectservice.TaskRecord{},
		projectReads: map[string]int{}, configReads: map[string]int{},
	}
}

func (f *previewFake) GetProject(_ context.Context, p *trust.Principal, id string) (projectservice.ProjectRecord, error) {
	f.principals = append(f.principals, p)
	f.projectReads[id]++
	if err := f.projectErr[id]; err != nil {
		return projectservice.ProjectRecord{}, err
	}
	record, ok := f.projects[id]
	if !ok {
		return projectservice.ProjectRecord{}, projectstore.ErrNotFound
	}
	return record, nil
}

func (f *previewFake) GetTask(_ context.Context, p *trust.Principal, projectID, taskID string) (projectservice.TaskRecord, error) {
	f.principals = append(f.principals, p)
	key := projectID + "/" + taskID
	if err := f.taskErr[key]; err != nil {
		return projectservice.TaskRecord{}, err
	}
	record, ok := f.tasks[key]
	if !ok {
		return projectservice.TaskRecord{}, projectstore.ErrNotFound
	}
	return record, nil
}

func (f *previewFake) GetWorkflowConfiguration(_ context.Context, p *trust.Principal, projectID string) (projectservice.WorkflowConfiguration, error) {
	f.principals = append(f.principals, p)
	f.configReads[projectID]++
	if err := f.workflowErr[projectID]; err != nil {
		return projectservice.WorkflowConfiguration{}, err
	}
	return projectservice.WorkflowConfiguration{Version: 1, Config: projectworkflow.Config{Statuses: []projectworkflow.Status{
		{ID: "todo", Name: "To do", Category: projectworkflow.CategoryNotStarted},
		{ID: "doing", Name: "In progress", Category: projectworkflow.CategoryActive},
		{ID: "done", Name: "Done", Category: projectworkflow.CategoryDone},
	}}}, nil
}

func (f *previewFake) ListTasks(_ context.Context, p *trust.Principal, projectID, cursor string, _ int, _ ...projectservice.TaskListFilter) (projectservice.TaskListPage, error) {
	f.principals = append(f.principals, p)
	pages := f.pages[projectID]
	index := 0
	if cursor != "" {
		_, _ = fmt.Sscanf(cursor, "page-%d", &index)
	}
	if index >= len(pages) {
		return projectservice.TaskListPage{}, nil
	}
	page := projectservice.TaskListPage{Tasks: pages[index]}
	if index+1 < len(pages) {
		page.Next = &projectservice.IDCursor{AfterID: fmt.Sprintf("page-%d", index+1)}
	}
	return page, nil
}

func activeProject(id, name string) projectservice.ProjectRecord {
	return projectservice.ProjectRecord{ID: id, Name: name, State: domainproject.LifecycleActive}
}

func previewByRef(t *testing.T, response *projectv1.GetProjectPreviewsResponse, project, task string) *projectv1.ProjectPreview {
	t.Helper()
	for _, item := range response.GetPreviews() {
		if item.GetProjectId() == project && item.GetTaskId() == task {
			return item
		}
	}
	t.Fatalf("no preview for %s/%s in %v", project, task, response.GetPreviews())
	return nil
}

func TestIssue3751_BatchAnswersEveryRefInOneCall(t *testing.T) {
	ctx, principal := testContext(t)
	fake := newPreviewFake()
	fake.projects["p1"] = activeProject("p1", "Payroll")
	fake.tasks["p1/t1"] = projectservice.TaskRecord{ID: "t1", ProjectID: "p1", Title: " Fix rounding ", StatusID: "doing", Priority: "HIGH", AssigneeID: "u9", DueDate: "2026-12-01"}
	fake.tasks["p1/t2"] = projectservice.TaskRecord{ID: "t2", ProjectID: "p1", Title: "Ship", StatusID: "done"}
	fake.pages["p1"] = [][]projectservice.TaskRecord{
		{{ID: "t1", StatusID: "doing"}, {ID: "t2", StatusID: "done"}},
		{{ID: "t3", StatusID: "done"}, {ID: "t4", StatusID: "todo", Archived: true}},
	}
	s := &server{service: fake}
	response, err := s.GetProjectPreviews(ctx, &projectv1.GetProjectPreviewsRequest{Refs: []*projectv1.ProjectPreviewRef{
		{ProjectId: "p1", TaskId: "t1"}, {ProjectId: "p1", TaskId: "t2"}, {ProjectId: "p1"}, {ProjectId: "p1", TaskId: "t1"},
	}})
	if err != nil {
		t.Fatalf("GetProjectPreviews: %v", err)
	}
	if len(response.GetPreviews()) != 3 {
		t.Fatalf("a repeated ref must be answered once, got %d previews", len(response.GetPreviews()))
	}
	task := previewByRef(t, response, "p1", "t1")
	if task.GetState() != projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY || task.GetTitle() != "Fix rounding" || task.GetProjectName() != "Payroll" ||
		task.GetStatusName() != "In progress" || task.GetStatusCategory() != projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_ACTIVE ||
		task.GetPriority() != projectv1.TaskPriority_TASK_PRIORITY_HIGH || task.GetDueDate() != "2026-12-01" || task.GetAssigneeId() != "u9" {
		t.Fatalf("task preview = %+v", task)
	}
	board := previewByRef(t, response, "p1", "")
	if board.GetState() != projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY || !board.GetProgressKnown() || board.GetDoneTasks() != 2 || board.GetTotalTasks() != 3 {
		t.Fatalf("board preview must count live tasks across pages and skip archived, got %+v", board)
	}
	if fake.projectReads["p1"] != 1 || fake.configReads["p1"] != 1 {
		t.Fatalf("project read %d times and workflow read %d times for one project; want once each", fake.projectReads["p1"], fake.configReads["p1"])
	}
	for _, seen := range fake.principals {
		if seen != principal {
			t.Fatal("a read ran as someone other than the verified caller")
		}
	}
}

func TestIssue3751_ItemsAreClassedNotCollapsed(t *testing.T) {
	ctx, _ := testContext(t)
	fake := newPreviewFake()
	fake.projects["live"] = activeProject("live", "Live")
	fake.projects["old"] = projectservice.ProjectRecord{ID: "old", Name: "Old", State: domainproject.LifecycleArchived}
	fake.projectErr["secret"] = domainproject.ErrNotAuthorized
	fake.projectErr["flaky"] = status.Error(codes.Unavailable, "down")
	fake.tasks["live/ok"] = projectservice.TaskRecord{ID: "ok", ProjectID: "live", Title: "Fine", StatusID: "todo"}
	fake.tasks["live/arch"] = projectservice.TaskRecord{ID: "arch", ProjectID: "live", Title: "Old", Archived: true}
	fake.taskErr["live/denied"] = domainproject.ErrNotAuthorized
	fake.taskErr["live/slow"] = context.DeadlineExceeded
	fake.tasks["live/other"] = projectservice.TaskRecord{ID: "different", ProjectID: "live", Title: "Wrong"}
	fake.tasks["live/blank"] = projectservice.TaskRecord{ID: "blank", ProjectID: "live", Title: "  "}
	s := &server{service: fake}
	refs := []*projectv1.ProjectPreviewRef{
		{ProjectId: "live", TaskId: "ok"}, {ProjectId: "live", TaskId: "gone"}, {ProjectId: "live", TaskId: "arch"},
		{ProjectId: "live", TaskId: "denied"}, {ProjectId: "live", TaskId: "slow"}, {ProjectId: "live", TaskId: "other"},
		{ProjectId: "live", TaskId: "blank"}, {ProjectId: "old", TaskId: "x"}, {ProjectId: "secret"}, {ProjectId: "flaky"},
		{ProjectId: "nowhere"}, {ProjectId: ""},
	}
	response, err := s.GetProjectPreviews(ctx, &projectv1.GetProjectPreviewsRequest{Refs: refs})
	if err != nil {
		t.Fatalf("one bad item must not fail the batch: %v", err)
	}
	want := map[string]projectv1.ProjectPreviewState{
		"live/ok": projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY, "live/gone": projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_MISSING,
		"live/arch": projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_ARCHIVED, "live/denied": projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_RESTRICTED,
		"live/slow": projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_FAILED, "live/other": projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_RESTRICTED,
		"live/blank": projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_RESTRICTED, "old/x": projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_ARCHIVED,
		"secret/": projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_RESTRICTED, "flaky/": projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_FAILED,
		"nowhere/": projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_MISSING, "/": projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_RESTRICTED,
	}
	if len(response.GetPreviews()) != len(want) {
		t.Fatalf("got %d previews, want %d", len(response.GetPreviews()), len(want))
	}
	for _, item := range response.GetPreviews() {
		key := item.GetProjectId() + "/" + item.GetTaskId()
		if item.GetState() != want[key] {
			t.Errorf("%s state = %v, want %v", key, item.GetState(), want[key])
		}
		if item.GetState() != projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY && item.GetState() != projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_ARCHIVED && (item.GetTitle() != "" || item.GetProjectName() != "" || item.GetAssigneeId() != "" || item.GetTotalTasks() != 0) {
			t.Errorf("%s: a non-ready answer carries detail: %+v", key, item)
		}
	}
}

func TestIssue3751_Security_RefusedItemRevealsNothing(t *testing.T) {
	ctx, _ := testContext(t)
	fake := newPreviewFake()
	fake.projects["hidden"] = activeProject("hidden", "Layoffs 2027")
	fake.projectErr["hidden"] = domainproject.ErrNotAuthorized
	s := &server{service: fake}
	response, err := s.GetProjectPreviews(ctx, &projectv1.GetProjectPreviewsRequest{Refs: []*projectv1.ProjectPreviewRef{{ProjectId: "hidden", TaskId: "t"}, {ProjectId: "hidden"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range response.GetPreviews() {
		if item.GetState() != projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_RESTRICTED || item.GetProjectName() != "" || item.GetTitle() != "" {
			t.Fatalf("refused item leaked: %+v", item)
		}
	}
	if fake.projectReads["hidden"] != 1 {
		t.Fatalf("a refused project was asked %d times; want once", fake.projectReads["hidden"])
	}
}

func TestIssue3751_Security_WorkflowRefusalKeepsCardWithoutStatus(t *testing.T) {
	ctx, _ := testContext(t)
	fake := newPreviewFake()
	fake.projects["p"] = activeProject("p", "Alpha")
	fake.tasks["p/t"] = projectservice.TaskRecord{ID: "t", ProjectID: "p", Title: "Task", StatusID: "doing"}
	fake.workflowErr["p"] = domainproject.ErrNotAuthorized
	s := &server{service: fake}
	response, err := s.GetProjectPreviews(ctx, &projectv1.GetProjectPreviewsRequest{Refs: []*projectv1.ProjectPreviewRef{{ProjectId: "p", TaskId: "t"}, {ProjectId: "p"}}})
	if err != nil {
		t.Fatal(err)
	}
	task := previewByRef(t, response, "p", "t")
	if task.GetState() != projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY || task.GetStatusName() != "" {
		t.Fatalf("task without workflow access = %+v", task)
	}
	if board := previewByRef(t, response, "p", ""); board.GetState() != projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY || board.GetProgressKnown() {
		t.Fatalf("board without workflow access must not claim progress: %+v", board)
	}
}

func TestIssue3751_Security_RequestBoundsAndAdmission(t *testing.T) {
	ctx, _ := testContext(t)
	fake := newPreviewFake()
	s := &server{service: fake}
	if _, err := s.GetProjectPreviews(context.Background(), &projectv1.GetProjectPreviewsRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("no trusted context = %v, want Unauthenticated", err)
	}
	refs := make([]*projectv1.ProjectPreviewRef, MaxProjectPreviewRefs+1)
	for i := range refs {
		refs[i] = &projectv1.ProjectPreviewRef{ProjectId: fmt.Sprintf("p%d", i)}
	}
	if _, err := s.GetProjectPreviews(ctx, &projectv1.GetProjectPreviewsRequest{Refs: refs}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversize batch = %v, want InvalidArgument", err)
	}
	if len(fake.principals) != 0 {
		t.Fatal("an oversize batch reached the application service")
	}
	if _, err := (&server{}).GetProjectPreviews(ctx, &projectv1.GetProjectPreviewsRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("no service = %v, want Unavailable", err)
	}
}

func TestIssue3751_CancelledBatchIsRetryableNotRestricted(t *testing.T) {
	ctx, _ := testContext(t)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	fake := newPreviewFake()
	fake.projects["p"] = activeProject("p", "Alpha")
	s := &server{service: fake}
	response, err := s.GetProjectPreviews(ctx, &projectv1.GetProjectPreviewsRequest{Refs: []*projectv1.ProjectPreviewRef{{ProjectId: "p", TaskId: "t"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := response.GetPreviews()[0].GetState(); got != projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_FAILED {
		t.Fatalf("cancelled batch state = %v, want FAILED", got)
	}
}

// TestIssue3751_Security_BoardReadsAreBoundedPerRequest proves a batch of large
// boards cannot fan out into unbounded task-page reads.
func TestIssue3751_Security_BoardReadsAreBoundedPerRequest(t *testing.T) {
	ctx, _ := testContext(t)
	fake := newPreviewFake()
	refs := make([]*projectv1.ProjectPreviewRef, 0, MaxProjectPreviewRefs)
	for i := 0; i < MaxProjectPreviewRefs; i++ {
		id := fmt.Sprintf("big%d", i)
		fake.projects[id] = activeProject(id, id)
		pages := make([][]projectservice.TaskRecord, previewProgressPages)
		for page := range pages {
			pages[page] = []projectservice.TaskRecord{{ID: fmt.Sprintf("%s-%d", id, page), StatusID: "todo"}}
		}
		fake.pages[id] = pages
		refs = append(refs, &projectv1.ProjectPreviewRef{ProjectId: id})
	}
	counting := &pageCountingFake{previewFake: fake}
	s := &server{service: counting}
	response, err := s.GetProjectPreviews(ctx, &projectv1.GetProjectPreviewsRequest{Refs: refs})
	if err != nil {
		t.Fatal(err)
	}
	if counting.listCalls > previewRequestPages {
		t.Fatalf("one request read %d task pages; the bound is %d", counting.listCalls, previewRequestPages)
	}
	if len(response.GetPreviews()) != MaxProjectPreviewRefs {
		t.Fatalf("every board must still be answered, got %d", len(response.GetPreviews()))
	}
	counted := 0
	for _, item := range response.GetPreviews() {
		if item.GetState() != projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY {
			t.Fatalf("a board past the budget must still be a ready card, got %+v", item)
		}
		if item.GetProgressKnown() {
			counted++
		} else if item.GetTotalTasks() != 0 || item.GetDoneTasks() != 0 {
			t.Fatalf("an unfinished count must not claim totals, got %+v", item)
		}
	}
	if counted != previewRequestPages/previewProgressPages {
		t.Fatalf("%d boards were counted; the budget covers %d", counted, previewRequestPages/previewProgressPages)
	}
}

type pageCountingFake struct {
	*previewFake
	listCalls int
}

func (f *pageCountingFake) ListTasks(ctx context.Context, p *trust.Principal, projectID, cursor string, limit int, filter ...projectservice.TaskListFilter) (projectservice.TaskListPage, error) {
	f.listCalls++
	return f.previewFake.ListTasks(ctx, p, projectID, cursor, limit, filter...)
}
