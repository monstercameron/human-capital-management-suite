package humanwork_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type hwWeekendCalendar struct{ version string }

func (w hwWeekendCalendar) CalendarVersion() string { return w.version }

func (w hwWeekendCalendar) IsWorkingDay(d values.LocalDate) (bool, error) {
	wd := time.Date(int(d.Year()), d.Month(), int(d.Day()), 12, 0, 0, 0, time.UTC).Weekday()
	return wd != time.Saturday && wd != time.Sunday, nil
}

func hwDataset() values.DatasetVersions {
	return values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "cal-2026a"}
}

func hwDeadlineExpr() *values.TimeExpr {
	return &values.TimeExpr{
		AnchorPath:     "run.start_date",
		OffsetKind:     values.TimeExprOffsetBusinessDays,
		OffsetDays:     3,
		Calendar:       values.CalendarRef{Ref: "US-FED", Version: "cal-2026a"},
		Zone:           values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"},
		Disambiguation: values.DisambiguationRejectGap,
	}
}

func TestCompileDeadlineExpr(t *testing.T) {
	spec := validSpec()
	spec.Deadline.DecideBy = values.Instant{}
	spec.Deadline.Expiry = values.NewInstant(time.Date(2027, time.January, 15, 12, 0, 0, 0, time.UTC))
	spec.DeadlineExpr = hwDeadlineExpr()
	req, err := humanwork.Compile(spec)
	if err != nil {
		t.Fatal(err)
	}
	if req.Digest() == "" {
		t.Fatal("expression requirement has no digest")
	}
	if req.DeadlineExpr == nil || req.DeadlineExpr.OffsetDays != 3 {
		t.Fatalf("expression lost in compile: %+v", req.DeadlineExpr)
	}
	// The compiled requirement never aliases its spec.
	spec.DeadlineExpr.OffsetDays = 99
	if req.DeadlineExpr.OffsetDays != 3 {
		t.Fatal("compiled expression aliases the spec")
	}
	plain, err := humanwork.Compile(validSpec())
	if err != nil {
		t.Fatal(err)
	}
	if plain.Digest() == req.Digest() {
		t.Fatal("expression and absolute requirements share a digest")
	}

	red := validSpec()
	red.DeadlineExpr = hwDeadlineExpr()
	if _, err := humanwork.Compile(red); !errors.Is(err, humanwork.ErrInvalidRequirement) {
		t.Errorf("decide-by plus expression err = %v, want ErrInvalidRequirement", err)
	}
	noExpiry := validSpec()
	noExpiry.Deadline = humanwork.Deadline{}
	noExpiry.DeadlineExpr = hwDeadlineExpr()
	if _, err := humanwork.Compile(noExpiry); !errors.Is(err, humanwork.ErrInvalidRequirement) {
		t.Errorf("expression without expiry err = %v, want ErrInvalidRequirement", err)
	}
	badExpr := validSpec()
	badExpr.Deadline.DecideBy = values.Instant{}
	badExpr.DeadlineExpr = &values.TimeExpr{AnchorPath: "run.x", OffsetKind: "FORTNIGHTS"}
	if _, err := humanwork.Compile(badExpr); !errors.Is(err, humanwork.ErrInvalidRequirement) {
		t.Errorf("invalid expression err = %v, want ErrInvalidRequirement", err)
	}
}

func TestResolveDeadline(t *testing.T) {
	spec := validSpec()
	spec.Deadline.DecideBy = values.Instant{}
	spec.Deadline.Expiry = values.NewInstant(time.Date(2027, time.January, 15, 12, 0, 0, 0, time.UTC))
	spec.DeadlineExpr = hwDeadlineExpr()
	req, err := humanwork.Compile(spec)
	if err != nil {
		t.Fatal(err)
	}
	anchor := values.NewInstant(time.Date(2026, time.October, 23, 15, 0, 0, 0, time.UTC))
	dl, err := req.ResolveDeadline(anchor, hwWeekendCalendar{version: "cal-2026a"}, hwDataset())
	if err != nil {
		t.Fatal(err)
	}
	// Friday +3 business days = Wednesday start of day EDT.
	want := values.NewInstant(time.Date(2026, time.October, 28, 4, 0, 0, 0, time.UTC))
	if dl.DecideBy != want {
		t.Fatalf("DecideBy = %s, want %s", dl.DecideBy, want)
	}
	if dl.Expiry != spec.Deadline.Expiry {
		t.Fatal("expiry is not the declared absolute expiry")
	}
	// A requirement without an expression resolves to its declared deadline.
	plain, err := humanwork.Compile(validSpec())
	if err != nil {
		t.Fatal(err)
	}
	pdl, err := plain.ResolveDeadline(anchor, nil, hwDataset())
	if err != nil {
		t.Fatal(err)
	}
	if pdl != plain.Deadline {
		t.Fatal("absolute deadline did not pass through")
	}
	// An expression resolving past the absolute expiry is refused.
	past := validSpec()
	past.Deadline.DecideBy = values.Instant{}
	past.Deadline.Expiry = values.NewInstant(time.Date(2026, time.October, 24, 15, 0, 0, 0, time.UTC))
	past.DeadlineExpr = &values.TimeExpr{AnchorPath: "run.x", OffsetKind: values.TimeExprOffsetDuration, OffsetSeconds: 30 * 86400}
	pastReq, err := humanwork.Compile(past)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pastReq.ResolveDeadline(anchor, hwWeekendCalendar{version: "cal-2026a"}, hwDataset()); err == nil {
		t.Fatal("deadline past expiry resolved without error")
	}
}
