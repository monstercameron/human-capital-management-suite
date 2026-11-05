package chatstore

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestTodo_CHATBUG_096_Prefilter: a rare word makes the message search judge
// every post (authority and rendering per row) instead of asking the database
// for candidates first. With three hundred posts and two carrying the rare
// word (one as written, one only in its stored translation), the search finds
// both while rendering a handful of rows, not hundreds.
func TestTodo_CHATBUG_096_Prefilter(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversationRow(t, s, Conversation{ID: "room", TenantID: "tenant", Kind: "PRIVATE_CHANNEL", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "alice", HomeTenantID: "tenant", Role: "manager", State: "active"}})
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) SELECT 'post-'||lpad(i::text,6,'0'),'tenant','room','alice','tenant',i,'filler common text '||i FROM generate_series(1,300) i`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('rare-post','tenant','room','alice','tenant',301,'standup moved for the quince review')`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('translated-post','tenant','room','alice','tenant',302,'filler common text')`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO chatrender_rendering(tenant_id,post_id,revision,tone,language,rendering) VALUES('tenant','translated-post',1,'as-written','de','{"text":"Quince Übersetzung"}')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rendered := 0
	r := chatsearch.NewRegistry()
	if err := s.RegisterChatSearchRendering(r, func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }, func(_ context.Context, _ chatsearch.Actor, row chatsearch.Row) (string, error) {
		rendered++
		if row.Target.MessageID == "translated-post" {
			return "Quince Übersetzung", nil
		}
		return row.Text, nil
	}); err != nil {
		t.Fatal(err)
	}
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}, Query: "quince", Filters: chatsearch.Filters{Kind: chatsearch.Message}, Limit: 20, At: time.Now().UTC()}
	got, err := r.Search(ctx, q)
	if err != nil || len(got.Groups) != 1 || len(got.Groups[0].Rows) != 2 {
		t.Fatalf("rare word %+v %v", got, err)
	}
	if rendered > 6 {
		t.Fatalf("the search rendered %d rows for 2 matches; the database never narrowed it", rendered)
	}
}
