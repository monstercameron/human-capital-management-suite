package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHATSEARCH_001_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, person := range []string{"alice", "bob"} {
		room := person + "-room"
		seedConversationRow(t, s, Conversation{ID: room, TenantID: "tenant", Kind: "PRIVATE_CHANNEL", Name: person + " budget", Description: "budget topic", OwnerID: person, Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: person, HomeTenantID: "tenant", Role: "manager", State: "active"}})
		refs, _ := json.Marshal([]chat.Reference{{Kind: chat.MediaAttachment, ID: "file", Display: person + " budget.pdf"}, {Kind: chat.PersonMention, TenantID: "tenant", ID: person, Display: person}})
		post, e := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: room, AuthorID: person, ClientKey: "post", Body: person + " budget https://example.test", References: refs})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.sendPostRaw(ctx, SendRequest{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: room, AuthorID: person, ClientKey: "reply", ParentID: post.ID, Body: person + " budget reply"}); e != nil {
			t.Fatal(e)
		}
		if e = s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
			_, e := tx.Exec(ctx, `INSERT INTO chat_pin(tenant_id,home_tenant_id,conversation_id,post_id,member_id) VALUES($1,$1,$2,$3,$4)`, "tenant", room, post.ID, person)
			if e != nil {
				return e
			}
			_, e = tx.Exec(ctx, `INSERT INTO chat_reaction(tenant_id,home_tenant_id,post_id,member_id,emoji) VALUES($1,$1,$2,$3,'yes')`, "tenant", post.ID, person)
			return e
		}); e != nil {
			t.Fatal(e)
		}
		if _, e = s.MutateChannelTodo(ctx, "tenant", "tenant", room, person, 1, ChannelTodoMutation{Operation: "ADD", Text: person + " budget todo", SourcePostID: post.ID}, func(context.Context) error { return nil }); e != nil {
			t.Fatal(e)
		}
		if _, e = s.MutateChannelPoll(ctx, "tenant", "tenant", room, person, 1, ChannelPollMutation{Operation: "CREATE", Question: person + " budget poll", Options: []string{"budget yes", "budget no"}}, func(context.Context) error { return nil }); e != nil {
			t.Fatal(e)
		}
		if _, e = NewDurableEphemeralStore(s).PutEphemeral(ctx, chat.EphemeralPost{ID: person + "-answer", TenantID: "tenant", ConversationID: room, ThreadID: post.ID, RecipientHomeTenantID: "tenant", RecipientSubjectID: person, Body: person + " budget answer", OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: now.Add(time.Hour), DurableCopyConversationID: room, DurableCopyPostID: post.ID, ThreadLink: "/chat#thread=" + post.ID}); e != nil {
			t.Fatal(e)
		}
	}
	r := chatsearch.NewRegistry()
	authority := func(_ context.Context, a chatsearch.Actor, row chatsearch.Row) (bool, error) {
		return row.Target.ConversationID == a.PersonID+"-room", nil
	}
	if e := s.RegisterChatSearch(r, authority); e != nil {
		t.Fatal(e)
	}
	for _, person := range []string{"alice", "bob"} {
		for _, kind := range []chatsearch.Kind{chatsearch.Message, chatsearch.Thread, chatsearch.Conversation, chatsearch.File, chatsearch.Pin, chatsearch.Todo, chatsearch.Poll, chatsearch.AgentAnswer} {
			t.Run(person+"/"+string(kind), func(t *testing.T) {
				q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: person}, Query: "budget", Filters: chatsearch.Filters{Kind: kind}, At: time.Now().UTC(), Limit: 50}
				got, e := r.Search(ctx, q)
				if e != nil || len(got.Groups) != 1 || got.Groups[0].Count != 1 || len(got.Groups[0].Rows) != 1 {
					t.Fatalf("search %+v %v", got, e)
				}
				row := got.Groups[0].Rows[0]
				if row.Target.ConversationID != person+"-room" {
					t.Fatalf("leaked %+v", row)
				}
				if kind == chatsearch.Thread && (row.Target.ThreadID == "" || row.Target.Sequence != 2 || row.Target.ThreadSequence != 1) {
					t.Fatalf("reply lost its exact parent and message anchors: %+v", row.Target)
				}
				if kind == chatsearch.AgentAnswer && (!row.Private || row.OwnerID != person) {
					t.Fatalf("private envelope %+v", row)
				}
				q.Query = "budget absent"
				empty, e := r.Search(ctx, q)
				if e != nil || len(empty.Groups) != 0 || empty.NextCursor != "" {
					t.Fatalf("empty %+v %v", empty, e)
				}
			})
		}
	}
	if e := r.ValidateStoredKinds([]chatsearch.Kind{chatsearch.Message, chatsearch.Thread, chatsearch.Conversation, chatsearch.File, chatsearch.Pin, chatsearch.Todo, chatsearch.Poll, chatsearch.AgentAnswer}); e != nil {
		t.Fatal(e)
	}
	// The same content set must disappear from every source on a leave,
	// including counts, paging and names that could hint at the former room.
	if e := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE chat_membership SET state='left',left_at=now() WHERE tenant_id='tenant' AND member_id='alice'`)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []chatsearch.Kind{chatsearch.Message, chatsearch.Thread, chatsearch.Conversation, chatsearch.File, chatsearch.Pin, chatsearch.Todo, chatsearch.Poll, chatsearch.AgentAnswer} {
		q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}, Query: "budget", Filters: chatsearch.Filters{Kind: kind}, At: time.Now().UTC()}
		got, e := r.Search(ctx, q)
		encoded, _ := json.Marshal(got)
		if e != nil || len(got.Groups) != 0 || got.NextCursor != "" || strings.Contains(string(encoded), "bob-room") || strings.Contains(string(encoded), "alice-room") {
			t.Fatalf("leave leaked %s: %+v %v", kind, got, e)
		}
	}
}

func TestTodo_CHATSEARCH_001_Security(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversationRow(t, s, Conversation{ID: "room", TenantID: "tenant", Kind: "PRIVATE_CHANNEL", Name: "budget", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "alice", HomeTenantID: "tenant", Role: "manager", State: "active"}, {MemberID: "bob", HomeTenantID: "tenant", Role: "member", State: "active"}})
	post, e := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "room", AuthorID: "alice", ClientKey: "post", Body: "budget secret"})
	if e != nil {
		t.Fatal(e)
	}
	authority := func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }
	source := &ChatSearchSource{store: s, kind: chatsearch.Message, authority: authority}
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}, Query: "budget", At: time.Now().UTC(), Limit: 20}
	rows, e := source.Search(ctx, q)
	if e != nil || len(rows) != 1 {
		t.Fatalf("search %v %v", rows, e)
	}
	if e = s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE chat_post SET tombstoned=true,body='' WHERE tenant_id=$1 AND id=$2`, "tenant", post.ID)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if open, e := source.CanOpen(ctx, q.Actor, rows[0]); e != nil || open {
		t.Fatalf("deleted row reopened %v %v", open, e)
	}
	for _, actor := range []chatsearch.Actor{{TenantID: "other", HomeTenantID: "tenant", PersonID: "alice"}, {TenantID: "tenant", HomeTenantID: "elsewhere", PersonID: "alice"}, {TenantID: "tenant", HomeTenantID: "tenant", PersonID: "outsider"}} {
		q.Actor = actor
		got, e := source.Search(ctx, q)
		if e != nil || len(got) != 0 {
			t.Fatalf("scope %v = %+v %v", actor, got, e)
		}
	}
	if !errors.Is(s.RegisterChatSearch(chatsearch.NewRegistry(), nil), chatsearch.ErrInvalid) {
		t.Fatal("nil authority accepted")
	}
	if _, e := chatsearchCatalog("unknown"); !errors.Is(e, chatsearch.ErrRegistry) {
		t.Fatal(e)
	}
}

