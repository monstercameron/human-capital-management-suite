package projectlinkstore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
)

// A task can link an in-progress workflow run by its journey ID, and the
// widened kind constraint still refuses a journey carrying chat or document
// selectors.
func TestJourneyLinkPersists(t *testing.T) {
	links, projects, _ := linkFixture(t)
	ctx := context.Background()
	project := projectstore.ProjectRecord{ID: "journey-project", TenantID: "tenant-a", OwnerID: "owner", Name: "Project", Timezone: "UTC"}
	if err := projects.CreateProject(ctx, project, "owner", "HUMAN", "journey-create-project"); err != nil {
		t.Fatal(err)
	}
	task := projectstore.TaskRecord{ID: "journey-task", TenantID: "tenant-a", ProjectID: project.ID, Title: "Task", StatusID: "todo"}
	if err := projects.CreateTask(ctx, task, "owner", "HUMAN", "journey-create-task"); err != nil {
		t.Fatal(err)
	}
	ref := projectlink.Reference{Kind: projectlink.Journey, ID: "01a0d8e5-4980-7611-9f4b-995d01f979d3"}
	if err := links.Add(ctx, LinkRecord{ID: "journey-link", TenantID: "tenant-a", ProjectID: project.ID, TaskID: task.ID, Reference: ref}); err != nil {
		t.Fatalf("add journey link: %v", err)
	}
	page, err := links.List(ctx, "tenant-a", project.ID, task.ID, "", 10)
	if err != nil || len(page) != 1 || page[0].Reference != ref {
		t.Fatalf("journey link page=%#v err=%v", page, err)
	}
	bad := projectlink.Reference{Kind: projectlink.Journey, ID: "journey-2", ConversationID: "c-1"}
	if err := links.Add(ctx, LinkRecord{ID: "journey-bad", TenantID: "tenant-a", ProjectID: project.ID, TaskID: task.ID, Reference: bad}); err == nil {
		t.Fatal("journey link with a conversation selector accepted")
	}
}

func TestListByTargetFindsLinkingTasks(t *testing.T) {
	links, projects, _ := linkFixture(t)
	ctx := context.Background()
	project := projectstore.ProjectRecord{ID: "rev-project", TenantID: "tenant-a", OwnerID: "owner", Name: "Project", Timezone: "UTC"}
	if err := projects.CreateProject(ctx, project, "owner", "HUMAN", "rev-create-project"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"rev-task-1", "rev-task-2"} {
		if err := projects.CreateTask(ctx, projectstore.TaskRecord{ID: id, TenantID: "tenant-a", ProjectID: project.ID, Title: "Task " + id, StatusID: "todo"}, "owner", "HUMAN", "create-"+id); err != nil {
			t.Fatal(err)
		}
	}
	journey := projectlink.Reference{Kind: projectlink.Journey, ID: "journey-77"}
	for i, id := range []string{"rev-task-1", "rev-task-2"} {
		if err := links.Add(ctx, LinkRecord{ID: "rev-link-" + id, TenantID: "tenant-a", ProjectID: project.ID, TaskID: id, Reference: journey}); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	got, err := links.ListByTarget(ctx, "tenant-a", projectlink.Journey, "journey-77", 10)
	if err != nil || len(got) != 2 || got[0].Title != "Task rev-task-1" || got[1].StatusID != "todo" {
		t.Fatalf("reverse lookup=%#v err=%v", got, err)
	}
	if other, err := links.ListByTarget(ctx, "tenant-b", projectlink.Journey, "journey-77", 10); err != nil || len(other) != 0 {
		t.Fatalf("cross-tenant reverse lookup=%#v err=%v", other, err)
	}
	if _, err := links.ListByTarget(ctx, "tenant-a", projectlink.ChatPost, "p-1", 10); err == nil {
		t.Fatal("reverse lookup accepted a chat target")
	}
}
