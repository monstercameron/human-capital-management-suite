package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func issue3980Render(t *testing.T, locale string, p JourneyPreview) string {
	t.Helper()
	p.IntentID, p.Readable, p.State, p.Worker = "j1", true, "ready", "Andre"
	m := Model{Locale: locale, EmbedOrigin: "https://hcm.example", JourneyPreviews: map[string]JourneyPreview{"j1": p}}
	markup, err := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, "https://hcm.example/workspace/app/journeys?journey=j1")...))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func issue3980Preview(stage string) JourneyPreview {
	return JourneyPreview{Stage: stage, StageTone: JourneyStageGroup(stage), RequestedByViewer: true, Effective: "2026-12-01", Confirmed: "2026-11-20T09:30:00Z"}
}

func TestIssue3980_ScheduledChangeIsNotPresentedAsApplied(t *testing.T) {
	markup := issue3980Render(t, "en-US", issue3980Preview("JOURNEY_STAGE_WAITING_EFFECTIVE_DATE"))
	for _, want := range []string{
		"Help me stop or revise this request", "Scheduled, not applied yet",
		"Nothing has changed on the employee record yet.",
		"Last updated Nov 20", "Nothing is cancelled, changed or sent from here.",
		"A cancellation request is not a cancellation", "pending, accepted, too late, or in need of repair",
		"prepare a new proposal", "approved again", `href="/workspace/app/journeys?journey=j1"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("scheduled explainer missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "has already taken effect") {
		t.Errorf("a scheduled change must not read as applied:\n%s", markup)
	}
}

func TestIssue3980_AppliedChangeNeverClaimsCancellationUndoesIt(t *testing.T) {
	for _, stage := range []string{"JOURNEY_STAGE_EXECUTED", "JOURNEY_STAGE_OBSERVING_EFFECTS", "JOURNEY_STAGE_AWAITING_ACKNOWLEDGEMENT", "JOURNEY_STAGE_RECORDED", "JOURNEY_STAGE_COMPLETED"} {
		markup := issue3980Render(t, "en-US", issue3980Preview(stage))
		for _, want := range []string{"Already applied", "has already taken effect", "Cancelling cannot undo it", "permitted correction"} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s: missing %q:\n%s", stage, want, markup)
			}
		}
		for _, bad := range []string{"rolled back", "rollback", "Scheduled, not applied yet", "cancellation request is not a cancellation"} {
			if strings.Contains(markup, bad) {
				t.Errorf("%s: must not say %q:\n%s", stage, bad, markup)
			}
		}
	}
}

func TestIssue3980_RepairRequiredPointsToGovernedRepair(t *testing.T) {
	markup := issue3980Render(t, "en-US", issue3980Preview("JOURNEY_STAGE_REPAIR_REQUIRED"))
	for _, want := range []string{"Needs repair", "governed repair", "Cancelling does not fix it"} {
		if !strings.Contains(markup, want) {
			t.Errorf("repair explainer missing %q:\n%s", want, markup)
		}
	}
}

func TestIssue3980_UnknownStageAndMissingConfirmationAreSaidPlainly(t *testing.T) {
	p := issue3980Preview("JOURNEY_STAGE_UNSPECIFIED")
	p.Confirmed = ""
	markup := issue3980Render(t, "en-US", p)
	for _, want := range []string{"State unknown", "could not be confirmed", "When this was last updated was not reported"} {
		if !strings.Contains(markup, want) {
			t.Errorf("unknown explainer missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "Scheduled, not applied yet") || strings.Contains(markup, "Already applied") {
		t.Errorf("unknown must not guess:\n%s", markup)
	}
}

func TestIssue3980_EndedRequestAndPending(t *testing.T) {
	if m := issue3980Render(t, "en-US", issue3980Preview("JOURNEY_STAGE_REJECTED")); !strings.Contains(m, "Ended without being applied") || strings.Contains(m, "cancellation request is not") {
		t.Errorf("ended request:\n%s", m)
	}
	m := issue3980Render(t, "en-US", issue3980Preview("JOURNEY_STAGE_MANAGER_APPROVAL"))
	if !strings.Contains(m, "Not applied yet, still in review") {
		t.Errorf("pending request:\n%s", m)
	}
}

func TestIssue3980_FailedTerminalNeverClaimsNotApplied(t *testing.T) {
	markup := issue3980Render(t, "en-US", issue3980Preview("JOURNEY_STAGE_FAILED"))
	for _, want := range []string{"Ended, outcome not clear", "not clear whether any part"} {
		if !strings.Contains(markup, want) {
			t.Errorf("failed explainer missing %q:\n%s", want, markup)
		}
	}
	for _, bad := range []string{"was not applied", "Ended without being applied", "nothing left to stop"} {
		if strings.Contains(markup, bad) {
			t.Errorf("a failed terminal must not claim %q:\n%s", bad, markup)
		}
	}
}

func TestIssue3980_OfferedOnlyToTheRequester_Security(t *testing.T) {
	p := issue3980Preview("JOURNEY_STAGE_WAITING_EFFECTIVE_DATE")
	p.RequestedByViewer = false
	if markup := issue3980Render(t, "en-US", p); strings.Contains(markup, "Help me stop or revise") {
		t.Errorf("only the requester is offered this:\n%s", markup)
	}
	// A card that is locked never shows it.
	m := Model{JourneyPreviews: map[string]JourneyPreview{"j1": {IntentID: "j1", State: "restricted", RequestedByViewer: true, Stage: "JOURNEY_STAGE_WAITING_EFFECTIVE_DATE"}}, EmbedOrigin: "https://hcm.example"}
	markup, _ := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, "https://hcm.example/workspace/app/journeys?journey=j1")...))
	if strings.Contains(markup, "Help me stop or revise") {
		t.Errorf("locked card leaked the explainer:\n%s", markup)
	}
}

func TestIssue3980_ExplainerHoldsNoPersonalDetailsAndSitsBesideTheLink(t *testing.T) {
	p := issue3980Preview("JOURNEY_STAGE_WAITING_EFFECTIVE_DATE")
	p.ResolveFindings = EncodeJourneyResolveFindings([]JourneyResolveFinding{{Severity: "INFO", Code: "x"}})
	markup := issue3980Render(t, "en-US", p)
	if a, d := strings.Index(markup, "</a>"), strings.Index(markup, `data-journey-stop-revise`); a < 0 || d < a {
		t.Errorf("disclosure must sit beside the card link (a=%d d=%d):\n%s", a, d, markup)
	}
	if !strings.Contains(markup, "<details") || !strings.Contains(markup, "<summary") {
		t.Errorf("explainer must be a keyboard-operable disclosure:\n%s", markup)
	}
}

func TestIssue3980_GermanAndArabicCopy(t *testing.T) {
	for _, stage := range []string{"JOURNEY_STAGE_WAITING_EFFECTIVE_DATE", "JOURNEY_STAGE_COMPLETED", "JOURNEY_STAGE_REPAIR_REQUIRED", "JOURNEY_STAGE_UNSPECIFIED", "JOURNEY_STAGE_REJECTED", "JOURNEY_STAGE_AWAITING_APPROVAL"} {
		en := issue3980Render(t, "en-US", issue3980Preview(stage))
		de := issue3980Render(t, "de-DE", issue3980Preview(stage))
		ar := issue3980Render(t, "ar", issue3980Preview(stage))
		if !strings.Contains(de, "Hilfe: Antrag stoppen oder überarbeiten") || !strings.Contains(ar, "ساعدني في إيقاف هذا الطلب أو تعديله") {
			t.Errorf("%s: localized summary missing", stage)
		}
		if strings.Contains(de, "Help me stop") || strings.Contains(ar, "Help me stop") || strings.Contains(de, "Last updated") {
			t.Errorf("%s: English leaked into localized card", stage)
		}
		if en == de || en == ar {
			t.Errorf("%s: localized markup identical to English", stage)
		}
	}
	if de := issue3980Render(t, "de-DE", issue3980Preview("JOURNEY_STAGE_COMPLETED")); !strings.Contains(de, "Bereits umgesetzt") {
		t.Errorf("german applied label missing:\n%s", de)
	}
}
