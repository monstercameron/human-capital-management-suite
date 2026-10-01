package projectservice

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
)

type pm004SuspensionStore struct {
	*fakeStore
	gotReason string
}

func (s *pm004SuspensionStore) SuspendProject(_ context.Context, tenantID, projectID string, expected uint64, actor, origin, reason, key string) (ProjectRecord, error) {
	if tenantID != "tenant-a" || projectID != "p1" || actor != "alice" || origin != "HUMAN" || key == "" {
		return ProjectRecord{}, errors.New("unexpected suspension request")
	}
	s.gotReason = reason
	p := s.projects[projectID]
	if p.Revision != expected {
		return ProjectRecord{}, project.ErrRevisionConflict
	}
	p.State, p.Revision = project.LifecycleSuspended, expected+1
	s.projects[projectID] = p
	return p, nil
}

func TestTodo_PM_004_Service(t *testing.T) {
	store := &pm004SuspensionStore{fakeStore: newFakeStore()}
	store.projects["p1"] = ProjectRecord{ID: "p1", TenantID: "tenant-a", OwnerID: "owner-a", Name: "Operations", Timezone: "UTC", State: project.LifecycleActive, Revision: 1}
	svc := Service{Auth: &fakeAuth{}, Commands: store, Reads: store}
	if _, err := svc.SuspendProject(context.Background(), testPrincipal(t), SuspendProjectRequest{ProjectID: "p1", ExpectedProjectRevision: 1, IdempotencyKey: "suspend-1"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing reason error = %v", err)
	}
	result, err := svc.SuspendProject(context.Background(), testPrincipal(t), SuspendProjectRequest{ProjectID: "p1", Reason: "incident response", ExpectedProjectRevision: 1, IdempotencyKey: "suspend-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != project.LifecycleSuspended || result.Revision != 2 || store.gotReason != "incident response" {
		t.Fatalf("suspension result = %+v reason=%q", result, store.gotReason)
	}
}
