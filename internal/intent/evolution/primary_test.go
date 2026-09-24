package evolution

import (
	"errors"
	"testing"
)

// TestLiveIntentVersionEvolutionRequiresCompatibleBindingOrSuccessorIntent
// is the PRIMARY test for INTENT-028's dormant policy primitives: it proves
// compatibility classification, supersession record construction, and
// in-place edit detection. It does not exercise a production publish or live
// instance migration path.
func TestLiveIntentVersionEvolutionRequiresCompatibleBindingOrSuccessorIntent(t *testing.T) {
	published := promotionV1()

	// A compatible successor needs no supersession record under the declared
	// policy. This is a classification result, not a production binding.
	compatibleSuccessor := promotionV2CompatibleOptionalAdded()
	compatReport, err := CompatibilityCheck(published, compatibleSuccessor)
	if err != nil {
		t.Fatalf("compatible check: unexpected error: %v", err)
	}
	if !compatReport.OK() {
		t.Fatalf("adding an optional input must be COMPATIBLE, got %s: %+v",
			compatReport.Verdict, compatReport.Violations)
	}
	if _, err := NewSupersessionRecord(compatReport, "unnecessary", "author-1", "approver-1",
		mustInstant(2026, 9, 5), PolicyContinueOnOld); !errors.Is(err, ErrSupersessionRequiresIncompatibility) {
		t.Fatalf("a compatible successor must refuse a supersession record, got %v", err)
	}

	// An incompatible successor can be described by an immutable, digested
	// supersession record. This validates its shape, not approval or application
	// to a live instance.
	incompatibleSuccessor := promotionV2RequiredInputAdded()
	incompatReport, err := CompatibilityCheck(published, incompatibleSuccessor)
	if err != nil {
		t.Fatalf("incompatible check: unexpected error: %v", err)
	}
	if incompatReport.OK() {
		t.Fatal("a new required input must be INCOMPATIBLE")
	}
	if len(incompatReport.Violations) != 1 || incompatReport.Violations[0].Field != "compensation_committee_approval_ref" {
		t.Fatalf("violation must name the exact offending field, got %+v", incompatReport.Violations)
	}

	// Self-approval never lets an incompatible change through.
	if _, err := NewSupersessionRecord(incompatReport, "adds mandatory sign-off",
		"same-principal", "same-principal", mustInstant(2026, 9, 5), PolicyMigrateWithPreview,
	); !errors.Is(err, ErrApproverIsAuthor) {
		t.Fatalf("want ErrApproverIsAuthor for self-approval, got %v", err)
	}

	record, err := NewSupersessionRecord(incompatReport, "adds mandatory compensation-committee sign-off",
		"author-1", "approver-1", mustInstant(2026, 9, 5), PolicyMigrateWithPreview)
	if err != nil {
		t.Fatalf("a properly authored and approved supersession must be legal: %v", err)
	}
	if err := record.Verify(); err != nil {
		t.Fatalf("a freshly minted supersession must verify against its own digest: %v", err)
	}

	// Rollback / rewrite refusal: nothing about a stored record can be
	// edited in place without breaking its digest.
	tampered := record
	tampered.Reason = "a different story, told after the fact"
	if err := tampered.Verify(); !errors.Is(err, ErrSupersessionTampered) {
		t.Fatalf("editing a recorded field in place must be caught by Verify, got %v", err)
	}

	// Path 3 does not exist: a candidate claiming the SAME ref as the
	// published version but different content is refused outright, by
	// digest, whatever CompatibilityCheck would otherwise have said about
	// its content as a genuinely new version.
	editedInPlace := published
	editedInPlace.ApprovalRequired = false
	if err := RefuseInPlaceEdit(published, editedInPlace); !errors.Is(err, ErrInPlaceEdit) {
		t.Fatalf("an in-place edit of a published version must be refused by digest, got %v", err)
	}

	// A genuinely new version (a different, advancing Ref) is never judged
	// as an in-place edit, however different its content.
	if err := RefuseInPlaceEdit(published, incompatibleSuccessor); err != nil {
		t.Fatalf("a new version must not be judged as an in-place edit: %v", err)
	}
}
