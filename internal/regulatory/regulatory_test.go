package regulatory

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func mustDate(t *testing.T, s string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(s)
	if err != nil {
		t.Fatalf("date %s: %v", s, err)
	}
	return d
}

func cite(section string) legal.Citation {
	return legal.Citation{
		SourceFile: "planning/research/state-employment-law/california.md",
		Section:    section,
		Status:     legal.ReviewStatusUnreviewed,
	}
}

func testPack(t *testing.T) legal.RulePack {
	t.Helper()
	start := mustDate(t, "2026-01-01")
	window, err := legal.NewOpenEffectiveWindow(start)
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	pack := legal.RulePack{
		PackID:       "workforce-action-notices",
		Version:      1,
		Jurisdiction: legal.Jurisdiction{Country: "US", State: "CA"},
		Window:       window,
		MiniWARNTriggers: []legal.MiniWARNTrigger{
			{ID: "ca-mini-warn", EmployeeThreshold: 50, LayoffWindowDays: 30, NoticeDays: 60, Citation: cite("Cal. Lab. Code mini-WARN")},
		},
		Notices: []legal.NoticeObligation{
			{ID: "release-consideration", Who: "employer_to_worker", TimingDirection: "AFTER", TimingDays: 21, Channel: "written", Citation: cite("release consideration")},
			{ID: "release-revocation", Who: "worker_to_employer", TimingDirection: "AFTER", TimingDays: 7, Channel: "written", Citation: cite("release revocation")},
			{ID: "screening-wait", Who: "employer_to_worker", TimingDirection: "BEFORE", TimingDays: 5, Channel: "written", Citation: cite("screening wait")},
			{ID: "leave-designation", Who: "employer_to_worker", TimingDirection: "AFTER", TimingDays: 5, Channel: "written", Citation: cite("leave designation")},
		},
		FinalPayDeadlines: []legal.FinalPayDeadline{
			{ID: "final-pay", Trigger: "involuntary separation", DeadlineDescription: "due immediately at separation", Citation: cite("final pay")},
		},
	}
	if err := pack.Validate(); err != nil {
		t.Fatalf("pack: %v", err)
	}
	return pack
}

// TestTodo_WF_CAP_011 proves the PRIMARY matrix of WF-CAP-011: rule packs
// publish the timing and thresholds; aggregate_layoff_window computes
// rolling site counts; release_disclosure lists the decisional unit; and
// each deadline resolves as a statute reference usable by TimeExpr.
func TestTodo_WF_CAP_011(t *testing.T) {
	pack := testPack(t)
	asOf := mustDate(t, "2026-10-22")

	events := []LayoffEvent{
		{SiteID: "site-oakland", Date: mustDate(t, "2026-10-01"), WorkerID: "worker-1"},
		{SiteID: "site-oakland", Date: mustDate(t, "2026-10-10"), WorkerID: "worker-2"},
		{SiteID: "site-oakland", Date: mustDate(t, "2026-09-01"), WorkerID: "worker-old"},
		{SiteID: "site-reno", Date: mustDate(t, "2026-10-05"), WorkerID: "worker-3"},
	}
	windows, err := AggregateLayoffWindow(pack, "site-oakland", events, asOf)
	if err != nil {
		t.Fatalf("AggregateLayoffWindow: %v", err)
	}
	if len(windows) != 1 {
		t.Fatalf("windows = %d, want one per mini-WARN trigger", len(windows))
	}
	window := windows[0]
	if window.Count != 2 || window.Threshold != 50 || window.Tripped {
		t.Fatalf("window = %+v, want the 2 in-window site events under the 50 threshold", window)
	}
	if window.WindowStart.String() != "2026-09-22" || window.WindowEnd.String() != "2026-10-22" {
		t.Fatalf("window = %s..%s, want the 30-day roll", window.WindowStart, window.WindowEnd)
	}
	if !window.HasNoticeDue || window.NoticeDue.String() != "2026-08-02" {
		t.Fatalf("notice due = %s/%v, want 60 days before the earliest in-window layoff", window.NoticeDue, window.HasNoticeDue)
	}

	disclosure, err := ReleaseDisclosure(pack, DecisionalUnit{
		UnitID: "unit-rif-7",
		Members: []UnitMember{
			{WorkerID: "worker-b", JobTitle: "analyst", Eligible: true},
			{WorkerID: "worker-a", JobTitle: "analyst", Eligible: true},
			{WorkerID: "worker-c", JobTitle: "manager", Eligible: false},
		},
	}, "release-consideration", "release-revocation", mustDate(t, "2026-10-01"))
	if err != nil {
		t.Fatalf("ReleaseDisclosure: %v", err)
	}
	if len(disclosure.Members) != 2 || disclosure.Members[0].WorkerID != "worker-a" || disclosure.Members[1].WorkerID != "worker-b" {
		t.Fatalf("decisional unit = %+v, want the eligible members sorted", disclosure.Members)
	}
	if disclosure.ConsiderationDue.String() != "2026-10-22" || disclosure.RevocationEnd.String() != "2026-10-08" {
		t.Fatalf("consideration/revocation = %s/%s, want pack-timed 21/7 day windows", disclosure.ConsiderationDue, disclosure.RevocationEnd)
	}
	if disclosure.Digest == "" {
		t.Fatal("disclosure carries no digest")
	}

	ref, err := DeadlineFor(pack, "screening-wait", mustDate(t, "2026-10-10"))
	if err != nil {
		t.Fatalf("DeadlineFor: %v", err)
	}
	if !ref.HasDue || ref.Due.String() != "2026-10-05" {
		t.Fatalf("screening wait due = %s, want 5 days before the decision", ref.Due)
	}
	expr := ref.TimeExpr()
	for _, want := range []string{"workforce-action-notices", "screening-wait", "2026-10-05", "BEFORE"} {
		if !strings.Contains(expr, want) {
			t.Fatalf("time expression %q misses %q", expr, want)
		}
	}
	prose, err := DeadlineFor(pack, "final-pay", mustDate(t, "2026-10-10"))
	if err != nil {
		t.Fatalf("DeadlineFor: %v", err)
	}
	if prose.HasDue || prose.Description == "" {
		t.Fatalf("prose deadline = %+v, want an unresolved reference carrying its description", prose)
	}
	if _, err := DeadlineFor(pack, "no-such-obligation", mustDate(t, "2026-10-10")); err == nil {
		t.Fatal("unknown obligation resolved to a deadline")
	}
}