func TestTodo_CHATSEARCH_002_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversationRow(t, s, Conversation{ID: "room", TenantID: "tenant", Kind: "PRIVATE_CHANNEL", Name: "budget", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "alice", HomeTenantID: "tenant", Role: "manager", State: "active"}})
	for i := 0; i < 4; i++ {
		if _, e := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "room", AuthorID: "alice", ClientKey: fmt.Sprint(i), Body: "budget https://example.test"}); e != nil {
			t.Fatal(e)
		}
	}
	r := chatsearch.NewRegistry()
	if e := s.RegisterChatSearch(r, func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }); e != nil {
		t.Fatal(e)
	}
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}, Query: "budget", Filters: chatsearch.Filters{Kind: chatsearch.Message, Person: "alice", Link: true, Mine: true, On: time.Now().UTC().Format("2006-01-02")}, Limit: 2, At: time.Now().UTC()}
	first, e := r.Search(ctx, q)
	if e != nil || first.NextCursor == "" || len(first.Groups) != 1 || first.Groups[0].Count != 2 {
		t.Fatalf("first %+v %v", first, e)
	}
	q.Cursor = first.NextCursor
	second, e := r.Search(ctx, q)
	if e != nil || second.NextCursor != "" || len(second.Groups) != 1 || second.Groups[0].Count != 2 {
		t.Fatalf("second %+v %v", second, e)
	}
	for _, a := range first.Groups[0].Rows {
		for _, b := range second.Groups[0].Rows {
			if a.ID == b.ID {
				t.Fatal("duplicate page row")
			}
		}
	}
	q.Cursor = ""
	q.Filters.File = true
	empty, e := r.Search(ctx, q)
	if e != nil || len(empty.Groups) != 0 {
		t.Fatalf("combined file filter %+v %v", empty, e)
	}
}

