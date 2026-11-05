package signal_test

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

type signalWeekendCalendar struct{ version string }

func (w signalWeekendCalendar) CalendarVersion() string { return w.version }

func (w signalWeekendCalendar) IsWorkingDay(d values.LocalDate) (bool, error) {
	wd := time.Date(int(d.Year()), d.Month(), int(d.Day()), 12, 0, 0, 0, time.UTC).Weekday()
	return wd != time.Saturday && wd != time.Sunday, nil
}

func closeExprNode() *workflow.CompiledNode {
	return &workflow.CompiledNode{
		ID:   "signal.background_check_result",
		Type: workflow.StepSignal,
		Signal: &workflow.CompiledSignal{
			EventType:                "background_check.completed",
			CorrelationKeyExpression: "candidate_id",
			ExpectedSchemaRef:        workflow.SchemaRef{SchemaID: "schema.background_check_result", Version: 1, ProtobufFullName: "schema.background_check_result"},
			AcceptedSources:          []string{"vendor.checkr"},
			Ordering:                 "MONOTONIC_SEQUENCE",
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
		},
	}
}

func TestFromCompiledCloseTimeExpr(t *testing.T) {
	anchor := values.NewInstant(time.Date(2026, time.October, 23, 15, 0, 0, 0, time.UTC))
	ctx := signal.InstanceContext{
		Tenant:             "acme-corp",
		WorkflowInstanceID: "wf-instance-1",
		CorrelationValue:   "cand-42",
		Anchor:             anchor,
		Calendars:          signalWeekendCalendar{version: "cal-2026a"},
		Dataset:            values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "cal-2026a"},
	}
	sub, err := signal.FromCompiled(closeExprNode(), ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := values.NewInstant(time.Date(2026, time.October, 28, 4, 0, 0, 0, time.UTC))
	if sub.ClosesAt != want {
		t.Fatalf("ClosesAt = %s, want %s", sub.ClosesAt, want)
	}
	if sub.DeadlineDefault.Schema != "hcmnext.signal.TimeoutDefault/v1" {
		t.Fatalf("default lost in translation: %+v", sub.DeadlineDefault)
	}
	if sub.Digest() == baseSubscription().Digest() {
		t.Fatal("expression subscription shares the plain digest")
	}
	conflict := ctx
	conflict.ClosesAt = want
	if _, err := signal.FromCompiled(closeExprNode(), conflict); err == nil {
		t.Fatal("expression plus context close instant combined without error")
	}
	noAnchor := ctx
	noAnchor.Anchor = values.Instant{}
	if _, err := signal.FromCompiled(closeExprNode(), noAnchor); err == nil {
		t.Fatal("unset anchor resolved without error")
	}
	bad := closeExprNode()
	bad.Signal.CloseTimeExpr.OffsetKind = "FORTNIGHTS"
	if _, err := signal.FromCompiled(bad, ctx); err == nil {
		t.Fatal("bogus offset kind translated without error")
	}
}

func TestLateSignalCarriesTimeoutDefault(t *testing.T) {
	sub := baseSubscription()
	sub.ClosesAt = mustInstant(t, "2026-06-17T12:00:00Z")
	sub.DeadlineDefault = values.DeadlineDefault{
		Schema:        "hcmnext.signal.TimeoutDefault/v1",
		CanonicalJSON: `{"verdict":"NO_RESPONSE"}`,
	}
	sig := baseSignal(t, []byte(`{"result":"clear"}`))
	res, err := signal.Accept(sub, sig, nil, testVerifier, mustInstant(t, "2026-06-17T12:00:01Z"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != signal.StatusRefusedLate {
		t.Fatalf("status = %s, want REFUSED_LATE", res.Status)
	}
	if res.DefaultOutputDigest != sub.DeadlineDefault.Digest() {
		t.Fatalf("default digest = %q, want %q", res.DefaultOutputDigest, sub.DeadlineDefault.Digest())
	}
	out := res.ToNodeOutcome(sub.NodeID)
	if out.Outcome != "TIMED_OUT" || out.OutputDigest != sub.DeadlineDefault.Digest() {
		t.Fatalf("node outcome = %+v, want TIMED_OUT with the default digest", out)
	}
	plain := baseSubscription()
	plain.ClosesAt = sub.ClosesAt
	pres, err := signal.Accept(plain, sig, nil, testVerifier, mustInstant(t, "2026-06-17T12:00:01Z"))
	if err != nil {
		t.Fatal(err)
	}
	if pout := pres.ToNodeOutcome(plain.NodeID); pout.Outcome != "TIMED_OUT" || pout.OutputDigest != "" {
		t.Fatalf("default-free outcome = %+v, want TIMED_OUT with no digest", pout)
	}
}
