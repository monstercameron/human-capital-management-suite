package chatstore

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"testing"
)

func chatcmd002Fixture(t *testing.T, kind string) (*Store, chat.Chatcmd002Request, chat.Post) {
	t.Helper()
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "cards", TenantID: "tenant-a", Kind: kind, OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}, {MemberID: "worker", Role: "member", State: "active"}})
	card := chat.Chatcmd002Card{Kind: "todo", Title: "Launch checklist", Todo: &chat.Chatcmd002Todo{Tick: "anyone", Items: []chat.Chatcmd002Task{{ChannelTodoItem: chat.ChannelTodoItem{ID: "task-1", Text: "Book the room", CreatedBy: "owner", CreatedByHomeTenantID: "tenant-a"}, AssigneeID: "worker", AssigneeHomeTenantID: "tenant-a", AssigneeName: "Worker"}}}}
	body, err := card.Body()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "cards", AuthorID: "owner", ClientKey: "card-1", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	post, err := chatPost(raw)
	if err != nil {
		t.Fatal(err)
	}
	request := chat.Chatcmd002Request{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "worker"}, TenantID: "tenant-a", ConversationID: "cards", PostID: post.ID, ExpectedRevision: post.Revision, Mutation: chat.Chatcmd002Mutation{Operation: "TICK", ItemID: "task-1", Completed: true}}
	return s, request, post
}
func TestTodo_CHATCMD_002_Integration(t *testing.T) {
	for _, kind := range []string{"PUBLIC_CHANNEL", "PRIVATE_CHANNEL", "DIRECT", "GROUP"} {
		t.Run(kind, func(t *testing.T) {
			s, r, post := chatcmd002Fixture(t, kind)
			ctx := context.Background()
			view, err := s.Chatcmd002Read(ctx, r, todoAllow)
			if err != nil || !view.CanTick["task-1"] || view.CanManage {
				t.Fatalf("read %+v %v", view, err)
			}
			second, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "cards", AuthorID: "owner", ClientKey: "card-2", Body: post.Body, ParentID: post.ID})
			if err != nil {
				t.Fatal(err)
			}
			updated, err := s.Chatcmd002Mutate(ctx, r, todoAllow)
			card, ok := chat.Chatcmd002Decode(updated.Body)
			if err != nil || !ok || updated.Revision != post.Revision+1 || !card.Todo.Items[0].Completed || card.Todo.Items[0].CompletedBySubjectID != "worker" || card.Todo.Items[0].CompletedAtUnix == 0 || !card.Interacted {
				t.Fatalf("tick %+v %+v %v", updated, card, err)
			}
			r.PostID = second.ID
			r.ExpectedRevision = uint64(second.Revision)
			untouched, err := s.Chatcmd002Read(ctx, r, todoAllow)
			if err != nil || untouched.Card.Todo.Items[0].Completed {
				t.Fatalf("second list changed %+v %v", untouched, err)
			}
			if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
				var events, revisions int
				if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='post.edited'`, r.TenantID, post.ID).Scan(&events); err != nil {
					return err
				}
				if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2 AND revision=$3`, r.TenantID, post.ID, updated.Revision).Scan(&revisions); err != nil {
					return err
				}
				if events != 1 || revisions != 1 {
					t.Errorf("events %d revisions %d", events, revisions)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestTodo_CHATCMD_002_Security(t *testing.T) {
	s, r, post := chatcmd002Fixture(t, "PRIVATE_CHANNEL")
	ctx := context.Background()
	for _, principal := range []chat.Principal{{TenantID: "tenant-a", SubjectID: "outsider"}, {TenantID: "tenant-b", SubjectID: "worker"}} {
		denied := r
		denied.Principal = principal
		if _, err := s.Chatcmd002Mutate(ctx, denied, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
			t.Fatalf("foreign actor %v", err)
		}
	}
	if _, err := s.Chatcmd002Mutate(ctx, r, func(context.Context) error { return chat.ErrPermissionDenied }); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("authority bypass %v", err)
	}
	denied := r
	denied.Mutation = chat.Chatcmd002Mutation{Operation: "CLOSE"}
	if _, err := s.Chatcmd002Mutate(ctx, denied, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("member closed %v", err)
	}
	denied.Principal.SubjectID = "owner"
	closed, err := s.Chatcmd002Mutate(ctx, denied, todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	r.ExpectedRevision = closed.Revision
	if _, err := s.Chatcmd002Mutate(ctx, r, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("closed list ticked %v", err)
	}
	denied.ExpectedRevision = closed.Revision
	denied.Mutation.Operation = "REOPEN"
	reopened, err := s.Chatcmd002Mutate(ctx, denied, todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	r.ExpectedRevision = reopened.Revision
	updated, err := s.Chatcmd002Mutate(ctx, r, todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := chat.Chatcmd002Decode(post.Body)
	denied.ExpectedRevision = updated.Revision
	denied.Mutation = chat.Chatcmd002Mutation{Operation: "EDIT", Card: &original}
	if _, err := s.Chatcmd002Mutate(ctx, denied, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("edited after first tick %v", err)
	}
	r.ExpectedRevision = post.Revision
	if _, err := s.Chatcmd002Mutate(ctx, r, todoAllow); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale revision accepted %v", err)
	}
}
func TestTodo_CHATCMD_002_Property(t *testing.T) {
	s, r, _ := chatcmd002Fixture(t, "DIRECT")
	ctx := context.Background()
	updated, err := s.Chatcmd002Mutate(ctx, r, todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	r.ExpectedRevision = updated.Revision
	again, err := s.Chatcmd002Mutate(ctx, r, todoAllow)
	if err != nil || again.Revision != updated.Revision {
		t.Fatalf("idempotent tick %+v %v", again, err)
	}
	r.Mutation.Completed = false
	unticked, err := s.Chatcmd002Mutate(ctx, r, todoAllow)
	card, _ := chat.Chatcmd002Decode(unticked.Body)
	if err != nil || card.Todo.Items[0].Completed || card.Todo.Items[0].CompletedBySubjectID != "" || card.Todo.Items[0].CompletedAtUnix != 0 {
		t.Fatalf("untick %+v %v", card, err)
	}
}
func TestTodo_CHATCMD_002_Fault(t *testing.T) {
	s, r, post := chatcmd002Fixture(t, "GROUP")
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, r.TenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE FUNCTION chatcmd002_fail_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected event failure'; END $$`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `CREATE TRIGGER chatcmd002_fail_event BEFORE INSERT ON chat_outbox FOR EACH ROW EXECUTE FUNCTION chatcmd002_fail_event()`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Chatcmd002Mutate(ctx, r, todoAllow); err == nil {
		t.Fatal("event failure committed")
	}
	view, err := s.Chatcmd002Read(ctx, r, todoAllow)
	if err != nil || view.Card.Todo.Items[0].Completed {
		t.Fatalf("rollback %+v %v", view, err)
	}
	if err := s.RunTenantTx(ctx, r.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DROP TRIGGER chatcmd002_fail_event ON chat_outbox`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	retried, err := s.Chatcmd002Mutate(ctx, r, todoAllow)
	if err != nil || retried.Revision != post.Revision+1 {
		t.Fatalf("retry %+v %v", retried, err)
	}
}
func TestTodo_CHATCMD_004(t *testing.T) {
	c := chat.Chatcmd002Card{Kind: "todo", Todo: &chat.Chatcmd002Todo{Tick: "assignee"}}
	p := Post{TenantID: "t", AuthorHomeTenantID: "t", AuthorID: "author"}
	task := chat.Chatcmd002Task{AssigneeHomeTenantID: "t", AssigneeID: "worker"}
	if chatcmd002CanTick(c, p, task, chat.Principal{TenantID: "t", SubjectID: "author"}) || !chatcmd002CanTick(c, p, task, chat.Principal{TenantID: "t", SubjectID: "worker"}) {
		t.Fatal("assignee completion policy wrong")
	}
	c.Todo.Tick = "author"
	if !chatcmd002CanTick(c, p, task, chat.Principal{TenantID: "t", SubjectID: "author"}) {
		t.Fatal("author denied")
	}
	c.Todo.Tick = "anyone"
	if !chatcmd002CanTick(c, p, task, chat.Principal{TenantID: "t", SubjectID: "worker"}) {
		t.Fatal("member denied")
	}
}
