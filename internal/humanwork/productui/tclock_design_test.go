package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"strings"
	"testing"
	"time"
)

func clockAllowedView() View {
	view := testView(PageClock)
	view.EffectivePermissions = []RolePagePermission{{Page: PageClock, View: true, Create: true, Update: true}}
	view.EffectiveFeatures = []RoleFeaturePermission{
		{Page: PageClock, Feature: FeatureContent, View: true},
		{Page: PageClock, Feature: "clock_actions", View: true, Create: true, Update: true},
		{Page: PageClock, Feature: "kiosk_access", View: true, Create: true, Update: true},
	}
	return view
}

func TestClockPhaseIsInferredFromOfferedActions(t *testing.T) {
	in := &ActionLinkProps{Label: "Clock in", Href: "/workspace/app/time/clock/in"}
	out := &ActionLinkProps{Label: "Clock out", Href: "/workspace/app/time/clock/out"}
	brk := &ActionLinkProps{Label: "Start break", Href: "/workspace/app/time/clock/break"}
	end := &ActionLinkProps{Label: "End break", Href: "/workspace/app/time/clock/break-end"}
	for name, tc := range map[string]struct {
		projection ClockProjection
		want       ClockPhase
	}{
		"nothing offered":        {ClockProjection{}, ClockPhaseUnknown},
		"only clock in":          {ClockProjection{ClockIn: in}, ClockPhaseOut},
		"clock out and break":    {ClockProjection{ClockOut: out, StartBreak: brk}, ClockPhaseIn},
		"end break wins":         {ClockProjection{ClockOut: out, EndBreak: end}, ClockPhaseBreak},
		"unusable clock out":     {ClockProjection{ClockIn: in, ClockOut: &ActionLinkProps{Label: "Clock out"}}, ClockPhaseOut},
		"ambiguous stays asking": {ClockProjection{ClockIn: in, ClockOut: out}, ClockPhaseUnknown},
		"explicit phase wins":    {ClockProjection{Phase: ClockPhaseBreak, ClockIn: in}, ClockPhaseBreak},
	} {
		if got := tc.projection.clockPhase(); got != tc.want {
			t.Fatalf("%s: phase = %v, want %v", name, got, tc.want)
		}
	}
}

func TestClockPageOffersOnePrimaryActionPerPhase(t *testing.T) {
	view := clockAllowedView()
	in := &ActionLinkProps{Label: "Clock in", Href: "/workspace/app/time/clock/in"}
	out := &ActionLinkProps{Label: "Clock out", Href: "/workspace/app/time/clock/out"}
	brk := &ActionLinkProps{Label: "Start break", Href: "/workspace/app/time/clock/break"}
	end := &ActionLinkProps{Label: "End break", Href: "/workspace/app/time/clock/break-end"}

	clockedOut := renderTime(t, ClockPage(view, ClockProjection{State: ClockProjectionReady, StatusLabel: "Clocked out", ClockIn: in}))
	if strings.Count(clockedOut, "clock-primary") != 1 || !strings.Contains(clockedOut, `data-phase="out"`) || !strings.Contains(clockedOut, `href="/workspace/app/time/clock/in"`) {
		t.Fatalf("clocked-out page must have one Clock in primary: %s", clockedOut)
	}
	onShift := renderTime(t, ClockPage(view, ClockProjection{State: ClockProjectionReady, StatusLabel: "On the clock", ClockOut: out, StartBreak: brk}))
	if strings.Contains(onShift, `href="/workspace/app/time/clock/out"`) {
		t.Fatalf("clock out must ask before it records, not be a bare link: %s", onShift)
	}
	if !strings.Contains(onShift, `<button class="button primary clock-primary"`) || !strings.Contains(onShift, `href="/workspace/app/time/clock/break"`) || !strings.Contains(onShift, `data-phase="in"`) {
		t.Fatalf("on-shift page lost its guarded clock-out or break action: %s", onShift)
	}
	onBreak := renderTime(t, ClockPage(view, ClockProjection{State: ClockProjectionReady, StatusLabel: "On break", EndBreak: end, ClockOut: out}))
	if !strings.Contains(onBreak, `data-phase="break"`) || !strings.Contains(onBreak, `href="/workspace/app/time/clock/break-end"`) || strings.Index(onBreak, "End break") > strings.Index(onBreak, "Clock out") {
		t.Fatalf("on-break page must lead with End break: %s", onBreak)
	}
}

