package main

import (
	"testing"
	"time"
)

// Every moment of the seeded fortnight, laid onto office hours, reads as a
// working hour in Denver and keeps its order.
func TestChatSeedWorkdayKeepsOrderAndOfficeHours(t *testing.T) {
	office := chatSeedOfficeZone("ironridge-demo")
	if office.String() != "America/Denver" {
		t.Fatalf("Ironridge office zone = %s", office)
	}
	if zone := chatSeedOfficeZone("harborcare-demo"); zone.String() != "America/New_York" {
		t.Fatalf("HarborCare office zone = %s", zone)
	}
	start := time.Date(2026, time.September, 18, 3, 17, 0, 0, time.UTC)
	var previous time.Time
	for step := 0; step < 14*24*4; step++ {
		raw := start.Add(time.Duration(step) * 15 * time.Minute)
		got := chatSeedWorkday(raw, office)
		local := got.In(office)
		if local.Hour() < 8 || local.Hour() >= 18 {
			t.Fatalf("%s became %s, outside 08:00 to 18:00", raw.In(office).Format("Mon 15:04"), local.Format("Mon 15:04"))
		}
		if !previous.IsZero() && got.Before(previous) {
			t.Fatalf("%s moved before the moment ahead of it", raw.In(office).Format("Mon 15:04"))
		}
		previous = got
	}
}

func TestChatSeedReplyTimeNeverLandsAtNight(t *testing.T) {
	office := chatSeedOfficeZone("ironridge-demo")
	for _, clock := range []string{"2026-09-22 17:55", "2026-09-22 21:40", "2026-09-22 02:46", "2026-09-22 11:04"} {
		at, err := time.ParseInLocation("2006-01-02 15:04", clock, office)
		if err != nil {
			t.Fatal(err)
		}
		got := chatSeedReplyTime(at, office).In(office)
		if got.Hour() < 8 || got.Hour() >= 18 {
			t.Errorf("reply at %s became %s", clock, got.Format("Mon 15:04"))
		}
		if got.Before(at) && at.In(office).Hour() >= 8 {
			t.Errorf("reply at %s moved earlier, to %s", clock, got.Format("Mon 15:04"))
		}
	}
}
