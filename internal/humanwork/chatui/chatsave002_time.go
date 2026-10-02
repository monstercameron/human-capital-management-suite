package chatui

import (
	"math"
	"time"
)

// CHATSAVE-002. Times in the Saved panel. Every function takes the reader's
// "now" and writes in its zone, so the labels follow the reader's wall clock;
// none reads the clock or the machine's zone itself.

func chatsave002Clock(locale string, t time.Time) string {
	switch dateLocale(locale) {
	case "de":
		return t.Format("15:04")
	case "ar":
		return arabicDigits(t.Format("15:04"))
	}
	return t.Format("3:04 PM")
}

func chatsave002DayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// chatsave002DayOffset is how many calendar days at lies from now's day.
func chatsave002DayOffset(at, now time.Time) int {
	return int(math.Round(chatsave002DayStart(at).Sub(chatsave002DayStart(now)).Hours() / 24))
}

func chatsave002Weekday(locale string, t time.Time) string {
	switch dateLocale(locale) {
	case "de":
		return deWeekdays[t.Weekday()]
	case "ar":
		return arWeekdays[t.Weekday()]
	}
	return t.Weekday().String()
}

// chatsave002Day names the day of at: Today, Yesterday, Tomorrow, the weekday
// within the coming week when weekdays is set, otherwise the short date, with
// the year when it is not this year.
func chatsave002Day(locale string, at, now time.Time, weekdays bool) string {
	at = at.In(now.Location())
	offset := chatsave002DayOffset(at, now)
	switch {
	case offset == 0:
		return chatsave002Text(locale, "today")
	case offset == -1:
		return chatsave002Text(locale, "yesterday")
	case offset == 1:
		return chatsave002Text(locale, "tomorrow")
	case weekdays && offset > 1 && offset < 7:
		return chatsave002Weekday(locale, at)
	case at.Year() != now.Year():
		return formatDay(locale, at, true)
	}
	return formatShortDate(locale, at)
}

// SavedTimeLabel is a message's time in the list: the clock for today, the day
// and the clock for any other day.
func SavedTimeLabel(locale string, at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	at = at.In(now.Location())
	if chatsave002DayOffset(at, now) == 0 {
		return chatsave002Clock(locale, at)
	}
	return chatsave002Day(locale, at, now, false) + ", " + chatsave002Clock(locale, at)
}

// SavedDueLabel is a reminder's time on its chip: "Tomorrow 9:00 AM",
// "Monday 9:00 AM", "Oct 14 9:00 AM".
func SavedDueLabel(locale string, at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	at = at.In(now.Location())
	return chatsave002Day(locale, at, now, true) + " " + chatsave002Clock(locale, at)
}

// SavedDoneLabel is the date a message was ticked off: "Today", "Yesterday" or
// the short date.
func SavedDoneLabel(locale string, at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	return chatsave002Day(locale, at, now, false)
}

// SavedReminderOption is one entry of the reminder menu; Key names what the
// click asks for and SavedReminderAt turns it into a time.
type SavedReminderOption struct{ Key, Label string }

// SavedReminderOptions are the menu's entries at now: In 1 hour, This
// afternoon (only while it is morning), Tomorrow, Next Monday, and the date picker.
func SavedReminderOptions(locale string, now time.Time) []SavedReminderOption {
	nine := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, now.Location())
	options := []SavedReminderOption{{"hour", chatsave002Text(locale, "p_hour")}}
	if now.Hour() < 12 {
		options = append(options, SavedReminderOption{"afternoon", chatsave002Text(locale, "p_afternoon")})
	}
	options = append(options,
		SavedReminderOption{"tomorrow", chatsave002Text(locale, "p_tomorrow", "time", chatsave002Clock(locale, nine))},
		SavedReminderOption{"monday", chatsave002Text(locale, "p_monday", "time", chatsave002Clock(locale, nine))},
		SavedReminderOption{"pick", chatsave002Text(locale, "p_pick")})
	return options
}

// SavedReminderAt is the time a preset means at now. The date picker has no
// time of its own and answers false.
func SavedReminderAt(key string, now time.Time) (time.Time, bool) {
	day := func(add, hour int) time.Time {
		return time.Date(now.Year(), now.Month(), now.Day()+add, hour, 0, 0, 0, now.Location())
	}
	switch key {
	case "hour":
		return now.Add(time.Hour), true
	case "afternoon":
		return day(0, 15), true
	case "tomorrow":
		return day(1, 9), true
	case "monday":
		ahead := (8 - int(now.Weekday())) % 7
		if ahead == 0 {
			ahead = 7
		}
		return day(ahead, 9), true
	}
	return time.Time{}, false
}

// SavedReminderOverdue is true for a reminder whose time has passed.
func SavedReminderOverdue(due, now time.Time) bool {
	return !due.IsZero() && due.Before(now)
}
