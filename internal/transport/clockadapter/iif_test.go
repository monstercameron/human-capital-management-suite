package clockadapter

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const timerIIF = "!TIMERHDR\tDATE\tJOB\tEMP\tITEM\tPITEM\tDURATION\tPROJ\tNOTE\tBILLINGSTATUS\n" +
	"TIMEACT\t2026-09-28\tAcme:Install\tAda Lovelace\tService\tRegular\t02:30\tWest\tquoted note\t1\n"

func TestParseQuickBooksIIF_NormalizesTimeActivity(t *testing.T) {
	got, err := ParseQuickBooksIIF([]byte(timerIIF), time.FixedZone("America/New_York", -4*60*60))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.ContentHash, "sha256:") || len(got.ContentHash) != len("sha256:")+64 {
		t.Fatalf("content hash: %q", got.ContentHash)
	}
	if len(got.Activities) != 1 {
		t.Fatalf("activities: %+v", got.Activities)
	}
	a := got.Activities[0]
	if a.Employee != "Ada Lovelace" || a.Job != "Acme:Install" || a.Duration != 150*time.Minute {
		t.Fatalf("activity: %+v", a)
	}
	if a.Date.Location().String() != "America/New_York" || a.Date.Hour() != 0 || a.Source != "quickbooks_iif" {
		t.Fatalf("date/source: %v %q", a.Date, a.Source)
	}
	if len(got.Unsupported) != 5 {
		t.Fatalf("unsupported fields: %+v", got.Unsupported)
	}
}

func TestParseQuickBooksIIF_RequiresTimezoneAndRejectsMalformed(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"no header", "TIMEACT\t2026-09-28\tjob\temp\t01:00\n"},
		{"duplicate header", "!TIMERHDR\tDATE\tEMP\tEMP\tJOB\tDURATION\n"},
		{"short row", "!TIMERHDR\tDATE\tJOB\tEMP\tDURATION\nTIMEACT\t2026-09-28\tjob\n"},
		{"bad duration", "!TIMERHDR\tDATE\tJOB\tEMP\tDURATION\nTIMEACT\t2026-09-28\tjob\temp\t1.5\n"},
		{"unknown record", "!TIMERHDR\tDATE\tJOB\tEMP\tDURATION\nBOGUS\t2026-09-28\tjob\temp\t01:00\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseQuickBooksIIF([]byte(tc.body), time.UTC)
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("want malformed, got %v", err)
			}
		})
	}
	if _, err := ParseQuickBooksIIF([]byte(timerIIF)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("want explicit timezone error, got %v", err)
	}
}

func TestParseQuickBooksIIF_QuotesAndDateForms(t *testing.T) {
	body := "!TIMEACT\tDATE\tEMP\tJOB\tDURATION\tNOTE\n" +
		"TIMEACT\t09/28/2026\t\"Ada, Lovelace\"\t\"Acme:Install\"\t00:05\t\"a tab-safe note\"\n"
	got, err := ParseQuickBooksIIF([]byte(body), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if got.Activities[0].Employee != "Ada, Lovelace" || got.Activities[0].Duration != 5*time.Minute {
		t.Fatalf("quoted activity: %+v", got.Activities[0])
	}
}

func TestParseQuickBooksIIF_DoesNotInventEndTime(t *testing.T) {
	got, err := ParseQuickBooksIIF([]byte(timerIIF), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Activities[0].Date.Add(got.Activities[0].Duration).Equal(time.Date(2026, 9, 28, 2, 30, 0, 0, time.UTC)) {
		t.Fatal("duration arithmetic should remain caller-owned")
	}
}
