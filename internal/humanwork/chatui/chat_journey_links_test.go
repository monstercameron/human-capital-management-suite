package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestJourneyReferencesAndCards(t *testing.T) {
	origin := "https://hcm.example"
	body := "Status: https://hcm.example/workspace/app/journeys?journey=01a0-abc&locale=en-US and https://evil.example/workspace/app/journeys?journey=x"
	refs := JourneyReferences(body, origin)
	if len(refs) != 1 || refs[0].IntentID != "01a0-abc" {
		t.Fatalf("refs = %#v", refs)
	}
	m := Model{EmbedOrigin: origin, JourneyPreviews: map[string]JourneyPreview{"01a0-abc": {IntentID: "01a0-abc", Readable: true, State: "ready", Worker: "Andre", From: "SEC-ENG · P4", To: "SEC-DIR · M4", Stage: "JOURNEY_STAGE_MANAGER_APPROVAL", StageTone: "active", Effective: "2026-11-01", Approver: "Elena Park"}}}
	markup, err := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, body)...))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Andre", "SEC-DIR · M4", "Manager approval", "Effective Nov 1", "Approver: Elena Park", `href="/workspace/app/journeys?journey=01a0-abc"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("card missing %q:\n%s", want, markup)
		}
	}
	m.JourneyPreviews["01a0-abc"] = JourneyPreview{IntentID: "01a0-abc", State: "restricted", Worker: "Secret"}
	locked, _ := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, body)...))
	if strings.Contains(locked, "Secret") || strings.Contains(locked, "href=") {
		t.Fatalf("restricted journey leaked:\n%s", locked)
	}
}
