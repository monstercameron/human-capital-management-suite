package chatstore

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// CHATBUG-022 (speed): a keyword search over what one person may read must not
// cost a database or policy call per post. Twenty thousand posts across five
// rooms, the reader a member of four: the message source answers inside 300 ms
// and returns exactly the readable matches, none from the fifth room, and asks
// the authority only about rows that matched the words.
func TestTodo_CHATBUG_022_Performance(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		room := "room-" + string(rune('0'+i))
		members := []Membership{{MemberID: "danny", Role: "manager", State: "active"}}
		if i < 4 {
			members = append(members, Membership{MemberID: "alice", Role: "member", State: "active"})
		}
		seedConversationRow(t, s, Conversation{ID: room, TenantID: "tenant", Kind: "PRIVATE_CHANNEL", Name: room, OwnerID: "danny", Lifecycle: "ACTIVE", SettingsRevision: 1}, members)
	}
	const posts = 20000
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,created_at,updated_at)
 SELECT 'perf-'||g,'tenant','room-'||(g%5),'danny','tenant',(g/5)+1000,
  'routine update number '||g||CASE WHEN g%997=0 THEN ' - the Survey closes tonight' ELSE '' END,
  now()-(g||' seconds')::interval, now() FROM generate_series(1,$1::int) g`, posts)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	want := 0
	for g := 1; g <= posts; g++ {
		if g%997 == 0 && g%5 != 4 {
			want++
		}
	}
	r := chatsearch.NewRegistry()
	calls := 0
	authority := func(_ context.Context, a chatsearch.Actor, row chatsearch.Row) (bool, error) {
		calls++
		return strings.HasPrefix(row.Target.ConversationID, "room-") && row.Target.ConversationID != "room-4" || a.PersonID == "danny", nil
	}
	if err := s.RegisterChatSearch(r, authority); err != nil {
		t.Fatal(err)
	}
	alice := chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}
	request := chatsearch.Request{Actor: alice, Query: "survey", Filters: chatsearch.Filters{Kind: chatsearch.Message}, At: time.Now().UTC(), Limit: 50}
	if _, err := r.Search(ctx, request); err != nil { // warm the plan and the connection
		t.Fatal(err)
	}
	best := time.Hour
	var got chatsearch.Response
	var err error
	for i := 0; i < 5; i++ {
		calls = 0
		started := time.Now()
		got, err = r.Search(ctx, request)
		took := time.Since(started)
		t.Logf("run %d: %s, %d authority calls", i+1, took, calls)
		if err != nil || len(got.Unavailable) != 0 {
			t.Fatalf("message source failed: %v %v", err, got.Unavailable)
		}
		if took < best {
			best = took
		}
	}
	n := 0
	for _, group := range got.Groups {
		for _, row := range group.Rows {
			if row.Target.ConversationID == "room-4" {
				t.Fatalf("a row from a room the reader is not in: %+v", row)
			}
			n++
		}
	}
	if n != want {
		t.Fatalf("readable matches = %d, want %d", n, want)
	}
	if calls > 4*(want+1)+10 {
		t.Fatalf("%d authority calls for %d matching rows: the policy is being asked about posts that cannot match", calls, want)
	}
	t.Logf("message source: %d rows from %d posts, best of 5 = %s", n, posts, best)
	if best > 300*time.Millisecond {
		t.Fatalf("message source took %s, want under 300ms", best)
	}
}