func TestClockPageTellsTheWorkerWhatHappened(t *testing.T) {
	view := clockAllowedView()
	in := &ActionLinkProps{Label: "Clock in", Href: "/workspace/app/time/clock/in"}
	failed := renderTime(t, ClockPage(view, ClockProjection{State: ClockProjectionReady, ClockIn: in, ErrorLabel: clockText(view.Locale, "action_error")}))
	if !strings.Contains(failed, `role="alert"`) || !strings.Contains(failed, "try again") || !strings.Contains(failed, `href="/workspace/app/time/clock/in"`) {
		t.Fatalf("a failed punch must say what to do and keep the action available: %s", failed)
	}
	busy := renderTime(t, ClockPage(view, ClockProjection{State: ClockProjectionReady, ClockIn: in, Busy: true}))
	if !strings.Contains(busy, "Recording your punch") || !strings.Contains(busy, `aria-busy="true"`) || strings.Contains(busy, `href="/workspace/app/time/clock/in"`) {
		t.Fatalf("busy state must announce itself and withdraw the action: %s", busy)
	}
	done := renderTime(t, ClockPage(view, ClockProjection{State: ClockProjectionReady, ClockIn: in, ReceiptTraceLabel: "R-1"}))
	if !strings.Contains(done, "Punch recorded") || !strings.Contains(done, "Confirmation code: R-1") {
		t.Fatalf("a recorded punch must be confirmed with its code: %s", done)
	}
}

func TestClockPageSideLinksStayInsideTheTimeArea(t *testing.T) {
	view := clockAllowedView()
	markup := renderTime(t, ClockPage(view, ClockProjection{State: ClockProjectionReady,
		FixPunch: &ActionLinkProps{Label: "Fix a missing punch", Href: "/workspace/app/time/missing-punch"},
		Timecard: &ActionLinkProps{Label: "View my timecard", Href: "/workspace/app/admin"}}))
	if !strings.Contains(markup, "Fix a missing punch") || strings.Contains(markup, "View my timecard") {
		t.Fatalf("side links must be admitted by prefix: %s", markup)
	}
}

func TestClockConfirmPanelNamesTheConsequence(t *testing.T) {
	markup := renderTime(t, clockConfirmPanel(ResolveProductLocale("en-US"), textNodeForTest("YES"), textNodeForTest("NO")))
	for _, want := range []string{"Clock out now?", "stops your work timer", `role="group"`, "YES", "NO"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("confirm panel missing %q: %s", want, markup)
		}
	}
}

func TestMissingPunchReviewSummarisesInPlainWords(t *testing.T) {
	view := testView(PageMissingPunch)
	proposed := time.Date(2026, 9, 27, 15, 30, 0, 0, time.Local)
	projection := MissingPunchAdminProjection{State: MissingPunchReady, Decider: &missingPunchDeciderSpy{}, Pending: []MissingPunchReviewView{
		{RequestID: "r1", WorkerLabel: "Jordan", Reason: "Phone died", ProposedOutAt: proposed, OriginalAt: proposed.Add(-8 * time.Hour), Revision: 1, IdempotencyKey: "k"},
		{RequestID: "r2", WorkerLabel: "Priya", PeriodClosed: true, Revision: 1, IdempotencyKey: "k2"},
	}}
	markup := renderTime(t, MissingPunchAdminPage(view, projection))
	for _, want := range []string{"asks to change the clock-out to", "<bdi>", "Reason given", "Phone died", "Approve correction", "Reject request", "Note to the worker (required)", "Pay period reopen reference"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("review missing %q: %s", want, markup)
		}
	}
	if strings.Count(markup, "Pay period reopen reference") != 1 {
		t.Fatalf("the reopen reference belongs only to a closed period: %s", markup)
	}
	empty := renderTime(t, MissingPunchAdminPage(view, MissingPunchAdminProjection{State: MissingPunchReady, Decider: &missingPunchDeciderSpy{}}))
	if !strings.Contains(empty, "No corrections are waiting for review") {
		t.Fatalf("empty review lacks direction: %s", empty)
	}
}

func TestKioskIdleShowsTheKeypadAtOnce(t *testing.T) {
	markup := renderTime(t, KioskPage(testView(PageClock), KioskProjection{Screen: KioskScreenIdle, Online: true, SiteLabel: "Riverside", OnIdentify: func(string) {}}))
	for _, want := range []string{`id="kiosk-credential"`, `inputmode="none"`, `role="group"`, "Delete last digit", "Clear", "Continue"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("idle kiosk missing %q: %s", want, markup)
		}
	}
	for digit := 0; digit <= 9; digit++ {
		if !strings.Contains(markup, ">"+string(rune('0'+digit))+"</button>") {
			t.Fatalf("keypad is missing digit %d", digit)
		}
	}
	if strings.Contains(markup, "admin") || strings.Contains(markup, "/workspace") || strings.Contains(markup, "href=") {
		t.Fatalf("kiosk leaked a route: %s", markup)
	}
}

