package defaultproduct

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// ALIGN-044: accessible failure and recovery pages are seeded per error
// class. Every failure names its recovery action and an accessibility
// label a screen reader announces; a failure class without a seeded page,
// a recovery action, or a label cannot ship, so no product error is a dead
// end and none is invisible to assistive technology.

// Failure errors.
var (
	ErrFailureInvalid = errors.New("defaultproduct: failure page is invalid")
	ErrFailureUnknown = errors.New("defaultproduct: error class has no failure page")
)

// FailurePage is one seeded failure: the error class it serves, the page
// that renders it, the governed recovery action, and the announced label.
type FailurePage struct {
	ErrorClass         string `json:"error_class"`
	PageID             string `json:"page_id"`
	RecoveryAction     string `json:"recovery_action"`
	AccessibilityLabel string `json:"accessibility_label"`
}

// FailureRegistry is the seeded failure set with its digest.
type FailureRegistry struct {
	Pages  []FailurePage `json:"pages"`
	Digest string        `json:"digest"`
}

// DefaultFailurePages seeds the failure and recovery pages.
func DefaultFailurePages() FailureRegistry {
	registry := FailureRegistry{Pages: []FailurePage{
		{ErrorClass: "unauthorized", PageID: "failure.unauthorized.page", RecoveryAction: "failure.request_access", AccessibilityLabel: "Access denied. Request access or return home."},
		{ErrorClass: "stale_projection", PageID: "failure.stale.page", RecoveryAction: "failure.refetch", AccessibilityLabel: "Data is out of date. Refresh to load the latest version."},
		{ErrorClass: "invalid_submission", PageID: "failure.invalid.page", RecoveryAction: "failure.correct", AccessibilityLabel: "Submission was invalid. Correct the highlighted fields and resubmit."},
		{ErrorClass: "service_unavailable", PageID: "failure.unavailable.page", RecoveryAction: "failure.retry", AccessibilityLabel: "Service is unavailable. Retry or contact support."},
	}}
	registry.Digest = registry.computeDigest()
	return registry
}

// Validate enforces unique safe error classes, page IDs, recovery actions,
// and non-empty accessibility labels.
func (r FailureRegistry) Validate() error {
	seen := make(map[string]bool)
	for _, page := range r.Pages {
		if !safeID(page.ErrorClass) {
			return fmt.Errorf("%w: error class %q", ErrFailureInvalid, page.ErrorClass)
		}
		if seen[page.ErrorClass] {
			return fmt.Errorf("%w: duplicate error class %q", ErrFailureInvalid, page.ErrorClass)
		}
		seen[page.ErrorClass] = true
		if !safeID(page.PageID) || !safeID(page.RecoveryAction) {
			return fmt.Errorf("%w: class %q needs a page and a recovery action", ErrFailureInvalid, page.ErrorClass)
		}
		if len(page.AccessibilityLabel) < 8 {
			return fmt.Errorf("%w: class %q needs an announced accessibility label", ErrFailureInvalid, page.ErrorClass)
		}
	}
	return nil
}

// PageFor returns the failure page for an error class, or a typed refusal
// when the class has no seeded page.
func (r FailureRegistry) PageFor(class string) (FailurePage, error) {
	for _, page := range r.Pages {
		if page.ErrorClass == class {
			return page, nil
		}
	}
	return FailurePage{}, fmt.Errorf("%w: %q", ErrFailureUnknown, class)
}

func (r FailureRegistry) computeDigest() string {
	pages := append([]FailurePage(nil), r.Pages...)
	sort.Slice(pages, func(i, j int) bool { return pages[i].ErrorClass < pages[j].ErrorClass })
	b, err := json.Marshal(pages)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyDigest reports whether the digest matches the seeded content.
func (r FailureRegistry) VerifyDigest() error {
	if r.Digest == "" || r.Digest != r.computeDigest() {
		return fmt.Errorf("%w: failure registry digest does not match its content", ErrFailureInvalid)
	}
	return nil
}
