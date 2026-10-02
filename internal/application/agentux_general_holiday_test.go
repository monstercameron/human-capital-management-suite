package application

import (
	"strings"
	"testing"
	"time"
)

func TestAgentUXGeneral_HolidayGuideCalendar(t *testing.T) {
	cases := []struct {
		month time.Month
		day   int
		want  time.Weekday
		row   string
	}{
		{time.January, 1, time.Thursday, "New Year's Day | Jan 1 | Thursday"},
		{time.January, 19, time.Monday, "Martin Luther King Jr. Day | Jan 19 | Monday"},
		{time.February, 16, time.Monday, "Presidents' Day | Feb 16 | Monday"},
		{time.May, 25, time.Monday, "Memorial Day | May 25 | Monday"},
		{time.June, 19, time.Friday, "Juneteenth | Jun 19 | Friday"},
		{time.July, 3, time.Friday, "Independence Day (observed) | Jul 3 | Friday"},
		{time.September, 7, time.Monday, "Labor Day | Sep 7 | Monday"},
		{time.November, 26, time.Thursday, "Thanksgiving Day | Nov 26 | Thursday"},
		{time.November, 27, time.Friday, "Day after Thanksgiving | Nov 27 | Friday"},
		{time.December, 25, time.Friday, "Christmas Day | Dec 25 | Friday"},
	}
	for _, tc := range cases {
		if got := time.Date(2026, tc.month, tc.day, 0, 0, 0, 0, time.UTC).Weekday(); got != tc.want {
			t.Errorf("2026-%02d-%02d weekday=%s, want %s", tc.month, tc.day, got, tc.want)
		}
		if !strings.Contains(localAgentDemoHolidayMarkdown, "| "+tc.row+" |") {
			t.Errorf("holiday guide is missing row %q", tc.row)
		}
	}
	for _, rule := range []string{"Offices are closed on each listed day", "Saturday is observed on the Friday before", "Sunday is observed on the Monday after", "one and a half times"} {
		if !strings.Contains(localAgentDemoHolidayMarkdown, rule) {
			t.Errorf("holiday guide is missing rule %q", rule)
		}
	}
}
