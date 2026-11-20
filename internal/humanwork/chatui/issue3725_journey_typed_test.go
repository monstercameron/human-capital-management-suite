package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func typedJourneyRef(id string) ChatReference {
	return ChatReference{Kind: ChatReferenceJourney, TenantID: "t1", ID: id, Display: "Promotion request"}
}

func readyJourneyModel() Model {
	return Model{EmbedOrigin: "https://hcm.example", Locale: "en-US", JourneyPreviews: map[string]JourneyPreview{
		"j1": {IntentID: "j1", Readable: true, State: "ready", Worker: "Andre", From: "SEC-ENG · P4", To: "SEC-DIR · M4", Stage: "JOURNEY_STAGE_MANAGER_APPROVAL", StageTone: "active"},
	}}
}

// The card comes from the typed reference: an author who edits the address out
// of the message keeps the card, and it reads the journey live.
func TestIssue3725_TypedJourneyReferenceNeedsNoUrlInTheBody(t *testing.T) {
	m := readyJourneyModel()
	m.renderWorkReferences = []ChatReference{typedJourneyRef("j1")}
	card, err := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, "please approve")...))
	if err != nil || !strings.Contains(card, "Andre") || !strings.Contains(card, `href="/workspace/app/journeys?journey=j1"`) {
		t.Fatalf("card missing without a URL (err=%v):\n%s", err, card)
	}
	if plain, _ := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, "please approve")...)); !strings.Contains(plain, "Andre") {
		t.Fatalf("plain render lost the card")
	}
	m.renderWorkReferences = nil
	if nodes := journeyPreviewEmbeds(m, "please approve"); len(nodes) != 0 {
		t.Fatal("without a typed reference or an address there is nothing to draw")
	}
}

// A hand-pasted address still renders, and an address that names the journey
// the typed reference vouches for unfurls even from another origin. The card
// for the two is drawn once.
func TestIssue3725_TypedJourneyReferenceVouchesForTheAddressAndDrawsOneCard(t *testing.T) {
	m := readyJourneyModel()
	body := "look\nhttps://old-host.example/workspace/app/journeys?journey=j1"
	if got := JourneyReferences(body, m.EmbedOrigin); len(got) != 0 {
		t.Fatalf("precondition: foreign origin matched with no typed reference: %#v", got)
	}
	m.renderWorkReferences = []ChatReference{typedJourneyRef("j1")}
	refs := JourneyReferencesFor(body, m.EmbedOrigin, m.renderWorkReferences)
	if len(refs) != 1 || refs[0].IntentID != "j1" || refs[0].Start < 0 {
		t.Fatalf("refs = %#v", refs)
	}
	cards, _ := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, body)...))
	if strings.Count(cards, "chat-journey-embed") < 1 || strings.Count(cards, `data-action="open-journey-reference"`) != 1 {
		t.Fatalf("want one card:\n%s", cards)
	}
	inline, _ := ui.RenderToString(ui.Fragment(journeyReferenceBody(m, body)...))
	if strings.Contains(inline, "old-host.example") || !strings.Contains(inline, "Andre") {
		t.Fatalf("raw address leaked into the body:\n%s", inline)
	}
}

// A typed reference that is not in the body is not drawn inline as text.
func TestIssue3725_TypedOnlyJourneyReferenceAddsNoInlineText(t *testing.T) {
	m := readyJourneyModel()
	m.renderWorkReferences = []ChatReference{typedJourneyRef("j1")}
	inline, _ := ui.RenderToString(ui.Fragment(journeyReferenceBody(m, "please approve")...))
	if strings.Contains(inline, "Andre") || !strings.Contains(inline, "please approve") {
		t.Fatalf("inline body = %s", inline)
	}
}

// A reference the reader may not open arrives with no id: it names nothing and
// draws nothing, and a malformed id is ignored.
func TestIssue3725_RestrictedOrMalformedTypedJourneyReferenceDrawsNothing_Security(t *testing.T) {
	m := readyJourneyModel()
	m.renderWorkReferences = []ChatReference{{Kind: ChatReferenceJourney, Display: "Restricted workflow request"}, typedJourneyRef("../x"), {Kind: ChatReferenceProjectTask, ID: "j1"}}
	if refs := JourneyReferencesFor("hello", m.EmbedOrigin, m.renderWorkReferences); len(refs) != 0 {
		t.Fatalf("refs = %#v", refs)
	}
	if nodes := journeyPreviewEmbeds(m, "hello"); len(nodes) != 0 {
		t.Fatal("an id-less reference drew a card")
	}
}

// Until the viewer's own read of the journey answers, a typed reference draws
// the loading card, never the stored name.
func TestIssue3725_TypedJourneyReferenceShowsNoStoredNameBeforeTheLiveReadAnswers(t *testing.T) {
	m := Model{EmbedOrigin: "https://hcm.example", Locale: "en-US"}
	m.renderWorkReferences = []ChatReference{typedJourneyRef("j1")}
	card, _ := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, "x")...))
	if !strings.Contains(card, `data-embed-state="loading"`) || strings.Contains(card, "Promotion request") {
		t.Fatalf("loading card = %s", card)
	}
}
