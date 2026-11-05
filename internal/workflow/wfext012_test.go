package workflow_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	stepapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
	stepsignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
	steptask "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/task"
	stepwait "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
)

// ext012WeekendCalendar is a CalendarDataset with a bare weekend rule.
type ext012WeekendCalendar struct{ version string }

func (w ext012WeekendCalendar) CalendarVersion() string { return w.version }

func (w ext012WeekendCalendar) IsWorkingDay(d values.LocalDate) (bool, error) {
	wd := time.Date(int(d.Year()), d.Month(), int(d.Day()), 12, 0, 0, 0, time.UTC).Weekday()
	return wd != time.Saturday && wd != time.Sunday, nil
}

func ext012Dataset() values.DatasetVersions {
	return values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "cal-2026a"}
}

func ext012Anchor() values.Instant {
	// Friday 2026-10-23 15:00 UTC = 11:00 America/New_York.
	return values.NewInstant(time.Date(2026, time.October, 23, 15, 0, 0, 0, time.UTC))
}

// ext012FireAt is Friday +3 business days = Wednesday start of day EDT.
func ext012FireAt() values.Instant {
	return values.NewInstant(time.Date(2026, time.October, 28, 4, 0, 0, 0, time.UTC))
}

func ext012WaitSpec() *workflow.WaitSpec {
	return &workflow.WaitSpec{
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
	}
}

func ext012SignalSpec() *workflow.SignalSpec {
	return &workflow.SignalSpec{
		EventType:                "hcmnext.events.promotion_ack",
		CorrelationKeyExpression: "worker_id",
		ExpectedSchemaRef:        fixtureSchemaRef("PromotionAckPayload"),
		AcceptedSources:          []string{"hcmnext.integrations.hris"},
		Ordering:                 workflow.SignalOrderingNone,
		CloseTimeExpr: &workflow.TimeExprSpec{
			AnchorPath:      "run.start_date",
			OffsetKind:      "BUSINESS_DAYS",
			OffsetDays:      3,
			CalendarRef:     "US-FED",
			CalendarVersion: "cal-2026a",
			ZoneID:          "America/New_York",
			ZoneTzdbVersion: "2026a",
			Disambiguation:  "REJECT_GAP",
		},
		TimeoutDefault: &workflow.DeadlineDefaultSpec{
			Schema:        "hcmnext.signal.TimeoutDefault/v1",
			CanonicalJSON: `{"verdict":"NO_RESPONSE"}`,
		},
	}
}

func ext012TaskNode() steptask.CompiledTaskNode {
	return steptask.CompiledTaskNode{
		WorkflowID: "wf.ext012", WorkflowVersion: 1, NodeID: "fix_task", WorkType: "ext012.review",
		OutputSchema:        workflow.SchemaRef{SchemaID: "hcmnext.task.ext012.Output", Version: 1, ProtobufFullName: "hcmnext.task.ext012.Output"},
		FormDefinition:      steptask.VersionedRef{Ref: "form.ext012.review", Version: 1},
		AccessibilityPolicy: steptask.VersionedRef{Ref: "policy.accessibility.default", Version: 1},
		AccommodationPolicy: steptask.VersionedRef{Ref: "policy.accommodation.default", Version: 1},
		Deadline: values.TimeExpr{
			AnchorPath:     "run.start_date",
			OffsetKind:     values.TimeExprOffsetBusinessDays,
			OffsetDays:     3,
			Calendar:       values.CalendarRef{Ref: "US-FED", Version: "cal-2026a"},
			Zone:           values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"},
			Disambiguation: values.DisambiguationRejectGap,
		},
		DeadlineDefault: values.DeadlineDefault{
			Schema:        "hcmnext.task.ext012.TimeoutDefault/v1",
			CanonicalJSON: `{"verdict":"NO_RESPONSE"}`,
		},
	}
}

func ext012ApprovalRequirement() humanwork.ApprovalRequirement {
	return humanwork.ApprovalRequirement{
		RequirementID: "req.ext012/v1", Revision: 1,
		Quorum:   humanwork.Quorum{MinApprovals: 1},
		Deadline: humanwork.Deadline{Expiry: values.NewInstant(time.Date(2027, time.January, 15, 12, 0, 0, 0, time.UTC))},
		DeadlineExpr: &values.TimeExpr{
			AnchorPath:     "run.start_date",
			OffsetKind:     values.TimeExprOffsetBusinessDays,
			OffsetDays:     3,
			Calendar:       values.CalendarRef{Ref: "US-FED", Version: "cal-2026a"},
			Zone:           values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"},
			Disambiguation: values.DisambiguationRejectGap,
		},
	}
}

