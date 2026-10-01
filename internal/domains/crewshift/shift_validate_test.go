package crewshift

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestBreakKindValid(t *testing.T) {
	if !BreakMeal.Valid() || !BreakRest.Valid() {
		t.Fatalf("declared break kinds should be valid")
	}
	if BreakKind("BOGUS").Valid() {
		t.Fatalf("BreakKind BOGUS should not be valid")
	}
}

func TestBreakSegmentValidate(t *testing.T) {
	shift := fixtureDraft("break-seg").Work
	valid := BreakSegment{Kind: BreakMeal, Interval: Interval{Start: shift.Start.Add(time.Hour), End: shift.Start.Add(90 * time.Minute)}}
	if err := valid.Validate(shift); err != nil {
		t.Fatalf("valid break segment: %v", err)
	}

	badKind := valid
	badKind.Kind = "BOGUS"
	if err := badKind.Validate(shift); !errors.Is(err, ErrInvalidShift) {
		t.Fatalf("break segment with a bogus kind = %v, want ErrInvalidShift", err)
	}

	outside := BreakSegment{Kind: BreakMeal, Interval: Interval{Start: shift.End, End: shift.End.Add(time.Hour)}}
	if err := outside.Validate(shift); !errors.Is(err, ErrInvalidShift) {
		t.Fatalf("break segment outside the shift = %v, want ErrInvalidShift", err)
	}

	empty := BreakSegment{Kind: BreakMeal, Interval: Interval{Start: shift.Start, End: shift.Start}}
	if err := empty.Validate(shift); !errors.Is(err, ErrInvalidShift) {
		t.Fatalf("zero-length break segment = %v, want ErrInvalidShift", err)
	}
}

func TestBreakPlanValidateOverlap(t *testing.T) {
	shift := fixtureDraft("break-plan").Work
	plan := BreakPlan{Segments: []BreakSegment{
		{Kind: BreakMeal, Interval: Interval{Start: shift.Start.Add(time.Hour), End: shift.Start.Add(2 * time.Hour)}},
		{Kind: BreakRest, Interval: Interval{Start: shift.Start.Add(90 * time.Minute), End: shift.Start.Add(100 * time.Minute)}},
	}}
	if err := plan.Validate(shift); !errors.Is(err, ErrInvalidShift) {
		t.Fatalf("overlapping break plan = %v, want ErrInvalidShift", err)
	}
}

