package chatrecipient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("zone data: %v", err)
	}
	return loc
}

// weekOf is an active week where every day is the given day.
func weekOf(day QuietDay) QuietWeek {
	var w QuietWeek
	w.Active = true
	for i := range w.Days {
		w.Days[i] = day
	}
	return w
}

func TestIssue3920_WeeklyDayModes(t *testing.T) {
	week := weekOf(QuietDay{Mode: QuietDayNone})
	week.Days[time.Saturday] = QuietDay{Mode: QuietDayAll}
	week.Days[time.Sunday] = QuietDay{Mode: QuietDayAll}
	week.Days[time.Monday] = QuietDay{Mode: QuietDayInterval, StartMinute: 12 * 60, EndMinute: 13 * 60}
	q := QuietHours{Enabled: true, Timezone: "UTC", StartMinute: 22 * 60, EndMinute: 7 * 60, Schedule: QuietSchedule{Week: week}}
	// 2026-03-07 is a Saturday.
	at := func(day, h, m int) time.Time { return time.Date(2026, 3, day, h, m, 0, 0, time.UTC) }
	for name, tc := range map[string]struct {
		at   time.Time
		want bool // quiet
	}{
		"whole Saturday is quiet":       {at(7, 15, 0), true},
		"whole Sunday is quiet":         {at(8, 3, 0), true},
		"Monday inside its interval":    {at(9, 12, 30), true},
		"Monday outside its interval":   {at(9, 14, 0), false},
		"Tuesday has no quiet":          {at(10, 3, 0), false},
		"Friday has no quiet":           {at(6, 23, 0), false},
		"the end of an interval is not": {at(9, 13, 0), false},
	} {
		if got := QuietAt(q, tc.at); got != tc.want {
			t.Errorf("%s: quiet = %v, want %v", name, got, tc.want)
		}
	}
}

func TestIssue3920_OvernightCrossesTheDayBoundary(t *testing.T) {
	week := weekOf(QuietDay{Mode: QuietDayNone})
	week.Days[time.Friday] = QuietDay{Mode: QuietDayInterval, StartMinute: 22 * 60, EndMinute: 7 * 60}
	q := QuietHours{Enabled: true, Timezone: "UTC", Schedule: QuietSchedule{Week: week}}
	// 2026-03-06 is a Friday.
	for name, tc := range map[string]struct {
		at   time.Time
		want bool
	}{
		"Friday evening":                     {time.Date(2026, 3, 6, 23, 0, 0, 0, time.UTC), true},
		"Saturday early morning":             {time.Date(2026, 3, 7, 6, 59, 0, 0, time.UTC), true},
		"Saturday after it ended":            {time.Date(2026, 3, 7, 7, 0, 0, 0, time.UTC), false},
		"Friday early morning is Thursdays":  {time.Date(2026, 3, 6, 3, 0, 0, 0, time.UTC), false},
		"Saturday evening has none":          {time.Date(2026, 3, 7, 23, 0, 0, 0, time.UTC), false},
		"Sunday early morning is not Friday": {time.Date(2026, 3, 8, 3, 0, 0, 0, time.UTC), false},
	} {
		if got := QuietAt(q, tc.at); got != tc.want {
			t.Errorf("%s: quiet = %v, want %v", name, got, tc.want)
		}
	}
}

// The schedule is read on the wall clock of the chosen zone, so the window keeps
// its clock times across a daylight-saving change.
func TestIssue3920_DaylightSavingKeepsTheWallClock(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	week := weekOf(QuietDay{Mode: QuietDayInterval, StartMinute: 22 * 60, EndMinute: 7 * 60})
	q := QuietHours{Enabled: true, Timezone: "America/New_York", Schedule: QuietSchedule{Week: week}}
	// US spring forward 2026-03-08 02:00 -> 03:00.
	for name, tc := range map[string]struct {
		at   time.Time
		want bool
	}{
		"before the change, still quiet at 01:30":  {time.Date(2026, 3, 8, 1, 30, 0, 0, ny), true},
		"after the change, still quiet at 06:59":   {time.Date(2026, 3, 8, 6, 59, 0, 0, ny), true},
		"07:00 wall time ends it on the short day": {time.Date(2026, 3, 8, 7, 0, 0, 0, ny), false},
		// US fall back 2026-11-01 02:00 -> 01:00 (the 01:00 hour happens twice).
		"the repeated hour is quiet the first time":  {time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC), true},
		"the repeated hour is quiet the second time": {time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC), true},
		"07:00 wall time ends it on the long day":    {time.Date(2026, 11, 1, 7, 0, 0, 0, ny), false},
	} {
		if got := QuietAt(q, tc.at); got != tc.want {
			t.Errorf("%s: quiet = %v, want %v", name, got, tc.want)
		}
	}
}

