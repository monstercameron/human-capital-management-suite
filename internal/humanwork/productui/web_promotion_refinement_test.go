package productui

import (
	"strings"
	"testing"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
)

func TestJourneysOverviewKeepsPromotionDiscoverableAndHistoryConnected(t *testing.T) {
	view := testView(PageJourneys)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`journeys-page`,
		">Promotion journeys<",
		"Promotion journeys",
		"Past workflows",
		"Jordan Lee",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("Journeys overview missing %q", want)
		}
	}
	if !strings.Contains(doc, `mode=new`) {
		t.Fatal("Journeys overview did not expose the authorized new-promotion route")
	}
	if strings.Contains(doc, "Create worker") || strings.Contains(doc, "New employee") {
		t.Fatal("Journeys overview mixed employee creation into workflow discovery")
	}
	denied := view
	denied.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true, Create: false}}
	deniedDoc, err := Render(denied)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(deniedDoc, `mode=new`) {
		t.Fatal("Journeys advertised a promotion action without create permission")
	}
}

func TestProposalAndApprovalStagesExposeTruthfulProgressStates(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	proposal := ResolveProposalGuide(locale, ProposalCollection{WorkerRef: "worker-1"})
	if proposal.Steps[0].State != "done" || proposal.Steps[1].State != "current" || proposal.Steps[2].State != "upcoming" {
		t.Fatalf("proposal states = %+v", proposal.Steps)
	}
	proposal = ResolveProposalGuide(locale, ProposalCollection{Current: "old", Proposed: "new", EffectiveDate: "2026-10-01"})
	if proposal.Current != 0 || proposal.Steps[1].State != "upcoming" || proposal.Steps[2].State != "upcoming" {
		t.Fatalf("unreachable proposal stages were marked done: %+v", proposal.Steps)
	}
	request := PublicationRequest{}
	approval := ResolveApprovalProgress(locale, request)
	if approval.Current != 0 || approval.Stages[0].State != "current" || approval.Stages[1].State != "upcoming" {
		t.Fatalf("approval states = %+v", approval)
	}
}

func TestPromotionReviewHelpersPreserveOrderInlineErrorsAndEffectiveBoundary(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	comparison := CompareFieldValues(locale, []FieldComparisonInput{
		{Label: "Role", Current: "Analyst", Proposed: "Manager"},
		{Label: "Location", Current: "New York", Proposed: "New York"},
	})
	if len(comparison.Rows) != 2 || comparison.ChangedCount != 1 || !comparison.Rows[0].Changed || comparison.Rows[1].Changed {
		t.Fatalf("comparison set = %+v", comparison)
	}
	fields := ResolveValidationFields(locale, ValidationState{Issues: []ValidationIssue{{FieldID: "date", Message: "Choose a date."}}}, []string{"worker", "date"})
	if len(fields) != 2 || fields[0].Invalid || !fields[1].Invalid || fields[1].Message != "Choose a date." {
		t.Fatalf("inline fields = %+v", fields)
	}
	missing := ExplainEffectiveDate(locale, "")
	if missing.Present || missing.Explanation != locale.Text("work.proposal_need_date") {
		t.Fatalf("missing date explanation = %+v", missing)
	}
	known := ExplainEffectiveDate(locale, "2026-10-01")
	if !known.Present || !strings.Contains(known.Explanation, "2026-10-01") {
		t.Fatalf("known date explanation = %+v", known)
	}
	obligation := ResolveObligationExplanation(locale, intentsv1.ObligationState_OBLIGATION_STATE_PENDING)
	if !obligation.Explained || obligation.Text == "" {
		t.Fatalf("pending obligation lost reviewed explanation: %+v", obligation)
	}
	unknown := ResolveObligationExplanation(locale, intentsv1.ObligationState_OBLIGATION_STATE_UNSPECIFIED)
	if unknown.Explained || unknown.Text != "" {
		t.Fatalf("unknown obligation exposed a made-up explanation: %+v", unknown)
	}
}
