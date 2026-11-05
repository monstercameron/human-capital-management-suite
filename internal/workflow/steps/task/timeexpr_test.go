package task_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	steptask "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/task"
)

type taskWeekendCalendar struct{ version string }

func (w taskWeekendCalendar) CalendarVersion() string { return w.version }

func (w taskWeekendCalendar) IsWorkingDay(d values.LocalDate) (bool, error) {
	wd := time.Date(int(d.Year()), d.Month(), int(d.Day()), 12, 0, 0, 0, time.UTC).Weekday()
	return wd != time.Saturday && wd != time.Sunday, nil
}

func deadlineTaskNode() steptask.CompiledTaskNode {
	node := compiledNode()
	node.WorkflowVersion = 1
	node.Deadline = values.TimeExpr{
		AnchorPath:     "run.start_date",
		OffsetKind:     values.TimeExprOffsetBusinessDays,
		OffsetDays:     3,
		Calendar:       values.CalendarRef{Ref: "US-FED", Version: "cal-2026a"},
		Zone:           values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"},
		Disambiguation: values.DisambiguationRejectGap,
	}
	node.DeadlineDefault = values.DeadlineDefault{
		Schema:        "hcmnext.task.review.TimeoutDefault/v1",
		CanonicalJSON: `{"verdict":"NO_RESPONSE"}`,
	}
	return node
}

func TestResolveClaimExpiry(t *testing.T) {
	node := deadlineTaskNode()
	anchor := values.NewInstant(time.Date(2026, time.October, 23, 15, 0, 0, 0, time.UTC))
	ds := values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "cal-2026a"}
	got, err := steptask.ResolveClaimExpiry(node, anchor, taskWeekendCalendar{version: "cal-2026a"}, ds)
	if err != nil {
		t.Fatal(err)
	}
	want := values.NewInstant(time.Date(2026, time.October, 28, 4, 0, 0, 0, time.UTC))
	if got != want {
		t.Fatalf("claim expiry = %s, want %s", got, want)
	}
	plain := compiledNode()
	plain.WorkflowVersion = 1
	if _, err := steptask.ResolveClaimExpiry(plain, anchor, taskWeekendCalendar{version: "cal-2026a"}, ds); !errors.Is(err, steptask.ErrNoDeadline) {
		t.Fatalf("deadline-free node err = %v, want ErrNoDeadline", err)
	}
}

func TestExpiredCarriesDeadlineDefault(t *testing.T) {
	node := deadlineTaskNode()
	item := pendingTaskItem(instanceFixture, node, "principal:reviewer", uuid.New())
	item.Status = workitem.StatusExpired
	c, err := steptask.NewContinuation(instanceFixture, node, item)
	if err != nil {
		t.Fatal(err)
	}
	now := values.NewInstant(fixedInstant.Add(48 * time.Hour))
	res, err := steptask.Resolve(c, item, nil, nil, now, steptask.Event{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != steptask.OutcomeExpired {
		t.Fatalf("outcome = %s, want EXPIRED", res.Outcome)
	}
	if res.DefaultOutputDigest != node.DeadlineDefault.Digest() {
		t.Fatalf("default digest = %q, want %q", res.DefaultOutputDigest, node.DeadlineDefault.Digest())
	}
	out := res.ToNodeOutcome(node.NodeID)
	if out.Outcome != "EXPIRED" || out.OutputDigest != node.DeadlineDefault.Digest() {
		t.Fatalf("node outcome = %+v, want EXPIRED with the default digest", out)
	}
	// Without a declared default the route still completes on the digest.
	plain := compiledNode()
	plain.WorkflowVersion = 1
	plainItem := pendingTaskItem(instanceFixture, plain, "principal:reviewer", uuid.New())
	plainItem.Status = workitem.StatusExpired
	pc, err := steptask.NewContinuation(instanceFixture, plain, plainItem)
	if err != nil {
		t.Fatal(err)
	}
	pres, err := steptask.Resolve(pc, plainItem, nil, nil, now, steptask.Event{})
	if err != nil {
		t.Fatal(err)
	}
	if pout := pres.ToNodeOutcome(plain.NodeID); pout.OutputDigest != pres.Digest {
		t.Fatalf("default-free outcome digest = %q, want resolution digest", pout.OutputDigest)
	}
}

func TestContinuationRejectsBadDeadline(t *testing.T) {
	node := deadlineTaskNode()
	node.Deadline.AnchorPath = ""
	item := pendingTaskItem(instanceFixture, node, "principal:reviewer", uuid.New())
	// An anchorless expression is unset, so the node still binds; a
	// half-declared default must not.
	node.DeadlineDefault.CanonicalJSON = ""
	if _, err := steptask.NewContinuation(instanceFixture, node, item); err == nil {
		t.Fatal("half-declared deadline default bound without error")
	}
	node.DeadlineDefault = values.DeadlineDefault{}
	node.Deadline = values.TimeExpr{AnchorPath: "run.x", OffsetKind: "FORTNIGHTS"}
	if _, err := steptask.NewContinuation(instanceFixture, node, item); err == nil {
		t.Fatal("invalid deadline expression bound without error")
	}
}
