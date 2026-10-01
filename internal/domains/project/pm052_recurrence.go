package project

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// RecurrenceFrequency is intentionally small. A schedule describes local
// calendar intent; it is not a duration-based timer.
type RecurrenceFrequency string

const (
	RecurrenceDaily   RecurrenceFrequency = "DAILY"
	RecurrenceWeekly  RecurrenceFrequency = "WEEKLY"
	RecurrenceMonthly RecurrenceFrequency = "MONTHLY"
)

type CatchUpPolicy string

const (
	CatchUpSkip   CatchUpPolicy = "SKIP"
	CatchUpAll    CatchUpPolicy = "ALL"
	CatchUpLatest CatchUpPolicy = "LATEST"
)

var (
	ErrInvalidRecurrence = errors.New("project: invalid recurrence schedule")
	ErrRecurrencePaused  = errors.New("project: recurrence schedule is paused")
)

// RecurrenceSchedule is a versioned, project-owned local-time policy. StartAt
// supplies both the local clock and the initial calendar anchor. Weekdays are
// used only by weekly schedules; DayOfMonth is used only by monthly schedules.
type RecurrenceSchedule struct {
	ID         string
	ProjectID  ProjectID
	Version    uint64
	Timezone   string
	Frequency  RecurrenceFrequency
	Interval   int
	StartAt    time.Time
	Weekdays   []time.Weekday
	DayOfMonth int
	Paused     bool
	PausedAt   time.Time
	ResumesAt  time.Time
	CatchUp    CatchUpPolicy
}

type RecurrenceOccurrence struct {
	ScheduleID      string
	ScheduleVersion uint64
	StableKey       string
	LocalStart      string
	StartsAt        time.Time
}

// Generate returns the occurrences in the half-open [from, through] window.
// Existing stable keys are treated as committed task-generation receipts, so
// retrying the same window cannot create a duplicate task.
func (s RecurrenceSchedule) Generate(from, through time.Time, existingKeys map[string]struct{}) ([]RecurrenceOccurrence, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if from.IsZero() || through.IsZero() || !from.Before(through) {
		return nil, ErrInvalidRecurrence
	}
	if s.Paused && (s.ResumesAt.IsZero() || through.Before(s.ResumesAt)) {
		return nil, ErrRecurrencePaused
	}

	location, _ := time.LoadLocation(s.Timezone)
	start := s.StartAt.In(location)
	windowStart := from.In(location)
	windowEnd := through.In(location)
	occurrences := make([]RecurrenceOccurrence, 0)
	for date := calendarDate(windowStart); !date.After(calendarDate(windowEnd)); date = date.AddDate(0, 0, 1) {
		candidate, ok := s.candidateOn(date, start, location)
		if !ok || candidate.Before(from) || !candidate.Before(through) {
			continue
		}
		if s.Paused && candidate.After(s.PausedAt) && (s.ResumesAt.IsZero() || candidate.Before(s.ResumesAt)) {
			if s.CatchUp == CatchUpSkip {
				continue
			}
		}
		occurrences = append(occurrences, s.occurrence(candidate, location))
	}
	if s.Paused && s.CatchUp == CatchUpLatest {
		occurrences = latestMissedOccurrence(occurrences, s.PausedAt, s.ResumesAt)
	}
	sort.Slice(occurrences, func(i, j int) bool { return occurrences[i].StartsAt.Before(occurrences[j].StartsAt) })
	result := make([]RecurrenceOccurrence, 0, len(occurrences))
	seen := make(map[string]struct{}, len(existingKeys)+len(occurrences))
	for key := range existingKeys {
		seen[key] = struct{}{}
	}
	for _, occurrence := range occurrences {
		if _, exists := seen[occurrence.StableKey]; exists {
			continue
		}
		seen[occurrence.StableKey] = struct{}{}
		result = append(result, occurrence)
	}
	return result, nil
}