func TestTodo_CHATSEARCH_002_ReaderRendering(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversationRow(t, s, Conversation{ID: "room", TenantID: "tenant", Kind: "PRIVATE_CHANNEL", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "alice", HomeTenantID: "tenant", Role: "manager", State: "active"}})
	post, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "room", AuthorID: "alice", ClientKey: "post", Body: "budget"})
	if err != nil {
		t.Fatal(err)
	}
	// The reader's rendering is a stored translation; the database prefilter
	// finds the post by it, and the rendering function then supplies the text.
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chatrender_rendering(tenant_id,post_id,revision,tone,language,rendering) VALUES('tenant',$1,1,'as-written','de','{"text":"Haushaltsplan"}')`, post.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	allowed, rendered := true, 0
	r := chatsearch.NewRegistry()
	if err := s.RegisterChatSearchRendering(r, func(_ context.Context, _ chatsearch.Actor, row chatsearch.Row) (bool, error) {
		return row.ID == "" || allowed, nil
	}, func(context.Context, chatsearch.Actor, chatsearch.Row) (string, error) {
		rendered++
		return "Haushaltsplan", nil
	}); err != nil {
		t.Fatal(err)
	}
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}, Query: "Haushalt", Filters: chatsearch.Filters{Kind: chatsearch.Message}, At: time.Now().UTC()}
	got, err := r.Search(ctx, q)
	if err != nil || len(got.Groups) != 1 || got.Groups[0].Rows[0].Text != "Haushaltsplan" || rendered < 2 {
		t.Fatalf("reader text %+v %v %d", got, err, rendered)
	}
	q.Query = "budget"
	got, err = r.Search(ctx, q)
	if err != nil || len(got.Groups) != 0 {
		t.Fatalf("matched text reader cannot see: %+v %v", got, err)
	}
	allowed, rendered = false, 0
	q.Query = "Haushalt"
	got, err = r.Search(ctx, q)
	if err != nil || len(got.Groups) != 0 || rendered != 0 {
		t.Fatalf("denied row reached rendering: %+v %v %d", got, err, rendered)
	}
}

func TestTodo_CHATSEARCH_002_Performance(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversationRow(t, s, Conversation{ID: "room", TenantID: "tenant", Kind: "PRIVATE_CHANNEL", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "alice", HomeTenantID: "tenant", Role: "manager", State: "active"}})
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,sequence,body) SELECT 'post-'||lpad(i::text,6,'0'),'tenant','room','alice',i,'budget common word' FROM generate_series(1,100000) i`)
		if err == nil {
			_, err = tx.Exec(ctx, `ANALYZE chat_post,chat_membership,chat_conversation`)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	r := chatsearch.NewRegistry()
	if err := s.RegisterChatSearch(r, func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}, Query: "budget", Filters: chatsearch.Filters{Kind: chatsearch.Message}, Limit: 20, At: time.Now().UTC()}

	start := time.Now()
	got, err := r.Search(ctx, q)
	elapsed := time.Since(start)
	if err != nil || len(got.Groups) != 1 || got.Groups[0].Count != 20 || got.NextCursor == "" {
		t.Fatalf("common word %+v %v", got, err)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("100,000-message search took %v (budget 300ms)", elapsed)
	}
	t.Logf("100,000-message common-word search: %v", elapsed)
}

func TestTodo_CHATSEARCH_001_Refill(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversationRow(t, s, Conversation{ID: "room", TenantID: "tenant", Kind: "PRIVATE_CHANNEL", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "alice", HomeTenantID: "tenant", Role: "manager", State: "active"}})
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,sequence,body) SELECT 'post-'||lpad(i::text,3,'0'),'tenant','room','alice',i,'budget secret' FROM generate_series(1,205) i`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	r := chatsearch.NewRegistry()
	if err := s.RegisterChatSearch(r, func(_ context.Context, _ chatsearch.Actor, row chatsearch.Row) (bool, error) {
		return row.ID == "" || row.Target.Sequence <= 5, nil
	}); err != nil {
		t.Fatal(err)
	}
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}, Query: "budget", Filters: chatsearch.Filters{Kind: chatsearch.Message}, Limit: 2, At: time.Now().UTC()}
	first, err := r.Search(ctx, q)
	if err != nil || len(first.Groups) != 1 || first.Groups[0].Count != 2 || first.NextCursor == "" || first.Groups[0].Rows[0].Target.Sequence > 5 {
		t.Fatalf("denied candidate windows hid readable rows: %+v %v", first, err)
	}
	q.Cursor = first.NextCursor
	second, err := r.Search(ctx, q)
	if err != nil || len(second.Groups) != 1 || second.Groups[0].Count != 2 || second.Groups[0].Rows[0].ID == first.Groups[0].Rows[0].ID {
		t.Fatalf("refill keyset %+v %v", second, err)
	}
}
