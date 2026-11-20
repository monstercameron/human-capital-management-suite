package chatstore

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func TestIssue3945_StoreOldestFirstPagesBothWays_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversationRow(t, s, Conversation{ID: "room", TenantID: "tenant", Kind: "PRIVATE_CHANNEL", Name: "room", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "alice", HomeTenantID: "tenant", Role: "manager", State: "active"}})
	var sent []string
	for i := 0; i < 5; i++ {
		post, e := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "room", AuthorID: "alice", ClientKey: string(rune('a' + i)), Body: "deadline note"})
		if e != nil {
			t.Fatal(e)
		}
		sent = append(sent, post.ID)
		time.Sleep(3 * time.Millisecond)
	}
	r := chatsearch.NewRegistry()
	if e := s.RegisterChatSearch(r, func(_ context.Context, _ chatsearch.Actor, row chatsearch.Row) (bool, error) {
		return row.Target.ConversationID == "room", nil
	}); e != nil {
		t.Fatal(e)
	}
	alice := chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}
	walkAs := func(actor chatsearch.Actor, order string) []string {
		q := chatsearch.Request{Actor: actor, Query: "deadline", Filters: chatsearch.Filters{Kind: chatsearch.Message}, Order: order, At: time.Now().UTC(), Limit: 2}
		var ids []string
		for i := 0; i < 10; i++ {
			got, e := r.Search(ctx, q)
			if e != nil {
				t.Fatal(e)
			}
			for _, g := range got.Groups {
				for _, row := range g.Rows {
					ids = append(ids, row.ID)
				}
			}
			if got.NextCursor == "" {
				return ids
			}
			q.Cursor = got.NextCursor
		}
		t.Fatal("cursor never ended")
		return nil
	}
	walk := func(order string) []string { return walkAs(alice, order) }
	oldest := walk(chatsearch.OrderOldest)
	if !reflect.DeepEqual(oldest, sent) {
		t.Fatalf("oldest first = %v want %v", oldest, sent)
	}
	newest := walk("")
	rev := make([]string, len(sent))
	for i, id := range sent {
		rev[len(sent)-1-i] = id
	}
	if !reflect.DeepEqual(newest, rev) {
		t.Fatalf("newest first = %v want %v", newest, rev)
	}
	// The order never widens who may read: a non-member and another tenant
	// walking oldest first find nothing.
	for _, actor := range []chatsearch.Actor{{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "outsider"}, {TenantID: "other", HomeTenantID: "tenant", PersonID: "alice"}} {
		if got := walkAs(actor, chatsearch.OrderOldest); len(got) != 0 {
			t.Fatalf("%+v read %v oldest first", actor, got)
		}
	}
}
