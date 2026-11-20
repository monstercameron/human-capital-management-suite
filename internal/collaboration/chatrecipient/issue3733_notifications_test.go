package chatrecipient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type notificationRepo struct {
	repo
	rows                []Notification
	err                 error
	host, home, subject string
	since               time.Time
	limit, lists, marks int
	marked              []string
	markedAll           bool
}

func (r *notificationRepo) ListNotifications(_ context.Context, host, home, subject string, since time.Time, limit int) ([]Notification, error) {
	r.lists++
	r.host, r.home, r.subject, r.since, r.limit = host, home, subject, since, limit
	return append([]Notification(nil), r.rows...), r.err
}

func (r *notificationRepo) MarkNotificationsRead(_ context.Context, host, home, subject string, _ time.Time, ids []string, all bool) (int64, error) {
	r.marks++
	r.host, r.home, r.subject, r.marked, r.markedAll = host, home, subject, ids, all
	return int64(len(ids)), r.err
}

func TestIssue3733_Notifications_DropsConversationsTheCallerCannotOpen_Security(t *testing.T) {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	db := &notificationRepo{rows: []Notification{
		{PostID: "old", ConversationID: "open", At: base, Body: strings.Repeat("é", 500)},
		{PostID: "s", ConversationID: "secret", At: base.Add(time.Hour), Body: "private plans"},
		{PostID: "g", ConversationID: "gone", At: base.Add(2 * time.Hour)},
		{PostID: "new", ConversationID: "open", At: base.Add(3 * time.Hour), Body: "later"},
	}}
	admission := &refusing{conversations: conversations{allowed: true, denied: map[string]bool{"host/secret": true}}, missing: map[string]bool{"gone": true}}
	s := &Service{Conversations: admission, Repo: db}
	now := base.Add(48 * time.Hour)
	got, err := s.Notifications(context.Background(), chat.Principal{TenantID: "home", SubjectID: "alice"}, "host", now)
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	// Newest first, and each item is keyed by its message.
	if got[0].PostID != "new" || got[1].PostID != "old" || got[0].ID != "new" {
		t.Fatalf("order = %s, %s", got[0].PostID, got[1].PostID)
	}
	if db.host != "host" || db.home != "home" || db.subject != "alice" || db.limit != NotificationsLimit || !db.since.Equal(now.Add(-NotificationsWindow)) {
		t.Fatalf("the list was not scoped to the caller: %+v", db)
	}
	if got[0].HostTenantID != "host" || len([]rune(got[1].Body)) != FollowedPreviewRunes || !strings.HasSuffix(got[1].Body, "…") {
		t.Fatalf("host or preview not set: %+v", got)
	}
	if admission.asked != 3 {
		t.Fatalf("each conversation must be admitted once, asked=%d", admission.asked)
	}
}

func TestIssue3733_Notifications_Refusals(t *testing.T) {
	ctx := context.Background()
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	now := time.Now()
	if _, err := (&Service{Conversations: conversations{allowed: true}, Repo: &repo{}}).Notifications(ctx, p, "host", now); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("a store that cannot list: %v", err)
	}
	db := &notificationRepo{}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	if _, err := s.Notifications(ctx, chat.Principal{TenantID: "home"}, "host", now); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no subject: %v", err)
	}
	if db.lists != 0 {
		t.Fatal("the store was read before the request was checked")
	}
	db.err = errors.New("db down")
	if _, err := s.Notifications(ctx, p, "host", now); err == nil {
		t.Fatal("a store failure was hidden")
	}
}

func TestIssue3733_MarkNotificationsRead_Validates(t *testing.T) {
	ctx := context.Background()
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	now := time.Now()
	db := &notificationRepo{}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	for name, c := range map[string]struct {
		ids []string
		all bool
	}{
		"nothing named":   {nil, false},
		"all and ids":     {[]string{"m1"}, true},
		"blank id":        {[]string{"m1", "  "}, false},
		"oversized id":    {[]string{strings.Repeat("x", 257)}, false},
		"too many ids":    {manyNotificationIDs(NotificationsMarkLimit + 1), false},
		"only duplicates": {[]string{" ", ""}, false},
	} {
		if _, err := s.MarkNotificationsRead(ctx, p, "host", c.ids, c.all, now); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Fatalf("%s: err=%v, want an invalid request", name, err)
		}
	}
	if db.marks != 0 {
		t.Fatal("an invalid request reached the store")
	}
	if _, err := s.MarkNotificationsRead(ctx, chat.Principal{TenantID: "home"}, "host", []string{"m1"}, false, now); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no subject: %v", err)
	}
	n, err := s.MarkNotificationsRead(ctx, p, "host", []string{"m1", " m2 ", "m1"}, false, now)
	if err != nil || n != 2 || db.marks != 1 || len(db.marked) != 2 || db.marked[1] != "m2" || db.subject != "alice" || db.home != "home" || db.markedAll {
		t.Fatalf("n=%d err=%v db=%+v", n, err, db)
	}
	if _, err = s.MarkNotificationsRead(ctx, p, "host", nil, true, now); err != nil || !db.markedAll {
		t.Fatalf("mark all: %v %+v", err, db)
	}
}

func manyNotificationIDs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "m" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	return out
}
