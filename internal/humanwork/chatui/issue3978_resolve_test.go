package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func issue3978Render(t *testing.T, locale string, p JourneyPreview) string {
	t.Helper()
	p.IntentID, p.Readable, p.State, p.Worker = "j1", true, "ready", "Andre"
	m := Model{Locale: locale, EmbedOrigin: "https://hcm.example", JourneyPreviews: map[string]JourneyPreview{"j1": p}}
	markup, err := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, "https://hcm.example/workspace/app/journeys?journey=j1")...))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func issue3978Blocked() JourneyPreview {
	return JourneyPreview{
		Stage: "JOURNEY_STAGE_BLOCKED", StageTone: "blocked", RequestedByViewer: true,
		Revision: "rev-7f3a91c2", NextStep: "JOURNEY_NEXT_STEP_CORRECT_PROPOSAL",
		ResolveFindings: EncodeJourneyResolveFindings([]JourneyResolveFinding{
			{Severity: "BLOCKING", Code: "promotion.pay_below_band_minimum"},
			{Severity: "BLOCKING", Code: "promotion.budget_authority_missing"},
			{Severity: "ADVISORY", Code: "promotion.budget_authority_observation_only"},
		}),
	}
}

func TestIssue3978_BlockedCardSeparatesStoppingFromInformationalFindings(t *testing.T) {
	markup := issue3978Render(t, "en-US", issue3978Blocked())
	for _, want := range []string{
		"Help me resolve this", "Stopping this request (2)", "For information (1)",
		"The proposed pay is outside the pay range for the new job and grade.",
		"You can correct this on the request, then submit it again.",
		"Budget could not be confirmed for this change.", "Ask Finance; you cannot change this yourself.",
		"Budget was only looked at, not set aside. Nothing is held yet.",
		"Revision rev-7f3a91c2", "Open the request for Andre", "Nothing is changed or sent from here.",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("resolve panel missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "promotion.pay_below_band_minimum") {
		t.Errorf("internal finding codes must not reach the reader:\n%s", markup)
	}
	// A disclosure inside the card's own link would nest interactive content.
	if a, d := strings.Index(markup, "</a>"), strings.Index(markup, "<details"); a < 0 || d < a {
		t.Errorf("the panel must sit beside the card link, not inside it (a=%d d=%d)", a, d)
	}
	if !strings.Contains(markup, `href="/workspace/app/journeys?journey=j1"`) {
		t.Errorf("no link to the request:\n%s", markup)
	}
}

func TestIssue3978_OfferedOnlyToTheRequesterOfABlockedRequest(t *testing.T) {
	other := issue3978Blocked()
	other.RequestedByViewer = false
	if markup := issue3978Render(t, "en-US", other); strings.Contains(markup, "Help me resolve this") {
		t.Errorf("a viewer who did not start the request must not get the panel:\n%s", markup)
	}
	awaiting := issue3978Blocked()
	awaiting.Stage, awaiting.StageTone = "JOURNEY_STAGE_AWAITING_APPROVAL", "active"
	if markup := issue3978Render(t, "en-US", awaiting); strings.Contains(markup, "Help me resolve this") {
		t.Errorf("a request that is not blocked must not offer the panel:\n%s", markup)
	}
}

func TestIssue3978_UnreadableChecksSayTheyAreUnavailableNotInvented(t *testing.T) {
	p := issue3978Blocked()
	p.ResolveFindings = ""
	markup := issue3978Render(t, "en-US", p)
	if !strings.Contains(markup, "The checks for this request could not be read right now.") {
		t.Errorf("missing honest unavailable state:\n%s", markup)
	}
	if strings.Contains(markup, "Stopping this request") || strings.Contains(markup, "outside the pay range") {
		t.Errorf("must not invent findings:\n%s", markup)
	}
	// Informational notes alone cannot explain a blocked request.
	p.ResolveFindings = EncodeJourneyResolveFindings([]JourneyResolveFinding{{Severity: "ADVISORY", Code: "promotion.budget_authority_observation_only"}})
	if markup = issue3978Render(t, "en-US", p); !strings.Contains(markup, "could not be read right now") || !strings.Contains(markup, "For information (1)") {
		t.Errorf("only informational notes read: want unavailable plus the note:\n%s", markup)
	}
}

func TestIssue3978_RecheckingMarksResultsPossiblyStale(t *testing.T) {
	p := issue3978Blocked()
	p.Stage, p.StageTone = "JOURNEY_STAGE_REVALIDATION", "active"
	markup := issue3978Render(t, "en-US", p)
	if !strings.Contains(markup, "This request is being rechecked. These results may be out of date.") {
		t.Errorf("missing stale notice:\n%s", markup)
	}
}

func TestIssue3978_DeniedFindingIsWithheld_Security(t *testing.T) {
	p := issue3978Blocked()
	p.ResolveFindings = EncodeJourneyResolveFindings([]JourneyResolveFinding{{Severity: "DENIED", Code: "promotion.subject_not_disclosable"}})
	markup := issue3978Render(t, "en-US", p)
	if !strings.Contains(markup, "Details of one check are not available to you.") ||
		!strings.Contains(markup, "Ask the person who owns this request or your HR administrator.") {
		t.Errorf("denied finding needs the non-disclosing wording:\n%s", markup)
	}
	for _, leak := range []string{"subject_not_disclosable", "not disclosable", "not active"} {
		if strings.Contains(markup, leak) {
			t.Errorf("denied finding leaked %q:\n%s", leak, markup)
		}
	}
}

func TestIssue3978_UnknownCodeFallsBackWithoutEngineProse(t *testing.T) {
	p := issue3978Blocked()
	p.ResolveFindings = EncodeJourneyResolveFindings([]JourneyResolveFinding{{Severity: "BLOCKING", Code: "future.new_rule"}})
	markup := issue3978Render(t, "en-US", p)
	if !strings.Contains(markup, "A check on this request did not pass.") || !strings.Contains(markup, "Open the request to see more.") {
		t.Errorf("unknown code fallback missing:\n%s", markup)
	}
}

func TestIssue3978_PositionAndManagerChecksHavePlainTopics(t *testing.T) {
	p := issue3978Blocked()
	p.ResolveFindings = EncodeJourneyResolveFindings([]JourneyResolveFinding{
		{Severity: "BLOCKING", Code: "promotion.target_position_reservation_conflict"},
		{Severity: "BLOCKING", Code: "promotion.manager_relationship_cycle"},
		{Severity: "BLOCKING", Code: "promotion.target_manager_not_found"},
	})
	markup := issue3978Render(t, "en-US", p)
	for _, want := range []string{"already set aside for that date", "report to themselves", "selected manager cannot be used", "Ask your HR administrator"} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "A check on this request did not pass.") {
		t.Errorf("known position and manager codes must not fall back:\n%s", markup)
	}
	for code := range resolveTopics {
		for i, title := range resolveTopics[code].title {
			if strings.TrimSpace(title) == "" {
				t.Errorf("topic %s has no title for locale %d", code, i)
			}
		}
	}
}

