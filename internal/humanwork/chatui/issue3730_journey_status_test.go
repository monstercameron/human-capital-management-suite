package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func issue3730Render(t *testing.T, locale string, p JourneyPreview) string {
	t.Helper()
	p.IntentID, p.Readable, p.State, p.Worker = "j1", true, "ready", "Andre"
	m := Model{Locale: locale, EmbedOrigin: "https://hcm.example", JourneyPreviews: map[string]JourneyPreview{"j1": p}}
	markup, err := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, "https://hcm.example/workspace/app/journeys?journey=j1")...))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestIssue3730_WaitingAndRequesterShown(t *testing.T) {
	markup := issue3730Render(t, "en-US", JourneyPreview{Stage: "JOURNEY_STAGE_MANAGER_APPROVAL", StageTone: "active", Approver: "Elena Park", RequestedByViewer: true, WaitingSince: "2026-11-05"})
	for _, want := range []string{"Requested by you", "Waiting since Nov 5"} {
		if !strings.Contains(markup, want) {
			t.Errorf("card missing %q:\n%s", want, markup)
		}
	}
}

func TestIssue3730_DecisionShowsDeciderAndReason(t *testing.T) {
	markup := issue3730Render(t, "en-US", JourneyPreview{Stage: "JOURNEY_STAGE_REJECTED", StageTone: "done", DecidedBy: "Elena Park", DecisionReason: "Over budget this quarter"})
	for _, want := range []string{"Decided by Elena Park", "Reason: Over budget this quarter"} {
		if !strings.Contains(markup, want) {
			t.Errorf("card missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "Waiting since") {
		t.Errorf("finished journey must not claim to be waiting:\n%s", markup)
	}
}

func TestIssue3730_WaitingHiddenWhenNotAwaitingApproval(t *testing.T) {
	markup := issue3730Render(t, "en-US", JourneyPreview{Stage: "JOURNEY_STAGE_WAITING_EFFECTIVE_DATE", StageTone: "active", WaitingSince: "2026-11-05"})
	if strings.Contains(markup, "Waiting since") {
		t.Errorf("unexpected waiting line:\n%s", markup)
	}
}

func TestIssue3730_LocalizedDeAndAr(t *testing.T) {
	p := JourneyPreview{Stage: "JOURNEY_STAGE_REJECTED", StageTone: "done", DecidedBy: "Elena Park", DecisionReason: "Budget", RequestedByViewer: true}
	de := issue3730Render(t, "de-DE", p)
	for _, want := range []string{"Entschieden von Elena Park", "Grund: Budget", "Von Ihnen beantragt"} {
		if !strings.Contains(de, want) {
			t.Errorf("de-DE missing %q:\n%s", want, de)
		}
	}
	ar := issue3730Render(t, "ar", p)
	if strings.Contains(ar, "Decided by") || strings.Contains(ar, "Reason:") || !strings.Contains(ar, "Elena Park") {
		t.Errorf("ar not localized:\n%s", ar)
	}
}

func TestIssue3730_FactsFromTimeline(t *testing.T) {
	at := func(d int) time.Time { return time.Date(2026, 11, d, 9, 0, 0, 0, time.UTC) }
	events := []JourneyStatusEvent{
		{At: at(1), Kind: "INTENT_CREATED", Actor: "system", Title: "Promotion proposed"},
		{At: at(2), Kind: "WORK_ITEM", Actor: "reviewer", Title: "Finance review assigned"},
		{At: at(4), Kind: "WORK_ITEM", Actor: "Elena Park", Title: "Finance review rejected", Detail: "decision_reason:  Over   budget  "},
		{At: at(5), Kind: "WORK_ITEM", Actor: "reviewer", Title: "Manager review assigned"},
	}
	waiting := JourneyStatusFacts("JOURNEY_STAGE_MANAGER_APPROVAL", events, true)
	if waiting.WaitingSince != "2026-11-05" || !waiting.RequestedByViewer || waiting.DecidedBy != "" {
		t.Fatalf("waiting facts = %+v", waiting)
	}
	done := JourneyStatusFacts("JOURNEY_STAGE_REJECTED", events[:3], false)
	if done.DecidedBy != "Elena Park" || done.DecisionReason != "Over budget" || done.WaitingSince != "" || done.RequestedByViewer {
		t.Fatalf("done facts = %+v", done)
	}
	approved := JourneyStatusFacts("JOURNEY_STAGE_COMPLETED", []JourneyStatusEvent{{At: at(4), Kind: "WORK_ITEM", Actor: "Elena Park", Title: "Manager review approved", Detail: "decision_reason:Fine"}}, false)
	if approved.DecidedBy != "Elena Park" || approved.DecisionReason != "Fine" {
		t.Fatalf("approved facts = %+v", approved)
	}
}

// A viewer the service redacts (actor "reviewer", empty detail) must learn no
// decider and no reason, and an unmarked detail is never shown as a reason.
func TestIssue3730_RedactedViewerLearnsNoDeciderOrReason_Security(t *testing.T) {
	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	redacted := JourneyStatusFacts("JOURNEY_STAGE_REJECTED", []JourneyStatusEvent{{At: at, Kind: "WORK_ITEM", Actor: "reviewer", Title: "Manager review rejected"}}, false)
	if redacted.DecidedBy != "" || redacted.DecisionReason != "" {
		t.Fatalf("redacted facts leaked: %+v", redacted)
	}
	raw := JourneyStatusFacts("JOURNEY_STAGE_REJECTED", []JourneyStatusEvent{{At: at, Kind: "WORK_ITEM", Actor: "Elena Park", Title: "Manager review rejected", Detail: "internal transition note"}}, false)
	if raw.DecisionReason != "" {
		t.Fatalf("unmarked detail shown as reason: %+v", raw)
	}
}

func TestIssue3730_ReasonIsCapped(t *testing.T) {
	long := strings.Repeat("x", 400)
	got := JourneyStatusFacts("JOURNEY_STAGE_REJECTED", []JourneyStatusEvent{{At: time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC), Kind: "WORK_ITEM", Actor: "Elena Park", Title: "Manager review rejected", Detail: "decision_reason:" + long}}, false)
	if n := len([]rune(got.DecisionReason)); n != journeyReasonLimit {
		t.Fatalf("reason length = %d, want %d", n, journeyReasonLimit)
	}
}
