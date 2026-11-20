package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

func TestIssue3789_Away_Integration(t *testing.T) {
	_, r := issue3733Seed(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	none, err := r.Away(ctx, "host", "u-2")
	if err != nil || none.Revision != 1 || none.Active(now) {
		t.Fatalf("never set: %+v %v", none, err)
	}
	until := now.Add(48 * time.Hour)
	saved, err := r.PutAway(ctx, "host", "u-2", chatrecipient.Away{Since: now, Until: until, CoverID: "u-1", NotifyCover: true}, 1)
	if err != nil || saved.Revision != 2 {
		t.Fatalf("first save: %+v %v", saved, err)
	}
	got, err := r.Away(ctx, "host", "u-2")
	if err != nil || got.CoverID != "u-1" || !got.NotifyCover || got.Hidden || !got.Until.Equal(until) || got.Revision != 2 {
		t.Fatalf("read back: %+v %v", got, err)
	}
	// A save against a revision somebody else already moved is a conflict.
	if _, err := r.PutAway(ctx, "host", "u-2", chatrecipient.Away{Since: now, Until: until}, 1); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	// A cover who is nobody in this company is refused, and nothing is stored.
	if _, err := r.PutAway(ctx, "host", "u-2", chatrecipient.Away{Since: now, Until: until, CoverID: "nobody"}, 2); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("unknown cover: %v", err)
	}
	if again, _ := r.Away(ctx, "host", "u-2"); again.CoverID != "u-1" || again.Revision != 2 {
		t.Fatalf("refused save changed the row: %+v", again)
	}

	shown, err := r.AwayOf(ctx, "host", []string{"u-1", "u-2"}, now)
	if err != nil || len(shown) != 1 || shown[0].MemberID != "u-2" || shown[0].CoverID != "u-1" || !shown[0].Until.Equal(until) {
		t.Fatalf("what a colleague sees: %+v %v", shown, err)
	}
	// Hidden by its owner: nobody else sees it, and the owner still reads it.
	if _, err := r.PutAway(ctx, "host", "u-2", chatrecipient.Away{Since: now, Until: until, CoverID: "u-1", Hidden: true}, 2); err != nil {
		t.Fatal(err)
	}
	if shown, _ := r.AwayOf(ctx, "host", []string{"u-2"}, now); len(shown) != 0 {
		t.Fatalf("hidden absence was shown: %+v", shown)
	}
	if own, _ := r.Away(ctx, "host", "u-2"); !own.Hidden || own.CoverID != "u-1" {
		t.Fatalf("owner lost their own setting: %+v", own)
	}
	// An ended absence is not shown.
	if shown, _ := r.AwayOf(ctx, "host", []string{"u-2"}, until.Add(time.Second)); len(shown) != 0 {
		t.Fatalf("ended absence was shown: %+v", shown)
	}
	// Ending it clears the cover and the dates.
	if _, err := r.PutAway(ctx, "host", "u-2", chatrecipient.Away{Hidden: true}, 3); err != nil {
		t.Fatal(err)
	}
	if end, _ := r.Away(ctx, "host", "u-2"); !end.Until.IsZero() || end.CoverID != "" || end.NotifyCover {
		t.Fatalf("ended: %+v", end)
	}
}

func TestIssue3789_AwayIsPerCompany_Security(t *testing.T) {
	_, r := issue3733Seed(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := r.PutAway(ctx, "host", "u-2", chatrecipient.Away{Since: now, Until: now.Add(time.Hour)}, 1); err != nil {
		t.Fatal(err)
	}
	if shown, err := r.AwayOf(ctx, "other-company", []string{"u-2"}, now); err != nil || len(shown) != 0 {
		t.Fatalf("another company saw the absence: %+v %v", shown, err)
	}
	if other, err := r.Away(ctx, "other-company", "u-2"); err != nil || other.Active(now) || other.Revision != 1 {
		t.Fatalf("another company read the row: %+v %v", other, err)
	}
	// A cover in another company is not a person of this one.
	if _, err := r.PutAway(ctx, "host", "u-2", chatrecipient.Away{Since: now, Until: now.Add(time.Hour), CoverID: "u-9"}, 2); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("foreign cover: %v", err)
	}
}

