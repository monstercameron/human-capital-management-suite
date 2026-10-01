package productui

// MaterialChangeReviewPattern records an established transaction-review
// pattern considered for material compensation changes. It is research data,
// not page markup: later workflows can evaluate the same choices without
// copying a modal or moving authority into the browser.
type MaterialChangeReviewPattern struct {
	Name       string
	Strength   string
	Limitation string
}

// MaterialChangeScenario is one decision case used by the review contract.
type MaterialChangeScenario string

const (
	MaterialChangeOrdinary        MaterialChangeScenario = "ordinary"
	MaterialChangeAboveBand       MaterialChangeScenario = "above-band"
	MaterialChangeBudgetShortfall MaterialChangeScenario = "budget-shortfall"
	MaterialChangeMissingRequired MaterialChangeScenario = "missing-required"
	MaterialChangeStaleBaseline   MaterialChangeScenario = "changed-stale-baseline"
	MaterialChangeMaskedPay       MaterialChangeScenario = "masked-compensation"
)

// MaterialChangeScenarioDecision is the server-owned review decision for a
// scenario. ReviewRequired describes the user-facing checkpoint; Blocking is
// the severity of a warning before the governed write is attempted.
type MaterialChangeScenarioDecision struct {
	Scenario       MaterialChangeScenario
	ReviewRequired bool
	Blocking       bool
	Facts          []string
	ReaderMessage  string
}

// MaterialChangeReviewContract is the reusable product decision for a
// material promotion or compensation change. The browser may present these
// facts progressively, but the server remains authoritative for validation,
// policy findings, stale baselines and the eventual write.
type MaterialChangeReviewContract struct {
	SelectedPattern     string
	ValidationTiming    string
	WarningSeverity     string
	ConfirmationContent []string
	MobileInteraction   string
	AssistiveTechnology []string
	Patterns            []MaterialChangeReviewPattern
	Scenarios           []MaterialChangeScenarioDecision
}

// DefaultMaterialChangeReviewContract returns the reviewed contract selected
// for UXLIVE-034. It combines a check-answers summary with a guarded final
// action: required-field feedback is immediate, but all policy and baseline
// checks run authoritatively before review and again at submit.
func DefaultMaterialChangeReviewContract() MaterialChangeReviewContract {
	return MaterialChangeReviewContract{
		SelectedPattern:  "check-answers-plus-guarded-submit",
		ValidationTiming: "required fields on blur and before review; policy, authorization and stale-baseline checks on review and again on submit",
		WarningSeverity:  "above-band, budget-shortfall and stale-baseline findings block submission; informational findings do not",
		ConfirmationContent: []string{
			"employee identity and target role",
			"current and proposed compensation with currency and pay period when authorized",
			"effective date",
			"business reason",
			"authoritative policy findings and the next safe correction",
		},
		MobileInteraction: "progressive review opens as a focused full-width checkpoint with the summary before the final action and a visible go-back path",
		AssistiveTechnology: []string{
			"review has a named heading and announced validation summary",
			"focus moves to the first invalid field and returns to the review trigger after dismissal",
			"final action names the material change; go-back is separate from canceling the governed record",
		},
		Patterns: []MaterialChangeReviewPattern{
			{
				Name:       "SAP Fiori critical confirmation and server warning",
				Strength:   "keeps critical actions explicit and lets server-owned warnings decide whether continuation is possible",
				Limitation: "a popup alone is too compressed for comparing several compensation facts",
			},
			{
				Name:       "ServiceNow preview and submit approval",
				Strength:   "shows approval conditions, steps and approvers before generating the request",
				Limitation: "workflow preview can overwhelm a proposer when technical routing is not their task",
			},
			{
				Name:       "GOV.UK check answers",
				Strength:   "gives a final structured chance to catch errors before a consequential submission",
				Limitation: "a static summary does not by itself express live policy warnings or stale data",
			},
		},
		Scenarios: []MaterialChangeScenarioDecision{
			{Scenario: MaterialChangeOrdinary, ReviewRequired: true, Facts: []string{"employee identity and target role", "current and proposed compensation", "effective date", "business reason"}, ReaderMessage: "Review the proposed change before it is submitted."},
			{Scenario: MaterialChangeAboveBand, ReviewRequired: true, Blocking: true, Facts: []string{"employee identity and target role", "proposed compensation", "above-band finding", "business reason"}, ReaderMessage: "The proposed compensation is above the authorized band; correct it before submitting."},
			{Scenario: MaterialChangeBudgetShortfall, ReviewRequired: true, Blocking: true, Facts: []string{"employee identity and target role", "proposed compensation", "budget-shortfall finding", "business reason"}, ReaderMessage: "The request exceeds the available budget; correct it before submitting."},
			{Scenario: MaterialChangeMissingRequired, ReviewRequired: false, Blocking: true, Facts: []string{"first invalid field", "field-level correction"}, ReaderMessage: "Complete the required fields before opening review."},
			{Scenario: MaterialChangeStaleBaseline, ReviewRequired: true, Blocking: true, Facts: []string{"employee identity and target role", "stale-baseline finding", "refresh-and-recompare action"}, ReaderMessage: "The underlying compensation or role changed; refresh and compare again."},
			{Scenario: MaterialChangeMaskedPay, ReviewRequired: true, Facts: []string{"employee identity and target role", "current compensation unavailable", "proposed compensation", "effective date", "business reason"}, ReaderMessage: "Current compensation is unavailable to this viewer; no masked value is disclosed."},
		},
	}
}