func TestIssue3920_ExistingDailyScheduleIsUnchanged(t *testing.T) {
	// A person who never touched the weekly schedule: the answer is the one the
	// daily interval gave before this capability existed.
	night := QuietHours{Enabled: true, Timezone: "UTC", StartMinute: 22 * 60, EndMinute: 7 * 60}
	allDay := QuietHours{Enabled: true, Timezone: "UTC", StartMinute: 0, EndMinute: 0}
	off := QuietHours{Timezone: "UTC", StartMinute: 22 * 60, EndMinute: 7 * 60}
	at := func(h int) time.Time { return time.Date(2026, 3, 4, h, 0, 0, 0, time.UTC) }
	for name, tc := range map[string]struct {
		q    QuietHours
		at   time.Time
		want bool
	}{
		"night, late":    {night, at(23), true},
		"night, midday":  {night, at(12), false},
		"all day":        {allDay, at(12), true},
		"switched off":   {off, at(23), false},
		"night, the end": {night, at(7), false},
	} {
		if got := QuietAt(tc.q, tc.at); got != tc.want {
			t.Errorf("%s: quiet = %v, want %v", name, got, tc.want)
		}
		// The old answer: ShouldNotify says "deliver" when it is not quiet.
		deliver, err := ShouldNotify(OptionalMessage, AllMessages, tc.q, tc.at)
		if err != nil || deliver == tc.want {
			t.Errorf("%s: ShouldNotify = %v (%v), want %v", name, deliver, err, !tc.want)
		}
	}
	// Turning the weekly schedule on starts from the same interval on every day,
	// so enabling it changes nothing.
	plain := night
	night.Schedule.Week = DefaultWeek(night)
	if !night.Schedule.Week.Active {
		t.Fatal("the derived week is not active")
	}
	for d := 0; d < 7; d++ {
		for _, h := range []int{0, 3, 6, 7, 12, 21, 22, 23} {
			when := time.Date(2026, 3, 1+d, h, 0, 0, 0, time.UTC)
			if QuietAt(night, when) != QuietAt(plain, when) {
				t.Fatalf("enabling the weekly schedule changed delivery at %v", when)
			}
		}
	}
	// A daily interval whose start equals its end is a whole quiet day, and the
	// derived week says so explicitly instead of keeping equal times.
	week := DefaultWeek(allDay)
	if week.Days[time.Wednesday].Mode != QuietDayAll {
		t.Fatalf("equal start and end derived %+v, want all day", week.Days[time.Wednesday])
	}
}

func TestIssue3920_ValidateWeek(t *testing.T) {
	ok := weekOf(QuietDay{Mode: QuietDayInterval, StartMinute: 22 * 60, EndMinute: 7 * 60})
	// Every night 22:00-07:00 spills into the next morning, and the next night
	// starts at 22:00, so nothing overlaps.
	if err := ValidateWeek(ok); err != nil {
		t.Fatalf("a repeating overnight window was refused: %v", err)
	}
	overlap := weekOf(QuietDay{Mode: QuietDayNone})
	overlap.Days[time.Monday] = QuietDay{Mode: QuietDayInterval, StartMinute: 22 * 60, EndMinute: 8 * 60}
	overlap.Days[time.Tuesday] = QuietDay{Mode: QuietDayInterval, StartMinute: 7 * 60, EndMinute: 9 * 60}
	var ve *WeekError
	if err := ValidateWeek(overlap); !errors.As(err, &ve) || !errors.Is(err, chat.ErrInvalidArgument) || ve.Reason != WeekOverlap || ve.Day != time.Tuesday {
		t.Fatalf("overlap = %v, want an overlap error on Tuesday", err)
	}
	empty := weekOf(QuietDay{Mode: QuietDayNone})
	empty.Days[time.Wednesday] = QuietDay{Mode: QuietDayInterval, StartMinute: 600, EndMinute: 600}
	if err := ValidateWeek(empty); !errors.As(err, &ve) || ve.Reason != WeekEmptyInterval || ve.Day != time.Wednesday {
		t.Fatalf("equal start and end = %v, want an empty-interval error on Wednesday (use all-day)", err)
	}
	bad := weekOf(QuietDay{Mode: "sometimes"})
	if err := ValidateWeek(bad); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("unknown mode = %v", err)
	}
	out := weekOf(QuietDay{Mode: QuietDayInterval, StartMinute: -1, EndMinute: 5})
	if err := ValidateWeek(out); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("minute out of range = %v", err)
	}
	// An overnight window that runs into a whole quiet day is one long pause, not
	// an overlap.
	merged := weekOf(QuietDay{Mode: QuietDayNone})
	merged.Days[time.Friday] = QuietDay{Mode: QuietDayInterval, StartMinute: 22 * 60, EndMinute: 7 * 60}
	merged.Days[time.Saturday] = QuietDay{Mode: QuietDayAll}
	if err := ValidateWeek(merged); err != nil {
		t.Fatalf("overnight into an all-day: %v", err)
	}
}

