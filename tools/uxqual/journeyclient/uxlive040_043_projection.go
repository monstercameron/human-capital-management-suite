package journeyclient

import (
	"net/url"
	"strings"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// promotionEmployeeSelectionHref keeps the promotion task context in the
// canonical People route. Eligibility is a route state owned by People, not a
// client-side filter, and locale is only emitted when it differs from the
// product default so durable navigation remains canonical.
func promotionEmployeeSelectionHref(locale string) string {
	copy := productui.ResolveProductLocale(locale)
	query := url.Values{}
	query.Set("eligible", "1")
	if copy.Resolved != productui.DefaultProductLocale {
		query.Set("locale", copy.Resolved)
	}
	return "/workspace/app/people?" + query.Encode()
}

func outcomeReasonLocale(locale string, head journey.JourneyCard, findings []*journeyv1.Finding) *journey.OutcomeReason {
	if !terminalNonSuccessStage(head.Stage) {
		return nil
	}
	copy := productui.ResolveProductLocale(locale)
	tone := "warning"
	if head.Stage == stageFailed || head.Stage == stageRepairRequired {
		tone = "danger"
	}
	reason := &journey.OutcomeReason{Stage: head.StageLabel, StoppedAt: head.StageLabel, Tone: tone}
	for _, finding := range findings {
		if finding == nil {
			continue
		}
		severity := severityOf(finding.GetSeverity())
		text := outcomeFindingText(copy, finding.GetCode())
		if severity == severityBlocking {
			reason.Blocking = append(reason.Blocking, text)
		} else if severity == severityWarning || severity == "needs-data" {
			reason.Supplemental = append(reason.Supplemental, text)
		}
	}
	if len(reason.Blocking) == 0 {
		reason.Summary = copy.Text("journey.outcome_reason_unavailable")
	} else if len(reason.Blocking) == 1 {
		reason.Summary = reason.Blocking[0]
	} else {
		reason.Summary = copy.Text("journey.outcome_reason_multiple")
	}
	if head.Stage == stageRepairRequired {
		reason.NextStep = copy.Text("journey.outcome_reason_repair")
	} else {
		reason.NextStep = copy.Text("journey.outcome_reason_next")
	}
	return reason
}

func terminalNonSuccessStage(stage string) bool {
	switch stage {
	case stageBlocked, stageRejected, stageFailed, stageRepairRequired:
		return true
	default:
		return false
	}
}

func outcomeFindingText(copy productui.LocaleContext, code string) string {
	switch strings.TrimSpace(code) {
	case "promotion.pay_below_band_minimum":
		return copy.Text("journey.blocked_pay_below_band")
	case "promotion.pay_above_band_maximum":
		return copy.Text("journey.blocked_pay_above_band")
	case "promotion.budget_observed_insufficient", "promotion.budget_shortfall":
		return copy.Text("journey.outcome_reason_budget")
	default:
		return copy.Text("journey.outcome_reason_unavailable")
	}
}

func progressLocale(locale string, head journey.JourneyCard, detail *journeyv1.JourneyDetail) *journey.Progress {
	phaseKey := ""
	switch head.Stage {
	case stageRevalidation:
		phaseKey = "journey.stage_revalidation"
	case stageExecuted:
		phaseKey = "journey.stage_recording"
	case stageObservingEffects:
		phaseKey = "journey.stage_observing_effects"
	default:
		return nil
	}
	copy := productui.ResolveProductLocale(locale)
	phase := copy.Text(phaseKey)
	status := progressStatus(detail.GetNodes())
	key := "journey.progress_active"
	switch status {
	case "retrying":
		key = "journey.progress_retrying"
	case "delayed":
		key = "journey.progress_delayed"
	case "repair-required":
		key = "journey.progress_repair"
	}
	last := latestProgress(locale, detail)
	return &journey.Progress{
		Phase:        phase,
		Summary:      copy.Text(key, map[string]string{"phase": phase}),
		LastProgress: last,
		NextAction:   copy.Text("journey.progress_next"),
		Status:       status,
	}
}

func progressStatus(nodes []*journeyv1.NodeExecution) string {
	status := "active"
	for _, node := range nodes {
		if node == nil {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(node.GetStatus())) {
		case "REPAIR_REQUIRED":
			return "repair-required"
		case "RETRYING", "RETRY", "RETRY_SCHEDULED":
			status = "retrying"
		case "DELAYED", "WAITING", "PAUSED", "TIMED_OUT":
			if status != "retrying" {
				status = "delayed"
			}
		}
	}
	return status
}

func latestProgress(locale string, detail *journeyv1.JourneyDetail) string {
	var latest protoTimestamp
	for _, event := range detail.GetTimeline() {
		if event != nil && newerTimestamp(event.GetAt(), latest) {
			latest = event.GetAt()
		}
	}
	for _, node := range detail.GetNodes() {
		if node == nil {
			continue
		}
		for _, timestamp := range []protoTimestamp{node.GetStartedAt(), node.GetCompletedAt(), node.GetRecordedAt()} {
			if newerTimestamp(timestamp, latest) {
				latest = timestamp
			}
		}
	}
	if latest == nil {
		return ""
	}
	return formatTimeLocale(locale, latest)
}

func newerTimestamp(candidate, current protoTimestamp) bool {
	candidateTime, candidateOK := timeOf(candidate)
	currentTime, currentOK := timeOf(current)
	return candidateOK && (!currentOK || candidateTime.After(currentTime))
}
