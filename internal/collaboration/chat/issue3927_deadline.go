package chat

import (
	"errors"
	"time"
)

// A poll's closing time can be chosen as an exact date and clock time in a
// named zone (#3927). The wall time is read in that zone and refused when it is
// not a real, single, future moment: a time that does not exist (the hour a
// clock skips in spring), one that happens twice (the hour a clock repeats in
// autumn), one that has passed, or text that is not a date and a time.

var (
	ErrDeadlineInvalid   = errors.New("chat: deadline is not a date and time")
	ErrDeadlinePast      = errors.New("chat: deadline has passed")
	ErrDeadlineNotExists = errors.New("chat: deadline falls in the hour a clock skips")
	ErrDeadlineAmbiguous = errors.New("chat: deadline falls in the hour a clock repeats")
)

// Chatcmd004ResolveDateTime reads date (2006-01-02) and clock (15:04) as a wall
// time in loc and returns the one instant it names, which must lie after now.
func Chatcmd004ResolveDateTime(date, clock string, loc *time.Location, now time.Time) (time.Time, error) {
	if loc == nil {
		loc = time.UTC
	}
	wall, err := time.Parse("2006-01-02 15:04", date+" "+clock)
	if err != nil {
		return time.Time{}, ErrDeadlineInvalid
	}
	// Every offset the zone uses within a day either side of the wall time; an
	// instant is a reading of the wall time only if the zone shows that same
	// wall time at it.
	var found []time.Time
	probe := wall.Add(-24 * time.Hour)
	for _, step := range []time.Duration{0, 12 * time.Hour, 24 * time.Hour, 36 * time.Hour, 48 * time.Hour} {
		_, offset := time.Date(probe.Year(), probe.Month(), probe.Day(), probe.Hour(), probe.Minute(), 0, 0, time.UTC).Add(step).In(loc).Zone()
		at := wall.Add(-time.Duration(offset) * time.Second)
		local := at.In(loc)
		if local.Year() != wall.Year() || local.Month() != wall.Month() || local.Day() != wall.Day() || local.Hour() != wall.Hour() || local.Minute() != wall.Minute() {
			continue
		}
		dup := false
		for _, f := range found {
			dup = dup || f.Equal(at)
		}
		if !dup {
			found = append(found, at)
		}
	}
	switch len(found) {
	case 0:
		return time.Time{}, ErrDeadlineNotExists
	case 1:
	default:
		return time.Time{}, ErrDeadlineAmbiguous
	}
	at := found[0].In(loc)
	if !at.After(now) {
		return time.Time{}, ErrDeadlinePast
	}
	return at, nil
}
