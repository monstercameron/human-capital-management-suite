package project

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectactivity"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

type pm031Access struct{ view bool }

func (a pm031Access) CanViewTask(ProjectID, TaskID) bool { return a.view }

func pm031Task() Task {
	return Task{ID: "task-a", ProjectID: "project-a", TenantID: "tenant-a", Title: "Prepare launch", Description: "Check readiness", Status: "doing", TypeID: "task", Priority: PriorityHigh, AssigneeID: "person-1", DueDate: "2026-10-02", Revision: 3}
}

func pm031Project() Project {
	return Project{ID: "project-a", TenantID: "tenant-a", OwnerID: "owner-1", Name: "Launch", Timezone: "America/New_York", State: LifecycleActive, Revision: 5}
}

func pm031Page() projectactivity.Page {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	return projectactivity.Page{
		Comments: []projectactivity.Comment{
			{TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-a", ID: "c1", CreatedAt: at, Revisions: []projectactivity.Revision{{Number: 1, Text: projectactivity.SafeText{Source: "hello", HTML: "<p>hello</p>"}, ActorID: "person-1", At: at}}},
			{TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-a", ID: "c2", CreatedAt: at, Revisions: []projectactivity.Revision{{Number: 1, Text: projectactivity.SafeText{Source: "noted", HTML: "<p>noted</p>", Mentions: []projectactivity.Mention{{SubjectID: "person-2", DisplayName: "Person 2"}}}, ActorID: "person-2", At: at}}},
		},
		Activity:   []projectactivity.Activity{{Sequence: 1, TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-a", CommentID: "c1", ActorID: "person-1", Kind: "COMMENT_CREATED", Revision: 1, At: at}},
		NextCursor: "cursor-next",
	}
}

func pm031AvailableLink() TaskDetailLink {
	return TaskDetailLink{LinkID: "link-1", Reference: projectlink.Reference{Kind: projectlink.DeployedDocument, ID: "doc-1", Version: "v3", ScopeID: "scope-1"}, Resolution: projectlink.Result{State: projectlink.Available, Preview: &projectlink.Preview{Kind: projectlink.DeployedDocument, ID: "doc-1", Title: "Launch plan", Snippet: "milestones", Version: "v3", ScopeID: "scope-1"}}}
}

func TestTodo_PM_031(t *testing.T) {
	// 2026-10-02 01:30 UTC is still 2026-10-01 in New York, so the task is
	// not overdue in project-local terms.
	now := time.Date(2026, 10, 2, 1, 30, 0, 0, time.UTC)
	detail, err := BuildTaskDetail(pm031Task(), pm031Project(), projectworkflow.CategoryActive, now, pm031Page(), []TaskDetailLink{pm031AvailableLink()}, pm031Access{view: true})
	if err != nil {
		t.Fatal(err)
	}
	if detail.TaskID != "task-a" || detail.Title != "Prepare launch" || detail.Description != "Check readiness" || detail.Status != "doing" || detail.TypeID != "task" || detail.Priority != PriorityHigh || detail.Revision != 3 {
		t.Fatalf("authorized fields missing: %+v", detail)
	}
	if detail.OwnerID != "owner-1" {
		t.Fatalf("owner missing: %+v", detail)
	}
	if !DetailAssigned(detail) || detail.AssigneeID != "person-1" {
		t.Fatalf("assignee missing: %+v", detail)
	}
	if detail.Due.DueDate != "2026-10-02" || detail.Due.AsOfDate != "2026-10-01" || detail.Due.Overdue || !DetailHasDueDate(detail) {
		t.Fatalf("local due state wrong: %+v", detail.Due)
	}
	if len(detail.Comments) != 2 || detail.Comments[0].ID != "c1" || detail.Comments[0].HTML != "<p>hello</p>" || detail.Comments[0].AuthorID != "person-1" {
		t.Fatalf("comments page wrong: %+v", detail.Comments)
	}
	if len(detail.Comments[1].Mentions) != 1 || detail.Comments[1].Mentions[0].SubjectID != "person-2" {
		t.Fatalf("comment mentions wrong: %+v", detail.Comments[1])
	}
	if len(detail.Activity) != 1 || detail.Activity[0].Kind != "COMMENT_CREATED" || detail.Activity[0].Sequence != 1 {
		t.Fatalf("activity page wrong: %+v", detail.Activity)
	}
	if detail.CommentsCursor != "cursor-next" || !DetailHasMoreComments(detail) {
		t.Fatalf("comment cursor not carried: %+v", detail)
	}
	if len(detail.Links) != 1 || detail.Links[0].State != projectlink.Available || detail.Links[0].Title != "Launch plan" || detail.Links[0].TargetID != "doc-1" {
		t.Fatalf("available link wrong: %+v", detail.Links)
	}
}

func TestTodo_PM_031_Browser(t *testing.T) {
	// Served empty states: unassigned task, no comments yet, restricted link.
	task := pm031Task()
	task.AssigneeID = ""
	task.DueDate = ""
	page := projectactivity.Page{}
	restricted := TaskDetailLink{LinkID: "link-9", Reference: projectlink.Reference{Kind: projectlink.ChatConversation, ID: "chat-1"}, Resolution: projectlink.Result{State: projectlink.Restricted}}
	detail, err := BuildTaskDetail(task, pm031Project(), projectworkflow.CategoryNotStarted, time.Date(2026, 10, 2, 1, 30, 0, 0, time.UTC), page, []TaskDetailLink{restricted}, pm031Access{view: true})
	if err != nil {
		t.Fatal(err)
	}
	if DetailAssigned(detail) || detail.AssigneeID != "" {
		t.Fatalf("unassigned state wrong: %+v", detail)
	}
	if DetailHasDueDate(detail) || detail.Due != (TaskDueState{}) {
		t.Fatalf("missing due state wrong: %+v", detail.Due)
	}
	if len(detail.Comments) != 0 || len(detail.Activity) != 0 || DetailHasMoreComments(detail) || detail.CommentsCursor != "" {
		t.Fatalf("empty discussion state wrong: %+v", detail)
	}
	if len(detail.Links) != 1 || detail.Links[0] != (TaskDetailLinkView{LinkID: "link-9", State: projectlink.Restricted}) {
		t.Fatalf("restricted link not neutral: %+v", detail.Links)
	}
}

func TestTodo_PM_031_Security(t *testing.T) {
	now := time.Date(2026, 10, 2, 1, 30, 0, 0, time.UTC)
	if _, err := BuildTaskDetail(pm031Task(), pm031Project(), projectworkflow.CategoryActive, now, pm031Page(), nil, pm031Access{}); err == nil {
		t.Fatal("unauthorized viewer received task detail")
	}
	if _, err := BuildTaskDetail(pm031Task(), pm031Project(), projectworkflow.CategoryActive, now, pm031Page(), nil, nil); err == nil {
		t.Fatal("nil access received task detail")
	}
	// A smuggled preview on a restricted resolution must be dropped.
	smuggled := TaskDetailLink{LinkID: "link-2", Reference: projectlink.Reference{Kind: projectlink.DeployedDocument, ID: "doc-9", Version: "v1", ScopeID: "scope-9"}, Resolution: projectlink.Result{State: projectlink.Restricted, Preview: &projectlink.Preview{Kind: projectlink.DeployedDocument, ID: "doc-9", Title: "Secret plan", Snippet: "secret", Version: "v1", ScopeID: "scope-9"}}}
	detail, err := BuildTaskDetail(pm031Task(), pm031Project(), projectworkflow.CategoryActive, now, pm031Page(), []TaskDetailLink{smuggled}, pm031Access{view: true})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Links[0] != (TaskDetailLinkView{LinkID: "link-2", State: projectlink.Restricted}) {
		t.Fatalf("restricted link leaked smuggled preview: %+v", detail.Links[0])
	}
	// A preview that does not match its reference fails closed to restricted.
	mismatched := TaskDetailLink{LinkID: "link-3", Reference: projectlink.Reference{Kind: projectlink.DeployedDocument, ID: "doc-1", Version: "v3", ScopeID: "scope-1"}, Resolution: projectlink.Result{State: projectlink.Available, Preview: &projectlink.Preview{Kind: projectlink.DeployedDocument, ID: "other-doc", Title: "Wrong doc", Version: "v3", ScopeID: "scope-1"}}}
	detail, err = BuildTaskDetail(pm031Task(), pm031Project(), projectworkflow.CategoryActive, now, pm031Page(), []TaskDetailLink{mismatched}, pm031Access{view: true})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Links[0] != (TaskDetailLinkView{LinkID: "link-3", State: projectlink.Restricted}) {
		t.Fatalf("mismatched preview was not contained: %+v", detail.Links[0])
	}
	// Unbounded comment pages are rejected, never rendered.
	big := pm031Page()
	for len(big.Comments) <= MaxTaskDetailComments {
		big.Comments = append(big.Comments, big.Comments[0])
	}
	if _, err := BuildTaskDetail(pm031Task(), pm031Project(), projectworkflow.CategoryActive, now, big, nil, pm031Access{view: true}); err == nil {
		t.Fatal("unbounded comment page was accepted")
	}
	// Ownerless projects cannot render a detail that hides the owner.
	ownerless := pm031Project()
	ownerless.OwnerID = ""
	if _, err := BuildTaskDetail(pm031Task(), ownerless, projectworkflow.CategoryActive, now, pm031Page(), nil, pm031Access{view: true}); err == nil {
		t.Fatal("ownerless detail was accepted")
	}
	// Tombstoned comments expose no text.
	tombstoned := pm031Page()
	tombstoned.Comments[0].Revisions = []projectactivity.Revision{{Number: 2, Text: projectactivity.SafeText{Source: "held", HTML: "<p>held</p>"}, Tombstone: true, ActorID: "person-1", At: now}}
	detail, err = BuildTaskDetail(pm031Task(), pm031Project(), projectworkflow.CategoryActive, now, tombstoned, nil, pm031Access{view: true})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Comments[0].HTML != "" || !DetailCommentTombstoned(detail.Comments[0]) {
		t.Fatalf("tombstoned comment leaked text: %+v", detail.Comments[0])
	}
}
