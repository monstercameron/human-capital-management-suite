package defaultproduct

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// ALIGN-043: the governed work-loop pages are seeded against intent
// actions. Every page names the semantic action it serves, the capability
// that action requires, and the fallback page when the action is not
// available. Routing resolves an action to its page only for admitted
// capabilities; anything else is refused before a page renders.

// Page errors.
var (
	ErrPageInvalid = errors.New("defaultproduct: work-loop page is invalid")
	ErrPageUnknown = errors.New("defaultproduct: intent action has no work-loop page")
	ErrPageDenied  = errors.New("defaultproduct: capability does not admit this work-loop page")
)

// WorkLoopPage is one seeded page serving one intent action.
type WorkLoopPage struct {
	ID                 string `json:"id"`
	IntentAction       string `json:"intent_action"`
	RequiredCapability string `json:"required_capability"`
	FallbackPage       string `json:"fallback_page"`
	Floorplan          string `json:"floorplan"`
}

// PageRegistry is the seeded work-loop page set with its digest.
type PageRegistry struct {
	Pages  []WorkLoopPage `json:"pages"`
	Digest string         `json:"digest"`
}

// DefaultPages seeds the governed work-loop pages.
func DefaultPages() PageRegistry {
	registry := PageRegistry{Pages: []WorkLoopPage{
		{ID: "promotion.detail.page", IntentAction: "promotion.view", RequiredCapability: "promotion.view", FallbackPage: "failure.unauthorized.page", Floorplan: "floorplan.detail"},
		{ID: "promotion.execute.page", IntentAction: "promotion.execute", RequiredCapability: "promotion.execute", FallbackPage: "promotion.detail.page", Floorplan: "floorplan.detail"},
		{ID: "promotion.list.page", IntentAction: "promotion.list", RequiredCapability: "promotion.view", FallbackPage: "failure.unauthorized.page", Floorplan: "floorplan.list"},
		{ID: "operations.repair.page", IntentAction: "operations.repair", RequiredCapability: "operations.repair", FallbackPage: "failure.unauthorized.page", Floorplan: "floorplan.detail"},
	}}
	registry.Digest = registry.computeDigest()
	return registry
}

// Validate enforces unique safe IDs, required actions and capabilities,
// and fallbacks that resolve to a seeded page (or the failure root).
func (r PageRegistry) Validate() error {
	seen := make(map[string]bool)
	for _, page := range r.Pages {
		if !safeID(page.ID) {
			return fmt.Errorf("%w: page id %q", ErrPageInvalid, page.ID)
		}
		if seen[page.ID] {
			return fmt.Errorf("%w: duplicate page %q", ErrPageInvalid, page.ID)
		}
		seen[page.ID] = true
		if !safeID(page.IntentAction) || !safeID(page.RequiredCapability) || !safeID(page.Floorplan) {
			return fmt.Errorf("%w: page %q needs an action, a capability, and a floorplan", ErrPageInvalid, page.ID)
		}
	}
	for _, page := range r.Pages {
		if !seen[page.FallbackPage] && page.FallbackPage != "failure.unauthorized.page" {
			return fmt.Errorf("%w: page %q fallback %q is not seeded", ErrPageInvalid, page.ID, page.FallbackPage)
		}
	}
	return nil
}

// RouteFor resolves an intent action to its page for admitted
// capabilities. An unknown action is refused; a known action without its
// capability resolves to the page's fallback, never to the page itself.
func (r PageRegistry) RouteFor(action string, admitted []string) (WorkLoopPage, error) {
	grants := make(map[string]bool, len(admitted))
	for _, capability := range admitted {
		grants[capability] = true
	}
	for _, page := range r.Pages {
		if page.IntentAction != action {
			continue
		}
		if grants[page.RequiredCapability] {
			return page, nil
		}
		for _, fallback := range r.Pages {
			if fallback.ID == page.FallbackPage {
				return fallback, fmt.Errorf("%w: %q", ErrPageDenied, action)
			}
		}
		return WorkLoopPage{ID: page.FallbackPage}, fmt.Errorf("%w: %q", ErrPageDenied, action)
	}
	return WorkLoopPage{}, fmt.Errorf("%w: %q", ErrPageUnknown, action)
}

func (r PageRegistry) computeDigest() string {
	pages := append([]WorkLoopPage(nil), r.Pages...)
	sort.Slice(pages, func(i, j int) bool { return pages[i].ID < pages[j].ID })
	b, err := json.Marshal(pages)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyDigest reports whether the digest matches the seeded content.
func (r PageRegistry) VerifyDigest() error {
	if r.Digest == "" || r.Digest != r.computeDigest() {
		return fmt.Errorf("%w: page registry digest does not match its content", ErrPageInvalid)
	}
	return nil
}
