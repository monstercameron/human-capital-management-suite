package productui

import "strings"

// JourneyStatusTaxonomyEntry is the single mapping from a journey stage to
// the vocabulary used by its badge, tracker filter, and Home summary.
type JourneyStatusTaxonomyEntry struct {
	Stage     string
	BadgeKey  string
	Filter    string
	HomeGroup string
}

const (
	JourneyHomeGroupInProgress = "in_progress"
	JourneyHomeGroupProblem    = "problem"
	JourneyHomeGroupClosed     = "closed"
)

// JourneyStatusTaxonomy returns a fresh ordered view so callers cannot mutate
// the shared product vocabulary.
func JourneyStatusTaxonomy() []JourneyStatusTaxonomyEntry {
	return []JourneyStatusTaxonomyEntry{
		{Stage: "PROPOSED", BadgeKey: "journey.stage_proposed", Filter: JourneyListStatusReview, HomeGroup: JourneyHomeGroupInProgress},
		{Stage: "AWAITING_APPROVAL", BadgeKey: "journey.stage_awaiting_approval", Filter: JourneyListStatusReview, HomeGroup: JourneyHomeGroupInProgress},
		{Stage: "FINANCE_APPROVAL", BadgeKey: "journey.stage_finance_approval", Filter: JourneyListStatusReview, HomeGroup: JourneyHomeGroupInProgress},
		{Stage: "MANAGER_APPROVAL", BadgeKey: "journey.stage_manager_approval", Filter: JourneyListStatusReview, HomeGroup: JourneyHomeGroupInProgress},
		{Stage: "REVALIDATION", BadgeKey: "journey.stage_revalidation", Filter: JourneyListStatusReview, HomeGroup: JourneyHomeGroupInProgress},
		{Stage: "REAPPROVAL", BadgeKey: "journey.stage_reapproval", Filter: JourneyListStatusReview, HomeGroup: JourneyHomeGroupInProgress},
		{Stage: "WAITING_EFFECTIVE_DATE", BadgeKey: "journey.stage_waiting_effective", Filter: JourneyListStatusWaiting, HomeGroup: JourneyHomeGroupInProgress},
		{Stage: "EXECUTED", BadgeKey: "journey.stage_recording", Filter: JourneyListStatusWaiting, HomeGroup: JourneyHomeGroupInProgress},
		{Stage: "OBSERVING_EFFECTS", BadgeKey: "journey.stage_observing_effects", Filter: JourneyListStatusWaiting, HomeGroup: JourneyHomeGroupInProgress},
		{Stage: "AWAITING_ACKNOWLEDGEMENT", BadgeKey: "journey.stage_awaiting_acknowledgement", Filter: JourneyListStatusWaiting, HomeGroup: JourneyHomeGroupInProgress},
		{Stage: "BLOCKED", BadgeKey: "journey.stage_blocked", Filter: JourneyListStatusIssue, HomeGroup: JourneyHomeGroupProblem},
		{Stage: "FAILED", BadgeKey: "journey.stage_failed", Filter: JourneyListStatusIssue, HomeGroup: JourneyHomeGroupProblem},
		{Stage: "REPAIR_REQUIRED", BadgeKey: "journey.stage_repair_required", Filter: JourneyListStatusIssue, HomeGroup: JourneyHomeGroupProblem},
		{Stage: "COMPLETED", BadgeKey: "journey.stage_completed", Filter: JourneyListStatusClosed, HomeGroup: JourneyHomeGroupClosed},
		{Stage: "RECORDED", BadgeKey: "journey.stage_recorded", Filter: JourneyListStatusClosed, HomeGroup: JourneyHomeGroupClosed},
		{Stage: "REJECTED", BadgeKey: "journey.stage_rejected", Filter: JourneyListStatusClosed, HomeGroup: JourneyHomeGroupClosed},
	}
}

func JourneyStatusForStage(stage string) JourneyStatusTaxonomyEntry {
	normalized := strings.ToUpper(strings.TrimSpace(stage))
	switch normalized {
	case "WAITING_EFFECTIVE":
		normalized = "WAITING_EFFECTIVE_DATE"
	case "UPDATING_RECORD", "RECORDING":
		normalized = "EXECUTED"
	}
	for _, entry := range JourneyStatusTaxonomy() {
		if entry.Stage == normalized {
			return entry
		}
	}
	return JourneyStatusTaxonomyEntry{
		Stage: normalized, BadgeKey: "journey.stage_unknown",
		Filter: JourneyListStatusIssue, HomeGroup: JourneyHomeGroupProblem,
	}
}

// JourneyStatusFilterValues is the filter order shared by the tracker and
// the Home links. Open is the broad non-terminal view; the remaining values
// are the stage groups represented by the taxonomy table.
func JourneyStatusFilterValues() []string {
	return []string{JourneyListStatusOpen, JourneyListStatusReview, JourneyListStatusWaiting, JourneyListStatusIssue, JourneyListStatusClosed}
}

func JourneyHomeFilter(group string) string {
	switch strings.TrimSpace(group) {
	case JourneyHomeGroupInProgress:
		return JourneyListStatusOpen
	case JourneyHomeGroupProblem:
		return JourneyListStatusIssue
	case JourneyHomeGroupClosed:
		return JourneyListStatusClosed
	default:
		return ""
	}
}
