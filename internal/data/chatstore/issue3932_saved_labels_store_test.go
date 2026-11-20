package chatstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type issue3932Sink struct{ seen []chat.SavedItem }

func (s *issue3932Sink) NotifySaved(_ context.Context, _ chat.Principal, item chat.SavedItem, _ string) error {
	s.seen = append(s.seen, item)
	return nil
}

func issue3932Two(t *testing.T) (*Store, chat.Principal, chat.SavedItem, chat.SavedItem) {
	t.Helper()
	s, p, first, _ := chatsaveDB(t)
	ctx := context.Background()
	post, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", ConversationID: "c-1", AuthorID: "u-1", ClientKey: "saved-post-2", Body: "second"})
	if err != nil {
		t.Fatal(err)
	}
	second := chat.SavedItem{TenantID: "tenant-a", HomeTenantID: "tenant-a", PersonID: "u-1", ConversationID: "c-1", PostID: post.ID}
	for _, item := range []chat.SavedItem{first, second} {
		if _, err := s.SaveItem(ctx, p, item); err != nil {
			t.Fatal(err)
		}
	}
	return s, p, first, second
}

func issue3932Change(s *Store, p chat.Principal, item chat.SavedItem, change chat.SavedChange) (chat.SavedItem, error) {
	return s.ChangeSaved(context.Background(), p, item.TenantID, item.ConversationID, item.PostID, change)
}

func TestIssue3932_LabelsPersistAndSurviveDone_Integration(t *testing.T) {
	s, p, first, _ := issue3932Two(t)
	ctx := context.Background()
	got, err := issue3932Change(s, p, first, chat.SavedChange{AddLabels: []string{"Open enrollment", "open  ENROLLMENT", "Hiring"}})
	if err != nil || !reflect.DeepEqual(got.Labels, []string{"Open enrollment", "Hiring"}) {
		t.Fatalf("add=%+v %v", got.Labels, err)
	}
	done := chat.SavedDone
	got, err = issue3932Change(s, p, first, chat.SavedChange{State: &done})
	if err != nil || len(got.Labels) != 2 {
		t.Fatalf("done dropped labels: %+v %v", got.Labels, err)
	}
	got, err = issue3932Change(s, p, first, chat.SavedChange{RemoveLabels: []string{"hiring"}})
	if err != nil || !reflect.DeepEqual(got.Labels, []string{"Open enrollment"}) {
		t.Fatalf("remove=%+v %v", got.Labels, err)
	}
	page, err := s.ListSavedItems(ctx, p, first.TenantID, chat.SavedAll, chat.Page{})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("list=%+v %v", page, err)
	}
	for _, item := range page.Items {
		if item.PostID != first.PostID && len(item.Labels) != 0 {
			t.Fatalf("unlabelled item has labels: %+v", item.Labels)
		}
	}
}

func TestIssue3932_LabelLimits_Integration(t *testing.T) {
	s, p, first, second := issue3932Two(t)
	many := []string{}
	for i := 0; i < chat.SavedLabelsPerItem+1; i++ {
		many = append(many, fmt.Sprintf("topic %d", i))
	}
	if _, err := issue3932Change(s, p, first, chat.SavedChange{AddLabels: many}); !errors.Is(err, chat.ErrSavedLabelLimit) {
		t.Fatalf("per item limit: %v", err)
	}
	if _, err := issue3932Change(s, p, second, chat.SavedChange{AddLabels: []string{"x\ty,z"}}); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("bad label: %v", err)
	}
}