func TestShiftValidateBranches(t *testing.T) {
	base := fixtureDraft("validate-branches")

	t.Run("missing id", func(t *testing.T) {
		s := base
		s.ID = ""
		if err := s.Validate(); !errors.Is(err, ErrInvalidShift) {
			t.Fatalf("missing id = %v, want ErrInvalidShift", err)
		}
	})
	t.Run("invalid tenant", func(t *testing.T) {
		s := base
		s.Tenant = ""
		if err := s.Validate(); !errors.Is(err, ErrInvalidShift) {
			t.Fatalf("invalid tenant = %v, want ErrInvalidShift", err)
		}
	})
	t.Run("undeclared status", func(t *testing.T) {
		s := base
		s.Status = "BOGUS"
		if err := s.Validate(); !errors.Is(err, ErrInvalidShift) {
			t.Fatalf("undeclared status = %v, want ErrInvalidShift", err)
		}
	})
	t.Run("undeclared source", func(t *testing.T) {
		s := base
		s.Source = "BOGUS"
		if err := s.Validate(); !errors.Is(err, ErrInvalidShift) {
			t.Fatalf("undeclared source = %v, want ErrInvalidShift", err)
		}
	})
	t.Run("cross tenant worker", func(t *testing.T) {
		s := base
		s.WorkerRef = values.EntityRef{Tenant: "other-tenant", Kind: "worker", Id: fixtureWorker.Id}
		if err := s.Validate(); !errors.Is(err, ErrInvalidShift) {
			t.Fatalf("cross-tenant worker = %v, want ErrInvalidShift", err)
		}
	})
	t.Run("invalid work order when present", func(t *testing.T) {
		s := base
		s.HasWorkOrder = true
		s.WorkOrderRef = values.EntityRef{}
		if err := s.Validate(); !errors.Is(err, ErrInvalidShift) {
			t.Fatalf("invalid work order = %v, want ErrInvalidShift", err)
		}
	})
	t.Run("cross tenant work order", func(t *testing.T) {
		s := base
		s.HasWorkOrder = true
		s.WorkOrderRef = values.EntityRef{Tenant: "other-tenant", Kind: "work_order", Id: fixtureWorkOrder.Id}
		if err := s.Validate(); !errors.Is(err, ErrInvalidShift) {
			t.Fatalf("cross-tenant work order = %v, want ErrInvalidShift", err)
		}
	})
	t.Run("valid work order", func(t *testing.T) {
		s := base
		s.HasWorkOrder = true
		s.WorkOrderRef = fixtureWorkOrder
		if err := s.Validate(); err != nil {
			t.Fatalf("valid work order: %v", err)
		}
	})
	t.Run("missing timezone", func(t *testing.T) {
		s := base
		s.Timezone = ""
		if err := s.Validate(); !errors.Is(err, ErrInvalidShift) {
			t.Fatalf("missing timezone = %v, want ErrInvalidShift", err)
		}
	})
	t.Run("bogus timezone", func(t *testing.T) {
		s := base
		s.Timezone = "Nowhere/Fake"
		if err := s.Validate(); !errors.Is(err, ErrInvalidShift) {
			t.Fatalf("bogus timezone = %v, want ErrInvalidShift", err)
		}
	})
	t.Run("empty work interval", func(t *testing.T) {
		s := base
		s.Work = Interval{}
		if err := s.Validate(); !errors.Is(err, ErrInvalidShift) {
			t.Fatalf("empty work interval = %v, want ErrInvalidShift", err)
		}
	})
	t.Run("published without approval", func(t *testing.T) {
		s := base
		s.Status = StatusPublished
		if err := s.Validate(); !errors.Is(err, ErrInvalidShift) {
			t.Fatalf("published without approval = %v, want ErrInvalidShift", err)
		}
	})
}

func TestIntervalValidate(t *testing.T) {
	now := time.Now()
	if err := (Interval{}).Validate(); !errors.Is(err, ErrInvalidShift) {
		t.Fatalf("zero interval = %v, want ErrInvalidShift", err)
	}
	if err := (Interval{Start: now, End: now}).Validate(); !errors.Is(err, ErrInvalidShift) {
		t.Fatalf("zero-length interval = %v, want ErrInvalidShift", err)
	}
	if err := (Interval{Start: now, End: now.Add(time.Hour)}).Validate(); err != nil {
		t.Fatalf("valid interval: %v", err)
	}
}

func TestUnpaidMinutesIgnoresPaidBreaks(t *testing.T) {
	shift := fixtureDraft("unpaid-minutes")
	shift.Breaks = BreakPlan{Segments: []BreakSegment{
		{Kind: BreakMeal, Paid: false, Interval: Interval{Start: shift.Work.Start, End: shift.Work.Start.Add(30 * time.Minute)}},
		{Kind: BreakRest, Paid: true, Interval: Interval{Start: shift.Work.Start.Add(time.Hour), End: shift.Work.Start.Add(time.Hour + 15*time.Minute)}},
	}}
	if got, want := shift.Breaks.UnpaidMinutes(), int64(30); got != want {
		t.Fatalf("UnpaidMinutes() = %d, want %d", got, want)
	}
	if got, want := shift.WorkedMinutes(), shift.Work.Minutes()-30; got != want {
		t.Fatalf("WorkedMinutes() = %d, want %d", got, want)
	}
}
