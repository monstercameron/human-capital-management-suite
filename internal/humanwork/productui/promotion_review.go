package productui

// PROMOUX-005 GREEN clause: "current/proposed review shows manager,
// organization, position and affected direct-report scope." This file is
// the one seam that turns promotion.ManagementImpact and
// promotion.TargetPlacement -- the domain's own already-resolved facts --
// into presentation, mirroring the pattern position_picker.go and
// approval_disposition.go already established: a domain function decides,
// and the render path only localizes. It performs no cycle detection, no
// authorization and no graph traversal of its own; a candidate's
// reachability was already decided by internal/domains/org and composed by
// internal/domains/promotion before this file ever sees it.

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
)

// PromotionReviewProps is the presentation-ready review of what a management
// promotion changes: every field is already localized text, so
// [PromotionReview] never resolves locale copy or recomputes cycle safety
// itself.
type PromotionReviewProps struct {
	// Organization and Position are the current/proposed placement facts
	// GREEN also requires the review to show, already localized by the
	// caller from promotion.TargetPlacement -- this file adds nothing to
	// them beyond rendering.
	Organization string
	Position     string
	// HasTargetManager is false when no target-manager selection was
	// evaluated for this promotion; TargetManager then renders the "no
	// target manager selected" copy instead of an empty field.
	HasTargetManager bool
	TargetManager    string
	// AffectedDirectReportsCount is GREEN's "affected direct-report scope":
	// who else moves when this person moves. Zero renders the explicit "no
	// direct reports are affected" copy rather than silence.
	AffectedDirectReportsCount int
	// CycleSafe is promotion.ManagementImpact.CycleSafe, verbatim. It is
	// meaningful only when HasTargetManager is true.
	CycleSafe bool
	// ManagerUnchanged says the promotion names no new manager, so the
	// worker keeps their current reporting line (REV-091-02). With no
	// evaluated manager it renders "keeps the current manager" rather than
	// the "no target manager selected" copy, which would read as an omission.
	ManagerUnchanged bool
}

// PromotionReviewPropsFrom adapts an already-resolved
// [promotion.ManagementImpact] and the promotion's target placement into
// presentation. It is a pure field copy with locale formatting only: there
// is no branch here that could disagree with the domain's own decided
// impact.
func PromotionReviewPropsFrom(locale LocaleContext, target promotion.TargetPlacement, impact promotion.ManagementImpact) PromotionReviewProps {
	props := PromotionReviewProps{
		Organization: locale.Text("promotion_review.target_organization", map[string]string{"organization": target.OrgUnit}),
		Position:     locale.Text("promotion_review.target_position", map[string]string{"position": target.PositionID}),
	}
	if !impact.Evaluated() {
		return props
	}
	props.HasTargetManager = true
	props.TargetManager = locale.Text("promotion_review.target_manager", map[string]string{"manager": impact.TargetManager.Id})
	props.AffectedDirectReportsCount = len(impact.AffectedDirectReports)
	props.CycleSafe = impact.CycleSafe
	return props
}

// PromotionReview renders GREEN's review comparison: manager, organization,
// position and the affected direct-report scope, plus the cycle-safety
// verdict when a target manager was evaluated at all. It renders every fact
// unconditionally -- there is no collapsed or omitted state -- because the
// whole point of this review is that nothing about the reporting-line
// impact is left for a reviewer to infer.
func PromotionReview(locale LocaleContext, props PromotionReviewProps) ui.Node {
	manager := locale.Text("promotion_review.no_target_manager")
	if props.ManagerUnchanged {
		manager = locale.Text("promotion_review.manager_unchanged")
	}
	if props.HasTargetManager {
		manager = props.TargetManager
	}
	reports := locale.Text("promotion_review.no_affected_reports")
	if props.AffectedDirectReportsCount > 0 {
		reports = locale.Plural("promotion_review.affected_direct_reports", int64(props.AffectedDirectReportsCount))
	}
	// An organization or position the caller could not name is left out
	// rather than rendered as an empty label or a raw identifier.
	nodes := []ui.Node{html.P(html.Props{Class: "promotion-review-manager"}, ui.Text(manager))}
	if props.Organization != "" {
		nodes = append(nodes, html.P(html.Props{Class: "promotion-review-organization"}, ui.Text(props.Organization)))
	}
	if props.Position != "" {
		nodes = append(nodes, html.P(html.Props{Class: "promotion-review-position"}, ui.Text(props.Position)))
	}
	nodes = append(nodes, html.P(html.Props{Class: "promotion-review-affected-reports"}, ui.Text(reports)))
	if props.HasTargetManager {
		tone := "promotion-review-cycle-safe"
		cycle := locale.Text("promotion_review.cycle_safe")
		if !props.CycleSafe {
			tone = "promotion-review-cycle-unsafe"
			cycle = locale.Text("promotion_review.cycle_unsafe")
		}
		nodes = append(nodes, html.P(html.Props{Class: tone}, ui.Text(cycle)))
	}
	return html.Div(html.Props{Class: "promotion-review", Raw: map[string]any{"role": "group"}}, nodes...)
}
