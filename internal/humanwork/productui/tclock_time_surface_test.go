package productui

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type approverSpy struct{ got []string }

func (a *approverSpy) ApproveTimecards(ids []string) error { a.got = ids; return nil }

type revokerSpy struct{ got string }

func (r *revokerSpy) RevokeDevice(id string) error { r.got = id; return errors.New("unused") }

type publisherSpy struct{ called bool }

func (p *publisherSpy) PublishWeek() error { p.called = true; return nil }

func renderTime(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func timeLocales() []string { return []string{"en-US", "de-DE", "ar-SA"} }

func TestTimeSurfacesFailClosedWithoutProjection(t *testing.T) {
	for _, locale := range timeLocales() {
		view := testView(PageClock)
		view.Locale = ResolveProductLocale(locale)
		for name, node := range map[string]ui.Node{
			"timecard":   TimecardPage(view, TimecardProjection{}),
			"review":     TimeReviewPage(view, TimeReviewProjection{}),
			"exceptions": TimeExceptionsPage(view, TimeExceptionsProjection{}),
			"fleet":      ClockFleetPage(view, ClockFleetProjection{}),
			"schedule":   CrewSchedulePage(view, CrewScheduleProjection{}),
		} {
			markup := renderTime(t, node)
			if strings.Count(markup, "<h1") != 1 {
				t.Fatalf("%s/%s: want one h1 in %s", name, locale, markup)
			}
			if !strings.Contains(markup, `role="status"`) || strings.Contains(markup, "⟦") {
				t.Fatalf("%s/%s: unavailable state is not an explained status: %s", name, locale, markup)
			}
			if strings.Contains(markup, "<button") || strings.Contains(markup, "href=") {
				t.Fatalf("%s/%s: unavailable state exposed an action: %s", name, locale, markup)
			}
		}
	}
}

func TestTimeSurfaceCopyHasEveryKeyInEveryLocale(t *testing.T) {
	for key := range timeCopyEN {
		if timeCopyDE[key] == "" || timeCopyAR[key] == "" {
			t.Fatalf("time copy key %q is missing a German or Arabic translation", key)
		}
	}
	for key := range timeCopyDE {
		if timeCopyEN[key] == "" {
			t.Fatalf("German time copy has key %q with no English source", key)
		}
	}
}

func TestTimeLinkRefusesDestinationsOutsideTheTimeArea(t *testing.T) {
	view := testView(PageClock)
	for _, href := range []string{"", "https://evil.example/workspace/app/time", "//evil.example", "/workspace/app/admin", "/workspace/app/timeout", "javascript:alert(1)"} {
		if node := timeLink(view, &ActionLinkProps{Label: "Open", Href: href}, "button"); node != nil {
			t.Fatalf("href %q was accepted", href)
		}
	}
	if timeLink(view, &ActionLinkProps{Label: "Open", Href: "/workspace/app/time/timecard?week=1"}, "button") == nil {
		t.Fatal("a time-area destination was refused")
	}
}

func TestTimecardShowsProblemsBeforeSubmit(t *testing.T) {
	view := testView(PageClock)
	projection := TimecardProjection{
		State: TimeSurfaceReady, PeriodLabel: "Sep 22 – Sep 28", StatusLabel: "Open", StatusTone: TimeToneInfo,
		Totals:     []TimeFact{{Label: "Regular", Value: "38 h"}},
		SubmitNote: "Fix 1 day first.",
		Days: []TimecardDay{
			{ID: "wed", DateLabel: "Wed, Sep 24", TotalLabel: "—", Exceptions: []TimecardException{{Label: "Missing clock-out", Fix: &ActionLinkProps{Label: "Fix a missing punch", Href: "/workspace/app/time/missing-punch"}}}},
			{ID: "thu", DateLabel: "Thu, Sep 25", TotalLabel: "8 h", Punches: []TimecardPunch{{Label: "7:00 AM – 3:30 PM", Detail: "Riverside"}}},
		},
	}
	markup := renderTime(t, TimecardPage(view, projection))
	for _, want := range []string{"My timecard", "Sep 22 – Sep 28", "1 day needs your attention", "Missing clock-out", `href="/workspace/app/time/missing-punch"`, "Fix 1 day first.", "38 h"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("timecard missing %q in %s", want, markup)
		}
	}
	if !strings.Contains(markup, "disabled") || strings.Contains(markup, "/workspace/app/time/timecard/submit") || !strings.Contains(markup, "Submit timecard for approval") {
		t.Fatalf("with an open problem the submit action must stay visible but off, beside the reason: %s", markup)
	}
	if !strings.Contains(markup, `href="#timecard-day-wed"`) {
		t.Fatalf("the banner must lead to the day with the problem: %s", markup)
	}
	projection.Submit = &ActionLinkProps{Label: "Submit timecard", Href: "/workspace/app/time/timecard/submit"}
	if !strings.Contains(renderTime(t, TimecardPage(view, projection)), "Submit timecard") {
		t.Fatal("submit action was not rendered when the service offered it")
	}
}

