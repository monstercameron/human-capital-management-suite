package main

import (
	"testing"
	"time"
)

func TestChatSeedOfficeZone(t *testing.T) {
	if zone := chatSeedOfficeZone("ironridge-demo"); zone.String() != "America/Denver" {
		t.Fatalf("Ironridge office zone = %s", zone)
	}
	if zone := chatSeedOfficeZone("harborcare-demo"); zone.String() != "America/New_York" {
		t.Fatalf("HarborCare office zone = %s", zone)
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

// A reply that would land on a weekend, or after Friday's closing, goes to
// Monday morning.
func TestChatSeedReplyTimeSkipsTheWeekend(t *testing.T) {
	office := chatSeedOfficeZone("ironridge-demo")
	for clock, want := range map[string]string{
		"2026-09-26 11:00": "Mon 11:00", // Saturday
		"2026-09-27 15:30": "Mon 15:30", // Sunday
		"2026-09-25 19:10": "Mon 09:10", // Friday after closing
		"2026-09-25 16:00": "Fri 16:00", // Friday in hours is left alone
	} {
		at, err := time.ParseInLocation("2006-01-02 15:04", clock, office)
		if err != nil {
			t.Fatal(err)
		}
		if got := chatSeedReplyTime(at, office).In(office).Format("Mon 15:04"); got != want {
			t.Errorf("reply at %s became %s, want %s", clock, got, want)
		}
	}
}

func TestLatestOfficeMomentIsNeverAtNightOrOnAWeekend(t *testing.T) {
	office := chatSeedOfficeZone("ironridge-demo")
	for clock, want := range map[string]string{
		"2026-09-26 11:00": "Fri 18:00", // Saturday
		"2026-09-27 23:00": "Fri 18:00", // Sunday night
		"2026-09-28 06:00": "Fri 18:00", // Monday before opening
		"2026-09-28 10:00": "Mon 10:00", // Monday in hours is now itself
	} {
		at, err := time.ParseInLocation("2006-01-02 15:04", clock, office)
		if err != nil {
			t.Fatal(err)
		}
		if got := latestOfficeMoment(at.UTC(), office).In(office).Format("Mon 15:04"); got != want {
			t.Errorf("latest office moment at %s = %s, want %s", clock, got, want)
		}
	}
}