func findCompiledNode(t *testing.T, plan *workflow.CompiledWorkflow, id string) *workflow.CompiledNode {
	t.Helper()
	for i := range plan.Nodes {
		if plan.Nodes[i].ID == id {
			return &plan.Nodes[i]
		}
	}
	t.Fatalf("node %q not in compiled plan", id)
	return nil
}

// TestTodo_WF_EXT_012 is the PRIMARY contract: one TimeExpr shape (anchor
// mapping, offset, calendar ref, optional statute ref) is accepted by WAIT,
// TASK, APPROVAL and SIGNAL, and a deadline outcome route with a declared
// default payload expresses non-response.
func TestTodo_WF_EXT_012(t *testing.T) {
	def := waitSignalDefinition()
	def.Nodes[0].Wait = ext012WaitSpec()
	def.Nodes[1].Signal = ext012SignalSpec()
	plan, err := workflow.Compile(def, waitSignalOptions())
	if err != nil {
		t.Fatalf("time-expression definition does not compile: %v", err)
	}
	compiledWait := findCompiledNode(t, plan, wsNodeWait)
	if compiledWait.Wait == nil || compiledWait.Wait.TimeExpr == nil {
		t.Fatal("compiled WAIT node carries no time expression")
	}
	compiledSignal := findCompiledNode(t, plan, wsNodeSignal)
	if compiledSignal.Signal == nil || compiledSignal.Signal.CloseTimeExpr == nil || compiledSignal.Signal.TimeoutDefault == nil {
		t.Fatal("compiled SIGNAL node carries no close expression and timeout default")
	}

	anchor := ext012Anchor()
	cals := ext012WeekendCalendar{version: "cal-2026a"}
	ds := ext012Dataset()

	t.Run("WAIT_wakes_from_expression", func(t *testing.T) {
		node, err := stepwait.FromCompiled(compiledWait)
		if err != nil {
			t.Fatal(err)
		}
		node.WorkflowID, node.WorkflowVersion = wsWorkflowID, 1
		req, err := stepwait.ComputeTimerRequirementAtAnchor(node, anchor, cals, ds)
		if err != nil {
			t.Fatal(err)
		}
		if req.FireAt != ext012FireAt() {
			t.Fatalf("WAIT fire_at = %s, want %s", req.FireAt, ext012FireAt())
		}
	})

	t.Run("TASK_expires_with_default", func(t *testing.T) {
		node := ext012TaskNode()
		expiry, err := steptask.ResolveClaimExpiry(node, anchor, cals, ds)
		if err != nil {
			t.Fatal(err)
		}
		if expiry != ext012FireAt() {
			t.Fatalf("TASK claim expiry = %s, want %s", expiry, ext012FireAt())
		}
		instance := uuid.MustParse("40000000-0000-0000-0000-000000000012")
		item := workitem.WorkItem{
			TenantID: uuid.MustParse("50000000-0000-0000-0000-000000000012"), WorkItemID: uuid.New(),
			Kind: workitem.KindTask, WorkType: node.WorkType, Status: workitem.StatusExpired,
			WorkflowInstanceID: instance, NodeID: node.NodeID,
		}
		c, err := steptask.NewContinuation(instance, node, item)
		if err != nil {
			t.Fatal(err)
		}
		res, err := steptask.Resolve(c, item, nil, nil, values.NewInstant(expiry.Time().Add(time.Hour)), steptask.Event{})
		if err != nil {
			t.Fatal(err)
		}
		out := res.ToNodeOutcome(node.NodeID)
		if out.Outcome != "EXPIRED" || out.OutputDigest != node.DeadlineDefault.Digest() {
			t.Fatalf("TASK outcome = %+v, want EXPIRED with the default digest", out)
		}
	})

	t.Run("APPROVAL_deadline_resolves_and_expires_with_default", func(t *testing.T) {
		req := ext012ApprovalRequirement()
		dl, err := req.ResolveDeadline(anchor, cals, ds)
		if err != nil {
			t.Fatal(err)
		}
		if dl.DecideBy != ext012FireAt() {
			t.Fatalf("APPROVAL decide_by = %s, want %s", dl.DecideBy, ext012FireAt())
		}
		c := stepapproval.Continuation{
			WorkflowInstanceID: uuid.MustParse("40000000-0000-0000-0000-000000000013"),
			NodeID:             "fix_approval",
			Requirements: []stepapproval.Requirement{{
				RequirementID: "req.ext012/v1", RequirementRevision: 1,
				Quorum: 1, DecideBy: dl.DecideBy, Expiry: dl.Expiry,
			}},
		}.WithDeadlineDefault(values.DeadlineDefault{
			Schema:        "hcmnext.approval.ext012.TimeoutDefault/v1",
			CanonicalJSON: `{"verdict":"NO_RESPONSE"}`,
		})
		res, err := stepapproval.Resolve(c, nil, nil, values.NewInstant(dl.Expiry.Time().Add(time.Hour)),
			stepapproval.Event{Kind: stepapproval.EventDecisionsChanged})
		if err != nil {
			t.Fatal(err)
		}
		out := res.ToNodeOutcome("fix_approval")
		if out.Outcome != "EXPIRED" || out.OutputDigest == "" || out.OutputDigest == res.Digest {
			t.Fatalf("APPROVAL outcome = %+v, want EXPIRED with the default digest", out)
		}
	})

	t.Run("SIGNAL_closes_from_expression_and_times_out_with_default", func(t *testing.T) {
		sub, err := stepsignal.FromCompiled(compiledSignal, stepsignal.InstanceContext{
			Tenant:             "acme-corp",
			WorkflowInstanceID: "instance-ext012",
			CorrelationValue:   "worker-7",
			Anchor:             anchor,
			Calendars:          cals,
			Dataset:            ds,
		})
		if err != nil {
			t.Fatal(err)
		}
		if sub.ClosesAt != ext012FireAt() {
			t.Fatalf("SIGNAL closes_at = %s, want %s", sub.ClosesAt, ext012FireAt())
		}
		if sub.DeadlineDefault.Schema == "" {
			t.Fatal("SIGNAL subscription carries no timeout default")
		}
		late := sub.ClosesAt
		sig := stepsignal.Signal{
			Tenant: "acme-corp", Source: "hcmnext.integrations.hris",
			EventType: "hcmnext.events.promotion_ack", SchemaRef: sub.ExpectedSchemaRef,
			CorrelationKey: "worker_id", CorrelationValue: "worker-7",
			ReceivedAt: late,
		}
		res, err := stepsignal.Accept(sub, sig, nil, ext012AllowAll{}, late)
		if err != nil {
			t.Fatal(err)
		}
		out := res.ToNodeOutcome("signal_ack_received")
		if out.Outcome != "TIMED_OUT" || out.OutputDigest != sub.DeadlineDefault.Digest() {
			t.Fatalf("SIGNAL outcome = %+v, want TIMED_OUT with the default digest", out)
		}
	})
}

