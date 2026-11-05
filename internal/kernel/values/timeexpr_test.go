package values

import (
	"errors"
	"testing"
	"time"
)

// stubCalendar is a CalendarDataset over a weekend rule plus fixed holidays.
// Saturdays and Sundays never work; dates in holidays never work.
type stubCalendar struct {
	version  string
	holidays map[LocalDate]bool
}

func (s stubCalendar) CalendarVersion() string { return s.version }

func (s stubCalendar) IsWorkingDay(d LocalDate) (bool, error) {
	y, m, day := int(d.Year()), d.Month(), int(d.Day())
	wd := time.Date(y, m, day, 12, 0, 0, 0, time.UTC).Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return false, nil
	}
	if s.holidays[d] {
		return false, nil
	}
	return true, nil
}

func mustDate(t *testing.T, year int, month time.Month, day int) LocalDate {
	t.Helper()
	d, err := NewLocalDate(year, month, day)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func timeExprDataset() DatasetVersions {
	return DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "cal-2026a"}
}

func TestTimeExprValidate(t *testing.T) {
	zone := ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"}
	cal := CalendarRef{Ref: "US-FED", Version: "cal-2026a"}
	good := TimeExpr{
		AnchorPath:     "run.start_date",
		OffsetKind:     TimeExprOffsetBusinessDays,
		OffsetDays:     3,
		Calendar:       cal,
		Zone:           zone,
		Disambiguation: DisambiguationRejectGap,
		Statute:        &StatuteRef{RulePack: "flsa", Release: "2026-01", Statute: "203(o)"},
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("good TimeExpr rejected: %v", err)
	}
	cases := map[string]func(*TimeExpr){
		"empty anchor":           func(e *TimeExpr) { e.AnchorPath = "" },
		"unspecified offset":     func(e *TimeExpr) { e.OffsetKind = TimeExprOffsetUnspecified },
		"bogus offset":           func(e *TimeExpr) { e.OffsetKind = "FORTNIGHTS" },
		"missing calendar":       func(e *TimeExpr) { e.Calendar = CalendarRef{} },
		"missing zone":           func(e *TimeExpr) { e.Zone = ZoneRef{} },
		"missing disambiguation": func(e *TimeExpr) { e.Disambiguation = DisambiguationUnspecified },
		"bad statute":            func(e *TimeExpr) { e.Statute = &StatuteRef{} },
	}
	for name, mutate := range cases {
		e := good
		mutate(&e)
		if err := e.Validate(); !errors.Is(err, ErrTimeExprInvalid) {
			t.Errorf("%s: err = %v, want ErrTimeExprInvalid", name, err)
		}
	}
	dur := TimeExpr{AnchorPath: "run.requested_at", OffsetKind: TimeExprOffsetDuration, OffsetSeconds: 15 * 86400}
	if err := dur.Validate(); err != nil {
		t.Fatalf("duration TimeExpr rejected: %v", err)
	}
	badDur := dur
	badDur.OffsetSeconds = 0
	if err := badDur.Validate(); !errors.Is(err, ErrTimeExprInvalid) {
		t.Errorf("zero duration: err = %v, want ErrTimeExprInvalid", err)
	}
}

func TestAddBusinessDays(t *testing.T) {
	// Friday 2026-10-23; Monday 2026-10-26 is a holiday in the stub.
	cal := stubCalendar{version: "cal-2026a", holidays: map[LocalDate]bool{
		mustDate(t, 2026, time.October, 26): true,
	}}
	fri := mustDate(t, 2026, time.October, 23)
	got, err := AddBusinessDays(fri, 3, cal)
	if err != nil {
		t.Fatal(err)
	}
	// Fri -> Tue (Mon holiday, Sat/Sun weekend) -> Wed -> Thu.
	want := mustDate(t, 2026, time.October, 29)
	if got != want {
		t.Fatalf("AddBusinessDays(Fri,+3) = %s, want %s", got, want)
	}
	back, err := AddBusinessDays(got, -3, cal)
	if err != nil {
		t.Fatal(err)
	}
	if back != fri {
		t.Fatalf("AddBusinessDays round trip = %s, want %s", back, fri)
	}
	same, err := AddBusinessDays(fri, 0, cal)
	if err != nil {
		t.Fatal(err)
	}
	if same != fri {
		t.Fatalf("AddBusinessDays(Fri,0) = %s, want anchor unchanged", same)
	}
}

