// Package productui guides proposal collection. The guide
// turns a proposal's supplied facts — the worker it
// covers, its current and proposed values, its effective
// date — into ordered steps with completion derived from
// the inputs: an incomplete step names its requirement and
// the guide points at the first incomplete step. Readiness
// means every fact present; submission itself stays server
// authority. Completeness trims surrounding whitespace,
// and both values are required: a half-filled proposal is
// not ready.
package productui

import "strings"

// ProposalCollection is the caller-supplied facts one
// proposal guide collects. Values arrive already formatted;
// the guide only checks presence.
type ProposalCollection struct {
	WorkerRef     string
	Current       string
	Proposed      string
	EffectiveDate string
}

// ProposalGuideStep is one collection step: its catalog
// title, its requirement while incomplete, and whether its
// facts are present.
type ProposalGuideStep struct {
	Title    string
	Detail   string
	Complete bool
	// State is a truthful collection-stage state: done, current, or
	// upcoming. It is presentation metadata only and never means the
	// proposal passed service validation or policy.
	State string
}

// ProposalGuide is the resolved collection guide: ordered
// steps with Current pointing at the first incomplete
// step, or -1 when every step is complete.
type ProposalGuide struct {
	Steps   []ProposalGuideStep
	Current int
	// Complete reports every fact present. It never
	// authorizes submission.
	Complete bool
}

// ResolveProposalGuide resolves the collection guide for
// one proposal's supplied facts. Inputs are never mutated.
func ResolveProposalGuide(locale LocaleContext, input ProposalCollection) ProposalGuide {
	steps := []ProposalGuideStep{
		guideStep(locale, "work.proposal_step_worker", "work.proposal_need_worker", strings.TrimSpace(input.WorkerRef) != ""),
		guideStep(locale, "work.proposal_step_values", "work.proposal_need_values",
			strings.TrimSpace(input.Current) != "" && strings.TrimSpace(input.Proposed) != ""),
		guideStep(locale, "work.proposal_step_date", "work.proposal_need_date", strings.TrimSpace(input.EffectiveDate) != ""),
	}
	guide := ProposalGuide{Steps: steps, Current: -1, Complete: true}
	for i, step := range steps {
		if !step.Complete {
			guide.Current = i
			guide.Complete = false
			break
		}
	}
	for i := range guide.Steps {
		switch {
		case i == guide.Current:
			guide.Steps[i].State = "current"
		case guide.Current >= 0 && i > guide.Current:
			guide.Steps[i].State = "upcoming"
		case guide.Steps[i].Complete:
			guide.Steps[i].State = "done"
		default:
			guide.Steps[i].State = "upcoming"
		}
	}
	return guide
}

// guideStep resolves one step: its catalog title always,
// its requirement while its facts are absent.
func guideStep(locale LocaleContext, titleKey, needKey string, complete bool) ProposalGuideStep {
	step := ProposalGuideStep{Title: locale.Text(titleKey), Complete: complete}
	if !complete {
		step.Detail = locale.Text(needKey)
	}
	return step
}
