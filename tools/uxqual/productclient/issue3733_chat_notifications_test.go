package productclient

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestIssue3733_LoadChatNotifications(t *testing.T) {
	ctx := context.Background()
	feed := &ChatNotificationFeed{List: func(context.Context) ([]productui.ChatNotification, error) {
		return []productui.ChatNotification{{ID: "m1", AuthorID: "worker-1"}}, nil
	}}
	if items, unavailable := loadChatNotifications(ctx, feed); len(items) != 1 || unavailable {
		t.Fatalf("items=%+v unavailable=%v", items, unavailable)
	}
	// A failed read is reported as unavailable and never as an empty feed with
	// stale items.
	feed.List = func(context.Context) ([]productui.ChatNotification, error) { return nil, errors.New("down") }
	if items, unavailable := loadChatNotifications(ctx, feed); len(items) != 0 || !unavailable {
		t.Fatalf("items=%+v unavailable=%v", items, unavailable)
	}
	// No feed connected is an empty bell, not an error.
	if items, unavailable := loadChatNotifications(ctx, nil); items != nil || unavailable {
		t.Fatalf("items=%+v unavailable=%v", items, unavailable)
	}
}

func TestIssue3733_NamesAuthorsFromKnownPeopleOnly(t *testing.T) {
	items := []productui.ChatNotification{{ID: "a", AuthorID: "worker-1"}, {ID: "b", AuthorID: "worker-9"}, {ID: "c", AuthorID: "worker-2", AuthorName: "Kept Name"}, {ID: "d"}}
	people := []productui.Person{{ID: "p-1", WorkerID: "worker-1", Name: "Jo", PreferredName: "Joanna"}, {ID: "worker-2", Name: "Other"}}
	nameChatNotificationAuthors(items, people)
	if items[0].AuthorName != "Joanna" || items[1].AuthorName != "" || items[2].AuthorName != "Kept Name" || items[3].AuthorName != "" {
		t.Fatalf("names = %q %q %q %q", items[0].AuthorName, items[1].AuthorName, items[2].AuthorName, items[3].AuthorName)
	}
}

func TestIssue3733_ReadHandlersCallTheFeed(t *testing.T) {
	var ids []string
	var all bool
	feed := &ChatNotificationFeed{MarkRead: func(_ context.Context, got []string, wantAll bool) error {
		ids, all = got, wantAll
		return errors.New("refused")
	}}
	one, every := chatNotificationReadHandlers(context.Background(), feed)
	done := make(chan error, 1)
	one("m1", func(err error) { done <- err })
	if err := <-done; err == nil || len(ids) != 1 || ids[0] != "m1" || all {
		t.Fatalf("one: err=%v ids=%v all=%v; a refusal must reach the bell", err, ids, all)
	}
	every(func(err error) { done <- err })
	if err := <-done; err == nil || !all {
		t.Fatalf("all: err=%v ids=%v all=%v", err, ids, all)
	}
	if a, b := chatNotificationReadHandlers(context.Background(), nil); a != nil || b != nil {
		t.Fatal("with no feed the bell must not offer to mark anything")
	}
}
