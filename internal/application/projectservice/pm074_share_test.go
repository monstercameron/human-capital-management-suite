package projectservice

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
)

type pm074SharePort struct {
	tenant, actor, href, key string
	destination              TaskShareDestination
	calls                    int
}

func (p *pm074SharePort) ShareTaskLink(_ context.Context, tenant, actor string, destination TaskShareDestination, href, key string) error {
	p.tenant, p.actor, p.destination, p.href, p.key = tenant, actor, destination, href, key
	p.calls++
	return nil
}

func (p *pm074SharePort) AddTaskLink(context.Context, string, string, string, string, string, projectlink.Reference, uint64) (string, uint64, error) {
	return "", 0, errors.New("not used")
}

func (p *pm074SharePort) RemoveTaskLink(context.Context, string, string, string, string, string, uint64, string) (uint64, error) {
	return 0, errors.New("not used")
}

func (p *pm074SharePort) ListTaskLinks(context.Context, string, string, string, string, int) ([]TaskLinkRecord, *IDCursor, error) {
	return nil, nil, errors.New("not used")
}

func pm074Service(t *testing.T, port *pm074SharePort) (*Service, *fakeStore) {
	t.Helper()
	store := newFakeStore()
	store.projects["project-1"] = ProjectRecord{ID: "project-1", TenantID: "tenant-a", OwnerID: "alice", Revision: 1}
	store.tasks[taskKey("project-1", "task-1")] = TaskRecord{ID: "task-1", ProjectID: "project-1", TenantID: "tenant-a", Title: "Prepare launch", Description: "Entered by the user", Revision: 4}
	return &Service{Auth: &fakeAuth{}, Reads: store, Links: port}, store
}

func TestTodo_PM_074(t *testing.T) {
	port := &pm074SharePort{}
	service, _ := pm074Service(t, port)
	err := service.ShareTaskLink(context.Background(), testPrincipal(t), ShareTaskLinkRequest{ProjectID: "project-1", TaskID: "task-1", ExpectedTaskRevision: 4, IdempotencyKey: "share-1", Destination: TaskShareDestination{Kind: TaskShareChatPost, ConversationID: "conversation-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if port.href != "/workspace/app/project?project=project-1&task=task-1" || port.destination.ConversationID != "conversation-1" || port.tenant != "tenant-a" || port.actor != "alice" {
		t.Fatalf("share port received unsafe or unstable projection: %+v", port)
	}
}

func TestTodo_PM_074_Browser(t *testing.T) {
	port := &pm074SharePort{}
	service, _ := pm074Service(t, port)
	if err := service.ShareTaskLink(context.Background(), testPrincipal(t), ShareTaskLinkRequest{ProjectID: "project-1", TaskID: "task-1", ExpectedTaskRevision: 4, IdempotencyKey: "share-browser", Destination: TaskShareDestination{Kind: TaskShareChatPost, ConversationID: "conversation-1"}}); err != nil {
		t.Fatal(err)
	}
	if port.href == "" || port.href[0] != '/' || port.href == "https://evil.invalid/project" {
		t.Fatalf("share-back href was not a stable in-app route: %q", port.href)
	}
}

func TestTodo_PM_074_Security(t *testing.T) {
	port := &pm074SharePort{}
	service, _ := pm074Service(t, port)
	bad := ShareTaskLinkRequest{ProjectID: "project-1", TaskID: "task-1", ExpectedTaskRevision: 4, IdempotencyKey: "bad", Destination: TaskShareDestination{Kind: TaskShareDocumentCandidate, DocumentID: "doc-1", CandidateID: "candidate-1"}}
	if err := service.ShareTaskLink(context.Background(), testPrincipal(t), bad); !errors.Is(err, ErrInvalidRequest) || port.calls != 0 {
		t.Fatalf("document candidate without revision = %v, calls=%d", err, port.calls)
	}
	bad.Destination.ExpectedCandidateRevision = 2
	bad.ProjectID = "../private"
	if err := service.ShareTaskLink(context.Background(), testPrincipal(t), bad); !errors.Is(err, ErrInvalidRequest) || port.calls != 0 {
		t.Fatalf("unsafe project route = %v, calls=%d", err, port.calls)
	}
	bad.ProjectID = "project-1"
	bad.ExpectedTaskRevision = 3
	if err := service.ShareTaskLink(context.Background(), testPrincipal(t), bad); !errors.Is(err, project.ErrRevisionConflict) || port.calls != 0 {
		t.Fatalf("stale task share = %v, calls=%d", err, port.calls)
	}
}

func TestTodo_PM_074_Integration(t *testing.T) {
	port := &pm074SharePort{}
	service, _ := pm074Service(t, port)
	err := service.ShareTaskLink(context.Background(), testPrincipal(t), ShareTaskLinkRequest{ProjectID: "project-1", TaskID: "task-1", ExpectedTaskRevision: 4, IdempotencyKey: "share-doc", Destination: TaskShareDestination{Kind: TaskShareDocumentCandidate, DocumentID: "doc-1", CandidateID: "candidate-1", ExpectedCandidateRevision: 2}})
	if err != nil || port.calls != 1 || port.destination.Kind != TaskShareDocumentCandidate || port.destination.ExpectedCandidateRevision != 2 {
		t.Fatalf("document candidate share = %+v, %v", port, err)
	}
}
