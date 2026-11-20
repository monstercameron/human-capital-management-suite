package chatrecipient

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3920: quiet hours that match a person's weekly work pattern.
//
// A person keeps the daily interval they already had (QuietHours.StartMinute and
// EndMinute) and may switch on a weekly schedule, one entry per weekday: no quiet
// time, a whole quiet day, or an interval (which may run overnight into the next
// day). The weekly schedule starts from the daily interval on every day, so
// switching it on changes nothing until the person edits a day. A one-off pause
// ("quiet until ...") or a temporary override ("notify me anyway until ...") is
// kept apart from the recurring schedule and always has an explicit end; once it
// passes, the schedule is back on its own.
//
// All of it is read on the wall clock of the person's chosen zone, so a window
// keeps its clock times across a daylight-saving change. It applies to optional
// chat notices only: a governed HCM notice never passes through it.

// QuietDayMode says what a weekday's quiet time is.
const (
	QuietDayNone     = "none"     // no quiet time that day
	QuietDayAll      = "all"      // the whole day is quiet
	QuietDayInterval = "interval" // quiet from StartMinute; an end before the start runs into the next day
)

// Pause kinds. A pause silences optional notices until Until; a notify override
// lifts the schedule until Until.
const (
	PausePause  = "pause"
	PauseNotify = "notify"
)

// MaxPause is the longest a one-off pause or override may run.
const MaxPause = 30 * 24 * time.Hour

type QuietDay struct {
	Mode                   string
	StartMinute, EndMinute int
}

// QuietWeek is the weekly schedule, indexed by time.Weekday (Sunday is 0).
// Active says whether it replaces the daily interval; the days are kept while it
// is off so switching it back on restores them.
type QuietWeek struct {
	Active bool
	Days   [7]QuietDay
}

// QuietPause is the one-off pause or override. A zero Kind is none.
type QuietPause struct {
	Kind  string
	Until time.Time
}

func (p QuietPause) activeAt(at time.Time) bool {
	return p.Kind != "" && at.Before(p.Until)
}

// QuietSchedule is everything this issue adds to a person's quiet hours.
type QuietSchedule struct {
	Week     QuietWeek
	Pause    QuietPause
	Revision uint64
}

// ScheduleStore is the repository half that keeps the weekly schedule. A
// repository without it cannot offer the weekly schedule; the daily interval is
// unaffected.
type ScheduleStore interface {
	QuietSchedule(ctx context.Context, home, subject string) (QuietSchedule, error)
	PutQuietSchedule(ctx context.Context, home, subject string, s QuietSchedule, expected uint64) (QuietSchedule, error)
}

const (
	WeekBadMode       = "bad_mode"
	WeekBadMinute     = "bad_minute"
	WeekEmptyInterval = "empty_interval"
	WeekOverlap       = "overlap"
)

// WeekError names the day and the reason a weekly schedule was refused, so the
// editor can say which day to change in plain words.
type WeekError struct {
	Day    time.Weekday
	Reason string
}

func (e *WeekError) Error() string {
	return fmt.Sprintf("quiet schedule: %s on %s", e.Reason, e.Day)
}
func (e *WeekError) Unwrap() error { return chat.ErrInvalidArgument }

// DefaultWeek is the weekly schedule that behaves exactly like the daily
// interval: the same interval every day, or a whole quiet day when the daily
// start equals its end.
func DefaultWeek(q QuietHours) QuietWeek {
	day := QuietDay{Mode: QuietDayInterval, StartMinute: q.StartMinute, EndMinute: q.EndMinute}
	if q.StartMinute == q.EndMinute {
		day = QuietDay{Mode: QuietDayAll}
	}
	w := QuietWeek{Active: true}
	for i := range w.Days {
		w.Days[i] = day
	}
	return w
}

// ValidateWeek refuses a schedule that is ambiguous: an unknown mode, a minute
// out of range, an interval whose start equals its end (a whole quiet day is
// "all day"), or an overnight interval that overlaps the next day's own interval.
func ValidateWeek(w QuietWeek) error {
	for i, d := range w.Days {
		day := time.Weekday(i)
		switch d.Mode {
		case QuietDayNone, QuietDayAll:
		case QuietDayInterval:
			if d.StartMinute < 0 || d.StartMinute > 1439 || d.EndMinute < 0 || d.EndMinute > 1439 {
				return &WeekError{Day: day, Reason: WeekBadMinute}
			}
			if d.StartMinute == d.EndMinute {
				return &WeekError{Day: day, Reason: WeekEmptyInterval}
			}
		default:
			return &WeekError{Day: day, Reason: WeekBadMode}
		}
	}
	for i, d := range w.Days {
		if d.Mode != QuietDayInterval || d.EndMinute >= d.StartMinute {
			continue
		}
		next := w.Days[(i+1)%7]
		if next.Mode == QuietDayInterval && next.StartMinute < d.EndMinute {
			return &WeekError{Day: time.Weekday((i + 1) % 7), Reason: WeekOverlap}
		}
	}
	return nil
}