func TestIssue3920_NextPauseAndResume(t *testing.T) {
	week := weekOf(QuietDay{Mode: QuietDayNone})
	week.Days[time.Friday] = QuietDay{Mode: QuietDayInterval, StartMinute: 22 * 60, EndMinute: 7 * 60}
	week.Days[time.Saturday] = QuietDay{Mode: QuietDayAll}
	q := QuietHours{Enabled: true, Timezone: "UTC", Schedule: QuietSchedule{Week: week}}
	thu := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	next, quiet, ok := NextQuietChange(q, thu)
	if !ok || !quiet || !next.Equal(time.Date(2026, 3, 6, 22, 0, 0, 0, time.UTC)) {
		t.Fatalf("from Thursday: next = %v quiet=%v ok=%v, want Friday 22:00 pause", next, quiet, ok)
	}
	// Friday night through Saturday is one stretch: resume is Sunday 00:00.
	sat := time.Date(2026, 3, 7, 3, 0, 0, 0, time.UTC)
	next, quiet, ok = NextQuietChange(q, sat)
	if !ok || quiet || !next.Equal(time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("from Saturday: next = %v quiet=%v ok=%v, want Sunday 00:00 resume", next, quiet, ok)
	}
	// Nothing to pause for: no next change.
	none := QuietHours{Enabled: true, Timezone: "UTC", Schedule: QuietSchedule{Week: weekOf(QuietDay{Mode: QuietDayNone})}}
	if _, _, ok := NextQuietChange(none, thu); ok {
		t.Fatal("a week with no quiet time reported a next change")
	}
	// Switched off: no next change.
	q.Enabled = false
	if _, _, ok := NextQuietChange(q, thu); ok {
		t.Fatal("switched-off quiet hours reported a next change")
	}
	// Always quiet (every day all-day): no resume.
	always := QuietHours{Enabled: true, Timezone: "UTC", Schedule: QuietSchedule{Week: weekOf(QuietDay{Mode: QuietDayAll})}}
	if _, _, ok := NextQuietChange(always, thu); ok {
		t.Fatal("a week that is always quiet reported a resume")
	}
}

func TestIssue3920_PauseAndOverrideAreSeparateFromTheSchedule(t *testing.T) {
	now := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC) // midday: the schedule is not quiet
	until := now.Add(2 * time.Hour)
	q := QuietHours{Enabled: true, Timezone: "UTC", StartMinute: 22 * 60, EndMinute: 7 * 60,
		Schedule: QuietSchedule{Pause: QuietPause{Kind: PausePause, Until: until}}}
	if !QuietAt(q, now) {
		t.Fatal("a one-off pause did not silence optional notices")
	}
	next, quiet, ok := NextQuietChange(q, now)
	if !ok || quiet || !next.Equal(until) {
		t.Fatalf("pause: next = %v quiet=%v ok=%v, want resume at the explicit end", next, quiet, ok)
	}
	if QuietAt(q, until) {
		t.Fatal("the pause did not end at its explicit end")
	}
	// A pause works even while the recurring schedule is switched off.
	q.Enabled = false
	if !QuietAt(q, now) {
		t.Fatal("a pause must not depend on the recurring schedule being on")
	}
	// "Notify me anyway" lifts a scheduled quiet window until its end.
	night := time.Date(2026, 3, 5, 23, 0, 0, 0, time.UTC)
	o := QuietHours{Enabled: true, Timezone: "UTC", StartMinute: 22 * 60, EndMinute: 7 * 60,
		Schedule: QuietSchedule{Pause: QuietPause{Kind: PauseNotify, Until: night.Add(time.Hour)}}}
	if QuietAt(o, night) {
		t.Fatal("a temporary override did not lift the schedule")
	}
	next, quiet, ok = NextQuietChange(o, night)
	if !ok || !quiet || !next.Equal(night.Add(time.Hour)) {
		t.Fatalf("override: next = %v quiet=%v ok=%v, want the schedule back at the explicit end", next, quiet, ok)
	}
	// Once the end has passed, the recurring schedule is back on its own.
	if !QuietAt(o, night.Add(90*time.Minute)) {
		t.Fatal("the schedule did not return after the override ended")
	}
}

