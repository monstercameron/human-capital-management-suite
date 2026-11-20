package productclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestIssue3786_LoadChatDigest(t *testing.T) {
	ctx := context.Background()
	feed := &ChatDigestFeed{Read: func(context.Context) (*productui.ChatDigest, error) {
		return &productui.ChatDigest{Cadence: "daily", Due: true}, nil
	}}
	if d, unavailable := loadChatDigest(ctx, feed); d == nil || !d.Due || unavailable {
		t.Fatalf("d=%+v unavailable=%v", d, unavailable)
	}
	feed.Read = func(context.Context) (*productui.ChatDigest, error) { return nil, errors.New("down") }
	if d, unavailable := loadChatDigest(ctx, feed); d != nil || !unavailable {
		t.Fatalf("a failed read must say unavailable: d=%+v unavailable=%v", d, unavailable)
	}
	if d, unavailable := loadChatDigest(ctx, nil); d != nil || unavailable {
		t.Fatalf("no feed is nothing to draw: d=%+v unavailable=%v", d, unavailable)
	}
}

func TestIssue3786_NamesAuthorsFromKnownPeopleOnly(t *testing.T) {
	d := &productui.ChatDigest{Items: []productui.ChatDigestItem{{AuthorID: "worker-1"}, {AuthorID: "worker-9"}, {AuthorID: "worker-2", AuthorName: "Kept"}}}
	nameChatDigestAuthors(d, []productui.Person{{ID: "p-1", WorkerID: "worker-1", Name: "Jo", PreferredName: "Joanna"}})
	if d.Items[0].AuthorName != "Joanna" || d.Items[1].AuthorName != "" || d.Items[2].AuthorName != "Kept" {
		t.Fatalf("names = %q %q %q", d.Items[0].AuthorName, d.Items[1].AuthorName, d.Items[2].AuthorName)
	}
	nameChatDigestAuthors(nil, nil)
}

func TestIssue3786_HandlersCallTheFeed(t *testing.T) {
	var gotCadence string
	var gotMinute int
	var gotThrough time.Time
	feed := &ChatDigestFeed{
		Save: func(_ context.Context, cadence string, minute int) error {
			gotCadence, gotMinute = cadence, minute
			return errors.New("refused")
		},
		Seen: func(_ context.Context, through time.Time) error { gotThrough = through; return nil },
	}
	save, seen := chatDigestHandlers(context.Background(), feed)
	done := make(chan error, 1)
	save("daily", 480, func(err error) { done <- err })
	if err := <-done; err == nil || gotCadence != "daily" || gotMinute != 480 {
		t.Fatalf("save: err=%v %s %d; a refusal must reach the bell", err, gotCadence, gotMinute)
	}
	at := time.Date(2026, 3, 10, 8, 5, 0, 0, time.UTC)
	seen(at, func(err error) { done <- err })
	if err := <-done; err != nil || !gotThrough.Equal(at) {
		t.Fatalf("seen: err=%v through=%v", err, gotThrough)
	}
	if a, b := chatDigestHandlers(context.Background(), nil); a != nil || b != nil {
		t.Fatal("with no feed the bell must not offer a choice")
	}
}
