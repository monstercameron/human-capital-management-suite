package main

import (
	"errors"
	"testing"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIssue3751_RequestCarriesEveryRefOnce(t *testing.T) {
	refs := []chatui.ProjectTaskReference{{ProjectID: "p1", TaskID: "t1"}, {ProjectID: "p1"}, {ProjectID: "p2", TaskID: "t9"}}
	request := chatProjectPreviewRequest(refs)
	if len(request.GetRefs()) != 3 || request.GetRefs()[1].GetProjectId() != "p1" || request.GetRefs()[1].GetTaskId() != "" || request.GetRefs()[2].GetTaskId() != "t9" {
		t.Fatalf("request = %v", request)
	}
	if len(refs) > chatProjectTaskPreviewLimit {
		t.Fatal("test refs exceed the client claim limit")
	}
	// The server refuses a batch over 50 refs (project.MaxProjectPreviewRefs), so
	// the client must never claim more in one call.
	if chatProjectTaskPreviewLimit > 50 {
		t.Fatalf("client claims up to %d links per call but the server accepts 50", chatProjectTaskPreviewLimit)
	}
}

func TestIssue3751_BatchAnswersBuildCards(t *testing.T) {
	refs := []chatui.ProjectTaskReference{{ProjectID: "p1", TaskID: "t1"}, {ProjectID: "p1"}}
	response := &projectv1.GetProjectPreviewsResponse{Previews: []*projectv1.ProjectPreview{
		{ProjectId: "p1", TaskId: "t1", State: projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY, Title: "Fix", ProjectName: "Payroll", StatusName: "Done",
			StatusCategory: projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_DONE, Priority: projectv1.TaskPriority_TASK_PRIORITY_HIGH, DueDate: "2026-12-01", AssigneeId: "u9"},
		{ProjectId: "p1", State: projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY, Title: "Payroll", ProjectName: "Payroll", ProgressKnown: true, DoneTasks: 2, TotalTasks: 5},
	}}
	directory := func() (map[string]string, map[string]string) {
		return map[string]string{"u9": "Ada Lovelace"}, map[string]string{"u9": "photo.png"}
	}
	answers := chatProjectBatchAnswers(refs, response, nil, "u9", directory)
	task := answers[chatui.ProjectTaskPreviewKey("p1", "t1")]
	if task.State != "ready" || !task.Readable || task.Title != "Fix" || task.ProjectName != "Payroll" || task.Status != "Done" || task.StatusTone != "done" ||
		task.PriorityID != projectv1.TaskPriority_TASK_PRIORITY_HIGH.String() || task.Due != "2026-12-01" || task.Assignee != "Ada Lovelace" || task.AssigneePhoto != "photo.png" || task.Key == "" {
		t.Fatalf("task card = %+v", task)
	}
	board := answers[chatui.ProjectTaskPreviewKey("p1", "")]
	if board.State != "ready" || !board.IsBoard() || board.Done != 2 || board.Total != 5 {
		t.Fatalf("board card = %+v", board)
	}
}

func TestIssue3751_BatchAnswersClassEachState(t *testing.T) {
	state := func(s projectv1.ProjectPreviewState, id string) *projectv1.ProjectPreview {
		return &projectv1.ProjectPreview{ProjectId: "p", TaskId: id, State: s}
	}
	refs := []chatui.ProjectTaskReference{{ProjectID: "p", TaskID: "a"}, {ProjectID: "p", TaskID: "b"}, {ProjectID: "p", TaskID: "c"}, {ProjectID: "p", TaskID: "d"}, {ProjectID: "p", TaskID: "e"}, {ProjectID: "p", TaskID: "unanswered"}}
	response := &projectv1.GetProjectPreviewsResponse{Previews: []*projectv1.ProjectPreview{
		state(projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_RESTRICTED, "a"), state(projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_MISSING, "b"),
		state(projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_ARCHIVED, "c"), state(projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_FAILED, "d"),
		{ProjectId: "p", TaskId: "e", State: projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY, Title: " "},
	}}
	want := map[string]string{"a": "restricted", "b": "missing", "c": "archived", "d": "failed", "e": "restricted", "unanswered": "failed"}
	answers := chatProjectBatchAnswers(refs, response, nil, "u1", nil)
	for task, expected := range want {
		got := answers[chatui.ProjectTaskPreviewKey("p", task)]
		if got.State != expected || got.Readable || got.Title != "" {
			t.Errorf("%s = %+v, want state %s without detail", task, got, expected)
		}
	}
}

func TestIssue3751_FailedCallIsClassedNotRestricted(t *testing.T) {
	refs := []chatui.ProjectTaskReference{{ProjectID: "p", TaskID: "a"}}
	key := chatui.ProjectTaskPreviewKey("p", "a")
	cases := map[string]struct {
		err  error
		want string
	}{
		"unavailable": {status.Error(codes.Unavailable, "down"), "failed"},
		"deadline":    {status.Error(codes.DeadlineExceeded, "slow"), "failed"},
		"local":       {errors.New("socket closed"), "failed"},
		"refusal":     {status.Error(codes.PermissionDenied, "no"), "restricted"},
	}
	for name, tc := range cases {
		if got := chatProjectBatchAnswers(refs, nil, tc.err, "u1", nil)[key].State; got != tc.want {
			t.Errorf("%s: state %q, want %q", name, got, tc.want)
		}
	}
}

func TestIssue3751_AssigneeClaimRestsOnIdNotName(t *testing.T) {
	refs := []chatui.ProjectTaskReference{{ProjectID: "p", TaskID: "a"}}
	response := &projectv1.GetProjectPreviewsResponse{Previews: []*projectv1.ProjectPreview{
		{ProjectId: "p", TaskId: "a", State: projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY, Title: "T", StatusName: "Doing",
			StatusCategory: projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_ACTIVE, AssigneeId: "u1"},
	}}
	names := func() (map[string]string, map[string]string) { return map[string]string{"u1": "Pat"}, nil }
	if got := chatProjectBatchAnswers(refs, response, nil, "u1", names)[chatui.ProjectTaskPreviewKey("p", "a")]; !got.WaitsOnViewer || got.StatusTone != "doing" {
		t.Fatalf("assigned viewer = %+v", got)
	}
	if got := chatProjectBatchAnswers(refs, response, nil, "u2", names)[chatui.ProjectTaskPreviewKey("p", "a")]; got.WaitsOnViewer {
		t.Fatalf("another viewer must not be told the task waits on them: %+v", got)
	}
}

// TestIssue3750_ArchivedAnswerKeepsReadableTitle pins that an archived answer
// with a title is a readable card, and one without stays untitled.
func TestIssue3750_ArchivedAnswerKeepsReadableTitle(t *testing.T) {
	refs := []chatui.ProjectTaskReference{{ProjectID: "p", TaskID: "a"}, {ProjectID: "p"}, {ProjectID: "p", TaskID: "c"}}
	archived := projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_ARCHIVED
	response := &projectv1.GetProjectPreviewsResponse{Previews: []*projectv1.ProjectPreview{
		{ProjectId: "p", TaskId: "a", State: archived, Title: " Retired task "},
		{ProjectId: "p", State: archived, Title: "Old board"},
		{ProjectId: "p", TaskId: "c", State: archived},
	}}
	answers := chatProjectBatchAnswers(refs, response, nil, "u1", nil)
	if a := answers[chatui.ProjectTaskPreviewKey("p", "a")]; a.State != "archived" || !a.Readable || a.Title != "Retired task" {
		t.Errorf("archived task = %+v", a)
	}
	if b := answers[chatui.ProjectTaskPreviewKey("p", "")]; b.State != "archived" || !b.Readable || b.Title != "Old board" {
		t.Errorf("archived board = %+v", b)
	}
	if c := answers[chatui.ProjectTaskPreviewKey("p", "c")]; c.State != "archived" || c.Readable || c.Title != "" {
		t.Errorf("untitled archived = %+v", c)
	}
	if !chatProjectAnswerAccepted(chatui.ProjectTaskPreviewKey("p", "a"), chatui.ProjectTaskPreview{ProjectID: "p", TaskID: "a", State: "archived", Readable: true, Title: "x"}) {
		t.Error("cache must accept a titled archived answer")
	}
}