func TestIssue3932_PersonLabelLimit_Integration(t *testing.T) {
	s, p, first, _ := issue3932Two(t)
	ctx := context.Background()
	items := []chat.SavedItem{first}
	for i := 0; i < 5; i++ {
		post, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", ConversationID: "c-1", AuthorID: "u-1", ClientKey: fmt.Sprintf("limit-%d", i), Body: "more"})
		if err != nil {
			t.Fatal(err)
		}
		item := chat.SavedItem{TenantID: "tenant-a", HomeTenantID: "tenant-a", PersonID: "u-1", ConversationID: "c-1", PostID: post.ID}
		if _, err := s.SaveItem(ctx, p, item); err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	for i, item := range items[:5] {
		names := []string{}
		for j := 0; j < chat.SavedLabelsPerItem; j++ {
			names = append(names, fmt.Sprintf("label %d-%d", i, j))
		}
		if _, err := issue3932Change(s, p, item, chat.SavedChange{AddLabels: names}); err != nil {
			t.Fatalf("item %d: %v", i, err)
		}
	}
	if _, err := issue3932Change(s, p, items[5], chat.SavedChange{AddLabels: []string{"one too many"}}); !errors.Is(err, chat.ErrSavedLabelLimit) {
		t.Fatalf("person limit: %v", err)
	}
	if _, err := issue3932Change(s, p, items[5], chat.SavedChange{AddLabels: []string{"LABEL 0-0"}}); err != nil {
		t.Fatalf("reusing an existing label must still work: %v", err)
	}
}

func TestIssue3932_RenameAndRemoveNeverDeleteItems_Integration(t *testing.T) {
	s, p, first, second := issue3932Two(t)
	ctx := context.Background()
	if _, err := issue3932Change(s, p, first, chat.SavedChange{AddLabels: []string{"Hiring", "Handover"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := issue3932Change(s, p, second, chat.SavedChange{AddLabels: []string{"hiring", "Benefits"}}); err != nil {
		t.Fatal(err)
	}
	n, err := s.RenameSavedLabel(ctx, p, "HIRING", "Recruiting")
	if err != nil || n != 2 {
		t.Fatalf("rename n=%d %v", n, err)
	}
	// Renaming into a label an item already has merges them.
	n, err = s.RenameSavedLabel(ctx, p, "Handover", "recruiting")
	if err != nil || n != 1 {
		t.Fatalf("merge n=%d %v", n, err)
	}
	page, _ := s.ListSavedItems(ctx, p, first.TenantID, chat.SavedAll, chat.Page{})
	byPost := map[string][]string{}
	for _, item := range page.Items {
		byPost[item.PostID] = item.Labels
	}
	if !reflect.DeepEqual(byPost[first.PostID], []string{"Recruiting"}) || !reflect.DeepEqual(byPost[second.PostID], []string{"Recruiting", "Benefits"}) {
		t.Fatalf("after rename: %+v", byPost)
	}
	n, err = s.RemoveSavedLabel(ctx, p, "recruiting")
	if err != nil || n != 2 {
		t.Fatalf("remove n=%d %v", n, err)
	}
	page, _ = s.ListSavedItems(ctx, p, first.TenantID, chat.SavedAll, chat.Page{})
	if len(page.Items) != 2 {
		t.Fatalf("remove label deleted saved items: %d", len(page.Items))
	}
	for _, item := range page.Items {
		for _, l := range item.Labels {
			if l == "Recruiting" {
				t.Fatalf("label still present on %s", item.PostID)
			}
		}
	}
}

func TestIssue3932_LabelsArePrivate_Security(t *testing.T) {
	s, p, first, _ := issue3932Two(t)
	ctx := context.Background()
	if _, err := issue3932Change(s, p, first, chat.SavedChange{AddLabels: []string{"Mine"}}); err != nil {
		t.Fatal(err)
	}
	other := chat.Principal{TenantID: p.TenantID, SubjectID: "u-2"}
	if _, err := issue3932Change(s, other, first, chat.SavedChange{AddLabels: []string{"Theirs"}}); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("another person labelled my item: %v", err)
	}
	if n, err := s.RenameSavedLabel(ctx, other, "Mine", "Stolen"); err != nil || n != 0 {
		t.Fatalf("another person renamed my label: %d %v", n, err)
	}
	if n, err := s.RemoveSavedLabel(ctx, other, "Mine"); err != nil || n != 0 {
		t.Fatalf("another person removed my label: %d %v", n, err)
	}
	foreign := chat.Principal{TenantID: "tenant-b", SubjectID: "u-1"}
	if n, err := s.RemoveSavedLabel(ctx, foreign, "Mine"); err != nil || n != 0 {
		t.Fatalf("another tenant removed my label: %d %v", n, err)
	}
	if page, err := s.ListSavedItems(ctx, other, first.TenantID, chat.SavedAll, chat.Page{}); err != nil || len(page.Items) != 0 {
		t.Fatalf("another person lists my items: %+v %v", page, err)
	}
	mine, _ := s.ListSavedItems(ctx, p, first.TenantID, chat.SavedAll, chat.Page{})
	found := false
	for _, item := range mine.Items {
		found = found || reflect.DeepEqual(item.Labels, []string{"Mine"})
	}
	if !found {
		t.Fatal("my label was lost")
	}
}

func TestIssue3932_ReminderAndErasureCarryNoLabels_Integration(t *testing.T) {
	s, p, first, _ := issue3932Two(t)
	ctx := context.Background()
	later := time.Now().Add(time.Hour)
	if _, err := issue3932Change(s, p, first, chat.SavedChange{AddLabels: []string{"Secret topic"}, SetDue: true, DueAt: &later}); err != nil {
		t.Fatal(err)
	}
	err := s.chatsaveTx(ctx, p, p.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_saved_item SET due_at=now()-interval '1 minute' WHERE post_id=$1`, first.PostID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	page, _ := s.ListSavedItems(ctx, p, first.TenantID, chat.SavedTodo, chat.Page{})
	var due chat.SavedItem
	for _, item := range page.Items {
		if item.PostID == first.PostID {
			due = item
		}
	}
	sink := &issue3932Sink{}
	if err := s.DeliverSavedReminder(ctx, p, due, sink); err != nil || len(sink.seen) != 1 {
		t.Fatalf("deliver=%v seen=%d", err, len(sink.seen))
	}
	if len(sink.seen[0].Labels) != 0 || sink.seen[0].Note != "" {
		t.Fatalf("a reminder carried private labels: %+v", sink.seen[0])
	}
	if err := s.EraseSavedPerson(ctx, p, p.TenantID); err != nil {
		t.Fatal(err)
	}
	if page, _ := s.ListSavedItems(ctx, p, first.TenantID, chat.SavedAll, chat.Page{}); len(page.Items) != 0 {
		t.Fatalf("erasure left items with labels: %+v", page.Items)
	}
}