func TestIssue3978_LocalizedGermanAndArabic(t *testing.T) {
	de := issue3978Render(t, "de-DE", issue3978Blocked())
	for _, want := range []string{"Hilfe bei der Lösung", "Revision rev-7f3a91c2"} {
		if !strings.Contains(de, want) {
			t.Errorf("de-DE missing %q:\n%s", want, de)
		}
	}
	if strings.Contains(de, "Help me resolve") || strings.Contains(de, "Stopping this request") {
		t.Errorf("de-DE shows English:\n%s", de)
	}
	ar := issue3978Render(t, "ar", issue3978Blocked())
	if strings.Contains(ar, "Help me resolve") || strings.Contains(ar, "Stopping this request") || strings.Contains(ar, "Ask Finance") {
		t.Errorf("ar shows English:\n%s", ar)
	}
	if !strings.Contains(ar, "ساعدني في حل هذا") {
		t.Errorf("ar missing summary:\n%s", ar)
	}
}

func TestIssue3978_FindingsRoundTripAndSeverityVocabulary(t *testing.T) {
	in := []JourneyResolveFinding{{Severity: "BLOCKING", Code: "a.b"}, {Severity: "NEEDS_DATA", Code: "c.d"}}
	out := DecodeJourneyResolveFindings(EncodeJourneyResolveFindings(in))
	if len(out) != 2 || out[0] != in[0] || out[1] != in[1] {
		t.Fatalf("round trip = %+v", out)
	}
	for in, want := range map[string]string{"BLOCKING": "blocking", "BLOCKED": "blocking", "NEEDS_DATA": "needs-data", "DENIED": "denied", "ADVISORY": "info", "": "info", "READY": "info"} {
		if got := JourneyResolveSeverity(in); got != want {
			t.Errorf("severity(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIssue3978_NeedsDataCountsAsStopping(t *testing.T) {
	p := issue3978Blocked()
	p.ResolveFindings = EncodeJourneyResolveFindings([]JourneyResolveFinding{{Severity: "NEEDS_DATA", Code: "promotion.required_field_unavailable"}})
	markup := issue3978Render(t, "en-US", p)
	if !strings.Contains(markup, "Stopping this request (1)") || !strings.Contains(markup, "Some information needed for the checks could not be read.") {
		t.Errorf("needs-data finding not shown as stopping:\n%s", markup)
	}
}