func TestIssue3920_GovernedNoticesAreNeverSuppressed_Security(t *testing.T) {
	now := time.Date(2026, 3, 5, 23, 0, 0, 0, time.UTC)
	q := QuietHours{Enabled: true, Timezone: "UTC", Schedule: QuietSchedule{
		Week:  weekOf(QuietDay{Mode: QuietDayAll}),
		Pause: QuietPause{Kind: PausePause, Until: now.Add(time.Hour)}}}
	if ok, err := ShouldNotify(GovernedHCM, Muted, q, now); err != nil || !ok {
		t.Fatalf("a governed HCM notice was suppressed: %v %v", ok, err)
	}
	if ok, err := ShouldNotify(OptionalMessage, AllMessages, q, now); err != nil || ok {
		t.Fatalf("an optional message was delivered while quiet: %v %v", ok, err)
	}
	// An optional message outside quiet time is delivered.
	q.Schedule = QuietSchedule{Week: weekOf(QuietDay{Mode: QuietDayNone})}
	if ok, err := ShouldNotify(OptionalMention, AllMessages, q, now); err != nil || !ok {
		t.Fatalf("a mention outside quiet hours was held: %v %v", ok, err)
	}
}

func TestIssue3920_InQuietHoursFollowsTheSchedule(t *testing.T) {
	week := weekOf(QuietDay{Mode: QuietDayNone})
	week.Days[time.Thursday] = QuietDay{Mode: QuietDayAll}
	q := QuietHours{Enabled: true, Timezone: "UTC", StartMinute: 22 * 60, EndMinute: 7 * 60, Schedule: QuietSchedule{Week: week}}
	if !InQuietHours(q, time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)) {
		t.Fatal("scheduled sends ignore an all-day quiet Thursday")
	}
	if InQuietHours(q, time.Date(2026, 3, 4, 23, 0, 0, 0, time.UTC)) {
		t.Fatal("scheduled sends used the old daily interval on a day with no quiet time")
	}
}

type scheduleRepo struct {
	repo
	schedule QuietSchedule
	puts     int
}

func (r *scheduleRepo) QuietSchedule(context.Context, string, string) (QuietSchedule, error) {
	out := r.schedule
	if out.Revision == 0 {
		out.Revision = 1
	}
	return out, nil
}
func (r *scheduleRepo) PutQuietSchedule(_ context.Context, _, _ string, s QuietSchedule, expected uint64) (QuietSchedule, error) {
	r.puts++
	cur := r.schedule.Revision
	if cur == 0 {
		cur = 1
	}
	if expected != cur {
		return QuietSchedule{}, chat.ErrConflict
	}
	s.Revision = cur + 1
	r.schedule = s
	return s, nil
}

