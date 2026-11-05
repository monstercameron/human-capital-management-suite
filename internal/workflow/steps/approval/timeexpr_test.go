package approval_test

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	stepapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
)

type approvalWeekendCalendar struct{ version string }

func (w approvalWeekendCalendar) CalendarVersion() string { return w.version }

func (w approvalWeekendCalendar) IsWorkingDay(d values.LocalDate) (bool, error) {
	wd := time.Date(int(d.Year()), d.Month(), int(d.Day()), 12, 0, 0, 0, time.UTC).Weekday()
	return wd != time.Saturday && wd != time.Sunday, nil
}

func approvalDataset() values.DatasetVersions {
	return values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "cal-2026a"}
}

func exprRequirement() humanwork.ApprovalRequirement {
	req := requirement("req.expr/v1", 1, false)
	req.Deadline = humanwork.Deadline{Expiry: values.NewInstant(time.Date(2027, time.January, 15, 12, 0, 0, 0, time.UTC))}
	req.DeadlineExpr = &values.TimeExpr{
		AnchorPath:     "run.start_date",
		OffsetKind:     values.TimeExprOffsetBusinessDays,
		OffsetDays:     3,
		Calendar:       values.CalendarRef{Ref: "US-FED", Version: "cal-2026a"},
		Zone:           values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"},
		Disambiguation: values.DisambiguationRejectGap,
	}
	return req
}

func TestResolveRequirementDeadlines(t *testing.T) {
	set := humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{exprRequirement()}}
	anchor := values.NewInstant(time.Date(2026, time.October, 23, 15, 0, 0, 0, time.UTC))
	got, err := stepapproval.ResolveRequirementDeadlines(set, anchor, approvalWeekendCalendar{version: "cal-2026a"}, approvalDataset())
	if err != nil {
		t.Fatal(err)
	}
	want := values.NewInstant(time.Date(2026, time.October, 28, 4, 0, 0, 0, time.UTC))
	if got.Requirements[0].Deadline.DecideBy != want {
		t.Fatalf("DecideBy = %s, want %s", got.Requirements[0].Deadline.DecideBy, want)
	}
	if set.Requirements[0].Deadline.DecideBy.IsSet() {
		t.Fatal("resolution mutated the input set")
	}
	plain := humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{requirement("req.plain/v1", 1, false)}}
	kept, err := stepapproval.ResolveRequirementDeadlines(plain, anchor, nil, approvalDataset())
	if err != nil {
		t.Fatal(err)
	}
	if kept.Requirements[0].Deadline != plain.Requirements[0].Deadline {
		t.Fatal("absolute deadline did not pass through")
	}
	if _, err := stepapproval.ResolveRequirementDeadlines(set, values.Instant{}, approvalWeekendCalendar{version: "cal-2026a"}, approvalDataset()); err == nil {
		t.Fatal("unset anchor resolved without error")
	}
}

func TestApprovalExpiredCarriesDeadlineDefault(t *testing.T) {
	req := requirement("req.timeout/v1", 1, false)
	set := humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{req}}
	items := []workitem.WorkItem{pendingItem(itemAID, req, "principal:approver")}
	c := continuation(t, set, items)
	def := values.DeadlineDefault{
		Schema:        "hcmnext.approval.TimeoutDefault/v1",
		CanonicalJSON: `{"verdict":"NO_RESPONSE"}`,
	}
	if err := def.Validate(); err != nil {
		t.Fatal(err)
	}
	withDefault := c.WithDeadlineDefault(def)
	if withDefault.Digest == "" || withDefault.Digest == c.Digest {
		t.Fatal("default left the continuation digest unchanged")
	}
	late := values.NewInstant(expiry.Time().Add(time.Hour))
	res, err := stepapproval.Resolve(withDefault, items, nil, late, stepapproval.Event{Kind: stepapproval.EventDecisionsChanged})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != "EXPIRED" {
		t.Fatalf("outcome = %s, want EXPIRED", res.Outcome)
	}
	if res.DefaultOutputDigest != def.Digest() {
		t.Fatalf("default digest = %q, want %q", res.DefaultOutputDigest, def.Digest())
	}
	out := res.ToNodeOutcome("approve")
	if out.Outcome != "EXPIRED" || out.OutputDigest != def.Digest() {
		t.Fatalf("node outcome = %+v, want EXPIRED with the default digest", out)
	}
	plain, err := stepapproval.Resolve(c, items, nil, late, stepapproval.Event{Kind: stepapproval.EventDecisionsChanged})
	if err != nil {
		t.Fatal(err)
	}
	if pout := plain.ToNodeOutcome("approve"); pout.OutputDigest != plain.Digest {
		t.Fatalf("default-free outcome digest = %q, want resolution digest", pout.OutputDigest)
	}
}