// TestTodo_WF_CAP_011_Golden pins the rolling counts, the decisional-unit
// listing and the statute references for the fixture pack.
func TestTodo_WF_CAP_011_Golden(t *testing.T) {
	pack := testPack(t)
	asOf := mustDate(t, "2026-10-22")
	windows, err := AggregateLayoffWindow(pack, "site-oakland", []LayoffEvent{
		{SiteID: "site-oakland", Date: mustDate(t, "2026-10-01"), WorkerID: "worker-1"},
		{SiteID: "site-oakland", Date: mustDate(t, "2026-10-10"), WorkerID: "worker-2"},
	}, asOf)
	if err != nil {
		t.Fatalf("AggregateLayoffWindow: %v", err)
	}
	if len(windows) != 1 || windows[0].Count != 2 || windows[0].Threshold != 50 || windows[0].Tripped {
		t.Fatalf("golden window = %+v", windows)
	}
	disclosure, err := ReleaseDisclosure(pack, DecisionalUnit{
		UnitID:  "unit-rif-7",
		Members: []UnitMember{{WorkerID: "worker-a", JobTitle: "analyst", Eligible: true}},
	}, "release-consideration", "release-revocation", mustDate(t, "2026-10-01"))
	if err != nil {
		t.Fatalf("ReleaseDisclosure: %v", err)
	}
	const wantDigest = "sha256:47a51ee6128b16ab252720e0e7afd6efc4c18a341f3227b71a2a2247ed64ceeb"
	if disclosure.Digest != wantDigest {
		t.Fatalf("disclosure digest = %q, want %q", disclosure.Digest, wantDigest)
	}
	ref, err := DeadlineFor(pack, "ca-mini-warn", mustDate(t, "2026-12-01"))
	if err != nil {
		t.Fatalf("DeadlineFor: %v", err)
	}
	const wantExpr = `statute_due(pack="workforce-action-notices",version=1,jurisdiction="US-CA",obligation="ca-mini-warn",anchor="2026-12-01",direction="BEFORE",days=60,due="2026-10-02")`
	if ref.TimeExpr() != wantExpr {
		t.Fatalf("time expression = %q, want %q", ref.TimeExpr(), wantExpr)
	}
}
