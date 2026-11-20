package chatstore

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type issue3735Sink struct{ s *Store }

func (k issue3735Sink) NotifySaved(ctx context.Context, p chat.Principal, item chat.SavedItem, key string) error {
	return k.s.NotifySavedReminder(ctx, SavedReminderNotice{Home: p.TenantID, Person: p.SubjectID, Host: item.TenantID, Conversation: item.ConversationID, Post: item.PostID, Key: key})
}

func issue3735Notices(t *testing.T, s *Store, tenant, person string) []chat.ModerationNotice {
	t.Helper()
	var out []chat.ModerationNotice
	err := s.RunTenantTx(context.Background(), tenant, func(tx dbport.Tx) error {
		var e error
		out, e = moderationNoticesTx(context.Background(), tx, chat.Principal{TenantID: tenant, SubjectID: person}, tenant)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestIssue3735_SavedReminderDelivered_Integration: a due reminder is found by
// the scheduler's person list, becomes one private notice for its owner with no
// message text or note, is not repeated, and never reaches another person or
// another workspace.
func TestIssue3735_SavedReminderDelivered_Integration(t *testing.T) {
	s, p, item, _ := chatsaveDB(t)
	ctx := context.Background()
	if _, err := s.SaveItem(ctx, p, item); err != nil {
		t.Fatal(err)
	}
	note := "private follow up"
	due := time.Now().Add(-time.Minute)
	if _, err := s.ChangeSaved(ctx, p, item.TenantID, item.ConversationID, item.PostID, chat.SavedChange{Note: &note, SetDue: true, DueAt: &due}); err != nil {
		t.Fatal(err)
	}
	people, err := s.ListSavedReminderPeople(ctx, "tenant-a")
	if err != nil || len(people) != 1 || people[0] != "u-1" {
		t.Fatalf("people=%v err=%v", people, err)
	}
	if other, err := s.ListSavedReminderPeople(ctx, "tenant-b"); err != nil || len(other) != 0 {
		t.Fatalf("another workspace saw %v err=%v", other, err)
	}
	other := chat.Principal{TenantID: "tenant-a", SubjectID: "u-2"}
	sink := issue3735Sink{s}
	for i := 0; i < 2; i++ {
		page, err := s.ListSavedItems(ctx, p, "tenant-a", chat.SavedTodo, chat.Page{})
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("list=%+v %v", page, err)
		}
		if err := s.DeliverSavedReminder(ctx, p, page.Items[0], sink); err != nil {
			t.Fatal(err)
		}
	}
	got := issue3735Notices(t, s, "tenant-a", "u-1")
	if len(got) != 1 || got[0].Outcome != chat.ModerationOutcomeSavedReminder || got[0].ConversationID != item.ConversationID || got[0].PostID != item.PostID || got[0].Reason != "" {
		t.Fatalf("notices=%+v", got)
	}
	if rows := issue3735Notices(t, s, "tenant-a", other.SubjectID); len(rows) != 0 {
		t.Fatalf("another person was told: %+v", rows)
	}
	if people, err = s.ListSavedReminderPeople(ctx, "tenant-a"); err != nil || len(people) != 0 {
		t.Fatalf("delivered reminder still due: %v %v", people, err)
	}
	// A replayed delivery of the same key is the same notice.
	if err := s.NotifySavedReminder(ctx, SavedReminderNotice{Home: "tenant-a", Person: "u-1", Host: "tenant-a", Conversation: item.ConversationID, Post: item.PostID, Key: "saved:replay"}); err != nil {
		t.Fatal(err)
	}
	if err := s.NotifySavedReminder(ctx, SavedReminderNotice{Home: "tenant-a", Person: "u-1", Host: "tenant-a", Conversation: item.ConversationID, Post: item.PostID, Key: "saved:replay"}); err != nil {
		t.Fatal(err)
	}
	if got = issue3735Notices(t, s, "tenant-a", "u-1"); len(got) != 2 {
		t.Fatalf("replay made %d notices", len(got))
	}
	if err := s.NotifySavedReminder(ctx, SavedReminderNotice{Home: "tenant-a", Person: "", Key: "x"}); err == nil {
		t.Fatal("notice without a person accepted")
	}
}

// TestIssue3735_ServiceDispatchDelivers_Integration runs the whole path the
// scheduler runs: the chat service lists the person's due reminders from the
// real store, delivers each through the sink exactly once however often it is
// run, writes no message text, and still delivers after the person lost access
// to the channel the message is in. Another workspace is never reminded.
func TestIssue3735_ServiceDispatchDelivers_Integration(t *testing.T) {
	adapter := adapterDB(t)
	s := adapter.Store
	ctx := context.Background()
	seedConversation(t, s, "tenant-a")
	post, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", ConversationID: "c-1", AuthorID: "u-1", ClientKey: "saved-post", Body: "original current text"})
	if err != nil {
		t.Fatal(err)
	}
	p := chat.Principal{TenantID: "tenant-a", SubjectID: "u-1"}
	item := chat.SavedItem{TenantID: "tenant-a", HomeTenantID: "tenant-a", PersonID: "u-1", ConversationID: "c-1", PostID: post.ID}
	if _, err := s.SaveItem(ctx, p, item); err != nil {
		t.Fatal(err)
	}
	due := time.Now().Add(-time.Minute)
	if _, err := s.ChangeSaved(ctx, p, item.TenantID, item.ConversationID, item.PostID, chat.SavedChange{SetDue: true, DueAt: &due}); err != nil {
		t.Fatal(err)
	}
	// The person leaves the channel before the reminder runs.
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM chat_membership WHERE tenant_id='tenant-a' AND conversation_id=$1 AND member_id='u-1'`, item.ConversationID)
		return e
	}); err != nil {
		t.Fatalf("membership could not be removed: %v", err)
	}
	service := chat.NewService(adapter, time.Now)
	sink := issue3735Sink{s}
	for i := 0; i < 3; i++ {
		if err := service.DispatchSavedReminders(ctx, p, "tenant-a", sink); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	got := issue3735Notices(t, s, "tenant-a", "u-1")
	if len(got) != 1 || got[0].Outcome != chat.ModerationOutcomeSavedReminder || got[0].Reason != "" {
		t.Fatalf("notices=%+v", got)
	}
	if rows := issue3735Notices(t, s, "tenant-b", "u-1"); len(rows) != 0 {
		t.Fatalf("another workspace was told: %+v", rows)
	}
	// The same person id in another workspace owns none of these items: running
	// for them reaches nothing and tells nobody.
	if err := service.DispatchSavedReminders(ctx, chat.Principal{TenantID: "tenant-b", SubjectID: "u-1"}, "tenant-a", sink); err != nil {
		t.Fatal(err)
	}
	if rows := issue3735Notices(t, s, "tenant-b", "u-1"); len(rows) != 0 {
		t.Fatalf("another workspace was told: %+v", rows)
	}
	if got = issue3735Notices(t, s, "tenant-a", "u-1"); len(got) != 1 {
		t.Fatalf("notices=%+v", got)
	}
}
