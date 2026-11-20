package projectlinkstore

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
)

// Issue 3790: the work made from a chat message is found again by the
// conversation it came from, scoped to the tenant, the conversation and live
// tasks, so the message can show what it became.
func TestIssue3790_ListByChatConversationFindsTasksMadeFromMessages_Integration(t *testing.T) {
	links, projects, _ := linkFixture(t)
	ctx := context.Background()
	project := projectstore.ProjectRecord{ID: "i3790-project", TenantID: "tenant-a", OwnerID: "owner", Name: "Project", Timezone: "UTC"}
	if err := projects.CreateProject(ctx, project, "owner", "HUMAN", "i3790-create-project"); err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 12, 4, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"i3790-task-1", "i3790-task-2", "i3790-task-3"} {
		if err := projects.CreateTask(ctx, projectstore.TaskRecord{ID: id, TenantID: "tenant-a", ProjectID: project.ID, Title: "Task " + id, StatusID: "todo", AssigneeID: "dana", DueDate: &due}, "owner", "HUMAN", "create-"+id); err != nil {
			t.Fatal(err)
		}
	}
	add := func(linkID, taskID string, ref projectlink.Reference) {
		t.Helper()
		if err := links.Add(ctx, LinkRecord{ID: linkID, TenantID: "tenant-a", ProjectID: project.ID, TaskID: taskID, Reference: ref}); err != nil {
			t.Fatalf("add %s: %v", linkID, err)
		}
	}
	// Two tasks from one message, one from another message, one from another room.
	add("i3790-l1", "i3790-task-1", projectlink.Reference{Kind: projectlink.ChatPost, ID: "post-1", ConversationID: "conv-1"})
	add("i3790-l2", "i3790-task-2", projectlink.Reference{Kind: projectlink.ChatPost, ID: "post-1", ConversationID: "conv-1"})
	add("i3790-l3", "i3790-task-3", projectlink.Reference{Kind: projectlink.ChatPost, ID: "post-9", ConversationID: "conv-other"})

	got, err := links.ListByChatConversation(ctx, "tenant-a", "conv-1", 50)
	if err != nil || len(got) != 2 {
		t.Fatalf("conversation lookup=%#v err=%v", got, err)
	}
	for _, row := range got {
		if row.PostID != "post-1" || row.ProjectID != project.ID || row.StatusID != "todo" || row.AssigneeID != "dana" || row.DueDate != "2026-12-04" {
			t.Fatalf("row=%#v", row)
		}
	}
	if other, err := links.ListByChatConversation(ctx, "tenant-b", "conv-1", 50); err != nil || len(other) != 0 {
		t.Fatalf("cross-tenant lookup=%#v err=%v", other, err)
	}
	if _, err := links.ListByChatConversation(ctx, "tenant-a", "", 50); err == nil {
		t.Fatal("empty conversation accepted")
	}
}
