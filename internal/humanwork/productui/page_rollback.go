package productui

import (
	"fmt"
	"strings"
)

// PageRetirement is one page's dated end-of-life: the page, the
// epoch second it goes dark (zero retires at publication of the
// retirement), and the recorded reason. The reason is retained
// evidence, never optional: an unexplained end-of-life cannot be
// reviewed.
type PageRetirement struct {
	Page          PageID `json:"page"`
	EffectiveFrom int64  `json:"effective_from"`
	Reason        string `json:"reason"`
}

// RetirementVerdict is the retirement answer: compatible plus the
// stable reasons, in validation order, when not. Reasons stay nil
// on success.
type RetirementVerdict struct {
	Compatible bool
	Reasons    []string
}

// ValidatePageRetirement checks one retirement structurally: a
// named page, a non-negative date, and a non-blank reason.
// Violations accumulate in fixed order.
func ValidatePageRetirement(retirement PageRetirement) RetirementVerdict {
	var reasons []string
	if retirement.Page == "" {
		reasons = append(reasons, "missing retirement page")
	}
	if retirement.EffectiveFrom < 0 {
		reasons = append(reasons, "negative retirement date")
	}
	if strings.TrimSpace(retirement.Reason) == "" {
		reasons = append(reasons, "missing retirement reason")
	}
	if len(reasons) > 0 {
		return RetirementVerdict{Compatible: false, Reasons: reasons}
	}
	return RetirementVerdict{Compatible: true}
}

// VerifyRetirementTarget binds one retirement to publication
// history: the page must carry at least one published revision.
// A never-published page has nothing to end.
func VerifyRetirementTarget(log *PageRevisionLog, retirement PageRetirement) error {
	if _, ok := log.Latest(retirement.Page); !ok {
		return fmt.Errorf("productui: retirement targets unpublished page %q", retirement.Page)
	}
	return nil
}

// RetirementActiveAt reports whether one retirement darkens its
// page at one epoch second.
func RetirementActiveAt(retirement PageRetirement, now int64) bool {
	return now >= retirement.EffectiveFrom
}

// VerifyRollbackTarget verifies one rollback: moving a live scope
// back to an older revision. Rules, in order: the rollback must be
// a structurally valid rollout; it must name the live page; its
// version must be strictly older than the live version; every
// rollback scope must already be live-scoped (rollback returns
// scopes, never recruits new ones); and both endpoints must be
// log-pinned revisions — the baseline must exist before moving off
// it, and the destination must be published, never invented.
func VerifyRollbackTarget(log *PageRevisionLog, live, rollback PageRollout) error {
	if verdict := ValidatePageRollout(rollback); !verdict.Compatible {
		return fmt.Errorf("productui: invalid rollback rollout: %s", strings.Join(verdict.Reasons, "; "))
	}
	if rollback.Page != live.Page {
		return fmt.Errorf("productui: rollback crosses pages from %q to %q", live.Page, rollback.Page)
	}
	if rollback.Version >= live.Version {
		return fmt.Errorf("productui: rollback version %d is not older than live version %d", rollback.Version, live.Version)
	}
	liveScopes := map[string]bool{}
	for _, scope := range live.Scopes {
		liveScopes[scope.Scope] = true
	}
	for _, scope := range rollback.Scopes {
		if !liveScopes[scope.Scope] {
			return fmt.Errorf("productui: rollback scope %q is not in the live rollout", scope.Scope)
		}
	}
	if err := VerifyRolloutTarget(log, live); err != nil {
		return err
	}
	return VerifyRolloutTarget(log, rollback)
}

// RolloutServableAt reports whether one rollout serves its revision
// to a scope at one epoch second under an optional retirement: the
// rollout must be live and no matching retirement active. A nil
// retirement, or one naming another page, never darkens. Only a
// validated, target-verified rollout and retirement should feed
// serving reads.
func RolloutServableAt(rollout PageRollout, retirement *PageRetirement, scope string, now int64) bool {
	if !RolloutLiveAt(rollout, scope, now) {
		return false
	}
	if retirement != nil && retirement.Page == rollout.Page && RetirementActiveAt(*retirement, now) {
		return false
	}
	return true
}
