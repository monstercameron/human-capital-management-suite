package chatstore

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// TestIssue3679_MineFilterIsAnsweredByTheDatabase_Integration: is:mine on thread
// replies used to read every matching reply of every other author, authorize and
// render each one, and only then drop it, so a busy workspace spent the source's
// whole time limit and the page said the thread-reply source was unavailable.
// The database now keeps only the reader's own replies, so the per-row work is
// done for those alone.
func TestIssue3679_MineFilterIsAnsweredByTheDatabase_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversationRow(t, s, Conversation{ID: "room", TenantID: "tenant", Kind: "PRIVATE_CHANNEL", Name: "benefits", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{
		{MemberID: "alice", HomeTenantID: "tenant", Role: "manager", State: "active"},
		{MemberID: "bob", HomeTenantID: "tenant", Role: "member", State: "active"},
	})
	root, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "room", AuthorID: "bob", ClientKey: "root", Body: "open enrollment"})
	if err != nil {
		t.Fatal(err)
	}
	const others = 12
	for i := 0; i < others; i++ {
		if _, err = s.sendPostRaw(ctx, SendRequest{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "room", AuthorID: "bob", ClientKey: fmt.Sprint("bob", i), ParentID: root.ID, Body: "open enrollment reply"}); err != nil {
			t.Fatal(err)
		}
	}
	mine, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "room", AuthorID: "alice", ClientKey: "alice", ParentID: root.ID, Body: "open enrollment mine"})
	if err != nil {
		t.Fatal(err)
	}
	var rendered atomic.Int64
	r := chatsearch.NewRegistry()
	if err = s.RegisterChatSearchRendering(r,
		func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil },
		func(_ context.Context, _ chatsearch.Actor, row chatsearch.Row) (string, error) {
			rendered.Add(1)
			return row.Text, nil
		}); err != nil {
		t.Fatal(err)
	}
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}, Query: "open enrollment", Filters: chatsearch.Filters{Mine: true}, Limit: 20, At: time.Now().UTC()}
	got, err := r.Search(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Unavailable) != 0 {
		t.Fatalf("sources reported unavailable: %v", got.Unavailable)
	}
	found := false
	for _, g := range got.Groups {
		if g.Kind != chatsearch.Thread {
			continue
		}
		for _, row := range g.Rows {
			if row.ID == mine.ID {
				found = true
			} else if row.AuthorID != "alice" {
				t.Fatalf("a reply by %q was answered to is:mine", row.AuthorID)
			}
		}
	}
	if !found {
		t.Fatalf("the reader's own reply is missing: %+v", got.Groups)
	}
	// The reader's own reply is rendered once when it is found and once when the
	// answer is re-checked before it is returned; bob's replies and his root post
	// are dropped by the database and never rendered.
	if n := rendered.Load(); n != 2 {
		t.Fatalf("rendered %d rows for is:mine; only the reader's own reply should reach rendering", n)
	}
}