func TestIssue3920_PutQuietSchedule_Validates(t *testing.T) {
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	db := &scheduleRepo{}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	ctx := context.Background()
	now := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)

	overlap := weekOf(QuietDay{Mode: QuietDayNone})
	overlap.Days[time.Monday] = QuietDay{Mode: QuietDayInterval, StartMinute: 22 * 60, EndMinute: 8 * 60}
	overlap.Days[time.Tuesday] = QuietDay{Mode: QuietDayInterval, StartMinute: 7 * 60, EndMinute: 9 * 60}
	if _, err := s.PutQuietSchedule(ctx, p, QuietSchedule{Week: overlap}, 1, now); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("overlapping week = %v", err)
	}
	if _, err := s.PutQuietSchedule(ctx, p, QuietSchedule{Pause: QuietPause{Kind: PausePause}}, 1, now); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("a pause with no end = %v, want an explicit end", err)
	}
	if _, err := s.PutQuietSchedule(ctx, p, QuietSchedule{Pause: QuietPause{Kind: PausePause, Until: now.Add(-time.Hour)}}, 1, now); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("a pause that already ended = %v", err)
	}
	if _, err := s.PutQuietSchedule(ctx, p, QuietSchedule{Pause: QuietPause{Kind: PausePause, Until: now.Add(45 * 24 * time.Hour)}}, 1, now); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("a pause past the longest allowed = %v", err)
	}
	if _, err := s.PutQuietSchedule(ctx, p, QuietSchedule{Pause: QuietPause{Kind: "forever", Until: now.Add(time.Hour)}}, 1, now); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("an unknown pause kind = %v", err)
	}
	if _, err := s.PutQuietSchedule(ctx, chat.Principal{}, QuietSchedule{}, 1, now); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no caller = %v", err)
	}
	if _, err := s.PutQuietSchedule(ctx, p, QuietSchedule{}, 0, now); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no expected revision = %v", err)
	}
	if db.puts != 0 {
		t.Fatalf("%d writes happened for refused input", db.puts)
	}
	good := QuietSchedule{Week: weekOf(QuietDay{Mode: QuietDayAll}), Pause: QuietPause{Kind: PausePause, Until: now.Add(time.Hour)}}
	got, err := s.PutQuietSchedule(ctx, p, good, 1, now)
	if err != nil || db.puts != 1 || got.Week.Days[0].Mode != QuietDayAll || got.Pause.Kind != PausePause || got.Revision != 2 {
		t.Fatalf("a valid schedule: %+v %v", got, err)
	}
	if _, err := s.PutQuietSchedule(ctx, p, good, 1, now); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("a stale revision = %v, want a conflict", err)
	}
	// A pause that has ended is cleared on read.
	read, err := s.QuietSchedule(ctx, p, now.Add(2*time.Hour))
	if err != nil || read.Pause.Kind != "" {
		t.Fatalf("an ended pause is still shown: %+v %v", read, err)
	}
}

func TestIssue3920_PutQuietSchedule_NeedsAStoreThatKeepsIt(t *testing.T) {
	s := &Service{Conversations: conversations{allowed: true}, Repo: &repo{}}
	_, err := s.QuietSchedule(context.Background(), chat.Principal{TenantID: "h", SubjectID: "a"}, time.Now())
	if !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("a store without schedules = %v, want unavailable", err)
	}
}

func TestIssue3920_Explain(t *testing.T) {
	week := weekOf(QuietDay{Mode: QuietDayNone})
	week.Days[time.Saturday] = QuietDay{Mode: QuietDayAll}
	week.Days[time.Sunday] = QuietDay{Mode: QuietDayAll}
	week.Days[time.Monday] = QuietDay{Mode: QuietDayInterval, StartMinute: 22 * 60, EndMinute: 7 * 60}
	lines := ExplainWeek(week)
	if len(lines) != 7 {
		t.Fatalf("%d lines, want 7", len(lines))
	}
	if lines[time.Saturday].Kind != ExplainAllDay || lines[time.Tuesday].Kind != ExplainNone {
		t.Fatalf("explain: %+v", lines)
	}
	if l := lines[time.Monday]; l.Kind != ExplainOvernight || l.StartMinute != 22*60 || l.EndMinute != 7*60 || l.EndsDay != time.Tuesday {
		t.Fatalf("monday explain: %+v", l)
	}
	same := ExplainWeek(weekOf(QuietDay{Mode: QuietDayInterval, StartMinute: 600, EndMinute: 700}))
	if same[time.Monday].Kind != ExplainSameDay {
		t.Fatalf("same-day explain: %+v", same[time.Monday])
	}
}

func TestIssue3920_DigestAtQuietEndFollowsTheWeeklySchedule(t *testing.T) {
	week := weekOf(QuietDay{Mode: QuietDayNone})
	week.Days[time.Friday] = QuietDay{Mode: QuietDayInterval, StartMinute: 22 * 60, EndMinute: 7 * 60}
	week.Days[time.Saturday] = QuietDay{Mode: QuietDayAll}
	q := QuietHours{Enabled: true, Timezone: "UTC", StartMinute: 22 * 60, EndMinute: 7 * 60, Schedule: QuietSchedule{Week: week}}
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC) // Tuesday
	got, ok := DigestInstant(DigestPref{Cadence: DigestQuietEnd}, q, now)
	want := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC) // Sunday 00:00 ends Friday night + Saturday
	if !ok || !got.Equal(want) {
		t.Fatalf("digest instant = %v %v, want %v (the weekly schedule, not the daily end of 07:00)", got, ok, want)
	}
	// Without a weekly schedule the daily end still decides.
	q.Schedule = QuietSchedule{}
	got, ok = DigestInstant(DigestPref{Cadence: DigestQuietEnd}, q, now)
	if !ok || !got.Equal(time.Date(2026, 3, 10, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("daily digest instant = %v %v", got, ok)
	}
}
