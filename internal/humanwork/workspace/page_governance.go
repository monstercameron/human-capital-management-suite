package workspace

import (
	"fmt"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// PageGovernance is the serving-side view of the governed page platform:
// the revision ledger plus the rollouts and retirements published against
// it. It wraps productui's digest and validation logic as the single source
// of truth and adds only the rollout/retirement state the live page-serving
// handler resolves through. The zero value is ready to publish.
type PageGovernance struct {
	mu          sync.RWMutex
	log         *productui.PageRevisionLog
	rollouts    map[productui.PageID][]productui.PageRollout
	retirements map[productui.PageID]productui.PageRetirement
}

// NewPageGovernance returns an empty governance view.
func NewPageGovernance() *PageGovernance {
	return &PageGovernance{log: &productui.PageRevisionLog{}}
}

// PublishRevision records one page snapshot at one version in the ledger.
func (g *PageGovernance) PublishRevision(page productui.PageID, snapshot productui.PageDefinitionSnapshot, version int64) (productui.PageDefinitionRevision, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.log.Record(page, snapshot, version)
}

// PublishRollout validates one rollout against the ledger and stages it for
// serving. Re-publishing the identical rollout is idempotent; a rollout that
// fails structural validation, targets an unpublished revision, or pins a
// mismatched digest is refused. Rollout advances forward only: a rollout
// that would move a scope back to an older revision is refused here and must
// go through PublishRollback, the later lifecycle step, instead.
func (g *PageGovernance) PublishRollout(rollout productui.PageRollout) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if verdict := productui.ValidatePageRollout(rollout); !verdict.Compatible {
		return fmt.Errorf("workspace: invalid page rollout: %v", verdict.Reasons)
	}
	if err := productui.VerifyRolloutTarget(g.log, rollout); err != nil {
		return fmt.Errorf("workspace: %w", err)
	}
	existing := g.rollouts[rollout.Page]
	for _, staged := range existing {
		if staged.Version > rollout.Version && rolloutScopesOverlap(staged, rollout) {
			return fmt.Errorf("workspace: rollout moves page %q backward from version %d to %d; publish an explicit rollback", rollout.Page, staged.Version, rollout.Version)
		}
	}
	for _, staged := range existing {
		if staged.Version == rollout.Version {
			if staged.Digest != rollout.Digest {
				return fmt.Errorf("workspace: rollout conflict for page %q version %d", rollout.Page, rollout.Version)
			}
			return nil
		}
	}
	if g.rollouts == nil {
		g.rollouts = make(map[productui.PageID][]productui.PageRollout)
	}
	g.rollouts[rollout.Page] = append(existing, rollout)
	return nil
}

// PublishRollback moves the scopes it names back to an older published
// revision. Both endpoints must be ledger-pinned, the destination strictly
// older than the live version, and every rollback scope already live-scoped:
// rollback returns scopes, never recruits new ones. The rollback supersedes
// newer staged rollouts for its scopes, so the older revision is what the
// next resolve serves.
func (g *PageGovernance) PublishRollback(live, rollback productui.PageRollout) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := productui.VerifyRollbackTarget(g.log, live, rollback); err != nil {
		return fmt.Errorf("workspace: %w", err)
	}
	kept := g.rollouts[rollback.Page][:0]
	for _, staged := range g.rollouts[rollback.Page] {
		if rolloutScopesOverlap(staged, rollback) {
			continue
		}
		kept = append(kept, staged)
	}
	if g.rollouts == nil {
		g.rollouts = make(map[productui.PageID][]productui.PageRollout)
	}
	g.rollouts[rollback.Page] = append(kept, rollback)
	return nil
}

// rolloutScopesOverlap reports whether two rollouts name a shared scope.
func rolloutScopesOverlap(first, second productui.PageRollout) bool {
	scopes := make(map[string]bool, len(first.Scopes))
	for _, scope := range first.Scopes {
		scopes[scope.Scope] = true
	}
	for _, scope := range second.Scopes {
		if scopes[scope.Scope] {
			return true
		}
	}
	return false
}

// PublishRetirement records one page's end-of-life. The page must carry at
// least one published revision; a never-published page has nothing to end.
func (g *PageGovernance) PublishRetirement(retirement productui.PageRetirement) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if verdict := productui.ValidatePageRetirement(retirement); !verdict.Compatible {
		return fmt.Errorf("workspace: invalid page retirement: %v", verdict.Reasons)
	}
	if err := productui.VerifyRetirementTarget(g.log, retirement); err != nil {
		return fmt.Errorf("workspace: %w", err)
	}
	if g.retirements == nil {
		g.retirements = make(map[productui.PageID]productui.PageRetirement)
	}
	g.retirements[retirement.Page] = retirement
	return nil
}

// Resolve looks up the live rollout for one page and scope at one epoch
// second. It returns the rolled-out revision, whether the page is governed
// at all, whether it may be served, and the reason when it may not.
//
// The defined fallback policy: a page with no published rollout is
// ungoverned and serves the compiled registry definition, preserving the
// serving path that predates the ledger. A governed page serves only the
// highest-versioned rollout that is both live for the scope and digest-pinned
// to the ledger with no active retirement darkening it; anything else is
// refused so a retired or rolled-back page is never served.
func (g *PageGovernance) Resolve(page productui.PageID, scope string, now int64) (productui.PageDefinitionRevision, bool, bool, string) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	staged := g.rollouts[page]
	if len(staged) == 0 {
		return productui.PageDefinitionRevision{}, false, true, "no governed rollout; serving compiled registry"
	}
	retirement, retired := g.retirements[page]
	var darkening *productui.PageRetirement
	if retired {
		darkening = &retirement
	}
	var live *productui.PageRollout
	for i := range staged {
		candidate := staged[i]
		if !productui.RolloutServableAt(candidate, darkening, scope, now) {
			continue
		}
		if err := productui.VerifyRolloutTarget(g.log, candidate); err != nil {
			continue
		}
		if live == nil || candidate.Version > live.Version {
			next := candidate
			live = &next
		}
	}
	if live == nil {
		if retired && retirement.Page == page && productui.RetirementActiveAt(retirement, now) {
			return productui.PageDefinitionRevision{}, true, false, "page retired"
		}
		return productui.PageDefinitionRevision{}, true, false, "no live rollout covers scope"
	}
	revision, ok := g.log.Revision(page, live.Version)
	if !ok {
		return productui.PageDefinitionRevision{}, true, false, "rolled-out revision missing from ledger"
	}
	return revision, true, true, ""
}

// resolveGovernedRevision is the single revision-resolution helper the live
// page-serving handler and any future studio/admin API share, rather than
// duplicating rollout/retirement checks at each call site. A nil governance
// view serves the compiled registry, matching the pre-ledger behavior.
func (h *Handler) resolveGovernedRevision(page productui.PageID, scope string, now int64) (productui.PageDefinitionRevision, bool, bool, string) {
	if h == nil || h.pages == nil {
		return productui.PageDefinitionRevision{}, false, true, ""
	}
	return h.pages.Resolve(page, scope, now)
}