func inInterval(minute int, d QuietDay) bool {
	if d.StartMinute < d.EndMinute {
		return minute >= d.StartMinute && minute < d.EndMinute
	}
	return minute >= d.StartMinute
}

// baseQuiet is whether the recurring schedule (weekly when it is on, otherwise
// the daily interval) is quiet at the wall-clock moment in loc.
func baseQuiet(q QuietHours, local time.Time) bool {
	minute := local.Hour()*60 + local.Minute()
	if !q.Schedule.Week.Active {
		if q.StartMinute == q.EndMinute {
			return true
		}
		return inInterval(minute, QuietDay{StartMinute: q.StartMinute, EndMinute: q.EndMinute}) ||
			(q.StartMinute > q.EndMinute && minute < q.EndMinute)
	}
	today := q.Schedule.Week.Days[local.Weekday()]
	switch today.Mode {
	case QuietDayAll:
		return true
	case QuietDayInterval:
		if inInterval(minute, today) {
			return true
		}
	}
	yesterday := q.Schedule.Week.Days[(int(local.Weekday())+6)%7]
	return yesterday.Mode == QuietDayInterval && yesterday.EndMinute < yesterday.StartMinute && minute < yesterday.EndMinute
}

// QuietAt reports whether optional notices are held at the moment at: a pause
// holds them, a notify override lifts the schedule, and otherwise the recurring
// schedule decides when it is switched on.
func QuietAt(q QuietHours, at time.Time) bool {
	if q.Schedule.Pause.activeAt(at) {
		return q.Schedule.Pause.Kind == PausePause
	}
	if !q.Enabled {
		return false
	}
	loc, err := time.LoadLocation(q.Timezone)
	if err != nil {
		return false
	}
	return baseQuiet(q, at.In(loc))
}

// NextQuietChange is the next moment, after at, that optional notices pause or
// resume: quiet is true when they pause then. It is false when nothing changes
// in the coming days (quiet hours off, never quiet, or always quiet).
func NextQuietChange(q QuietHours, at time.Time) (next time.Time, quiet bool, ok bool) {
	loc := time.UTC
	if z, err := time.LoadLocation(q.Timezone); err == nil {
		loc = z
	}
	minutes := map[int]bool{0: true}
	if q.Schedule.Week.Active {
		for _, d := range q.Schedule.Week.Days {
			if d.Mode == QuietDayInterval {
				minutes[d.StartMinute], minutes[d.EndMinute] = true, true
			}
		}
	} else {
		minutes[q.StartMinute], minutes[q.EndMinute] = true, true
	}
	var candidates []time.Time
	local := at.In(loc)
	for day := -1; day <= 9; day++ {
		for m := range minutes {
			c := time.Date(local.Year(), local.Month(), local.Day()+day, m/60, m%60, 0, 0, loc)
			if c.After(at) {
				candidates = append(candidates, c)
			}
		}
	}
	if q.Schedule.Pause.Kind != "" && q.Schedule.Pause.Until.After(at) {
		candidates = append(candidates, q.Schedule.Pause.Until)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Before(candidates[j]) })
	current := QuietAt(q, at)
	for _, c := range candidates {
		if now := QuietAt(q, c); now != current {
			return c, now, true
		}
	}
	return time.Time{}, false, false
}

const (
	ExplainNone      = "none"
	ExplainAllDay    = "all_day"
	ExplainSameDay   = "same_day"
	ExplainOvernight = "overnight"
)

// ExplainLine describes one weekday's quiet time in words a person can read
// back: nothing, the whole day, a window inside the day, or a window that runs
// overnight and ends the next day.
type ExplainLine struct {
	Day                    time.Weekday
	Kind                   string
	StartMinute, EndMinute int
	EndsDay                time.Weekday
}

