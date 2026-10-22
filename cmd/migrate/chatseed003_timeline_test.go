package main

import (
	"testing"
	"time"
)

// isWorkingMoment reports a weekday between 08:00 and 18:00 (closing time itself
// counts: the latest moment of a run at night is exactly that).
func isWorkingMoment(at time.Time, office *time.Location) bool {
	local := at.In(office)
	if chatSeedIsWeekend(local, office) {
		return false
	}
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, office)
	since := local.Sub(midnight)
	return since >= chatSeedWorkdayStart && since <= chatSeedWorkdayEnd
}

// Whatever moment the run starts at, in either office zone and across both
// daylight-saving changes, every place the timeline hands out is a working
// moment inside the window, and moving along it never goes backward.
func TestTodo_CHATSEED_003(t *testing.T) {
	for _, tenant := range []string{"harborcare-demo", "ironridge-demo"} {
		office := chatSeedOfficeZone(tenant)
		for _, first := range []time.Time{
			time.Date(2026, time.September, 21, 0, 0, 0, 0, time.UTC),
			time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC), // the fortnight crosses the November change
			time.Date(2026, time.March, 4, 0, 0, 0, 0, time.UTC),    // and the March one
		} {
			for step := 0; step < 7*8; step++ {
				now := first.Add(time.Duration(step) * 3 * time.Hour)
				start := now.Add(-chatSeedDays * 24 * time.Hour)
				line := newChatSeedTimeline(start, now, office)
				if line.Empty() {
					t.Fatalf("%s: no office time between %s and %s", tenant, start, now)
				}
				var previous time.Time
				for fraction := 0.0; fraction < 1; fraction += 0.005 {
					at := line.At(fraction)
					if !isWorkingMoment(at, office) || at.Before(start) || at.After(now) {
						t.Fatalf("%s now=%s: At(%.3f) = %s is outside office hours or the window", tenant, now.In(office).Format("Mon 15:04"), fraction, at.In(office).Format("Mon 2 Jan 15:04"))
					}
					if at.Before(previous) {
						t.Fatalf("%s now=%s: At(%.3f) went back in time", tenant, now.In(office).Format("Mon 15:04"), fraction)
					}
					previous = at
					next := line.Advance(at, 7*time.Minute)
					if !isWorkingMoment(next, office) || next.After(now) || next.Before(at) {
						t.Fatalf("%s now=%s: Advance(%s, 7m) = %s", tenant, now.In(office).Format("Mon 15:04"), at.In(office).Format("Mon 15:04"), next.In(office).Format("Mon 2 Jan 15:04"))
					}
				}
				back := line.Back(20 * time.Hour)
				if !isWorkingMoment(back, office) || back.After(now) || back.Before(start) {
					t.Fatalf("%s now=%s: Back(20h) = %s", tenant, now.In(office).Format("Mon 15:04"), back.In(office).Format("Mon 2 Jan 15:04"))
				}
			}
		}
	}
}

// A step that would pass closing carries to the next office morning, and one
// from Friday afternoon to Monday.
func TestChatSeedTimelineCarriesOverNightAndWeekend(t *testing.T) {
	office := chatSeedOfficeZone("harborcare-demo")
	at := func(clock string) time.Time {
		parsed, err := time.ParseInLocation("2006-01-02 15:04", clock, office)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	line := newChatSeedTimeline(at("2026-09-14 00:00"), at("2026-09-30 12:00"), office)
	for _, c := range []struct {
		from string
		by   time.Duration
		want string
	}{
		{"2026-09-16 10:00", 5 * time.Minute, "Wed 10:05"},
		{"2026-09-16 17:55", 20 * time.Minute, "Thu 08:15"},
		{"2026-09-18 17:50", 30 * time.Minute, "Mon 08:20"},
	} {
		if got := line.Advance(at(c.from), c.by).In(office).Format("Mon 15:04"); got != c.want {
			t.Errorf("Advance(%s, %s) = %s, want %s", c.from, c.by, got, c.want)
		}
	}
	// Back from a window that ends at 12:00 on a Wednesday: four working hours
	// reach 08:00, and one more minute is Tuesday's closing.
	if got := line.Back(4 * time.Hour).In(office).Format("Mon 15:04"); got != "Wed 08:00" {
		t.Errorf("Back(4h) = %s, want Wed 08:00", got)
	}
	if got := line.Back(4*time.Hour + time.Minute).In(office).Format("Mon 15:04"); got != "Tue 17:59" {
		t.Errorf("Back(4h1m) = %s, want Tue 17:59", got)
	}
}
