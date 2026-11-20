package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

func issue3920Week() chatrecipient.QuietWeek {
	w := chatrecipient.QuietWeek{Active: true}
	for i := range w.Days {
		w.Days[i] = chatrecipient.QuietDay{Mode: chatrecipient.QuietDayNone}
	}
	w.Days[time.Saturday] = chatrecipient.QuietDay{Mode: chatrecipient.QuietDayAll}
	w.Days[time.Monday] = chatrecipient.QuietDay{Mode: chatrecipient.QuietDayInterval, StartMinute: 22 * 60, EndMinute: 7 * 60}
	return w
}

func TestIssue3920_QuietSchedule_Integration(t *testing.T) {
	_, r := issue3733Seed(t)
	ctx := context.Background()
	// Nobody has a schedule yet: the daily interval is untouched.
	none, err := r.QuietSchedule(ctx, "host", "u-1")
	if err != nil || none.Week.Active || none.Revision != 1 || none.Pause.Kind != "" {
		t.Fatalf("never chosen: %+v %v", none, err)
	}
	hours, err := r.QuietHours(ctx, "host", "u-1")
	if err != nil || hours.Schedule.Week.Active || hours.StartMinute != 1320 || hours.EndMinute != 420 {
		t.Fatalf("daily interval changed: %+v %v", hours, err)
	}
	until := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	saved, err := r.PutQuietSchedule(ctx, "host", "u-1", chatrecipient.QuietSchedule{Week: issue3920Week(), Pause: chatrecipient.QuietPause{Kind: chatrecipient.PausePause, Until: until}}, 1)
	if err != nil || saved.Revision != 2 {
		t.Fatalf("first save: %+v %v", saved, err)
	}
	got, err := r.QuietSchedule(ctx, "host", "u-1")
	if err != nil || !got.Week.Active || got.Revision != 2 || got.Week.Days[time.Saturday].Mode != chatrecipient.QuietDayAll ||
		got.Week.Days[time.Monday].StartMinute != 22*60 || got.Week.Days[time.Monday].EndMinute != 7*60 ||
		got.Pause.Kind != chatrecipient.PausePause || !got.Pause.Until.Equal(until) {
		t.Fatalf("read back: %+v %v", got, err)
	}
	// Every reader of the person's quiet hours sees it, and the daily interval is still there.
	hours, err = r.QuietHours(ctx, "host", "u-1")
	if err != nil || !hours.Schedule.Week.Active || hours.Schedule.Pause.Kind != chatrecipient.PausePause || hours.StartMinute != 1320 {
		t.Fatalf("quiet hours reads: %+v %v", hours, err)
	}
	// A stale revision is a conflict and changes nothing.
	if _, err := r.PutQuietSchedule(ctx, "host", "u-1", chatrecipient.QuietSchedule{Week: issue3920Week()}, 1); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale write = %v, want a conflict", err)
	}
	// The next write uses the revision read, and a pause can be cleared.
	cleared, err := r.PutQuietSchedule(ctx, "host", "u-1", chatrecipient.QuietSchedule{Week: issue3920Week()}, 2)
	if err != nil || cleared.Revision != 3 {
		t.Fatalf("second save: %+v %v", cleared, err)
	}
	if got, _ = r.QuietSchedule(ctx, "host", "u-1"); got.Pause.Kind != "" {
		t.Fatalf("the pause is still there: %+v", got.Pause)
	}
	// Saving the daily interval does not wipe the schedule.
	if _, err := r.PutQuietHours(ctx, "host", "u-1", chatrecipient.QuietHours{Timezone: "UTC", StartMinute: 60, EndMinute: 120, Enabled: true}, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ = r.QuietSchedule(ctx, "host", "u-1"); !got.Week.Active || got.Revision != 3 {
		t.Fatalf("the daily save wiped the weekly schedule: %+v", got)
	}
}

func TestIssue3920_QuietScheduleIsPrivateToThePerson_Security(t *testing.T) {
	_, r := issue3733Seed(t)
	ctx := context.Background()
	if _, err := r.PutQuietSchedule(ctx, "host", "u-1", chatrecipient.QuietSchedule{Week: issue3920Week()}, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.QuietSchedule(ctx, "host", "u-2"); got.Week.Active {
		t.Fatal("another person reads the schedule")
	}
	if got, _ := r.QuietSchedule(ctx, "other-tenant", "u-1"); got.Week.Active {
		t.Fatal("another company reads the schedule")
	}
	// Another person writing at revision 1 creates their own row; it cannot
	// replace u-1's.
	if _, err := r.PutQuietSchedule(ctx, "host", "u-2", chatrecipient.QuietSchedule{Week: chatrecipient.QuietWeek{Days: issue3920Week().Days}}, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.QuietSchedule(ctx, "host", "u-1"); !got.Week.Active || got.Revision != 2 {
		t.Fatalf("u-1's schedule was touched: %+v", got)
	}
}

func TestIssue3920_QuietScheduleTableRefusesBadRows(t *testing.T) {
	_, r := issue3733Seed(t)
	ctx := context.Background()
	bad := issue3920Week()
	// A pause with no end is refused by the table as well as the service.
	if _, err := r.PutQuietSchedule(ctx, "host", "u-1", chatrecipient.QuietSchedule{Week: bad, Pause: chatrecipient.QuietPause{Kind: "forever", Until: time.Now().Add(time.Hour)}}, 1); err == nil {
		t.Fatal("an unknown pause kind was stored")
	}
}
