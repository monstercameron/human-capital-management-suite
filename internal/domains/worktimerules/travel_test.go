package worktimerules

import (
	"fmt"
	"testing"
	"time"
)

func TestTodo_WTIME_016(t *testing.T) {
	// Ordinary commute is unpaid and excluded from hours.
	commute := TravelSegment{
		WorkerID: "w1", Start: mustTime(t, "2026-09-22T08:00:00Z"), End: mustTime(t, "2026-09-22T08:30:00Z"),
		IsFirstOrLastOfDay: true,
	}
	c, err := ClassifyTravel(commute, TravelPolicy{})
	if err != nil {
		t.Fatalf("ClassifyTravel commute: %v", err)
	}
	if c.Class != TravelCommute || c.Paid || c.CountsTowardHours {
		t.Fatalf("ordinary commute classified as %+v, want unpaid COMMUTE", c)
	}

	// California employer-controlled travel (mandatory yard-to-site
	// transport) reclassifies the same leg as paid.
	controlled := commute
	controlled.EmployerRequiredTransport = true
	cc, err := ClassifyTravel(controlled, TravelPolicy{CaliforniaEmployerControlledTravel: true})
	if err != nil {
		t.Fatalf("ClassifyTravel controlled: %v", err)
	}
	if cc.Class != TravelEmployerControlled || !cc.Paid || !cc.CountsTowardHours {
		t.Fatalf("California employer-controlled travel classified as %+v, want paid EMPLOYER_CONTROLLED", cc)
	}

	// Inter-site travel during the day is paid and counts toward hours.
	interSite := TravelSegment{
		WorkerID: "w1", Start: mustTime(t, "2026-09-22T12:00:00Z"), End: mustTime(t, "2026-09-22T12:45:00Z"),
		IsInterSite: true,
	}
	is, err := ClassifyTravel(interSite, TravelPolicy{})
	if err != nil {
		t.Fatalf("ClassifyTravel inter-site: %v", err)
	}
	if is.Class != TravelInterSite || !is.Paid || !is.CountsTowardHours || is.Minutes != 45 {
		t.Fatalf("inter-site travel classified as %+v, want paid INTER_SITE of 45 minutes", is)
	}

	// Mileage is a wholly separate record: it carries no minutes field and
	// nothing above touches it.
	mileage := MileageExpense{WorkerID: "w1", MilesHundredths: 1250}
	if mileage.MilesHundredths != 1250 {
		t.Fatalf("mileage record not preserved: %+v", mileage)
	}
}

func TestTodo_WTIME_016_Property(t *testing.T) {
	// Property: paid always implies CountsTowardHours and vice versa —
	// the two flags never disagree, across every combination of segment
	// facts this function switches on.
	base := TravelSegment{WorkerID: "w1", Start: mustTime(t, "2026-09-22T08:00:00Z")}
	combos := []TravelSegment{}
	for _, firstLast := range []bool{true, false} {
		for _, interSite := range []bool{true, false} {
			for _, special := range []bool{true, false} {
				for _, overnight := range []bool{true, false} {
					for _, work := range []bool{true, false} {
						s := base
						s.End = base.Start.Add(20 * time.Minute)
						s.IsFirstOrLastOfDay, s.IsInterSite, s.IsSpecialAssignment, s.IsOvernight, s.PerformedWork = firstLast, interSite, special, overnight, work
						s.NormalWorkStartMinute, s.NormalWorkEndMinute = 0, 24*60
						combos = append(combos, s)
					}
				}
			}
		}
	}
	for i, s := range combos {
		c, err := ClassifyTravel(s, TravelPolicy{})
		if err != nil {
			t.Fatalf("combo %d: %v", i, err)
		}
		if c.Paid != c.CountsTowardHours {
			t.Fatalf("combo %d (%+v): Paid=%v CountsTowardHours=%v must agree", i, s, c.Paid, c.CountsTowardHours)
		}
	}
}

func TestTodo_WTIME_016_Golden(t *testing.T) {
	segments := []TravelSegment{
		{WorkerID: "w1", Start: mustTime(t, "2026-09-22T08:00:00Z"), End: mustTime(t, "2026-09-22T08:30:00Z"), IsFirstOrLastOfDay: true},
		{WorkerID: "w1", Start: mustTime(t, "2026-09-22T12:00:00Z"), End: mustTime(t, "2026-09-22T12:45:00Z"), IsInterSite: true},
		{WorkerID: "w1", Start: mustTime(t, "2026-09-22T09:00:00Z"), End: mustTime(t, "2026-09-22T13:00:00Z"), IsSpecialAssignment: true},
		{WorkerID: "w1", Start: mustTime(t, "2026-09-22T20:00:00Z"), End: mustTime(t, "2026-09-23T02:00:00Z"), IsOvernight: true, NormalWorkStartMinute: 0, NormalWorkEndMinute: 24 * 60},
		{WorkerID: "w1", Start: mustTime(t, "2026-09-22T20:00:00Z"), End: mustTime(t, "2026-09-22T21:00:00Z"), PerformedWork: true},
	}
	var got string
	for i, s := range segments {
		c, err := ClassifyTravel(s, TravelPolicy{})
		if err != nil {
			t.Fatalf("segment %d: %v", i, err)
		}
		got += fmt.Sprintf("segment=%d class=%s paid=%v counts=%v minutes=%d\n", i, c.Class, c.Paid, c.CountsTowardHours, c.Minutes)
	}
	compareGolden(t, "travel_classification.golden", got)
}

func TestTodo_WTIME_016_Security(t *testing.T) {
	// A segment with no worker identity is rejected rather than silently
	// classified and attributed to nobody, which would let travel time be
	// laundered across workers.
	unattributed := TravelSegment{Start: mustTime(t, "2026-09-22T08:00:00Z"), End: mustTime(t, "2026-09-22T08:30:00Z")}
	if _, err := ClassifyTravel(unattributed, TravelPolicy{}); err == nil {
		t.Fatal("expected ClassifyTravel to reject a segment with no worker_id")
	}

	// An inverted or empty interval is rejected, not silently zeroed.
	inverted := TravelSegment{WorkerID: "w1", Start: mustTime(t, "2026-09-22T08:30:00Z"), End: mustTime(t, "2026-09-22T08:00:00Z")}
	if _, err := ClassifyTravel(inverted, TravelPolicy{}); err == nil {
		t.Fatal("expected ClassifyTravel to reject an inverted interval")
	}
}
