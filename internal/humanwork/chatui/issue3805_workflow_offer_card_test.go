package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func issue3805Render(t *testing.T, locale, user string, c chatui.AgentUXAmbientCard) string {
	t.Helper()
	catalog := productui.ResolveProductLocale(locale)
	m := chatui.Model{Locale: catalog.Resolved, CurrentUser: user, Text: func(key string) string { return catalog.Text(key) }}
	node := chatui.RenderAgentUXAmbientCard(m, c)
	if node == nil {
		return ""
	}
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func issue3805Card(state string) chatui.AgentUXAmbientCard {
	return chatui.AgentUXAmbientCard{ID: "offer-1", AgentID: "workflow-offer", AgentName: "Workflow Offer", Conversation: "general", Source: "post-1", Kind: "WORKFLOW_START", Scope: "PRIVATE", Person: "person", Reason: "workflow_match", Title: "Change of bank details", State: state, Revision: 3, CanManage: true}
}

func TestIssue3805_WorkflowOfferCard_OffersThreeAnswersAndNamesTheWorkflow(t *testing.T) {
	want := map[string][3]string{
		"en-US": {"Start a “Change of bank details” request?", "Start request", "Not now"},
		"de-DE": {"Antrag „Change of bank details“ starten?", "Antrag starten", "Jetzt nicht"},
		"ar":    {"هل تريد بدء طلب «Change of bank details»؟", "ابدأ الطلب", "ليس الآن"},
	}
	for locale, copy := range want {
		markup := issue3805Render(t, locale, "person", issue3805Card("OFFERED"))
		for _, text := range copy {
			if !strings.Contains(markup, text) {
				t.Fatalf("%s card lacks %q: %s", locale, text, markup)
			}
		}
		for _, action := range []string{"OPEN", "DISMISS", "NEVER"} {
			if !strings.Contains(markup, `data-ambient-action="`+action+`"`) {
				t.Fatalf("%s card lacks the %s answer: %s", locale, action, markup)
			}
		}
		if !strings.Contains(markup, `data-ambient-revision="3"`) || !strings.Contains(markup, `aria-labelledby="offer-1-heading"`) || !strings.Contains(markup, `id="offer-1-heading"`) {
			t.Fatalf("%s card is not labelled or revision-bound: %s", locale, markup)
		}
		if strings.Contains(markup, "⟦") || strings.Contains(markup, "issue3805.") || locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Fatalf("%s card has missing copy or direction: %s", locale, markup)
		}
		if !strings.Contains(markup, "agentux-workflow-offer") || strings.Contains(markup, `data-ambient-action="ADD"`) || strings.Contains(markup, `data-ambient-action="SET"`) {
			t.Fatalf("%s card is not a workflow offer: %s", locale, markup)
		}
	}
}

func TestIssue3805_WorkflowOfferCard_IsPrivateAndHidesTheMessage_Security(t *testing.T) {
	if markup := issue3805Render(t, "en-US", "someone-else", issue3805Card("OFFERED")); markup != "" {
		t.Fatalf("another viewer was drawn the author's offer: %s", markup)
	}
	card := issue3805Card("OFFERED")
	card.Reason, card.SourceHref = "workflow_match", ""
	markup := issue3805Render(t, "en-US", "person", card)
	for _, leak := range []string{"payroll", "I need to", "bank account number"} {
		if strings.Contains(markup, leak) {
			t.Fatalf("offer repeated message text %q: %s", leak, markup)
		}
	}
	if !strings.Contains(markup, "Nothing is started or sent until you review and submit the request yourself") {
		t.Fatalf("offer does not say it submits nothing: %s", markup)
	}
	if markup := issue3805Render(t, "en-US", "person", issue3805Card("DISMISSED")); markup != "" {
		t.Fatalf("dismissed offer drawn: %s", markup)
	}
}

func TestIssue3805_WorkflowOfferCard_AcceptedLinksToTheStartPageWithoutSubmitting(t *testing.T) {
	card := issue3805Card("ACCEPTED")
	card.StartHref = "/workspace/app/workflow-start/start/bank-details"
	for locale, want := range map[string]string{"en-US": "Open the request form", "de-DE": "Antragsformular öffnen", "ar": "افتح نموذج الطلب"} {
		markup := issue3805Render(t, locale, "person", card)
		if !strings.Contains(markup, `href="/workspace/app/workflow-start/start/bank-details"`) || !strings.Contains(markup, want) {
			t.Fatalf("%s accepted offer has no link: %s", locale, markup)
		}
		if strings.Contains(markup, `data-ambient-action="OPEN"`) || strings.Contains(markup, `type="submit"`) {
			t.Fatalf("%s accepted offer can be accepted or submitted again: %s", locale, markup)
		}
	}
	markup := issue3805Render(t, "en-US", "person", card)
	if !strings.Contains(markup, "Nothing has been submitted") {
		t.Fatalf("accepted offer implies a submission: %s", markup)
	}
	card.StartHref = "https://evil.example/start"
	if markup := issue3805Render(t, "en-US", "person", card); strings.Contains(markup, "evil.example") {
		t.Fatalf("off-site link drawn: %s", markup)
	}
}

func TestIssue3805_WorkflowOfferCard_UnavailableAndWithdrawnExplainThemselves(t *testing.T) {
	unavailable := issue3805Render(t, "en-US", "person", issue3805Card("UNAVAILABLE"))
	if !strings.Contains(unavailable, "not available to you here any more. Nothing was started") || strings.Contains(unavailable, `data-ambient-action="OPEN"`) || !strings.Contains(unavailable, `data-ambient-action="DISMISS"`) {
		t.Fatalf("unavailable offer: %s", unavailable)
	}
	for _, state := range []string{"CANCELLED", "SOURCE_UNAVAILABLE"} {
		card := issue3805Card(state)
		card.Title = ""
		markup := issue3805Render(t, "en-US", "person", card)
		if !strings.Contains(markup, "was deleted or changed, so the offer was withdrawn") || strings.Contains(markup, `data-ambient-action="OPEN"`) || strings.Contains(markup, "Change of bank details") {
			t.Fatalf("%s offer: %s", state, markup)
		}
	}
	busy := issue3805Card("OFFERED")
	busy.Busy, busy.Error = true, true
	markup := issue3805Render(t, "en-US", "person", busy)
	if !strings.Contains(markup, `role="alert"`) || !strings.Contains(markup, `role="status"`) || strings.Count(markup, "disabled") < 3 {
		t.Fatalf("busy offer keeps its buttons live or hides its error: %s", markup)
	}
	noManage := issue3805Card("OFFERED")
	noManage.CanManage = false
	if markup := issue3805Render(t, "en-US", "person", noManage); strings.Count(markup, "disabled") < 3 {
		t.Fatalf("offer is answerable without permission: %s", markup)
	}
}
