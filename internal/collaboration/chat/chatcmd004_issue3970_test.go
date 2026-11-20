package chat

import (
	"testing"
	"time"
)

// TestIssue3970_ISODeadlineStaysOnTheTypedDate: a date-only deadline is 23:59
// on the typed local date, also on the shorter and longer days of a daylight
// saving change, and agrees with the written-out form of the same date.
func TestIssue3970_ISODeadlineStaysOnTheTypedDate(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tz database")
	}
	now := time.Date(2027, 1, 5, 9, 0, 0, 0, zone)
	for _, c := range []struct{ iso, named string }{
		{"2027-03-13", "march 13 2027"}, {"2027-03-14", "march 14 2027"}, {"2027-03-15", "march 15 2027"},
		{"2027-11-06", "november 6 2027"}, {"2027-11-07", "november 7 2027"}, {"2027-11-08", "november 8 2027"},
		{"2027-06-10", "june 10 2027"},
	} {
		got, err := Chatcmd004ResolveDate(c.iso, now)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := Chatcmd004ResolveDate(c.named, now)
		if got.Format("2006-01-02 15:04") != c.iso+" 23:59" || !got.Equal(want) {
			t.Errorf("%s resolved to %s, written-out form %s", c.iso, got.Format(time.RFC3339), want.Format(time.RFC3339))
		}
	}
	d, _ := Chatcmd004ParseTodo("Review schedule by 2027-03-14", nil, "me", now, Chatcmd004ResolveDate)
	if len(d.Changes) != 1 || d.Changes[0].After != "2027-03-14" {
		t.Fatalf("preview date: %+v", d.Changes)
	}
	// Other zones, including one that skips midnight and one east of UTC.
	for _, name := range []string{"America/Sao_Paulo", "Europe/Berlin", "Pacific/Auckland", "Australia/Lord_Howe"} {
		z, err := time.LoadLocation(name)
		if err != nil {
			continue
		}
		for _, iso := range []string{"2027-03-28", "2027-10-31", "2027-04-04", "2027-09-26", "2027-10-03", "2027-04-04"} {
			got, _ := Chatcmd004ResolveDate(iso, time.Date(2027, 1, 5, 9, 0, 0, 0, z))
			if got.Format("2006-01-02 15:04") != iso+" 23:59" {
				t.Errorf("%s %s -> %s", name, iso, got.Format(time.RFC3339))
			}
		}
	}
}
