package timeclockapp

import (
	"strconv"
	"time"
)

// ClockFace is a wall-clock reading split for display: the large hour and
// minute, the smaller seconds, and the meridiem (empty for 24-hour locales).
type ClockFace struct {
	HourMinute string
	Seconds    string
	Meridiem   string
	// ISO is the machine-readable instant for a <time datetime> attribute.
	ISO string
}

// SiteLocation loads the site timezone from the device record, falling back
// to UTC for an empty or unknown name so the clock still runs.
func SiteLocation(name string) *time.Location {
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Face formats t in loc for locale l. en-US and ar use a 12-hour clock with
// a localized meridiem; de-DE uses 24 hours. Digits are localized.
func (l Locale) Face(t time.Time, loc *time.Location) ClockFace {
	local := t.In(loc)
	hour := local.Hour()
	meridiem := ""
	if l != LocaleDeDE {
		if hour < 12 {
			meridiem = l.Text("meridiem.am")
		} else {
			meridiem = l.Text("meridiem.pm")
		}
		hour %= 12
		if hour == 0 {
			hour = 12
		}
	}
	hm := strconv.Itoa(hour)
	if l == LocaleDeDE {
		hm = twoDigits(hour)
	}
	hm += ":" + twoDigits(local.Minute())
	return ClockFace{
		HourMinute: l.Digits(hm),
		Seconds:    l.Digits(twoDigits(local.Second())),
		Meridiem:   meridiem,
		ISO:        local.Format(time.RFC3339),
	}
}

// ShortTime is the one-line "10:42:13 AM" form used on the receipt.
func (l Locale) ShortTime(t time.Time, loc *time.Location) string {
	f := l.Face(t, loc)
	out := f.HourMinute + ":" + f.Seconds
	if f.Meridiem != "" {
		out += " " + f.Meridiem
	}
	return out
}

// Date formats the weekday and date line under the clock.
func (l Locale) Date(t time.Time, loc *time.Location) string {
	local := t.In(loc)
	return l.Text("date.format",
		"weekday", l.Text("day."+strconv.Itoa(int(local.Weekday()))),
		"month", l.Text("month."+strconv.Itoa(int(local.Month()))),
		"day", l.Digits(strconv.Itoa(local.Day())),
	)
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