func TestTimeReviewSplitsAttentionFromReadyAndGatesBulkApproval(t *testing.T) {
	view := testView(PageClock)
	spy := &approverSpy{}
	projection := TimeReviewProjection{
		State: TimeSurfaceReady, PeriodLabel: "Sep 22 – Sep 28", Approver: spy,
		Needs: []TimeReviewRow{{ID: "n1", Worker: "Jordan", HoursLabel: "31 h", Problems: []string{"Missing clock-out"}, Open: &ActionLinkProps{Label: "Review timecard", Href: "/workspace/app/time/timecard?worker=n1"}}},
		Ready: []TimeReviewRow{{ID: "r1", Worker: "Walt", HoursLabel: "40 h"}, {ID: "r2", Worker: "Maria", HoursLabel: "38 h"}},
	}
	markup := renderTime(t, TimeReviewPage(view, projection))
	if strings.Index(markup, "Jordan") > strings.Index(markup, "Walt") {
		t.Fatalf("attention rows must come before ready rows: %s", markup)
	}
	for _, want := range []string{"Approve timecards", "Needs your attention (1)", "Ready to approve (2)", "Select all", "Select timecards to approve them together.", "Missing clock-out"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("review missing %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, "Approve 1 selected") || strings.Contains(markup, "Yes, approve") || spy.got != nil {
		t.Fatalf("an approval was offered or run with nothing selected: %s", markup)
	}
	if strings.Count(markup, `type="checkbox"`) != 3 {
		t.Fatalf("only ready timecards (plus select-all) are selectable: %s", markup)
	}
	projection.Approver = nil
	if strings.Contains(renderTime(t, TimeReviewPage(view, projection)), `type="button"`) {
		t.Fatal("bulk controls rendered without an approver port")
	}
}

func TestTimeReviewEmptyStateSaysWhatToExpect(t *testing.T) {
	markup := renderTime(t, TimeReviewPage(testView(PageClock), TimeReviewProjection{State: TimeSurfaceReady, PeriodLabel: "Sep 22 – Sep 28"}))
	if !strings.Contains(markup, "No timecards to review") || !strings.Contains(markup, "appear here") {
		t.Fatalf("empty review lacks direction: %s", markup)
	}
}

func TestTimeReviewRouteEmbedsUnderTheShellHeading(t *testing.T) {
	view := testView(PageTimeApproval)
	view.TimeReviewProjection = TimeReviewProjection{State: TimeSurfaceReady, PeriodLabel: "Sep 22 – Sep 28", Ready: []TimeReviewRow{{ID: "r1", Worker: "Walt", HoursLabel: "40 h"}}}
	markup := renderTime(t, timeApprovalPage(view))
	if strings.Contains(markup, "<h1") || !strings.Contains(markup, "Walt") {
		t.Fatalf("routed review must not print a second h1: %s", markup)
	}
	fallback := renderTime(t, timeApprovalPage(testView(PageTimeApproval)))
	if !strings.Contains(fallback, "not available yet") {
		t.Fatalf("routed review lost its fail-closed state: %s", fallback)
	}
}

func TestTimeExceptionsListsOwnerKindAndActions(t *testing.T) {
	view := testView(PageClock)
	projection := TimeExceptionsProjection{
		State:   TimeSurfaceReady,
		Filters: []TimeExceptionFilter{{Label: "All problems", Count: "2", Href: "/workspace/app/time/exceptions", Current: true}, {Label: "Late", Href: "https://elsewhere.example"}},
		Rows: []TimeExceptionRow{{ID: "e1", Worker: "Jordan", DateLabel: "Wed, Sep 24", KindLabel: "Missing clock-out", KindTone: TimeToneBad, Detail: "No clock-out.", AgeLabel: "Waiting 4 days",
			Resolve: &ActionLinkProps{Label: "Fix punch", Href: "/workspace/app/time/correction?e=1"}, Open: &ActionLinkProps{Label: "Open timecard", Href: "/workspace/app/timeout"}}},
	}
	markup := renderTime(t, TimeExceptionsPage(view, projection))
	for _, want := range []string{"Time exceptions", "Jordan", "Missing clock-out", "Waiting 4 days", "Fix punch", `aria-current="true"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("exceptions missing %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, "elsewhere.example") || strings.Contains(markup, "Open timecard") {
		t.Fatalf("an off-area destination leaked: %s", markup)
	}
	empty := renderTime(t, TimeExceptionsPage(view, TimeExceptionsProjection{State: TimeSurfaceReady}))
	if !strings.Contains(empty, "No exceptions") {
		t.Fatalf("empty exceptions lack direction: %s", empty)
	}
}

func TestClockFleetPutsRevokeBehindNamedConfirmation(t *testing.T) {
	view := testView(PageClock)
	spy := &revokerSpy{}
	projection := ClockFleetProjection{
		State: TimeSurfaceReady, SummaryLabel: "2 devices · 1 needs attention", SummaryTone: TimeToneWarn, Revoker: spy,
		Devices: []ClockFleetDevice{
			{ID: "d1", Name: "Harbor tablet", SiteLabel: "Harbor Street", StatusLabel: "Offline", StatusTone: TimeToneBad, Issues: []string{"Its clock is 4 minutes ahead."}},
			{ID: "d2", Name: "Old tablet", StatusLabel: "Turned off", Revoked: true},
		},
	}
	markup := renderTime(t, ClockFleetPage(view, projection))
	for _, want := range []string{"Time clock devices", "Harbor tablet", "Its clock is 4 minutes ahead.", "Turn off this device"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("fleet missing %q in %s", want, markup)
		}
	}
	if strings.Count(markup, "Turn off this device") != 1 || strings.Contains(markup, "button destructive") || spy.got != "" {
		t.Fatalf("revoke must be offered once, unconfirmed, and never run at render: %s", markup)
	}
	projection.Revoker = nil
	if strings.Contains(renderTime(t, ClockFleetPage(view, projection)), "Turn off this device") {
		t.Fatal("revoke rendered without a revoker port")
	}
	empty := renderTime(t, ClockFleetPage(view, ClockFleetProjection{State: TimeSurfaceReady}))
	if !strings.Contains(empty, "No devices set up yet") {
		t.Fatalf("empty fleet lacks direction: %s", empty)
	}
}

func TestCrewScheduleMarksDraftsAndConflictsAndGatesPublish(t *testing.T) {
	view := testView(PageClock)
	spy := &publisherSpy{}
	projection := CrewScheduleProjection{
		State: TimeSurfaceReady, WeekLabel: "Sep 29 – Oct 5", StatusLabel: "3 drafts", StatusTone: TimeToneWarn,
		Days: []string{"Mon 29", "Tue 30"},
		Rows: []CrewScheduleRow{{ID: "w1", Worker: "Walt", Cells: [][]CrewShift{
			{{TimeLabel: "7:00 AM – 3:30 PM", SiteLabel: "Riverside", Draft: true}},
			{{TimeLabel: "6:30 AM – 3:00 PM", SiteLabel: "Harbor", ConflictLabel: "Double-booked"}},
		}}},
		Publisher: spy, UnpublishedCount: 3,
	}
	markup := renderTime(t, CrewSchedulePage(view, projection))
	for _, want := range []string{"Crew schedule", "Mon 29", "Draft", "Double-booked", "Publish this week", "3 shifts are drafts", `scope="col"`, `scope="row"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("schedule missing %q in %s", want, markup)
		}
	}
	if spy.called {
		t.Fatal("publish ran during render")
	}
	projection.Publisher = nil
	projection.PublishBlocked = "Resolve the double-booked shift first."
	blocked := renderTime(t, CrewSchedulePage(view, projection))
	if strings.Contains(blocked, "Publish this week") || !strings.Contains(blocked, "Resolve the double-booked shift first.") {
		t.Fatalf("blocked publish must explain itself and offer no button: %s", blocked)
	}
}

