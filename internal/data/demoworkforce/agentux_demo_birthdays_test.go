package demoworkforce

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAgentUXDemo_BirthdayData(t *testing.T) {
	m, d, ok := DemoBirthday("ir-001-ana-flores")
	m2, d2, ok2 := DemoBirthday("ir-001-ana-flores")
	if !ok || !ok2 || m != m2 || d != d2 || !DemoBirthdayToday(m, d, time.Date(2026, m, d, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("unstable or invalid birthday: %v/%d, %v/%d", m, d, m2, d2)
	}
	for _, key := range []string{"", " ", " worker "} {
		if _, _, valid := DemoBirthday(key); valid {
			t.Fatalf("accepted key %q", key)
		}
	}
	for _, tc := range []struct {
		date string
		want bool
	}{{"2027-02-28", true}, {"2027-03-01", false}, {"2028-02-28", false}, {"2028-02-29", true}} {
		today, err := time.Parse(time.DateOnly, tc.date)
		if err != nil {
			t.Fatal(err)
		}
		if got := DemoBirthdayToday(time.February, 29, today); got != tc.want {
			t.Errorf("%s: got %t", tc.date, got)
		}
	}
	if DemoBirthdayToday(time.April, 31, time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("accepted impossible birthday")
	}
}

func TestAgentUXDemo_BirthdayPack(t *testing.T) {
	for _, key := range []string{"ironridge-demo", "harborcare-demo"} {
		pack, ok := PackFor(key)
		if !ok {
			t.Fatalf("missing demo pack %s", key)
		}
		people, err := pack.Plan(uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)))
		if err != nil || len(people) != pack.WorkerCount {
			t.Fatalf("demo pack %s: %d %v", key, len(people), err)
		}
		for _, person := range people {
			month, day, valid := DemoBirthday(person.Row.WorkerKey)
			if !valid || person.BirthdayMonth != month || person.BirthdayDay != day {
				t.Fatalf("missing deterministic month/day for %s: %v/%d", person.Row.WorkerKey, person.BirthdayMonth, person.BirthdayDay)
			}
		}
	}
}
