package rules

import (
	"errors"
	"testing"
)

// TestTodo_REV_010_01 proves the approval-time record constructor the
// execution driver re-evaluates against (REV-010-01): a minted record cites
// the exact table version it was decided against and round-trips to a clean
// CONFIRMED verdict, while a missing approval binding or a malformed input
// is a typed refusal rather than a plannable record.
func TestTodo_REV_010_01(t *testing.T) {
	table := PromotionApprovalThresholdTable()

	approved, err := NewApprovedPlan(table, standardInput(), "sha256:governance-approval")
	if err != nil {
		t.Fatalf("NewApprovedPlan: %v", err)
	}
	if approved.Tier != ApprovalTierStandard {
		t.Fatalf("minted tier = %s, want STANDARD", approved.Tier)
	}
	if approved.TableID != PromotionApprovalTableID || approved.TableVersion != PromotionApprovalTableVersion {
		t.Fatalf("minted record cites %s@%s, want the published table", approved.TableID, approved.TableVersion)
	}
	if approved.InputDigest == "" || approved.ApprovalDigest != "sha256:governance-approval" {
		t.Fatalf("minted record drops its digests: %+v", approved)
	}
	confirmed, err := ReevaluatePromotionApproval(table, approved, standardInput())
	if err != nil {
		t.Fatalf("round-trip reevaluation: %v", err)
	}
	if confirmed.Verdict != VerdictConfirmed {
		t.Fatalf("round-trip verdict = %+v, want CONFIRMED", confirmed)
	}

	if _, err := NewApprovedPlan(table, standardInput(), ""); !errors.Is(err, ErrPlanNotApproved) {
		t.Fatalf("empty approval digest = %v, want ErrPlanNotApproved", err)
	}
	if _, err := NewApprovedPlan(table, standardInput(), "   "); !errors.Is(err, ErrPlanNotApproved) {
		t.Fatalf("blank approval digest = %v, want ErrPlanNotApproved", err)
	}
	broken := standardInput()
	broken.BandPosition = BandPositionUnspecified
	if _, err := NewApprovedPlan(table, broken, "sha256:governance-approval"); !errors.Is(err, ErrPromotionInputInvalid) {
		t.Fatalf("malformed input = %v, want ErrPromotionInputInvalid", err)
	}
}
