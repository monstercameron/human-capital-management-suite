package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatsaveSearchFixture struct{ items []chat.SavedItem }

func (s *chatsaveSearchFixture) SearchSaved(_ context.Context, p chat.Principal, _ string) ([]chat.SavedItem, error) {
	result := []chat.SavedItem{}
	for _, item := range s.items {
		if item.PersonID == p.SubjectID && item.HomeTenantID == p.TenantID {
			result = append(result, item)
		}
	}
	return result, nil
}

func TestTodo_CHATSAVE_001_Search(t *testing.T) {
	ctx := chatsaveHTTPContext(t, trust.SubjectKindHuman)
	actor := chatsearch.Actor{TenantID: "tenant-a", HomeTenantID: "tenant-a", PersonID: "person"}
	fixture := &chatsaveSearchFixture{items: []chat.SavedItem{{TenantID: "tenant-a", HomeTenantID: "tenant-a", PersonID: "person", ConversationID: "room", PostID: "post", Note: "private followup", CreatedAt: time.Now(), Availability: "no_access"}, {TenantID: "tenant-a", HomeTenantID: "tenant-a", PersonID: "other", Note: "private foreign"}}}
	registry := chatsearch.NewRegistry()
	if err := RegisterSavedMessagesSearch(registry, fixture); err != nil {
		t.Fatal(err)
	}
	result, err := registry.Search(ctx, chatsearch.Request{Actor: actor, Query: "kind:saved private", Limit: 20, At: time.Now()})
	if err != nil || len(result.Groups) != 1 || len(result.Groups[0].Rows) != 1 || result.Groups[0].Rows[0].Text != "private followup" || result.Groups[0].Rows[0].Target.MessageID != "" {
		t.Fatalf("private note search=%+v %v", result, err)
	}
	source := chatsaveSearchSource{reader: fixture}
	row := result.Groups[0].Rows[0]
	fixture.items[0].Note = "updated private note"
	if ok, err := source.CanOpen(ctx, actor, row); err != nil || ok {
		t.Fatal("stale saved search opened", err)
	}
	if _, err = source.Search(chatsaveHTTPContext(t, trust.SubjectKindAgent), chatsearch.Request{Actor: actor}); err != chat.ErrPermissionDenied {
		t.Fatal("agent saved search", err)
	}
}