func TestKioskConfirmOffersTheLogicalPunchFirst(t *testing.T) {
	view := testView(PageClock)
	for name, tc := range map[string]struct {
		phase        KioskPhase
		want, absent string
	}{
		"clocked in":  {KioskPhaseIn, "Clock out", "Clock in"},
		"clocked out": {KioskPhaseOut, "Clock in", "Clock out"},
	} {
		markup := renderTime(t, KioskPage(view, KioskProjection{Screen: KioskScreenConfirm, WorkerLabel: "Ava", StatusLabel: "Status", Phase: tc.phase, OnPunch: func(KioskPunchRequest) {}}))
		if !strings.Contains(markup, ">"+tc.want+"</button>") || strings.Contains(markup, ">"+tc.absent+"</button>") || !strings.Contains(markup, "Hello, Ava") {
			t.Fatalf("%s: wrong punch offered: %s", name, markup)
		}
	}
	unknown := renderTime(t, KioskPage(view, KioskProjection{Screen: KioskScreenConfirm, WorkerLabel: "Ava", OnPunch: func(KioskPunchRequest) {}}))
	if !strings.Contains(unknown, "Choose what you are doing") || !strings.Contains(unknown, ">Clock in</button>") || !strings.Contains(unknown, ">Clock out</button>") {
		t.Fatalf("an unknown state must ask and offer both: %s", unknown)
	}
	if !strings.Contains(unknown, "<details") || !strings.Contains(unknown, "Answer a question") {
		t.Fatalf("attestation, tips and job belong behind a disclosure: %s", unknown)
	}
}

func TestKioskReceiptShowsCountdownOnlyWhenTheHostResets(t *testing.T) {
	view := testView(PageClock)
	with := renderTime(t, KioskPage(view, KioskProjection{Screen: KioskScreenReceipt, ReceiptLabel: "Punch recorded", ReceiptSeq: 3, AutoResetSeconds: 10, OnDone: func() {}}))
	if !strings.Contains(with, "Back to the start screen in 10 seconds") || !strings.Contains(with, "--kiosk-return:10s") {
		t.Fatalf("countdown missing: %s", with)
	}
	without := renderTime(t, KioskPage(view, KioskProjection{Screen: KioskScreenReceipt, ReceiptLabel: "Punch recorded", ReceiptSeq: 3, OnDone: func() {}}))
	if strings.Contains(without, "Back to the start screen") {
		t.Fatalf("a countdown the host never honours is a lie: %s", without)
	}
}

func textNodeForTest(text string) ui.Node { return ui.Text(text) }

func TestKioskReceiptNamesWhoClockedWhatAndWhen(t *testing.T) {
	view := testView(PageClock)
	markup := renderTime(t, KioskPage(view, KioskProjection{Screen: KioskScreenReceipt, WorkerLabel: "Walt", ReceiptLabel: "Punch recorded", ReceiptSeq: 42, ReceiptEvent: KioskPunchClockOut, ReceiptTime: "3:34 PM", OnDone: func() {}}))
	for _, want := range []string{"you clocked out at", "3:34 PM", "Walt", "Punch number 42 on this clock"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("receipt missing %q: %s", want, markup)
		}
	}
	bare := renderTime(t, KioskPage(view, KioskProjection{Screen: KioskScreenReceipt, ReceiptLabel: "Punch recorded", ReceiptSeq: 42, OnDone: func() {}}))
	if strings.Contains(bare, "you clocked") {
		t.Fatalf("a receipt the device cannot fully name must not guess: %s", bare)
	}
}

func TestKioskOfflineSaysWhatStillWorks(t *testing.T) {
	markup := renderTime(t, KioskPage(testView(PageClock), KioskProjection{Screen: KioskScreenIdle, Online: false, OnIdentify: func(string) {}}))
	if !strings.Contains(markup, "scan your badge") || !strings.Contains(markup, "sent later") || !strings.Contains(markup, `placeholder="Enter PIN"`) {
		t.Fatalf("offline kiosk must say PINs are off, badges work and punches are kept: %s", markup)
	}
}

func TestTimeReviewAlwaysShowsTheApproveControl(t *testing.T) {
	markup := renderTime(t, TimeReviewPage(testView(PageClock), TimeReviewProjection{State: TimeSurfaceReady, PeriodLabel: "P", Approver: &approverSpy{}, Ready: []TimeReviewRow{{ID: "r1", Worker: "Walt", HoursLabel: "40 h"}}}))
	if !strings.Contains(markup, "Approve selected timecards") || !strings.Contains(markup, "disabled") {
		t.Fatalf("with nothing selected the approve action must stay visible but off: %s", markup)
	}
}
