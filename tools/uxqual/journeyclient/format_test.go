package journeyclient

import (
	"testing"
	"time"
)

// fakeTimestamp is the shape of a generated Protobuf timestamp, so this file
// can exercise the formatting seam without naming the wire type.
type fakeTimestamp struct {
	seconds int64
	nanos   int32
}

func (f fakeTimestamp) GetSeconds() int64 { return f.seconds }
func (f fakeTimestamp) GetNanos() int32   { return f.nanos }

func atUTC(t *testing.T, layout, value string) fakeTimestamp {
	t.Helper()
	parsed, err := time.Parse(layout, value)
	if err != nil {
		t.Fatalf("parsing %q: %v", value, err)
	}
	return fakeTimestamp{seconds: parsed.UTC().Unix()}
}

func TestTimeOfTreatsTheProto3ZeroAsUnset(t *testing.T) {
	if _, ok := timeOf(nil); ok {
		t.Error("a nil timestamp reported as set")
	}
	if _, ok := timeOf(fakeTimestamp{}); ok {
		t.Error("the proto3 zero value reported as set; the page would print 1 Jan 1970")
	}
	got, ok := timeOf(fakeTimestamp{seconds: 1757000000})
	if !ok {
		t.Fatal("a set timestamp reported as unset")
	}
	if got.Location() != time.UTC {
		t.Errorf("timeOf returned %v, want a UTC time", got.Location())
	}
}

func TestFormatTimeIsAbsoluteAndUTC(t *testing.T) {
	ts := atUTC(t, time.RFC3339, "2026-09-03T14:05:09Z")
	if got, want := formatTime(ts), "3 Sep 2026, 14:05 UTC"; got != want {
		t.Errorf("formatTime = %q, want %q", got, want)
	}
	if got := formatTime(fakeTimestamp{}); got != "" {
		t.Errorf("formatTime of an unset stamp = %q, want empty", got)
	}
	if got, want := formatTimeOr(fakeTimestamp{}, emDash), emDash; got != want {
		t.Errorf("formatTimeOr = %q, want %q", got, want)
	}
	if got, want := formatDateOf(ts), "3 Sep 2026"; got != want {
		t.Errorf("formatDateOf = %q, want %q", got, want)
	}
}

