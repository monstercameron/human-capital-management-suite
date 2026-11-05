package wait

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// weekendCalendar answers working-day questions with a bare weekend rule.
type weekendCalendar struct{ version string }

func (w weekendCalendar) CalendarVersion() string { return w.version }

func (w weekendCalendar) IsWorkingDay(d values.LocalDate) (bool, error) {
	wd := time.Date(int(d.Year()), d.Month(), int(d.Day()), 12, 0, 0, 0, time.UTC).Weekday()
	return wd != time.Saturday && wd != time.Sunday, nil
}

func timeExprFixtureNode() CompiledWaitNode {
	return CompiledWaitNode{
		WorkflowID:      "wf-ext012",
		WorkflowVersion: 1,
		NodeID:          "wait_deadline",
		WakeTimeExpr: values.TimeExpr{
			AnchorPath:     "run.start_date",
			OffsetKind:     values.TimeExprOffsetBusinessDays,
			OffsetDays:     3,
			Calendar:       values.CalendarRef{Ref: "US-FED", Version: "cal-2026a"},
			Zone:           values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"},
			Disambiguation: values.DisambiguationRejectGap,
			Statute:        &values.StatuteRef{RulePack: "flsa", Release: "2026-01", Statute: "203(o)"},
		},
		Zone:     values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"},
		Calendar: values.CalendarRef{Ref: "US-FED", Version: "cal-2026a"},
		Policy:   values.ReferenceUpdatePin,
	}
}

func TestTimeExprKindAndValidate(t *testing.T) {
	n := timeExprFixtureNode()
	if n.Kind() != WakeAtTimeExpr {
		t.Fatalf("Kind = %s, want AT_TIME_EXPR", n.Kind())
	}
	if err := n.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	conflicts := []func(*CompiledWaitNode){
		func(n *CompiledWaitNode) {
			n.WakeInstant = values.NewInstant(time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC))
		},
		func(n *CompiledWaitNode) {
			d, err := values.NewLocalDate(2026, time.October, 29)
			if err != nil {
				t.Fatal(err)
			}
			n.WakeLocalDate = d
		},
	}
	for i, mutate := range conflicts {
		dup := timeExprFixtureNode()
		mutate(&dup)
		if err := dup.Validate(); !errors.Is(err, ErrConflictingWakeCondition) {
			t.Errorf("conflict %d: err = %v, want ErrConflictingWakeCondition", i, err)
		}
	}
	bad := timeExprFixtureNode()
	bad.WakeTimeExpr.AnchorPath = ""
	if err := bad.Validate(); !errors.Is(err, ErrNoWakeCondition) {
		t.Errorf("empty expression: err = %v, want ErrNoWakeCondition", err)
	}
}

func TestFromCompiledTimeExpr(t *testing.T) {
	node := &workflow.CompiledNode{
		ID:   "wait_deadline",
		Type: workflow.StepWait,
		Wait: &workflow.CompiledWait{
			WakeKind:              workflow.WaitWakeAtTimeExpr,
			ZoneID:                "America/New_York",
			ZoneTzdbVersion:       "2026a",
			CalendarRef:           "US-FED",
			CalendarVersion:       "cal-2026a",
			ReferenceUpdatePolicy: "PIN",
			TimeExpr: &workflow.TimeExprSpec{
				AnchorPath:      "run.start_date",
				OffsetKind:      "BUSINESS_DAYS",
				OffsetDays:      3,
				Disambiguation:  "REJECT_GAP",
				StatuteRulePack: "flsa",
				StatuteRelease:  "2026-01",
				Statute:         "203(o)",
			},
		},
	}
	got, err := FromCompiled(node)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind() != WakeAtTimeExpr {
		t.Fatalf("Kind = %s, want AT_TIME_EXPR", got.Kind())
	}
	if got.WakeTimeExpr.Zone.ID != "America/New_York" || got.WakeTimeExpr.Calendar.Ref != "US-FED" {
		t.Fatalf("expression inherits no node-level zone/calendar: %+v", got.WakeTimeExpr)
	}
	if got.WakeTimeExpr.Statute == nil || got.WakeTimeExpr.Statute.Statute != "203(o)" {
		t.Fatalf("statute lost in translation: %+v", got.WakeTimeExpr.Statute)
	}
	bad := &workflow.CompiledNode{ID: "w", Type: workflow.StepWait, Wait: &workflow.CompiledWait{
		WakeKind: workflow.WaitWakeAtTimeExpr, ZoneID: "America/New_York", ZoneTzdbVersion: "2026a",
		CalendarRef: "US-FED", CalendarVersion: "cal-2026a", ReferenceUpdatePolicy: "PIN",
	}}
	if _, err := FromCompiled(bad); err == nil {
		t.Fatal("nil time expression translated without error")
	}
	bad.Wait.TimeExpr = &workflow.TimeExprSpec{AnchorPath: "run.x", OffsetKind: "FORTNIGHTS"}
	if _, err := FromCompiled(bad); err == nil {
		t.Fatal("bogus offset kind translated without error")
	}
}

func TestComputeTimerRequirementAtAnchor(t *testing.T) {
	n := timeExprFixtureNode()
	ds := values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "cal-2026a"}
	if _, err := ComputeTimerRequirement(n, ds); !errors.Is(err, ErrTimeExprAnchorRequired) {
		t.Fatalf("anchor-free compute err = %v, want ErrTimeExprAnchorRequired", err)
	}
	// Friday 2026-10-23 15:00 UTC; +3 business days = Wed 2026-10-28.
	anchor := values.NewInstant(time.Date(2026, time.October, 23, 15, 0, 0, 0, time.UTC))
	cals := weekendCalendar{version: "cal-2026a"}
	req, err := ComputeTimerRequirementAtAnchor(n, anchor, cals, ds)
	if err != nil {
		t.Fatal(err)
	}
	want := values.NewInstant(time.Date(2026, time.October, 28, 4, 0, 0, 0, time.UTC))
	if req.FireAt != want {
		t.Fatalf("FireAt = %s, want %s", req.FireAt, want)
	}
	if req.Evidence.Inputs["anchor_path"] != "run.start_date" || req.Evidence.Inputs["anchor"] == "" {
		t.Fatalf("evidence omits the anchor: %+v", req.Evidence.Inputs)
	}
	again, err := ComputeTimerRequirementAtAnchor(n, anchor, cals, ds)
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest != req.Digest || again.FireAt != req.FireAt {
		t.Fatal("anchored requirement is not deterministic")
	}
}
