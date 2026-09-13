package productui

import (
	"strings"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func testMoney(amount, currency string) values.Money {
	scale := int32(0)
	if dot := strings.IndexByte(amount, '.'); dot >= 0 {
		scale = int32(len(amount) - dot - 1)
	}
	money, err := values.NewMoney(amount, currency, scale, values.RoundingExactRequired)
	if err != nil {
		panic(err)
	}
	return money
}

// testView is deliberately test-only. Production code has no fixture
// provider and can obtain records only from its live service adapter.
func testView(page PageID) View {
	view := NewView(page, "tenant-test", "Taylor", "manager")
	view.Work = []WorkItem{
		{ID: "intent-1", Initials: "JL", Title: "Promotion journey", Person: "Jordan Lee", Summary: "ENG2 G6 → ENG3 G7", Status: "Awaiting approval", Due: "2026-09-15", Tone: "warning", Href: "/workspace/app/journeys?journey=intent-1", EffectiveDate: "2026-09-15",
			// PROMOUX-012: the server says the viewer holds this approval, which is
			// what places it in My Work at all.
			ViewerRelationships: []string{"ASSIGNEE"}, ViewerResponsibility: "ACTION_REQUIRED", NextStep: "approval_decision", AwaitsPerson: true, StatusProjection: CompleteStatusProjection(&intentsv1.LifecycleDimensions{Request: intentsv1.RequestState_REQUEST_STATE_SUBMITTED, Execution: intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED, Business: intentsv1.BusinessState_BUSINESS_STATE_NOT_STARTED, Consistency: intentsv1.ConsistencyState_CONSISTENCY_STATE_NOT_APPLICABLE, Obligation: intentsv1.ObligationState_OBLIGATION_STATE_PENDING})},
		{ID: "intent-2", Initials: "AP", Title: "Promotion journey", Person: "Avery Patel", PersonRef: "worker-avery", Summary: "DES2 G6 → DES3 G7", Status: "Completed", Tone: "success", Terminal: true, ViewerResponsibility: "CLOSED", EffectiveDate: "2026-08-01", CompletedAt: "4 Aug 2026 · 14:32 UTC", InstanceID: "instance-2", InstanceVersion: 9, MaterialDigest: "sha256:proposal-2", Href: "/workspace/app/journeys?journey=intent-2", StatusProjection: CompleteStatusProjection(&intentsv1.LifecycleDimensions{Request: intentsv1.RequestState_REQUEST_STATE_CLOSED, Execution: intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED, Business: intentsv1.BusinessState_BUSINESS_STATE_COMPLETED, Consistency: intentsv1.ConsistencyState_CONSISTENCY_STATE_CONSISTENT, Obligation: intentsv1.ObligationState_OBLIGATION_STATE_SATISFIED})},
	}
	view.People = []Person{
		{ID: "worker-jordan", Initials: "JL", Name: "Jordan Lee", Role: "ENG2 · G6", Team: "Strategy", Location: "New York", PromotionAvailability: PromotionEligible},
		{ID: "worker-avery", Initials: "AP", PhotoURL: "/workspace/assets/person-jane-small.jpg", Name: "Avery Patel", Role: "DES2 · G6", Team: "Product", Location: "Toronto", WorkerNumber: "NW-40118", JobCode: "DES2", Grade: "G6", PositionID: "pos-design", PayZone: "CA-ON", BasePay: testMoney("118000", "CAD"), BonusTarget: "0.12", HireDate: "2022-04-11", Source: "CREATED", PromotionAvailability: PromotionEligible},
		{ID: "worker-elena", Initials: "ER", Name: "Elena Ruiz", Role: "VP-PRODUCT · G10", Team: "Product", Location: "San Francisco", PromotionAvailability: PromotionEligible},
	}
	view.PersonWorkflows = []PersonWorkflow{
		{ID: "promotion", Name: "Promotion", Category: "Career & compensation", Description: "Propose a governed job and compensation change.", Href: "/workspace/app/journeys?mode=new&worker=worker-avery", LaunchHref: func(person string) string { return "/workspace/app/journeys?mode=new&worker=" + person }},
		{ID: "transfer", Name: "Internal transfer", Category: "Career mobility", Description: "Move a worker to another authorized position.", Href: "/workspace/app/journeys?worker=worker-avery", LaunchHref: func(person string) string { return "/workspace/app/journeys?worker=" + person }},
	}
	view.SelectedWork = "intent-1"
	view.SelectedPerson = "worker-avery"
	for index := range view.Navigation {
		if view.Navigation[index].Page == PageWork {
			view.Navigation[index].Count = len(view.Work)
		}
	}
	return view
}
