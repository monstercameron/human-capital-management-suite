package chatstore

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// chatbug096StallSource never answers before the registry's per-source budget
// ends, so the source is reported unavailable.
type chatbug096StallSource struct{ kind chatsearch.Kind }

func (s *chatbug096StallSource) Search(ctx context.Context, _ chatsearch.Request) ([]chatsearch.Row, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *chatbug096StallSource) CanOpen(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) {
	return true, nil
}

// TestTodo_CHATBUG_096_Integration: the served search over a real database
// with one stalled source. The store's sources answer through the registry
// while the stalled kind is the only one reported unavailable; and a page of
// rows is rechecked in one batch per source (CanOpenRows), so a post removed
// after the search no longer opens.
func TestTodo_CHATBUG_096_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversationRow(t, s, Conversation{ID: "room", TenantID: "tenant", Kind: "PRIVATE_CHANNEL", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "alice", HomeTenantID: "tenant", Role: "manager", State: "active"}})
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('found-post','tenant','room','alice','tenant',1,'standup moved for the quince review'),('gone-post','tenant','room','alice','tenant',2,'quince catering is ordered')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	r := chatsearch.NewRegistry()
	// Database-backed sources need a realistic budget on Windows runners; keep
	// it well below the five-second default while leaving the deliberately
	// stalled source as the only unavailable result.
	r.SourceTimeout = 2 * time.Second
	if err := s.RegisterChatSearchRendering(r, func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }, nil); err != nil {
		t.Fatal(err)
	}
	d, ok := r.Declaration(chatsearch.Person)
	if !ok {
		t.Fatal("the registry declares no person kind")
	}
	if err := r.Register(d, &chatbug096StallSource{kind: chatsearch.Person}); err != nil {
		t.Fatal(err)
	}
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}, Query: "quince", Limit: 20, At: time.Now().UTC()}
	got, err := r.Search(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Unavailable) != 1 || got.Unavailable[0] != chatsearch.Person {
		t.Fatalf("unavailable = %v, want only the stalled person source", got.Unavailable)
	}
	var messages []string
	for _, g := range got.Groups {
		if g.Kind != chatsearch.Message {
			continue
		}
		for _, row := range g.Rows {
			messages = append(messages, row.ID)
		}
	}
	if len(messages) != 2 {
		t.Fatalf("the answering message source returned %v, want both posts", messages)
	}

	// One batch rechecks the page: the rows the search returned open, a row
	// whose text changed since does not, and neither does an unknown row.
	rows := []chatsearch.Row{}
	for _, g := range got.Groups {
		rows = append(rows, g.Rows...)
	}
	if len(rows) == 0 {
		t.Fatal("the search returned no rows to recheck")
	}
	source := &ChatSearchSource{store: s, kind: chatsearch.Message, authority: func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }}
	batch, err := source.CanOpenRows(ctx, q.Actor, rows)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Kind != chatsearch.Message {
			continue
		}
		if !batch[row.ID] {
			t.Fatalf("a row the search just returned does not open: %+v", row)
		}
	}
	stale := rows[0]
	stale.Text += " edited after the search"
	unknown := stale
	unknown.ID, unknown.Target.MessageID = "no-such-post", "no-such-post"
	batch, err = source.CanOpenRows(ctx, q.Actor, []chatsearch.Row{stale, unknown})
	if err != nil {
		t.Fatal(err)
	}
	if batch[stale.ID] || batch[unknown.ID] {
		t.Fatalf("a stale or unknown row opens: %+v", batch)
	}

	// A post removed after the search no longer opens either.
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET tombstoned=true WHERE id='gone-post'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	batch, err = source.CanOpenRows(ctx, q.Actor, rows)
	if err != nil {
		t.Fatal(err)
	}
	if batch["gone-post"] || !batch["found-post"] {
		t.Fatalf("the recheck after a removal: %+v", batch)
	}
}