// ext012AllowAll is a signature Verifier that trusts every signal. The
// PRIMARY lateness check exercises the close time, not trust.
type ext012AllowAll struct{}

func (ext012AllowAll) Verify(stepsignal.Signal) error { return nil }

// ext012Calendar is a PROPERTY calendar fixture: a weekend predicate plus
// fixed holidays, over a pinned version.
type ext012Calendar struct {
	name     string
	version  string
	weekend  func(time.Weekday) bool
	holidays map[string]bool
}

func (c ext012Calendar) CalendarVersion() string { return c.version }

func (c ext012Calendar) IsWorkingDay(d values.LocalDate) (bool, error) {
	wd := time.Date(int(d.Year()), d.Month(), int(d.Day()), 12, 0, 0, 0, time.UTC).Weekday()
	if c.weekend(wd) {
		return false, nil
	}
	if c.holidays[d.String()] {
		return false, nil
	}
	return true, nil
}

func ext012PropertyCalendars() []ext012Calendar {
	satSun := func(wd time.Weekday) bool { return wd == time.Saturday || wd == time.Sunday }
	friSat := func(wd time.Weekday) bool { return wd == time.Friday || wd == time.Saturday }
	return []ext012Calendar{
		{name: "sat-sun-weekend", version: "prop-a", weekend: satSun},
		{name: "sat-sun-plus-holidays", version: "prop-b", weekend: satSun, holidays: map[string]bool{
			"2026-12-25": true, "2027-01-01": true, "2027-07-04": true,
		}},
		{name: "fri-sat-weekend", version: "prop-c", weekend: friSat, holidays: map[string]bool{
			"2027-01-01": true,
		}},
	}
}

func ext012CountWorking(t *testing.T, cal ext012Calendar, from, to values.LocalDate) int {
	t.Helper()
	n := 0
	for d := from; d.Compare(to) <= 0; d = d.AddDays(1) {
		working, err := cal.IsWorkingDay(d)
		if err != nil {
			t.Fatal(err)
		}
		if working {
			n++
		}
	}
	return n
}

