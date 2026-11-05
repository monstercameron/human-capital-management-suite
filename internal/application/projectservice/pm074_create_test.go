package projectservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
)

type pm074CreateCommands struct {
	*fakeStore
	linked TaskRecord
	ref    projectlink.Reference
	calls  int
}

func (s *pm074CreateCommands) CreateTaskWithLink(_ context.Context, t TaskRecord, _ uint64, _, _ string, ref projectlink.Reference) (TaskRecord, error) {
	s.linked, s.ref, s.calls = t, ref, s.calls+1
	t.Revision = 1
	return t, nil
}

type pm074ErrorResolver struct{ err error }

func (r pm074ErrorResolver) Resolve(context.Context, string, projectlink.Reference) (projectlink.Result, error) {
	return projectlink.Result{}, r.err
}

func pm074CreateService(t *testing.T, resolver interface {
	Resolve(context.Context, string, projectlink.Reference) (projectlink.Result, error)
},
) (*Service, *pm074CreateCommands) {
	t.Helper()
	store := newFakeStore()
	store.projects["project-1"] = ProjectRecord{ID: "project-1", TenantID: "tenant-a", OwnerID: "alice", Revision: 1}
	commands := &pm074CreateCommands{fakeStore: store}
	return &Service{Auth: &fakeAuth{}, Commands: commands, Reads: store, Workflows: &fakeWorkflowPort{version: 7, config: transitionConfig()}, Links: &pm074SharePort{}, LinkResolver: resolver}, commands
}

func pm074CreateRequest() CreateTaskRequest {
	return CreateTaskRequest{ProjectID: "project-1", Title: "Follow up", Description: "Entered by the user", InitialStatusID: "todo", TypeID: "task_default", ExpectedWorkflowRevision: 7, IdempotencyKey: "create-1", SourceReference: &projectlink.Reference{Kind: projectlink.ChatPost, ID: "post-1", ConversationID: "conv-1"}}
}

func TestTodo_PM_074_CreateFromSource(t *testing.T) {
	resolver := taskLinkResolver{result: projectlink.Result{State: projectlink.Available, Preview: &projectlink.Preview{Kind: projectlink.ChatPost, ID: "post-1", Title: "Secret post title", Snippet: "secret excerpt", ConversationID: "conv-1"}}}
	service, commands := pm074CreateService(t, resolver)
	got, err := service.CreateTask(context.Background(), testPrincipal(t), pm074CreateRequest())
	if err != nil {
		t.Fatal(err)
	}
	if commands.calls != 1 {
		t.Fatalf("linked create reached repository %d times", commands.calls)
	}
	if commands.ref != (projectlink.Reference{Kind: projectlink.ChatPost, ID: "post-1", ConversationID: "conv-1"}) {
		t.Fatalf("linked reference = %+v", commands.ref)
	}
	for field, value := range map[string]string{"title": commands.linked.Title, "description": commands.linked.Description} {
		if value == "" || strings.Contains(value, "Secret post title") || strings.Contains(value, "secret excerpt") {
			t.Fatalf("stored %s copied source text: %q", field, value)
		}
	}
	if commands.linked.Title != "Follow up" || commands.linked.Description != "Entered by the user" {
		t.Fatalf("stored task lost user-entered text: %+v", commands.linked)
	}
	if got.WorkflowRevision != 7 {
		t.Fatalf("created workflow revision = %d", got.WorkflowRevision)
	}
}

func TestTodo_PM_074_CreateSecurity(t *testing.T) {
	resolveErr := errors.New("resolver unavailable")
	for name, tc := range map[string]struct {
		resolver interface {
			Resolve(context.Context, string, projectlink.Reference) (projectlink.Result, error)
		}
		mutate func(*CreateTaskRequest)
		want   error
	}{
		"restricted source": {
			taskLinkResolver{result: projectlink.Result{State: projectlink.Restricted}},
			func(*CreateTaskRequest) {},
			ErrUnsupportedLink,
		},
		"resolver failure": {
			pm074ErrorResolver{err: resolveErr},
			func(*CreateTaskRequest) {},
			resolveErr,
		},
		"invalid reference": {
			taskLinkResolver{result: projectlink.Result{State: projectlink.Available}},
			func(r *CreateTaskRequest) { r.SourceReference = &projectlink.Reference{} },
			ErrInvalidRequest,
		},
	} {
		service, commands := pm074CreateService(t, tc.resolver)
		req := pm074CreateRequest()
		tc.mutate(&req)
		if _, err := service.CreateTask(context.Background(), testPrincipal(t), req); !errors.Is(err, tc.want) || commands.calls != 0 {
			t.Fatalf("%s: err=%v calls=%d, want %v with no write", name, err, commands.calls, tc.want)
		}
	}
	auth := &fakeAuth{denied: true}
	store := newFakeStore()
	store.projects["project-1"] = ProjectRecord{ID: "project-1", TenantID: "tenant-a", OwnerID: "alice", Revision: 1}
	commands := &pm074CreateCommands{fakeStore: store}
	service := &Service{Auth: auth, Commands: commands, Reads: store, Workflows: &fakeWorkflowPort{version: 7, config: transitionConfig()}, Links: &pm074SharePort{}, LinkResolver: taskLinkResolver{result: projectlink.Result{State: projectlink.Available}}}
	if _, err := service.CreateTask(context.Background(), testPrincipal(t), pm074CreateRequest()); err == nil || commands.calls != 0 {
		t.Fatalf("unauthorized create err=%v calls=%d", err, commands.calls)
	}
}

func TestTodo_PM_074_CreateIntegration(t *testing.T) {
	store := newFakeStore()
	store.projects["project-1"] = ProjectRecord{ID: "project-1", TenantID: "tenant-a", OwnerID: "alice", Revision: 1}
	replaying := &replayingCreateStore{fakeStore: store}
	service := &Service{Auth: &fakeAuth{}, Commands: replaying, Reads: store, Workflows: &fakeWorkflowPort{version: 7, config: transitionConfig()}, Links: &pm074SharePort{}, LinkResolver: taskLinkResolver{result: projectlink.Result{State: projectlink.Available}}}
	if _, err := service.CreateTask(context.Background(), testPrincipal(t), pm074CreateRequest()); !errors.Is(err, ErrUnavailable) || replaying.called {
		t.Fatalf("partial write without linked repository: err=%v called=%v", err, replaying.called)
	}
}