func TestTimeSurfacesRenderInEveryLocaleWithoutMarkers(t *testing.T) {
	for _, locale := range timeLocales() {
		view := testView(PageClock)
		view.Locale = ResolveProductLocale(locale)
		nodes := []ui.Node{
			TimecardPage(view, TimecardProjection{State: TimeSurfaceReady, PeriodLabel: "P", StatusLabel: "S", Days: []TimecardDay{{ID: "a", DateLabel: "D", TotalLabel: "1 h", Exceptions: []TimecardException{{Label: "X"}}}}}),
			TimeReviewPage(view, TimeReviewProjection{State: TimeSurfaceReady, PeriodLabel: "P", Approver: &approverSpy{}, Needs: []TimeReviewRow{{ID: "n", Worker: "W"}}, Ready: []TimeReviewRow{{ID: "r", Worker: "V"}}}),
			ClockFleetPage(view, ClockFleetProjection{State: TimeSurfaceReady, Revoker: &revokerSpy{}, Devices: []ClockFleetDevice{{ID: "d", Name: "N"}}}),
			CrewSchedulePage(view, CrewScheduleProjection{State: TimeSurfaceReady, WeekLabel: "W", Days: []string{"M"}, Publisher: &publisherSpy{}, UnpublishedCount: 1, Rows: []CrewScheduleRow{{ID: "w", Worker: "W"}}}),
		}
		for _, node := range nodes {
			markup := renderTime(t, node)
			if strings.Contains(markup, "⟦") || strings.Contains(markup, "{count}") || strings.Contains(markup, "{name}") || strings.Contains(markup, "{period}") {
				t.Fatalf("%s leaked a placeholder or unresolved key: %s", locale, markup)
			}
		}
	}
}

func TestTimeUnavailableOffersOnlyARetryThatNavigatesToTheSameRoute(t *testing.T) {
	view := testView(PageTimeApproval)
	var navigated string
	view.Navigate = func(href string) { navigated = href }
	markup := renderTime(t, TimeReviewPage(view, TimeReviewProjection{}))
	if strings.Count(markup, "<button") != 1 || !strings.Contains(markup, "Try again") || strings.Contains(markup, "href=") {
		t.Fatalf("unavailable state must offer exactly one retry button and no link: %s", markup)
	}
	if navigated != "" {
		t.Fatal("render navigated")
	}
}
