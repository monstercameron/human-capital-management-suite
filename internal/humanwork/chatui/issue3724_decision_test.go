package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func issue3724Preview() JourneyPreview {
	return JourneyPreview{
		IntentID: "j1", Readable: true, State: "ready", Worker: "Andre Silva", From: "SAL-AE3 · P4", To: "SAL-DIR · M4",
		Stage: "JOURNEY_STAGE_FINANCE_APPROVAL", StageTone: "active", Effective: "2026-12-01",
		Approver: "Thomas Baker", WaitsOnViewer: true, Revision: "rev-7f3a91c2",
	}
}

type issue3724Calls struct {
	decided []string
	toggled []string
	drafts  []string
}

func issue3724Verb(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

func issue3724Model(locale string, p JourneyPreview, state JourneyDecisionState, calls *issue3724Calls) Model {
	m := Model{Locale: locale, EmbedOrigin: "https://hcm.example", JourneyPreviews: map[string]JourneyPreview{"j1": p}}
	if state != (JourneyDecisionState{}) {
		m.JourneyDecisions = map[string]JourneyDecisionState{"j1": state}
	}
	if calls != nil {
		m.Callbacks.DecideJourney = func(id string, approve bool) {
			calls.decided = append(calls.decided, id+":"+issue3724Verb(approve, "approve", "reject"))
		}
		m.Callbacks.ToggleJourneyDecision = func(id string, open bool) {
			calls.toggled = append(calls.toggled, id+":"+issue3724Verb(open, "open", "close"))
		}
		m.Callbacks.SetJourneyDecisionDraft = func(id, text string) { calls.drafts = append(calls.drafts, id+":"+text) }
	}
	return m
}

func issue3724Markup(t *testing.T, m Model) string {
	t.Helper()
	out, err := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, "https://hcm.example/workspace/app/journeys?journey=j1")...))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestIssue3724_RoutedApproverGetsAReviewEntryThatSitsBesideTheCardLink(t *testing.T) {
	markup := issue3724Markup(t, issue3724Model("en-US", issue3724Preview(), JourneyDecisionState{}, &issue3724Calls{}))
	for _, want := range []string{`data-action="journey-decision-toggle"`, "Review and decide", `aria-expanded="false"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("closed card missing %q:\n%s", want, markup)
		}
	}
	// Nothing can be decided from a closed card.
	if strings.Contains(markup, `data-action="journey-decide-approve"`) {
		t.Errorf("a closed card offers Approve:\n%s", markup)
	}
	if a, b := strings.Index(markup, "</a>"), strings.Index(markup, "journey-decision-toggle"); a < 0 || b < a {
		t.Errorf("the control must sit beside the card link, not inside it (a=%d b=%d)", a, b)
	}
}

func TestIssue3724_OpenReviewShowsTheExactProposalBeforeAnyDecision(t *testing.T) {
	m := issue3724Model("en-US", issue3724Preview(), JourneyDecisionState{Open: true}, &issue3724Calls{})
	markup := issue3724Markup(t, m)
	for _, want := range []string{
		"Review before you decide", "Andre Silva", "SAL-AE3 · P4", "SAL-DIR · M4", "Effective", "Thomas Baker",
		"Revision rev-7f3a91c2", "Your decision covers this approval step only",
		"is refused", // a changed proposal is refused rather than approved
		`data-action="journey-decide-approve"`, `data-action="journey-decide-reject"`,
		`aria-expanded="true"`, "Reason", "Open the request for Andre Silva",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("open review missing %q:\n%s", want, markup)
		}
	}
}

func TestIssue3724_OtherViewersGetAReasonNotButtons_Security(t *testing.T) {
	p := issue3724Preview()
	p.WaitsOnViewer = false
	markup := issue3724Markup(t, issue3724Model("en-US", p, JourneyDecisionState{Open: true}, &issue3724Calls{}))
	for _, want := range []string{"Waiting on Thomas Baker", "Only the routed approver can decide"} {
		if !strings.Contains(markup, want) {
			t.Errorf("non-approver card missing %q:\n%s", want, markup)
		}
	}
	for _, bad := range []string{"journey-decide-approve", "journey-decide-reject", "journey-decision-toggle", "journey-decision-reason"} {
		if strings.Contains(markup, bad) {
			t.Errorf("a viewer who is not the routed approver sees %q:\n%s", bad, markup)
		}
	}
	// A finished request waits on nobody.
	p.Stage, p.StageTone = "JOURNEY_STAGE_COMPLETED", "done"
	if markup := issue3724Markup(t, issue3724Model("en-US", p, JourneyDecisionState{}, &issue3724Calls{})); strings.Contains(markup, "Only the routed approver") {
		t.Errorf("a completed request claims to wait on its approver:\n%s", markup)
	}
}

func TestIssue3724_NoRevisionMeansNoDecision_Security(t *testing.T) {
	p := issue3724Preview()
	p.Revision = ""
	m := issue3724Model("en-US", p, JourneyDecisionState{Open: true}, &issue3724Calls{})
	markup := issue3724Markup(t, m)
	if !strings.Contains(markup, "could not be confirmed") {
		t.Errorf("no explanation for a missing revision:\n%s", markup)
	}
	for _, action := range []string{"journey-decide-approve", "journey-decide-reject"} {
		i := strings.Index(markup, `data-action="`+action+`"`)
		if i >= 0 && !strings.Contains(markup[max(0, i-250):min(len(markup), i+250)], `aria-disabled="true"`) {
			t.Errorf("%s is live without a reviewed revision:\n%s", action, markup)
		}
	}
	calls := &issue3724Calls{}
	m = issue3724Model("en-US", p, JourneyDecisionState{Open: true}, calls)
	journeyDecisionAct(m, "journey-decide-approve", "j1")
	if len(calls.decided) != 0 {
		t.Errorf("a decision was sent without a reviewed revision: %+v", calls)
	}
}

func TestIssue3724_DraftSurvivesAnErrorAndErrorsAreAnnounced(t *testing.T) {
	for _, tc := range []struct{ state, want string }{
		{"reason", "Give a reason to reject"},
		{"stale", "changed after you opened it"},
		{"denied", "not allowed to decide"},
		{"unavailable", "could not be recorded"},
	} {
		m := issue3724Model("en-US", issue3724Preview(), JourneyDecisionState{Open: true, Reason: "Band exceeds budget", Error: tc.state}, &issue3724Calls{})
		markup := issue3724Markup(t, m)
		if !strings.Contains(markup, tc.want) {
			t.Errorf("%s: missing %q:\n%s", tc.state, tc.want, markup)
		}
		if !strings.Contains(markup, `role="alert"`) {
			t.Errorf("%s: error is not announced:\n%s", tc.state, markup)
		}
		if !strings.Contains(markup, `data-chat-value="Band exceeds budget"`) {
			t.Errorf("%s: the reason draft was lost:\n%s", tc.state, markup)
		}
	}
}

func TestIssue3724_ReceiptSurvivesTheCardMovingOn(t *testing.T) {
	p := issue3724Preview()
	p.WaitsOnViewer, p.Stage = false, "JOURNEY_STAGE_MANAGER_APPROVAL"
	markup := issue3724Markup(t, issue3724Model("en-US", p, JourneyDecisionState{Open: true, Receipt: "approved"}, &issue3724Calls{}))
	if !strings.Contains(markup, "You approved this step") || !strings.Contains(markup, `role="status"`) {
		t.Errorf("no announced receipt after the decision:\n%s", markup)
	}
	if strings.Contains(markup, `data-action="journey-decide-approve"`) {
		t.Errorf("a decided step offers Approve again:\n%s", markup)
	}
	rejected := issue3724Markup(t, issue3724Model("en-US", p, JourneyDecisionState{Open: true, Receipt: "rejected"}, &issue3724Calls{}))
	if !strings.Contains(rejected, "You rejected this step") {
		t.Errorf("no rejection receipt:\n%s", rejected)
	}
}

func TestIssue3724_BusyDecisionCannotBeSentTwice(t *testing.T) {
	markup := issue3724Markup(t, issue3724Model("en-US", issue3724Preview(), JourneyDecisionState{Open: true, Busy: "approve"}, &issue3724Calls{}))
	if !strings.Contains(markup, `aria-busy="true"`) || strings.Count(markup, `aria-disabled="true"`) < 2 {
		t.Errorf("a decision in flight leaves live buttons:\n%s", markup)
	}
}

func TestIssue3724_ActionsCallTheCallbacksOnlyForTheRoutedApprover_Security(t *testing.T) {
	calls := &issue3724Calls{}
	m := issue3724Model("en-US", issue3724Preview(), JourneyDecisionState{Open: true, Reason: "ok"}, calls)
	if !journeyDecisionAct(m, "journey-decision-toggle", "j1") || !journeyDecisionAct(m, "journey-decide-approve", "j1") || !journeyDecisionAct(m, "journey-decide-reject", "j1") {
		t.Fatal("actions were not handled")
	}
	if got := strings.Join(calls.toggled, ","); got != "j1:close" {
		t.Errorf("toggle = %q, want j1:close", got)
	}
	if got := strings.Join(calls.decided, ","); got != "j1:approve,j1:reject" {
		t.Errorf("decided = %q", got)
	}

	// Not the routed approver: the same actions reach nothing.
	p := issue3724Preview()
	p.WaitsOnViewer = false
	calls = &issue3724Calls{}
	m = issue3724Model("en-US", p, JourneyDecisionState{Open: true}, calls)
	for _, action := range []string{"journey-decision-toggle", "journey-decide-approve", "journey-decide-reject"} {
		journeyDecisionAct(m, action, "j1")
	}
	if len(calls.decided)+len(calls.toggled) != 0 {
		t.Errorf("a viewer who is not the routed approver reached a callback: %+v", calls)
	}
	// A decision already in flight is not sent again.
	calls = &issue3724Calls{}
	m = issue3724Model("en-US", issue3724Preview(), JourneyDecisionState{Open: true, Busy: "approve"}, calls)
	journeyDecisionAct(m, "journey-decide-approve", "j1")
	if len(calls.decided) != 0 {
		t.Errorf("a second decision was sent while one was in flight: %+v", calls)
	}
}

func TestIssue3724_CopyIsLocalised(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		m := issue3724Model(locale, issue3724Preview(), JourneyDecisionState{Open: true, Error: "stale"}, &issue3724Calls{})
		markup := issue3724Markup(t, m)
		for _, english := range []string{"Review before you decide", ">Approve<", ">Reject<", "changed after you opened it", "Your decision covers"} {
			if strings.Contains(markup, english) {
				t.Errorf("%s still shows English %q:\n%s", locale, english, markup)
			}
		}
	}
	p := issue3724Preview()
	p.WaitsOnViewer = false
	for _, locale := range []string{"de-DE", "ar"} {
		if markup := issue3724Markup(t, issue3724Model(locale, p, JourneyDecisionState{}, &issue3724Calls{})); strings.Contains(markup, "Only the routed approver") {
			t.Errorf("%s still shows English for the refusal:\n%s", locale, markup)
		}
	}
}

func TestIssue3724_ComposerPreviewOffersNoDecision_Security(t *testing.T) {
	m := issue3724Model("en-US", issue3724Preview(), JourneyDecisionState{Open: true}, &issue3724Calls{})
	out, err := ui.RenderToString(ui.Fragment(journeyDraftEmbeds(m, "https://hcm.example/workspace/app/journeys?journey=j1")...))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "journey-decide-") || strings.Contains(out, "journey-decision-") {
		t.Errorf("an unsent draft offers a decision:\n%s", out)
	}
}
