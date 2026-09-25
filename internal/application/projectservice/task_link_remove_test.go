package projectservice

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type taskLinkAuth struct {
	calls  int
	denyAt int
	denied error
}

func (a *taskLinkAuth) Authorize(_ context.Context, _ *trust.Principal, _ string, _ projectaccess.Capability) error {
	a.calls++
	if a.denyAt > 0 && a.calls >= a.denyAt {
		return a.denied
	}
	return nil
}
func (*taskLinkAuth) AuthorizeCreate(context.Context, *trust.Principal) error       { return nil }
func (*taskLinkAuth) AuthorizeListProjects(context.Context, *trust.Principal) error { return nil }

type taskLinkRepo struct {
	rows           []TaskLinkRecord
	added, removed int
	revision       uint64
}

func (r *taskLinkRepo) AddTaskLink(context.Context, string, string, string, string, string, projectlink.Reference, uint64) (string, uint64, error) {
	r.added++
	return "link-1", 2, nil
}
func (r *taskLinkRepo) RemoveTaskLink(context.Context, string, string, string, string, string, uint64, string) (uint64, error) {
	r.removed++
	return r.revision, nil
}
func (r *taskLinkRepo) ListTaskLinks(context.Context, string, string, string, string, int) ([]TaskLinkRecord, *IDCursor, error) {
	return r.rows, nil, nil
}

type taskLinkResolver struct {
	result    projectlink.Result
	onResolve func()
}

func (r taskLinkResolver) Resolve(context.Context, string, projectlink.Reference) (projectlink.Result, error) {
	if r.onResolve != nil {
		r.onResolve()
	}
	return r.result, nil
}

func TestTodo_PM_020_RestrictedTaskLinkHidesReferenceAndRechecksReadAccess(t *testing.T) {
	denied := errors.New("membership revoked")
	auth := &taskLinkAuth{denied: denied}
	repo := &taskLinkRepo{rows: []TaskLinkRecord{{ID: "opaque-link", Reference: projectlink.Reference{Kind: projectlink.ChatPost, ID: "secret-post", ConversationID: "secret-conversation"}}}}
	svc := Service{Auth: auth, Links: repo, LinkResolver: taskLinkResolver{result: projectlink.Result{State: projectlink.Restricted}}}
	links, _, err := svc.ListTaskLinks(context.Background(), testPrincipal(t), "project-1", "task-1", "", 10)
	if err != nil || len(links) != 1 {
		t.Fatalf("list=%+v err=%v", links, err)
	}
	if links[0].ID != "opaque-link" || links[0].Reference != (projectlink.Reference{}) || links[0].Resolution.State != projectlink.Restricted || links[0].Resolution.Preview != nil {
		t.Fatalf("restricted target leaked: %+v", links[0])
	}
	if auth.calls != 2 {
		t.Fatalf("read authorization calls=%d, want before and after resolve", auth.calls)
	}

	auth = &taskLinkAuth{denyAt: 2, denied: denied}
	svc.Auth = auth
	if _, _, err := svc.ListTaskLinks(context.Background(), testPrincipal(t), "project-1", "task-1", "", 10); !errors.Is(err, denied) {
		t.Fatalf("revoked read err=%v", err)
	}
}

func TestTodo_PM_020_AddAndRemoveRecheckEditAccessBeforeWrite(t *testing.T) {
	denied := errors.New("membership revoked")
	auth := &taskLinkAuth{denyAt: 2, denied: denied}
	repo := &taskLinkRepo{revision: 6}
	ref := projectlink.Reference{Kind: projectlink.ChatConversation, ID: "conv-1"}
	svc := Service{Auth: auth, Links: repo, LinkResolver: taskLinkResolver{result: projectlink.Result{State: projectlink.Available}, onResolve: func() { auth.denyAt = 2 }}}
	if _, _, err := svc.AddTaskLink(context.Background(), testPrincipal(t), AddTaskLinkRequest{ProjectID: "project-1", TaskID: "task-1", ExpectedTaskRevision: 1, IdempotencyKey: "add-key", Reference: ref}); !errors.Is(err, denied) {
		t.Fatalf("revoked add err=%v", err)
	}
	if repo.added != 0 {
		t.Fatalf("add reached repository %d times", repo.added)
	}

	auth = &taskLinkAuth{denyAt: 1, denied: denied}
	svc.Auth = auth
	if _, err := svc.RemoveTaskLink(context.Background(), testPrincipal(t), RemoveTaskLinkRequest{ProjectID: "project-1", TaskID: "task-1", LinkID: "link-1", ExpectedTaskRevision: 5, IdempotencyKey: "remove-key"}); !errors.Is(err, denied) {
		t.Fatalf("denied remove err=%v", err)
	}
	if repo.removed != 0 {
		t.Fatalf("remove reached repository %d times", repo.removed)
	}
}

type workOrderReverseAuth struct{ deniedProject string }

func (a workOrderReverseAuth) Authorize(_ context.Context, _ *trust.Principal, projectID string, _ projectaccess.Capability) error {
	if projectID == a.deniedProject {
		return errors.New("project access denied")
	}
	return nil
}
func (workOrderReverseAuth) AuthorizeCreate(context.Context, *trust.Principal) error { return nil }
func (workOrderReverseAuth) AuthorizeListProjects(context.Context, *trust.Principal) error {
	return nil
}

type workOrderReverseRepo struct {
	taskLinkRepo
	kind projectlink.Kind
	rows []LinkedTaskRecord
}

func (r *workOrderReverseRepo) ListTasksLinkingTo(_ context.Context, _ string, kind projectlink.Kind, _ string, _ int) ([]LinkedTaskRecord, error) {
	r.kind = kind
	return r.rows, nil
}

func TestTodo_PM_020_WorkOrderReverseLinksRequireReadableProjects(t *testing.T) {
	repo := &workOrderReverseRepo{rows: []LinkedTaskRecord{
		{ProjectID: "project-visible", TaskID: "task-a"},
		{ProjectID: "project-private", TaskID: "task-b"},
	}}
	svc := Service{Auth: workOrderReverseAuth{deniedProject: "project-private"}, Links: repo}
	got, err := svc.ListTasksLinkingTo(context.Background(), testPrincipal(t), projectlink.WorkOrder, "01a0d8e5-4980-7611-9f4b-995d01f979d3", 10)
	if err != nil || repo.kind != projectlink.WorkOrder || len(got) != 1 || got[0].ProjectID != "project-visible" {
		t.Fatalf("authorized work-order reverse links=%+v kind=%q err=%v", got, repo.kind, err)
	}
	if _, err := svc.ListTasksLinkingTo(context.Background(), testPrincipal(t), projectlink.ChatPost, "post-1", 10); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unsupported reverse kind err=%v", err)
	}
}