func TestFormatDate(t *testing.T) {
	cases := map[string]string{
		"2026-06-01": "1 Jun 2026",
		"2026-12-31": "31 Dec 2026",
		"":           "",
		// Anything the engine sends that is not an ISO date is shown as it
		// arrived rather than blanked: it is still a governed fact.
		"next quarter": "next quarter",
		"2026-6-1":     "2026-6-1",
	}
	for in, want := range cases {
		if got := formatDate(in); got != want {
			t.Errorf("formatDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatMoneyGroupsAndRounds(t *testing.T) {
	cases := map[string]string{
		"93000":         "93,000.00",
		"93000.00":      "93,000.00",
		"98000.005":     "98,000.01",
		"1234567.891":   "1,234,567.89",
		"999":           "999.00",
		"1000":          "1,000.00",
		"-2500.5":       "-2,500.50",
		"0":             "0.00",
		"":              "",
		"not-a-decimal": "not-a-decimal",
	}
	for in, want := range cases {
		if got := formatMoney(in); got != want {
			t.Errorf("formatMoney(%q) = %q, want %q", in, got, want)
		}
	}
	if got, want := formatAmount("USD", "93000"), "USD 93,000.00"; got != want {
		t.Errorf("formatAmount = %q, want %q", got, want)
	}
	if got, want := formatAmount("", "93000"), "93,000.00"; got != want {
		t.Errorf("formatAmount with no currency = %q, want %q", got, want)
	}
	if got := formatAmount("USD", ""); got != "" {
		t.Errorf("formatAmount of nothing = %q, want empty", got)
	}
}

func TestPercentDelta(t *testing.T) {
	cases := []struct {
		from, to string
		want     string
		ok       bool
	}{
		{"93000.00", "98000.00", "+5.4%", true},
		{"121000.00", "138000.00", "+14.0%", true},
		{"100000.00", "90000.00", "-10.0%", true},
		{"93000.00", "93000.00", "", false},
		// A one-cent change on a six-figure salary rounds to nothing, and
		// "+0.0%" would be a lie dressed as a number.
		{"100000.00", "100000.01", "", false},
		{"0", "98000.00", "", false},
		{"", "98000.00", "", false},
		{"93000.00", "", "", false},
	}
	for _, c := range cases {
		got, ok := percentDelta(c.from, c.to)
		if got != c.want || ok != c.ok {
			t.Errorf("percentDelta(%q, %q) = (%q, %v), want (%q, %v)", c.from, c.to, got, ok, c.want, c.ok)
		}
	}
}

// TestPercentDeltaIsExactNotFloating pins the reason big.Rat is used: the
// same computation in float64 does not round to the same tenth.
func TestPercentDeltaIsExactNotFloating(t *testing.T) {
	got, ok := percentDelta("0.07", "0.21")
	if !ok || got != "+200.0%" {
		t.Errorf("percentDelta(0.07, 0.21) = (%q, %v), want (+200.0%%, true)", got, ok)
	}
}

func TestAmountDelta(t *testing.T) {
	cases := []struct {
		currency, from, to string
		want               string
		ok                 bool
	}{
		{"USD", "93000.00", "98000.00", "+USD 5,000.00", true},
		{"USD", "98000.00", "93000.00", "-USD 5,000.00", true},
		{"", "93000.00", "98000.00", "+5,000.00", true},
		{"USD", "93000.00", "93000.00", "", false},
		{"USD", "", "98000.00", "", false},
	}
	for _, c := range cases {
		got, ok := amountDelta(c.currency, c.from, c.to)
		if got != c.want || ok != c.ok {
			t.Errorf("amountDelta(%q, %q, %q) = (%q, %v), want (%q, %v)",
				c.currency, c.from, c.to, got, ok, c.want, c.ok)
		}
	}
}

func TestPayLine(t *testing.T) {
	cases := []struct {
		name                        string
		currency, current, proposed string
		want                        string
	}{
		{"the whole line", "USD", "93000.00", "98000.00", "USD 93,000.00 → 98,000.00 (+5.4%)"},
		{"no change drops the percentage", "USD", "93000.00", "93000.00", "USD 93,000.00 → 93,000.00"},
		{"no current base", "USD", "", "98000.00", "USD 98,000.00"},
		{"no proposal", "USD", "93000.00", "", "USD 93,000.00"},
		{"nothing at all", "USD", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := payLine(c.currency, c.current, c.proposed); got != c.want {
				t.Errorf("payLine = %q, want %q", got, c.want)
			}
		})
	}
}

func TestHeadline(t *testing.T) {
	cases := []struct {
		name                                           string
		curJob, curGrade, targetJob, targetGrade, want string
	}{
		{"both sides", "OPS-HRBP2", "P2", "OPS-HRBP3", "P3", "OPS-HRBP2 · P2 → OPS-HRBP3 · P3"},
		{"no current placement", "", "", "OPS-HRBP3", "P3", "OPS-HRBP3 · P3"},
		{"no target", "OPS-HRBP2", "P2", "", "", "OPS-HRBP2 · P2"},
		{"grade only", "", "P2", "", "P3", "P2 → P3"},
		{"nothing", "", "", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := headline(c.curJob, c.curGrade, c.targetJob, c.targetGrade)
			if got != c.want {
				t.Errorf("headline = %q, want %q", got, c.want)
			}
		})
	}
}

func TestGroupThousands(t *testing.T) {
	cases := map[string]string{
		"1.00":          "1.00",
		"12.00":         "12.00",
		"123.00":        "123.00",
		"1234.00":       "1,234.00",
		"1234567890.00": "1,234,567,890.00",
		"-1234.00":      "-1,234.00",
		"1234":          "1,234",
	}
	for in, want := range cases {
		if got := groupThousands(in); got != want {
			t.Errorf("groupThousands(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTodo_UXAUDIT_006_I18N_DetailMoneyAndDates(t *testing.T) {
	ts := atUTC(t, time.RFC3339, "2026-09-03T14:05:09Z")
	if got, want := formatAmountLocale("de-DE", "USD", "1234.50"), "1.234,50\u00a0USD"; got != want {
		t.Errorf("German exact money = %q, want %q", got, want)
	}
	if got, want := formatDateLocale("de-DE", "2026-09-03"), "03.09.2026"; got != want {
		t.Errorf("German effective date = %q, want %q", got, want)
	}
	if got := formatTimeLocale("de-DE", ts); got != "03.09.2026, 14:05 UTC" {
		t.Errorf("German audit time = %q", got)
	}
	if got, ok := amountDeltaLocale("de-DE", "USD", "93000.00", "98000.00"); !ok || got != "+5.000,00\u00a0USD" {
		t.Errorf("German exact delta = (%q, %v)", got, ok)
	}
	if got, ok := percentDeltaLocale("de-DE", "93000.00", "98000.00"); !ok || got != "+5,4\u00a0%" {
		t.Errorf("German exact percent = (%q, %v)", got, ok)
	}
	if got := formatAmountLocale("ar", "USD", "1234.50"); got == "" || got == "USD 1,234.50" {
		t.Errorf("Arabic money was not localized: %q", got)
	}
	if got, want := formatDateLocale("ar", "2026-09-03"), "٣ سبتمبر ٢٠٢٦"; got != want {
		t.Errorf("Arabic effective date = %q, want %q", got, want)
	}
	if got, want := formatAmountLocale("en-US", "USD", "1234.50"), "USD 1,234.50"; got != want {
		t.Errorf("English compatibility = %q, want %q", got, want)
	}
}
