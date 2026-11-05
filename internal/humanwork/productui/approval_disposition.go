package productui

// PROMOUX-003 GREEN: "the UI states Waiting for <role>, the assigned person
// or protected-group label, the due date, the viewer's acting authority and
// why the action is or is not available." This file is the one seam that
// turns internal/humanwork/workitem.ApprovalDisposition -- the domain's own
// already-decided verdict, computed by
// internal/humanwork/workitem.ResolveApprovalDisposition -- into
// presentation. It performs no authorization, no separation-of-duties
// check and no membership computation of its own: every fact rendered here
// is read from the disposition, never recomputed. A different verdict can
// only come from calling ResolveApprovalDisposition with different inputs,
// never from editing this file (PROMOUX-003's REFACTOR clause).
//
// This mirrors the pattern promotion_availability.go already established
// for PROMOUX-001/002 (ResolvePromotionAvailability decides; the render path
// only turns the decided code into localized copy) rather than inventing a
// second convention for the same kind of fact.

import (
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
)

// ApprovalDispositionProjection is the presentation projection of one
// approval work item's disposition: stable references and booleans, never
// rendered text, so [approvalDispositionCardProps] is the only place that
// resolves locale copy from it. A nil *ApprovalDispositionProjection on a
// [WorkItem] means no disposition was computed for it -- not an approval
// item, or an adapter that has not wired one yet -- and renders no
// disposition block at all rather than a misleading default.
type ApprovalDispositionProjection struct {
	_                  incomparable
	WaitingForRef      string
	AssignedRef        string
	AssignedIsGroup    bool
	DueAt              time.Time
	ViewerAuthorityRef string
	Available          bool
	Reason             string
}

// ApprovalDispositionProjectionFrom adapts an already-resolved
// [workitem.ApprovalDisposition] into this package's presentation
// projection. It is a pure field-for-field copy: there is no branch here
// that could disagree with the domain's own verdict.
func ApprovalDispositionProjectionFrom(d workitem.ApprovalDisposition) ApprovalDispositionProjection {
	return ApprovalDispositionProjection{
		WaitingForRef: d.WaitingForRef, AssignedRef: d.AssignedRef, AssignedIsGroup: d.AssignedIsGroup,
		DueAt: d.DueAt, ViewerAuthorityRef: d.ViewerAuthorityRef, Available: d.Available, Reason: d.Reason,
	}
}

// approvalDispositionCardProps localizes an already-resolved disposition
// into the five GREEN facts, each a complete, ready-to-render sentence, for
// [ApprovalDispositionCard] to render verbatim. d is nil exactly when the
// work item carries no disposition; Show is false in that case and every
// other field is the zero value, so a caller that forgets the nil check
// still renders nothing rather than an empty, misleading card.
func approvalDispositionCardProps(locale LocaleContext, d *ApprovalDispositionProjection) ApprovalDispositionCardProps {
	if d == nil {
		return ApprovalDispositionCardProps{}
	}
	due := locale.Text("common.not_reported")
	if !d.DueAt.IsZero() {
		due = locale.FormatDate(d.DueAt)
	}
	viewerAuthority := locale.Text("work.approval_no_acting_authority")
	if d.ViewerAuthorityRef != "" {
		viewerAuthority = locale.Text("work.approval_viewer_authority",
			map[string]string{"role": approvalRoleLabel(locale, d.ViewerAuthorityRef)})
	}
	return ApprovalDispositionCardProps{
		Show: true,
		WaitingFor: locale.Text("work.approval_waiting_for",
			map[string]string{"role": approvalRoleLabel(locale, d.WaitingForRef)}),
		AssignedTo: locale.Text("work.approval_assigned_to",
			map[string]string{"assignee": approvalAssignedLabel(locale, d.AssignedRef, d.AssignedIsGroup)}),
		AssignedIsGroup: d.AssignedIsGroup,
		Due:             locale.Text("work.approval_due", map[string]string{"date": due}),
		ViewerAuthority: viewerAuthority,
		Available:       d.Available,
		Reason:          approvalAvailabilityReason(locale, d.Available, d.Reason),
	}
}

// approvalRoleLabel humanizes a stable approval authority-class reference
// (e.g. "approval.promotion.finance_partner/v1", the exact shape
// internal/workflow/promotionexec compiles) into a role name, by the
// convention of that reference family: the dot-segment immediately before
// its "/v<n>" version suffix. It never invents a role and never renders the
// raw internal reference to an ordinary user: an unparseable or unknown
// reference falls back to one generic, non-revealing label, the same
// fail-closed shape [PromotionAvailabilityReason] uses for an unrecognized
// code.
func approvalRoleLabel(locale LocaleContext, ref string) string {
	slug := approvalRoleSlug(ref)
	if slug == "" {
		return locale.Text("work.approval_role_unknown")
	}
	return humanizeSlug(slug)
}

func approvalRoleSlug(ref string) string {
	base := ref
	if i := strings.IndexByte(base, '/'); i >= 0 {
		base = base[:i]
	}
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		base = base[i+1:]
	}
	return base
}

func humanizeSlug(slug string) string {
	words := strings.Split(slug, "_")
	out := make([]string, 0, len(words))
	for _, w := range words {
		if w == "" {
			continue
		}
		out = append(out, strings.ToUpper(w[:1])+w[1:])
	}
	return strings.Join(out, " ")
}

// approvalAssignedLabel renders the assigned-person-or-protected-group
// label GREEN requires. A candidate-set owner is never named by its raw
// resolution reference (RouteFromAssignment mints one that carries an
// expression digest -- an internal identifier, not a label): it always
// renders the generic protected-group phrase instead. A single assigned
// principal has no directory-backed display name reachable from this
// package, so its reference is humanized the same way a role slug is
// (stripping a "principal:" prefix and title-casing the rest) rather than
// shown as a raw internal identifier verbatim.
func approvalAssignedLabel(locale LocaleContext, ref string, isGroup bool) string {
	if isGroup {
		return locale.Text("work.approval_protected_group")
	}
	if ref == "" {
		return locale.Text("work.approval_role_unknown")
	}
	name := strings.TrimPrefix(ref, "principal:")
	return humanizeSlug(strings.ReplaceAll(name, "-", "_"))
}

// approvalAvailabilityReason maps the disposition's own stable reason code
// to localized copy. The mapping is exhaustive with no permissive default:
// an available disposition always states the authority it relies on, and
// every declared unavailable code has its own explanation. An unrecognized
// code -- one this build does not know about yet -- falls back to the same
// generic, non-revealing text as [DispositionNoAuthority] rather than
// rendering blank, and it deliberately never repeats the identity of
// whoever completed a conflicting sibling requirement: the disposition
// itself never carries that identity (see
// [workitem.ResolveApprovalDisposition]), so there is nothing here that
// could leak it.
func approvalAvailabilityReason(locale LocaleContext, available bool, reason string) string {
	if available {
		return locale.Text("work.approval_available")
	}
	switch reason {
	case workitem.DispositionNoAuthority:
		return locale.Text("work.approval_no_authority")
	case workitem.DispositionNotActionable:
		return locale.Text("work.approval_not_actionable")
	case workitem.DispositionSeparationConflict:
		return locale.Text("work.approval_separation_conflict")
	default:
		return locale.Text("work.approval_no_authority")
	}
}