// ExplainWeek returns one line per weekday, Sunday first.
func ExplainWeek(w QuietWeek) []ExplainLine {
	out := make([]ExplainLine, 7)
	for i, d := range w.Days {
		line := ExplainLine{Day: time.Weekday(i), Kind: ExplainNone, EndsDay: time.Weekday(i)}
		switch d.Mode {
		case QuietDayAll:
			line.Kind = ExplainAllDay
		case QuietDayInterval:
			line.StartMinute, line.EndMinute = d.StartMinute, d.EndMinute
			line.Kind = ExplainSameDay
			if d.EndMinute < d.StartMinute {
				line.Kind, line.EndsDay = ExplainOvernight, time.Weekday((i+1)%7)
			}
		}
		out[i] = line
	}
	return out
}

func (s *Service) scheduleStore() (ScheduleStore, error) {
	if s == nil || s.Repo == nil {
		return nil, chat.ErrUnavailable
	}
	store, ok := s.Repo.(ScheduleStore)
	if !ok {
		return nil, chat.ErrUnavailable
	}
	return store, nil
}

// QuietSchedule returns the caller's weekly schedule and pause. A pause whose
// end has passed is not returned.
func (s *Service) QuietSchedule(ctx context.Context, p chat.Principal, now time.Time) (QuietSchedule, error) {
	store, err := s.scheduleStore()
	if err != nil {
		return QuietSchedule{}, err
	}
	if p.TenantID == "" || p.SubjectID == "" {
		return QuietSchedule{}, chat.ErrInvalidArgument
	}
	out, err := store.QuietSchedule(ctx, p.TenantID, p.SubjectID)
	if err != nil {
		return QuietSchedule{}, err
	}
	if !out.Pause.activeAt(now) {
		out.Pause = QuietPause{}
	}
	return out, nil
}

// PutQuietSchedule saves the caller's weekly schedule and pause. The revision
// check is the same per-person, per-tenant check the daily interval uses.
func (s *Service) PutQuietSchedule(ctx context.Context, p chat.Principal, want QuietSchedule, expected uint64, now time.Time) (QuietSchedule, error) {
	store, err := s.scheduleStore()
	if err != nil {
		return QuietSchedule{}, err
	}
	if p.TenantID == "" || p.SubjectID == "" || expected == 0 {
		return QuietSchedule{}, chat.ErrInvalidArgument
	}
	if err := ValidateWeek(want.Week); err != nil {
		return QuietSchedule{}, err
	}
	switch want.Pause.Kind {
	case "":
		want.Pause = QuietPause{}
	case PausePause, PauseNotify:
		if !want.Pause.Until.After(now) || want.Pause.Until.After(now.Add(MaxPause)) {
			return QuietSchedule{}, errors.Join(chat.ErrInvalidArgument, errors.New("a pause needs an end within the next 30 days"))
		}
		want.Pause.Until = want.Pause.Until.UTC()
	default:
		return QuietSchedule{}, chat.ErrInvalidArgument
	}
	return store.PutQuietSchedule(ctx, p.TenantID, p.SubjectID, want, expected)
}

// LastQuietResume is the most recent moment, up to nine days back, that the
// recurring schedule (never a one-off pause) went from quiet to not quiet. It is
// false when the schedule has not ended a quiet stretch in that time.
func LastQuietResume(q QuietHours, now time.Time) (time.Time, bool) {
	q.Schedule.Pause = QuietPause{}
	if !q.Enabled {
		return time.Time{}, false
	}
	loc := time.UTC
	if z, err := time.LoadLocation(q.Timezone); err == nil {
		loc = z
	}
	minutes := map[int]bool{0: true}
	if q.Schedule.Week.Active {
		for _, d := range q.Schedule.Week.Days {
			if d.Mode == QuietDayInterval {
				minutes[d.StartMinute], minutes[d.EndMinute] = true, true
			}
		}
	} else {
		minutes[q.StartMinute], minutes[q.EndMinute] = true, true
	}
	var candidates []time.Time
	local := now.In(loc)
	for day := -9; day <= 0; day++ {
		for m := range minutes {
			c := time.Date(local.Year(), local.Month(), local.Day()+day, m/60, m%60, 0, 0, loc)
			if !c.After(now) {
				candidates = append(candidates, c)
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].After(candidates[j]) })
	for _, c := range candidates {
		if !QuietAt(q, c) && QuietAt(q, c.Add(-time.Minute)) {
			return c, true
		}
	}
	return time.Time{}, false
}
