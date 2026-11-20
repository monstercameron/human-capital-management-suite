package main

import (
	"strings"
	"testing"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestIssue3747_BoardsListOnlyOpenActiveProjectsMatchingName(t *testing.T) {
	projects := []*projectv1.Project{
		{ProjectId: "p2", Name: "Roadmap", Lifecycle: projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ACTIVE},
		{ProjectId: "p1", Name: "Hiring road", Lifecycle: projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ACTIVE},
		{ProjectId: "p3", Name: "Old roadmap", Lifecycle: projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ARCHIVED},
		{ProjectId: "p4", Name: "Payroll", Lifecycle: projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ACTIVE},
		{ProjectId: "", Name: "Road ghost", Lifecycle: projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ACTIVE},
		nil,
	}
	got := workSuggestBoards(projects, " ROAD ", 8)
	if len(got) != 2 || got[0].Title != "Hiring road" || got[1].Title != "Roadmap" {
		t.Fatalf("boards = %+v", got)
	}
	if got[0].Insert != chatui.ProjectBoardReferenceURL("p1") {
		t.Fatalf("insert = %q", got[0].Insert)
	}
	if all := workSuggestBoards(projects, "", 1); len(all) != 1 {
		t.Fatalf("limit ignored: %+v", all)
	}
}

func TestIssue3747_TicketsMatchByKeyOrTitleAndSkipArchived(t *testing.T) {
	project := &projectv1.Project{ProjectId: "p1", Name: "Core"}
	tasks := []*projectv1.ProjectTask{
		{TaskId: "ab12cd34-0000", Title: "Fix login"},
		{TaskId: "ef56ab78-0000", Title: "Update docs"},
		{TaskId: "99999999-0000", Title: "Fix archived", Archived: true},
		{TaskId: "11111111-0000", Title: "  "},
		nil,
	}
	byTitle := workSuggestTickets(project, tasks, "fix")
	if len(byTitle) != 1 || byTitle[0].Title != "Fix login" || !strings.Contains(byTitle[0].Detail, "T-AB12CD34") || !strings.Contains(byTitle[0].Detail, "Core") {
		t.Fatalf("by title = %+v", byTitle)
	}
	byKey := workSuggestTickets(project, tasks, "t-ef56")
	if len(byKey) != 1 || byKey[0].Title != "Update docs" || byKey[0].Insert != chatui.ProjectTaskReferenceURL("p1", "ef56ab78-0000") {
		t.Fatalf("by key = %+v", byKey)
	}
	if got := workSuggestTickets(project, tasks, "ab12"); len(got) != 1 {
		t.Fatalf("bare key fragment = %+v", got)
	}
}

func TestIssue3747_TicketSearchAsksForTextUnlessQueryIsAKey(t *testing.T) {
	if workTicketServerText("Fix login") != "Fix login" || workTicketServerText("t-ab12") != "" || workTicketServerText("T-") != "" {
		t.Fatal("key-shaped queries must not be sent as title text")
	}
}

func TestIssue3747_TicketSearchFansOutOverCappedProjects(t *testing.T) {
	var projects []*projectv1.Project
	for i := 0; i < 20; i++ {
		id := string(rune('a' + i))
		projects = append(projects, &projectv1.Project{ProjectId: id, Name: id, Lifecycle: projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ACTIVE})
	}
	if got := workTicketProjects(projects); len(got) != workTicketProjectCap {
		t.Fatalf("searched %d projects", len(got))
	}
}