func TestIssue3789_CoverNotices_Integration_Security(t *testing.T) {
	s, r := issue3733Seed(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, q := range []string{
		// u-3 mentions the person who is away in a room the cover is not in.
		issue3733Post("c-away", "secret-mention", 2, "u-3", "private plans", issue3787MentionU2, "", "1 hour"),
		// A mention in a room the cover has muted.
		issue3733Post("c-muted", "muted-for-cover", 2, "u-3", "@U2 hi", issue3787MentionU2, "", "1 hour"),
		// A mention that came before the absence began.
		issue3733Post("c-1", "before", 12, "u-3", "@U2 early", issue3787MentionU2, "", "9 hours"),
	} {
		if err := s.execTenant(ctx, "host", q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	// u-1 muted c-muted already (seed). u-2 is away from five hours ago, u-1 covers.
	set := func(a chatrecipient.Away, rev uint64) {
		t.Helper()
		if _, err := r.PutAway(ctx, "host", "u-2", a, rev); err != nil {
			t.Fatal(err)
		}
	}
	list := func(cover string) []chatrecipient.Notification {
		t.Helper()
		rows, err := r.CoverNotices(ctx, "host", "host", cover, now.Add(-chatrecipient.NotificationsWindow), now, 20)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	// Notices are off: nothing, even though the cover is named.
	set(chatrecipient.Away{Since: now.Add(-5 * time.Hour), Until: now.Add(48 * time.Hour), CoverID: "u-1"}, 1)
	if rows := list("u-1"); len(rows) != 0 {
		t.Fatalf("notices off yet listed: %+v", rows)
	}
	set(chatrecipient.Away{Since: now.Add(-5 * time.Hour), Until: now.Add(48 * time.Hour), CoverID: "u-1", NotifyCover: true}, 2)
	rows := list("u-1")
	got := issue3733Index(rows)
	if len(rows) != 1 || got["mention-of-other"].PostID == "" {
		t.Fatalf("cover notices: %+v", rows)
	}
	n := got["mention-of-other"]
	if n.CoveringID != "u-2" || n.Body != "" || n.ConversationName != "ops" || n.Read {
		t.Fatalf("notice shape: %+v", n)
	}
	for _, hidden := range []string{"secret-mention", "muted-for-cover", "before"} {
		if _, leaked := got[hidden]; leaked {
			t.Fatalf("%s reached the cover", hidden)
		}
	}
	// Only the named cover is told.
	if rows := list("u-3"); len(rows) != 0 {
		t.Fatalf("someone who is not the cover was told: %+v", rows)
	}
	// An ended absence tells nobody.
	set(chatrecipient.Away{Since: now.Add(-5 * time.Hour), Until: now.Add(48 * time.Hour), CoverID: "u-1", NotifyCover: true, Hidden: false}, 3)
	later := now.Add(72 * time.Hour)
	if rows, err := r.CoverNotices(ctx, "host", "host", "u-1", later.Add(-chatrecipient.NotificationsWindow), later, 20); err != nil || len(rows) != 0 {
		t.Fatalf("ended absence still notifies: %+v %v", rows, err)
	}
	// Reading marks it, and a post that is not a notice is skipped.
	marked, err := r.MarkCoverRead(ctx, "host", "host", "u-1", now.Add(-chatrecipient.NotificationsWindow), now, []string{"mention-of-other", "secret-mention", "plain"}, false)
	if err != nil || marked != 1 {
		t.Fatalf("mark: %d %v", marked, err)
	}
	if after := issue3733Index(list("u-1")); !after["mention-of-other"].Read {
		t.Fatalf("not read after marking: %+v", after)
	}
}
