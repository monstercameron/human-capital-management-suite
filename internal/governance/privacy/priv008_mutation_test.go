package privacy

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_PRIV_008_Mutation is the MUTATION matrix test for PRIV-008. Each
// case moves one semantic input across a decision boundary and proves the
// boundary moves with it.
func TestTodo_PRIV_008_Mutation(t *testing.T) {
	t.Run("remote access grants at high assurance and denies one level below", func(t *testing.T) {
		c := fixtureFTIClassification(t)
		if _, err := AuthorizeFTIAccess(FTIAccessSpec{
			Classification: c, Principal: "tax-admin-7",
			Assurance: trust.AssuranceHigh, Remote: true,
			KeyScope: c.KeyScope, Purpose: "tax_return_processing",
		}); err != nil {
			t.Errorf("AuthorizeFTIAccess(remote, high): %v, want grant", err)
		}
		if _, err := AuthorizeFTIAccess(FTIAccessSpec{
			Classification: c, Principal: "tax-admin-7",
			Assurance: trust.AssuranceSubstantial, Remote: true,
			KeyScope: c.KeyScope, Purpose: "tax_return_processing",
		}); !errors.Is(err, ErrFTIAccessDenied) {
			t.Errorf("AuthorizeFTIAccess(remote, substantial) = %v, want %v", err, ErrFTIAccessDenied)
		}
	})

	t.Run("local access grants at substantial assurance and denies one level below", func(t *testing.T) {
		c := fixtureFTIClassification(t)
		if _, err := AuthorizeFTIAccess(FTIAccessSpec{
			Classification: c, Principal: "tax-admin-7",
			Assurance: trust.AssuranceSubstantial, Remote: false,
			KeyScope: c.KeyScope, Purpose: "tax_return_processing",
		}); err != nil {
			t.Errorf("AuthorizeFTIAccess(local, substantial): %v, want grant", err)
		}
		if _, err := AuthorizeFTIAccess(FTIAccessSpec{
			Classification: c, Principal: "tax-admin-7",
			Assurance: trust.AssuranceLow, Remote: false,
			KeyScope: c.KeyScope, Purpose: "tax_return_processing",
		}); !errors.Is(err, ErrFTIAccessDenied) {
			t.Errorf("AuthorizeFTIAccess(local, low) = %v, want %v", err, ErrFTIAccessDenied)
		}
	})

	t.Run("a key scope off by one character is denied", func(t *testing.T) {
		c := fixtureFTIClassification(t)
		offByOne := c.KeyScope[:len(c.KeyScope)-1] + "X"
		if _, err := AuthorizeFTIAccess(FTIAccessSpec{
			Classification: c, Principal: "tax-admin-7",
			Assurance: trust.AssuranceHigh, Remote: true,
			KeyScope: offByOne, Purpose: "tax_return_processing",
		}); !errors.Is(err, ErrFTIAccessDenied) {
			t.Errorf("AuthorizeFTIAccess(off-by-one scope) = %v, want %v", err, ErrFTIAccessDenied)
		}
	})

	t.Run("a review due exactly at the annual window is recorded, one second later is not", func(t *testing.T) {
		c := fixtureFTIClassification(t)
		reviewedAt := mustInstant(t, fxFTIReviewedAt)
		exactWindow := reviewedAt.Time().Add(ftiReviewWindow)
		dueSec := exactWindow.Unix()
		if _, err := RecordSafeguardReview(SafeguardReviewSpec{
			ReviewID: "review-exact", ActivityRef: c.ActivityDigest,
			ReviewedAt: reviewedAt, Reviewer: "reviewer-1",
			Findings: "NO_FINDINGS",
			NextDue:  mustInstant(t, dueSec),
		}); err != nil {
			t.Errorf("RecordSafeguardReview(due exactly at window): %v, want success", err)
		}
		if _, err := RecordSafeguardReview(SafeguardReviewSpec{
			ReviewID: "review-late", ActivityRef: c.ActivityDigest,
			ReviewedAt: reviewedAt, Reviewer: "reviewer-1",
			Findings: "NO_FINDINGS",
			NextDue:  mustInstant(t, dueSec+1),
		}); !errors.Is(err, ErrFTIBlocked) {
			t.Errorf("RecordSafeguardReview(due one second past window) = %v, want %v", err, ErrFTIBlocked)
		}
	})

	t.Run("a review is current at its due instant and overdue one second after", func(t *testing.T) {
		c := fixtureFTIClassification(t)
		review, err := RecordSafeguardReview(SafeguardReviewSpec{
			ReviewID: "review-status", ActivityRef: c.ActivityDigest,
			ReviewedAt: mustInstant(t, fxFTIReviewedAt), Reviewer: "reviewer-1",
			Findings: "NO_FINDINGS", NextDue: mustInstant(t, fxFTIReviewedAt+30*86400),
		})
		if err != nil {
			t.Fatalf("RecordSafeguardReview: %v", err)
		}
		if got := review.Status(mustInstant(t, fxFTIReviewedAt+30*86400)); got != ReviewCurrent {
			t.Errorf("Status(at due instant) = %s, want %s (inclusive boundary)", got, ReviewCurrent)
		}
		if got := review.Status(mustInstant(t, fxFTIReviewedAt+30*86400+1)); got != ReviewOverdue {
			t.Errorf("Status(one second past due) = %s, want %s", got, ReviewOverdue)
		}
	})

	t.Run("a stale review blocks the boundary even when everything else verifies", func(t *testing.T) {
		boundary := fixtureFTIBoundary(t)
		if err := VerifyBoundary(boundary, mustInstant(t, fxFTIReviewedAt+366*86400)); !errors.Is(err, ErrFTIBlocked) {
			t.Errorf("VerifyBoundary(past-due review) = %v, want %v", err, ErrFTIBlocked)
		}
	})

	t.Run("a disclosure at the head instant appends, one second earlier is refused", func(t *testing.T) {
		c := fixtureFTIClassification(t)
		log := NewFTIDisclosureLog(c)
		var err error
		log, err = log.Append(FTIDisclosureSpec{
			DisclosureID: "d1", At: mustInstant(t, fxFTIDisclosedAt),
			Recipient: "irs-mef", Authority: "auth-1", PayloadDigest: fxFTIPayloadDigest,
		})
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
		if _, err := log.Append(FTIDisclosureSpec{
			DisclosureID: "d2-same", At: mustInstant(t, fxFTIDisclosedAt),
			Recipient: "irs-mef", Authority: "auth-2", PayloadDigest: fxFTIPayloadDigest,
		}); err != nil {
			t.Errorf("Append(at head instant): %v, want success (order is non-decreasing)", err)
		}
		if _, err := log.Append(FTIDisclosureSpec{
			DisclosureID: "d3-early", At: mustInstant(t, fxFTIDisclosedAt-1),
			Recipient: "irs-mef", Authority: "auth-3", PayloadDigest: fxFTIPayloadDigest,
		}); !errors.Is(err, ErrFTIBlocked) {
			t.Errorf("Append(before head): %v, want %v", err, ErrFTIBlocked)
		}
	})

	t.Run("a redisclosure citing an unknown prior disclosure is refused", func(t *testing.T) {
		c := fixtureFTIClassification(t)
		log := NewFTIDisclosureLog(c)
		var err error
		log, err = log.Append(FTIDisclosureSpec{
			DisclosureID: "d1", At: mustInstant(t, fxFTIDisclosedAt),
			Recipient: "irs-mef", Authority: "auth-1", PayloadDigest: fxFTIPayloadDigest,
		})
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
		if _, err := log.Append(FTIDisclosureSpec{
			DisclosureID: "d2-ghost", At: mustInstant(t, fxFTIDisclosedAt+1),
			Recipient: "state-revenue-agency", Authority: "auth-2",
			Redisclosure: true, PriorDisclosure: "d0-never-happened",
			PayloadDigest: fxFTIPayloadDigest,
		}); !errors.Is(err, ErrFTIBlocked) {
			t.Errorf("Append(dangling redisclosure) = %v, want %v", err, ErrFTIBlocked)
		}
	})

	t.Run("a duplicate disclosure id is refused", func(t *testing.T) {
		c := fixtureFTIClassification(t)
		log := NewFTIDisclosureLog(c)
		var err error
		log, err = log.Append(FTIDisclosureSpec{
			DisclosureID: "d1", At: mustInstant(t, fxFTIDisclosedAt),
			Recipient: "irs-mef", Authority: "auth-1", PayloadDigest: fxFTIPayloadDigest,
		})
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
		if _, err := log.Append(FTIDisclosureSpec{
			DisclosureID: "d1", At: mustInstant(t, fxFTIDisclosedAt+1),
			Recipient: "irs-mef", Authority: "auth-2", PayloadDigest: fxFTIPayloadDigest,
		}); !errors.Is(err, ErrFTIBlocked) {
			t.Errorf("Append(duplicate id) = %v, want %v", err, ErrFTIBlocked)
		}
	})
}
