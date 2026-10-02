package chatstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type chatsaveTestSink struct {
	calls int
	fail  bool
}

func (s *chatsaveTestSink) NotifySaved(_ context.Context, p chat.Principal, item chat.SavedItem, key string) error {
	if s.fail {
		return chat.ErrUnavailable
	}
	if p.SubjectID != item.PersonID || key == "" {
		return chat.ErrPermissionDenied
	}
	s.calls++
	return nil
}

func chatsaveDB(t *testing.T) (*Store, chat.Principal, chat.SavedItem, string) {
	t.Helper()
	s, schema := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	p, err := s.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", ConversationID: "c-1", AuthorID: "u-1", ClientKey: "saved-post", Body: "original current text"})
	if err != nil {
		t.Fatal(err)
	}
	person := chat.Principal{TenantID: "tenant-a", SubjectID: "u-1"}
	return s, person, chat.SavedItem{TenantID: "tenant-a", HomeTenantID: "tenant-a", PersonID: "u-1", ConversationID: "c-1", PostID: p.ID}, schema
}

func TestTodo_CHATSAVE_001_Integration(t *testing.T) {
	s, p, item, _ := chatsaveDB(t)
	ctx := context.Background()
	x, err := s.SaveItem(ctx, p, item)
	if err != nil || x.State != chat.SavedTodo || x.Post != nil || x.Revision != 1 {
		t.Fatalf("save=%+v %v", x, err)
	}
	if _, err = s.SaveItem(ctx, p, item); err != nil {
		t.Fatal(err)
	}
	note := "private note"
	done := chat.SavedDone
	due := time.Now().Add(-time.Minute)
	x, err = s.ChangeSaved(ctx, p, item.TenantID, item.ConversationID, item.PostID, chat.SavedChange{State: &done, Note: &note, SetDue: true, DueAt: &due})
	if err != nil || x.Note != note || !x.DueAt.Equal(due.Truncate(time.Microsecond)) {
		t.Fatalf("change=%+v %v", x, err)
	}
	page, err := s.ListSavedItems(ctx, p, item.TenantID, chat.SavedDone, chat.Page{})
	if err != nil || len(page.Items) != 1 || page.Items[0].State != done {
		t.Fatalf("done=%+v %v", page, err)
	}
	page, err = s.ListSavedItems(ctx, chat.Principal{TenantID: p.TenantID, SubjectID: "other"}, item.TenantID, chat.SavedAll, chat.Page{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("cross-person read", err)
	}
	if _, err = s.ChangeSaved(ctx, chat.Principal{TenantID: p.TenantID, SubjectID: "other"}, item.TenantID, item.ConversationID, item.PostID, chat.SavedChange{Note: &note}); !errors.Is(err, chat.ErrNotFound) {
		t.Fatal("cross-person update", err)
	}
	state := chat.SavedTodo
	x, err = s.ChangeSaved(ctx, p, item.TenantID, item.ConversationID, item.PostID, chat.SavedChange{State: &state})
	if err != nil {
		t.Fatal(err)
	}
	sink := &chatsaveTestSink{fail: true}
	if err = s.DeliverSavedReminder(ctx, p, x, sink); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatal("failed reminder", err)
	}
	sink.fail = false
	for i := 0; i < 2; i++ {
		if err = s.DeliverSavedReminder(ctx, p, x, sink); err != nil {
			t.Fatal(err)
		}
	}
	if sink.calls != 1 {
		t.Fatalf("reminders=%d", sink.calls)
	}
	if err = s.RemoveSaved(ctx, p, item.TenantID, item.ConversationID, item.PostID); err != nil {
		t.Fatal(err)
	}
	page, err = s.ListSavedItems(ctx, p, item.TenantID, chat.SavedAll, chat.Page{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("remove failed")
	}
}

func TestTodo_CHATSAVE_001_Security(t *testing.T) {
	s, p, item, schema := chatsaveDB(t)
	ctx := context.Background()
	if _, err := s.SaveItem(ctx, p, item); err != nil {
		t.Fatal(err)
	}
	role := createChatRLSRole(t, s, schema)
	for _, person := range []string{"u-1", "other"} {
		err := s.chatsaveTx(ctx, chat.Principal{TenantID: p.TenantID, SubjectID: person}, item.TenantID, func(tx dbport.Tx) error {
			if _, err := tx.Exec(ctx, `SET LOCAL ROLE `+quoteChatIdentifier(role)); err != nil {
				return err
			}
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_saved_item`).Scan(&count); err != nil {
				return err
			}
			want := 0
			if person == "u-1" {
				want = 1
			}
			if count != want {
				t.Fatalf("RLS %s count=%d", person, count)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	err := s.chatsaveTx(ctx, chat.Principal{TenantID: p.TenantID, SubjectID: "other"}, item.TenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE `+quoteChatIdentifier(role)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO chat_saved_item(tenant_id,home_tenant_id,person_id,conversation_id,post_id,host_tenant_id) VALUES('tenant-a','tenant-a','u-1','c-1','forged','tenant-a')`)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("forged RLS write=%v", err)
	}
	if err = s.EraseSavedPerson(ctx, p, item.TenantID); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListSavedItems(ctx, p, item.TenantID, chat.SavedAll, chat.Page{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("erasure retained saves")
	}
	item.PersonID = "other"
	if _, err = s.SaveItem(ctx, p, item); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("forged owner", err)
	}
}

func TestTodo_CHATSAVE_001_PagingLimit(t *testing.T) {
	s, p, item, _ := chatsaveDB(t)
	ctx := context.Background()
	if _, err := s.SaveItem(ctx, p, item); err != nil {
		t.Fatal(err)
	}
	err := s.chatsaveTx(ctx, p, item.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_saved_item(tenant_id,home_tenant_id,person_id,conversation_id,post_id,created_at,host_tenant_id) SELECT 'tenant-a','tenant-a','u-1','c-1','fixture-'||n,now(),'tenant-a' FROM generate_series(1,4999) n`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.ListSavedItems(ctx, p, item.TenantID, chat.SavedAll, chat.Page{PageSize: 2})
	if err != nil || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatalf("page=%+v %v", page, err)
	}
	next, err := s.ListSavedItems(ctx, p, item.TenantID, chat.SavedAll, chat.Page{PageSize: 2, Cursor: page.NextCursor})
	if err != nil || len(next.Items) != 2 || next.Items[0].PostID == page.Items[1].PostID {
		t.Fatalf("next=%+v %v", next, err)
	}
	if _, err = s.ListSavedItems(ctx, chat.Principal{TenantID: p.TenantID, SubjectID: "other"}, item.TenantID, chat.SavedAll, chat.Page{Cursor: page.NextCursor}); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatal("foreign cursor", err)
	}
	if _, err = s.ListSavedItems(ctx, p, item.TenantID, chat.SavedDone, chat.Page{Cursor: page.NextCursor}); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatal("wrong tab cursor", err)
	}
	post, err := s.sendPostRaw(ctx, SendRequest{TenantID: p.TenantID, ConversationID: item.ConversationID, AuthorID: p.SubjectID, ClientKey: "over-limit", Body: "new post"})
	if err != nil {
		t.Fatal(err)
	}
	item.PostID = post.ID
	if _, err = s.SaveItem(ctx, p, item); !errors.Is(err, chat.ErrSavedLimit) {
		t.Fatalf("limit=%v", err)
	}
	seedConversationRow(t, s, Conversation{ID: "limit-foreign-room", TenantID: "host-b", Kind: "PUBLIC_CHANNEL", OwnerID: p.SubjectID, Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: p.SubjectID, HomeTenantID: p.TenantID, Role: "member", State: "active"}})
	foreignPost, err := s.sendPostRaw(ctx, SendRequest{TenantID: "host-b", HomeTenantID: p.TenantID, ConversationID: "limit-foreign-room", AuthorID: p.SubjectID, ClientKey: "foreign-limit", Body: "foreign source"})
	if err != nil {
		t.Fatal(err)
	}
	foreignItem := chat.SavedItem{TenantID: "host-b", HomeTenantID: p.TenantID, PersonID: p.SubjectID, ConversationID: "limit-foreign-room", PostID: foreignPost.ID}
	if _, err = s.SaveItem(ctx, p, foreignItem); !errors.Is(err, chat.ErrSavedLimit) {
		t.Fatalf("per-person limit across hosts=%v", err)
	}
	if err = s.EraseSavedPerson(ctx, p, item.TenantID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveItem(ctx, p, item); err != nil {
		t.Fatal("save after erasure", err)
	}
}