func TestResolveTimeExprBusinessDays(t *testing.T) {
	cal := stubCalendar{version: "cal-2026a", holidays: map[LocalDate]bool{
		mustDate(t, 2026, time.October, 26): true,
	}}
	expr := TimeExpr{
		AnchorPath:     "run.start_date",
		OffsetKind:     TimeExprOffsetBusinessDays,
		OffsetDays:     3,
		Calendar:       CalendarRef{Ref: "US-FED", Version: "cal-2026a"},
		Zone:           ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"},
		Disambiguation: DisambiguationRejectGap,
	}
	// Friday 2026-10-23 15:00 UTC = 11:00 EDT.
	anchor := NewInstant(time.Date(2026, time.October, 23, 15, 0, 0, 0, time.UTC))
	res, err := ResolveTimeExpr(expr, anchor, cal, timeExprDataset())
	if err != nil {
		t.Fatal(err)
	}
	// +3 business days from Fri = Thu 2026-10-29; start of day EDT = 04:00 UTC.
	want := NewInstant(time.Date(2026, time.October, 29, 4, 0, 0, 0, time.UTC))
	if res.FireAt != want {
		t.Fatalf("FireAt = %s, want %s", res.FireAt, want)
	}
	if len(res.Evidence.Steps) == 0 || len(res.Evidence.Inputs) == 0 {
		t.Fatal("resolution carries no evidence")
	}
	// A calendar dataset pinned to another version is refused, not guessed.
	wrong := stubCalendar{version: "cal-2025z"}
	if _, err := ResolveTimeExpr(expr, anchor, wrong, timeExprDataset()); !errors.Is(err, ErrTimeExprCalendar) {
		t.Fatalf("version mismatch err = %v, want ErrTimeExprCalendar", err)
	}
	// An unset anchor is refused.
	if _, err := ResolveTimeExpr(expr, Instant{}, cal, timeExprDataset()); !errors.Is(err, ErrTimeExprAnchor) {
		t.Fatalf("unset anchor err = %v, want ErrTimeExprAnchor", err)
	}
}

func TestResolveTimeExprDuration(t *testing.T) {
	expr := TimeExpr{AnchorPath: "run.requested_at", OffsetKind: TimeExprOffsetDuration, OffsetSeconds: 15 * 86400}
	anchor := NewInstant(time.Date(2026, time.October, 23, 15, 0, 0, 0, time.UTC))
	res, err := ResolveTimeExpr(expr, anchor, nil, timeExprDataset())
	if err != nil {
		t.Fatal(err)
	}
	want := NewInstant(time.Date(2026, time.November, 7, 15, 0, 0, 0, time.UTC))
	if res.FireAt != want {
		t.Fatalf("FireAt = %s, want %s", res.FireAt, want)
	}
}

func TestTimeExprResolver(t *testing.T) {
	cal := stubCalendar{version: "cal-2026a"}
	expr := TimeExpr{
		AnchorPath:     "run.start_date",
		OffsetKind:     TimeExprOffsetCalendarDays,
		OffsetDays:     1,
		Zone:           ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"},
		Calendar:       CalendarRef{Ref: "US-FED", Version: "cal-2026a"},
		Disambiguation: DisambiguationRejectGap,
	}
	anchor := NewInstant(time.Date(2026, time.October, 23, 15, 0, 0, 0, time.UTC))
	var resolve TimerResolver = expr.Resolver(anchor, cal)
	fireAt, err := resolve(timeExprDataset())
	if err != nil {
		t.Fatal(err)
	}
	// Sat 2026-10-24 start of day EDT = 04:00 UTC (calendar days ignore weekends).
	want := NewInstant(time.Date(2026, time.October, 24, 4, 0, 0, 0, time.UTC))
	if fireAt != want {
		t.Fatalf("resolver FireAt = %s, want %s", fireAt, want)
	}
}

func TestDeadlineDefaultDigest(t *testing.T) {
	d := DeadlineDefault{Schema: "hcmnext.ext012.TimeoutDefault/v1", CanonicalJSON: `{"verdict":"NO_RESPONSE"}`}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if d.Digest() == "" || d.Digest() != (DeadlineDefault{Schema: d.Schema, CanonicalJSON: d.CanonicalJSON}).Digest() {
		t.Fatal("default digest is empty or unstable")
	}
	other := DeadlineDefault{Schema: d.Schema, CanonicalJSON: `{"verdict":"OTHER"}`}
	if other.Digest() == d.Digest() {
		t.Fatal("distinct payloads share a digest")
	}
	if err := (DeadlineDefault{}).Validate(); !errors.Is(err, ErrTimeExprInvalid) {
		t.Fatalf("empty default err = %v, want ErrTimeExprInvalid", err)
	}
}