func (s RecurrenceSchedule) validate() error {
	if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(string(s.ProjectID)) == "" || s.Version == 0 || strings.TrimSpace(s.Timezone) == "" || s.StartAt.IsZero() || s.Interval < 1 {
		return ErrInvalidRecurrence
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return fmt.Errorf("%w: timezone", ErrInvalidRecurrence)
	}
	switch s.Frequency {
	case RecurrenceDaily:
	case RecurrenceWeekly:
		if len(s.Weekdays) == 0 {
			return ErrInvalidRecurrence
		}
		seen := map[time.Weekday]bool{}
		for _, weekday := range s.Weekdays {
			if seen[weekday] {
				return ErrInvalidRecurrence
			}
			seen[weekday] = true
		}
	case RecurrenceMonthly:
		if s.DayOfMonth < 1 || s.DayOfMonth > 31 {
			return ErrInvalidRecurrence
		}
	default:
		return ErrInvalidRecurrence
	}
	if s.PausedAt.IsZero() && !s.ResumesAt.IsZero() || !s.PausedAt.IsZero() && !s.ResumesAt.IsZero() && !s.PausedAt.Before(s.ResumesAt) {
		return ErrInvalidRecurrence
	}
	if s.Paused && s.PausedAt.IsZero() {
		return ErrInvalidRecurrence
	}
	switch s.CatchUp {
	case CatchUpSkip, CatchUpAll, CatchUpLatest:
	default:
		return ErrInvalidRecurrence
	}
	return nil
}

func (s RecurrenceSchedule) candidateOn(date, start time.Time, location *time.Location) (time.Time, bool) {
	startDate := calendarDate(start)
	switch s.Frequency {
	case RecurrenceDaily:
		days := daysBetween(startDate, date)
		if days < 0 || days%s.Interval != 0 {
			return time.Time{}, false
		}
	case RecurrenceWeekly:
		weekStart := startDate.AddDate(0, 0, -int(startDate.Weekday()))
		days := daysBetween(weekStart, date)
		if days < 0 || days/7%s.Interval != 0 {
			return time.Time{}, false
		}
		allowed := false
		for _, weekday := range s.Weekdays {
			allowed = allowed || weekday == date.Weekday()
		}
		if !allowed {
			return time.Time{}, false
		}
	case RecurrenceMonthly:
		months := (date.Year()-startDate.Year())*12 + int(date.Month()-startDate.Month())
		if months < 0 || months%s.Interval != 0 || date.Day() != s.DayOfMonth {
			return time.Time{}, false
		}
	}
	candidate := time.Date(date.Year(), date.Month(), date.Day(), start.Hour(), start.Minute(), start.Second(), start.Nanosecond(), location)
	return candidate, true
}

func (s RecurrenceSchedule) occurrence(candidate time.Time, location *time.Location) RecurrenceOccurrence {
	local := candidate.In(location)
	return RecurrenceOccurrence{
		ScheduleID:      s.ID,
		ScheduleVersion: s.Version,
		StableKey:       fmt.Sprintf("%s:v%d:%s", s.ID, s.Version, local.Format("2006-01-02T15:04:05.999999999")),
		LocalStart:      local.Format("2006-01-02T15:04:05"),
		StartsAt:        candidate,
	}
}

func latestMissedOccurrence(occurrences []RecurrenceOccurrence, pausedAt, resumesAt time.Time) []RecurrenceOccurrence {
	var latest *RecurrenceOccurrence
	for i := range occurrences {
		candidate := occurrences[i]
		if candidate.StartsAt.After(pausedAt) && (resumesAt.IsZero() || candidate.StartsAt.Before(resumesAt)) {
			copy := candidate
			latest = &copy
		}
	}
	if latest == nil {
		return occurrences
	}
	result := make([]RecurrenceOccurrence, 0, len(occurrences))
	for _, occurrence := range occurrences {
		missed := occurrence.StartsAt.After(pausedAt) && (resumesAt.IsZero() || occurrence.StartsAt.Before(resumesAt))
		if !missed || occurrence.StableKey == latest.StableKey {
			result = append(result, occurrence)
		}
	}
	return result
}

func calendarDate(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func daysBetween(from, to time.Time) int {
	fromUTC := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	toUTC := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	return int(toUTC.Sub(fromUTC).Hours() / 24)
}
