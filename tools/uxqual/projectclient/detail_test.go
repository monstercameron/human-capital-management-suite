package projectclient

import (
	"testing"
	"time"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestEnrichDetailResolvesPeopleAndEditingIdentity(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	task := &projectv1.ProjectTask{TaskId: "t-1", ProjectId: "p-1", StatusId: "todo", AssigneeId: "hc-003-evelyn-morgan", Revision: 7, WorkflowRevision: 2, Priority: projectv1.TaskPriority_TASK_PRIORITY_HIGH, TaskTypeId: "task_default"}
	detail := EnrichDetail(projectui.DetailModel{Title: "Task"}, DetailInputs{
		Task: task, ProjectID: "p-1", ProjectName: "Launch", WorkflowRevision: 3,
		Statuses:  []*projectv1.ProjectStatus{{StatusId: "todo", Name: "To do", AllowedNextStatusIds: []string{"doing"}}, {StatusId: "doing", Name: "In progress"}, {StatusId: "done", Name: "Done"}},
		TaskTypes: []*projectv1.ProjectTaskType{{TaskTypeId: "task_default", Name: "Task"}},
		Comments: []*projectv1.ProjectTaskComment{
			{CommentId: "c-2", CurrentRevision: 1, SourceText: "second draft", ActorId: "hc-050-rafael-torres", CreatedAt: timestamppb.New(now.Add(time.Hour))},
			{CommentId: "c-2", CurrentRevision: 2, SourceText: "second", ActorId: "hc-050-rafael-torres", CreatedAt: timestamppb.New(now.Add(2 * time.Hour))},
			{CommentId: "c-1", CurrentRevision: 1, SourceText: "first", ActorId: "hc-003-evelyn-morgan", CreatedAt: timestamppb.New(now)},
			{CommentId: "c-3", CurrentRevision: 1, SourceText: "gone", ActorId: "hc-003-evelyn-morgan", CreatedAt: timestamppb.New(now)},
			{CommentId: "c-3", CurrentRevision: 2, Tombstone: true, ActorId: "hc-003-evelyn-morgan"},
		},
		Members: []*projectv1.ProjectMember{{UserId: "hc-050-rafael-torres", State: "ACTIVE"}, {UserId: "hc-009-nia-brooks", State: "INVITED"}, {UserId: "hc-003-evelyn-morgan", State: "ACTIVE"}},
		Names:   map[string]string{"hc-050-rafael-torres": "Rafael Torres"},
		Viewer:  "hc-050-rafael-torres", CanEdit: true,
	})
	if detail.TaskRevision != 7 || detail.WorkflowRevision != 3 || detail.ProjectName != "Launch" || detail.Type != "Task" {
		t.Fatalf("identity = %+v", detail)
	}
	if len(detail.StatusOptions) != 2 || detail.StatusOptions[1].ID != "doing" || !detail.CanMoveStatus {
		t.Fatalf("status options = %+v", detail.StatusOptions)
	}
	if detail.Assignee != "Evelyn Morgan" || detail.PriorityID != "TASK_PRIORITY_HIGH" {
		t.Fatalf("assignee/priority = %q %q", detail.Assignee, detail.PriorityID)
	}
	if len(detail.Members) != 2 || detail.Members[0].Name != "Evelyn Morgan" || detail.Members[1].Name != "Rafael Torres" {
		t.Fatalf("members = %+v", detail.Members)
	}
	if len(detail.Comments) != 2 || detail.Comments[0].Body != "first" || !detail.Comments[1].Own || !detail.Comments[1].Edited || detail.Comments[1].Revision != 2 || detail.Comments[1].Body != "second" || detail.Comments[0].Own {
		t.Fatalf("comments = %+v", detail.Comments)
	}
}

func TestPersonNameNeverShowsRawIDs(t *testing.T) {
	for id, want := range map[string]string{"hc-021-maya-patel": "Maya Patel", "employee:hc-005-mei-chen": "Mei Chen", "1234": "Former member"} {
		if got := PersonName(id, nil); got != want {
			t.Errorf("PersonName(%q) = %q, want %q", id, got, want)
		}
	}
}