// TestTodo_WF_EXT_012_Property proves the business-day arithmetic invariants
// across calendars: a zero offset is the identity, a non-zero offset lands
// on a working day, the count of working days between anchor and result is
// exactly the offset, and larger offsets never resolve earlier.
func TestTodo_WF_EXT_012_Property(t *testing.T) {
	cals := ext012PropertyCalendars()
	start, err := values.NewLocalDate(2026, time.January, 1)
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewLocalDate(2027, time.December, 31)
	if err != nil {
		t.Fatal(err)
	}
	offsets := []int{-30, -10, -3, -1, 0, 1, 2, 5, 10, 30}
	for _, cal := range cals {
		for d := start; d.Compare(end) <= 0; d = d.AddDays(1) {
			for _, n := range offsets {
				got, err := values.AddBusinessDays(d, n, cal)
				if err != nil {
					t.Fatalf("%s: AddBusinessDays(%s,%d): %v", cal.name, d, n, err)
				}
				if n == 0 {
					if got != d {
						t.Fatalf("%s: AddBusinessDays(%s,0) = %s, want identity", cal.name, d, got)
					}
					continue
				}
				working, err := cal.IsWorkingDay(got)
				if err != nil {
					t.Fatal(err)
				}
				if !working {
					t.Fatalf("%s: AddBusinessDays(%s,%d) = %s, not a working day", cal.name, d, n, got)
				}
				var between int
				if n > 0 {
					between = ext012CountWorking(t, cal, d.AddDays(1), got)
				} else {
					between = ext012CountWorking(t, cal, got, d.AddDays(-1))
				}
				if between != n && between != -n {
					t.Fatalf("%s: AddBusinessDays(%s,%d) = %s spans %d working days", cal.name, d, n, got, between)
				}
			}
			fwd, err := values.AddBusinessDays(d, 5, cal)
			if err != nil {
				t.Fatal(err)
			}
			fwdMore, err := values.AddBusinessDays(d, 6, cal)
			if err != nil {
				t.Fatal(err)
			}
			if fwdMore.Compare(fwd) <= 0 {
				t.Fatalf("%s: +6 (%s) does not resolve after +5 (%s)", cal.name, fwdMore, fwd)
			}
		}
	}
	// The same anchor and offset resolves differently under different
	// calendars: the property genuinely ranges across calendars.
	anchor, err := values.NewLocalDate(2026, time.December, 24)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[values.LocalDate]bool{}
	for _, cal := range cals {
		got, err := values.AddBusinessDays(anchor, 2, cal)
		if err != nil {
			t.Fatal(err)
		}
		seen[got] = true
	}
	if len(seen) < 2 {
		t.Fatalf("anchor %s +2 business days resolves identically on every calendar", anchor)
	}
}

// TestTodo_WF_EXT_012_Recovery proves the resolved instant is stable after
// restart: recomputing against the pinned dataset reproduces the fire-at
// instant and requirement digest, and a PIN replay keeps the pinned instant
// even when the current dataset has moved on.
func TestTodo_WF_EXT_012_Recovery(t *testing.T) {
	def := waitSignalDefinition()
	def.Nodes[0].Wait = ext012WaitSpec()
	plan, err := workflow.Compile(def, waitSignalOptions())
	if err != nil {
		t.Fatalf("time-expression definition does not compile: %v", err)
	}
	node, err := stepwait.FromCompiled(findCompiledNode(t, plan, wsNodeWait))
	if err != nil {
		t.Fatal(err)
	}
	node.WorkflowID, node.WorkflowVersion = wsWorkflowID, 1
	anchor := ext012Anchor()
	cals := ext012WeekendCalendar{version: "cal-2026a"}
	ds := ext012Dataset()

	before, err := stepwait.ComputeTimerRequirementAtAnchor(node, anchor, cals, ds)
	if err != nil {
		t.Fatal(err)
	}
	// Restart: recompute from the declaration, anchor and pinned dataset.
	after, err := stepwait.ComputeTimerRequirementAtAnchor(node, anchor, cals, ds)
	if err != nil {
		t.Fatal(err)
	}
	if after.FireAt != before.FireAt || after.Digest != before.Digest {
		t.Fatal("resolved instant or digest moved across a restart")
	}

	ref := values.TimerReference{
		Zone:     node.Zone,
		Calendar: node.Calendar,
		Policy:   values.ReferenceUpdatePin,
	}
	resolve := node.WakeTimeExpr.Resolver(anchor, cals)
	replay, err := values.ReplayTimer(ref, before.FireAt, ds, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Effective != before.FireAt || replay.DatasetChanged || replay.WouldChange || replay.ReviewRequired {
		t.Fatalf("PIN replay = %+v, want the pinned instant unchanged", replay)
	}
	// The dataset moves on but the pinned calendar data does not: PIN keeps
	// the deadline and records the skew instead of guessing.
	moved := values.DatasetVersions{TzdbVersion: "2026b", CalendarVersion: "cal-2026b"}
	skewed, err := values.ReplayTimer(ref, before.FireAt, moved, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if skewed.Effective != before.FireAt || !skewed.DatasetChanged || skewed.ReviewRequired {
		t.Fatalf("skewed PIN replay = %+v, want pinned instant kept without review", skewed)
	}
	if skewed.Evidence.RecomputeError == "" {
		t.Fatal("skewed replay recorded no recompute error")
	}
}
