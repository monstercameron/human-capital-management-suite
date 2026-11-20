package chatfilter

import (
	"strings"
	"testing"
)

func dlpEval(t *testing.T, name, action, body string) Result {
	t.Helper()
	e, err := NewRegistry().Compile([]Definition{{ID: "dlp", Name: "Sensitive data", Version: "1.0.0", Kind: "detector", Match: []string{name}, Action: action}})
	if err != nil {
		t.Fatalf("compile %s: %v", name, err)
	}
	r, err := e.Evaluate(Input{Tenant: "t", Channel: "c", Subject: "s", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestIssue3781_DLPDetectorsCatchEachClass(t *testing.T) {
	cases := []struct{ name, body, secret string }{
		{"dlp-compensation", "Her salary is $85,000 a year, keep it quiet", "$85,000"},
		{"dlp-compensation", "we pay €4,500 per month", "€4,500"},
		{"dlp-medical", "He was diagnosed with something last week", "diagnosed"},
		{"dlp-bank", "pay to DE89 3704 0044 0532 0130 00 today", "DE89 3704 0044 0532 0130 00"},
		{"dlp-immigration", "passport number: X1234567 attached", "X1234567"},
		{"dlp-pii", "ssn 123-45-6789 ok", "123-45-6789"},
		{"dlp-pii", "date of birth: 1984-03-02", "1984-03-02"},
	}
	for _, c := range cases {
		r := dlpEval(t, c.name, "block", c.body)
		if len(r.Hits) == 0 {
			t.Fatalf("%s did not catch %q", c.name, c.body)
		}
		got := c.body[r.Hits[0].Span.Start:r.Hits[0].Span.End]
		if !strings.Contains(got, c.secret) {
			t.Fatalf("%s span %q does not cover %q", c.name, got, c.secret)
		}
		err := r.Refusal()
		if err == nil {
			t.Fatalf("%s: block did not refuse", c.name)
		}
		if strings.Contains(err.Error(), c.secret) {
			t.Fatalf("refusal leaks matched text: %v", err)
		}
	}
}

func TestIssue3781_DLPDetectorsLeaveOrdinaryTextAlone(t *testing.T) {
	for _, body := range []string{
		"Lunch at 12:30, the team meeting moved to room 4",
		"Our raise in awareness went well",  // no amount
		"DE00 1234 5678 9012 3456 78",       // fails the IBAN check
		"visa for the conference is sorted", // no number
		"order 123456789 shipped",
	} {
		for _, n := range DLPDetectorNames {
			if r := dlpEval(t, n, "block", body); len(r.Hits) != 0 {
				t.Fatalf("%s flagged %q", n, body)
			}
		}
	}
}

func TestIssue3781_DLPMaskHidesOnlyTheSensitivePart(t *testing.T) {
	r := dlpEval(t, "dlp-compensation", "mask", "Her salary is $85,000 a year")
	if strings.Contains(r.Masked, "85,000") || !strings.Contains(r.Masked, "year") {
		t.Fatalf("masked = %q", r.Masked)
	}
}

func TestIssue3781_DLPClassOfEveryDetectorIsKnown(t *testing.T) {
	for _, n := range DLPDetectorNames {
		if c, ok := DLPDetectorClass(n); !ok || c == "" {
			t.Fatalf("%s has no class", n)
		}
	}
}

// An IBAN followed by an upper-case word of four letters must still be caught:
// the check must not fail because the pattern swallowed the next word.
func TestIssue3781_DLPBankCatchesIBANBeforeUppercaseWord(t *testing.T) {
	body := "ES91 2100 0418 4502 0005 1332 PAID by transfer"
	r := dlpEval(t, "dlp-bank", "block", body)
	if len(r.Hits) != 1 {
		t.Fatalf("hits = %d", len(r.Hits))
	}
	if got := body[r.Hits[0].Span.Start:r.Hits[0].Span.End]; got != "ES91 2100 0418 4502 0005 1332" {
		t.Fatalf("span = %q", got)
	}
}
