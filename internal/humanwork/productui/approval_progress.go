// Package productui presents approval progress through the
// governed publication pipeline. Progress reads the
// pipeline's gates — composition validation, preview
// evidence, review approval — in pipeline order without
// running the mechanical publish: each stage reports its
// verdict with its requirement while blocked. Complete
// means every gate passes; it never publishes and never
// authorizes. Requests are never mutated.
package productui

import (
	"reflect"
	"strings"
)

// ApprovalStage is one pipeline gate's presented state:
// its catalog title, its requirement while blocked, and
// whether the gate passes.
type ApprovalStage struct {
	Title    string
	Detail   string
	Complete bool
	// State is done, current, or upcoming. Approval progress describes
	// observed gates only; it never implies publication or execution.
	State string
}

// ApprovalProgress is the resolved approval progress:
// ordered stages with Current pointing at the first
// blocked stage, or -1 when every gate passes.
type ApprovalProgress struct {
	Stages   []ApprovalStage
	Current  int
	Complete bool
	Passed   int
}

// ResolveApprovalProgress resolves approval progress for
// one publication request, reading the governed gates in
// pipeline order.
func ResolveApprovalProgress(locale LocaleContext, request PublicationRequest) ApprovalProgress {
	stages := []ApprovalStage{
		approvalStage(locale, "work.approval_step_validation", "work.approval_need_validation",
			ValidateComposition(request.Draft, request.Catalog, request.Registry).Compatible),
		approvalStage(locale, "work.approval_step_preview", "work.approval_need_preview",
			approvalPreviewMatches(request)),
		approvalStage(locale, "work.approval_step_review", "work.approval_need_review",
			request.Review.Approved && strings.TrimSpace(request.Review.Reviewer) != ""),
	}
	progress := ApprovalProgress{Stages: stages, Current: -1, Complete: true}
	for i, stage := range stages {
		if stage.Complete {
			progress.Passed++
		}
		if !stage.Complete {
			progress.Current = i
			progress.Complete = false
			break
		}
	}
	// Count all passed gates even when an earlier gate blocks the pipeline;
	// this is useful summary context and does not change the first-blocked
	// stage semantics.
	if progress.Current >= 0 {
		progress.Passed = 0
		for _, stage := range stages {
			if stage.Complete {
				progress.Passed++
			}
		}
	}
	for i := range progress.Stages {
		switch {
		case i == progress.Current:
			progress.Stages[i].State = "current"
		case progress.Current >= 0 && i > progress.Current:
			progress.Stages[i].State = "upcoming"
		case progress.Stages[i].Complete:
			progress.Stages[i].State = "done"
		default:
			progress.Stages[i].State = "upcoming"
		}
	}
	return progress
}

// approvalStage resolves one stage: its catalog title
// always, its requirement while its gate fails.
func approvalStage(locale LocaleContext, titleKey, needKey string, complete bool) ApprovalStage {
	stage := ApprovalStage{Title: locale.Text(titleKey), Complete: complete}
	if !complete {
		stage.Detail = locale.Text(needKey)
	}
	return stage
}

// approvalPreviewMatches mirrors the pipeline's preview
// gate: the plan validates, targets the draft page, and
// expands exactly to the carried evidence.
func approvalPreviewMatches(request PublicationRequest) bool {
	if !ValidatePreviewPlan(request.Preview).Compatible {
		return false
	}
	if request.Preview.Page != request.Draft.Page {
		return false
	}
	expanded, err := ExpandPreviewPlan(request.Preview)
	if err != nil {
		return false
	}
	return reflect.DeepEqual(expanded, request.PreviewCases)
}
