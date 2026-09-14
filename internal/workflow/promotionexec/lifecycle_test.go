package promotionexec

import (
	"errors"
	"testing"
	"time"
)

func approvalAuthority(principal string, class AuthorityClass) ApprovalAuthority {
	return ApprovalAuthority{PrincipalID: principal, Class: class, AuthorityRef: "authority:" + string(class), Scope: "acme/engineering", Active: true}
}

func TestPromotionLifecycleDistinctAuthorityClasses(t *testing.T) {
	finance := approvalAuthority("principal:finance", AuthorityClassFinancePartner)
	manager := approvalAuthority("principal:manager", AuthorityClassCurrentManager)
	if err := ValidatePromotionApprovers([]ApprovalAuthority{finance, manager}); err != nil {
		t.Fatalf("distinct promotion authorities refused: %v", err)
	}
	if err := ValidatePromotionApprovers([]ApprovalAuthority{finance, approvalAuthority("principal:other", AuthorityClassFinancePartner)}); !errors.Is(err, ErrApprovalAuthorityNotDistinct) {
		t.Fatalf("duplicate finance class error = %v, want ErrApprovalAuthorityNotDistinct", err)
	}
	if err := ValidatePromotionApprovers([]ApprovalAuthority{finance, approvalAuthority("principal:finance", AuthorityClassCurrentManager)}); !errors.Is(err, ErrApprovalAuthorityNotDistinct) {
		t.Fatalf("one principal in two classes error = %v, want ErrApprovalAuthorityNotDistinct", err)
	}
}

func TestPromotionApprovalRequirementsCarryAuthorityInvalidatorsAndCrossClassSoD(t *testing.T) {
	when := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	finance, err := CompileFinanceApprovalRequirement("principal:finance", when)
	if err != nil {
		t.Fatalf("finance requirement: %v", err)
	}
	manager, err := CompileManagerApprovalRequirement("principal:manager", when)
	if err != nil {
		t.Fatalf("manager requirement: %v", err)
	}
	if !finance.Separation.OneRequirementPerPrincipal || !manager.Separation.OneRequirementPerPrincipal {
		t.Fatal("promotion requirements do not enforce one principal per authority class")
	}
	for _, requirement := range []struct {
		name string
		got  []string
	}{
		{"finance", []string{"rule.promotion.invalidate.authority_revoked/v1", "rule.promotion.invalidate.deadline/v1"}},
		{"manager", []string{"rule.promotion.invalidate.authority_revoked/v1", "rule.promotion.invalidate.deadline/v1"}},
	} {
		var actual []string
		source := finance
		if requirement.name == "manager" {
			source = manager
		}
		for _, invalidator := range source.Invalidators {
			if invalidator.RuleID != "rule.promotion.invalidate.material_change/v1" {
				actual = append(actual, invalidator.RuleID)
			}
		}
		if len(actual) != len(requirement.got) {
			t.Fatalf("%s invalidators = %v, want %v", requirement.name, actual, requirement.got)
		}
		for _, want := range requirement.got {
			found := false
			for _, got := range actual {
				if got == want {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s invalidators = %v, missing %q", requirement.name, actual, want)
			}
		}
	}
}

func TestPromotionLifecycleCurrentAuthorityMustReproducePinnedBinding(t *testing.T) {
	at := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	pinned := approvalAuthority("principal:manager", AuthorityClassCurrentManager)
	binding := ApprovalAuthorityBinding{ProposalRevisionID: "revision:1", MaterialDigest: "sha256:proposal", RequirementID: ApprovalManager, Authority: pinned}
	if err := ValidateCurrentAuthority(binding, pinned, at); err != nil {
		t.Fatalf("current authority refused: %v", err)
	}
	for name, current := range map[string]ApprovalAuthority{
		"principal changed": {PrincipalID: "principal:other", Class: pinned.Class, AuthorityRef: pinned.AuthorityRef, Scope: pinned.Scope, Active: true},
		"class changed":     {PrincipalID: pinned.PrincipalID, Class: AuthorityClassFinancePartner, AuthorityRef: pinned.AuthorityRef, Scope: pinned.Scope, Active: true},
		"grant changed":     {PrincipalID: pinned.PrincipalID, Class: pinned.Class, AuthorityRef: "authority:revoked", Scope: pinned.Scope, Active: true},
		"scope changed":     {PrincipalID: pinned.PrincipalID, Class: pinned.Class, AuthorityRef: pinned.AuthorityRef, Scope: "acme/other", Active: true},
		"validity changed":  {PrincipalID: pinned.PrincipalID, Class: pinned.Class, AuthorityRef: pinned.AuthorityRef, Scope: pinned.Scope, Active: true, ValidFrom: at.Add(-time.Hour), ValidUntil: at.Add(time.Hour)},
		"revoked":           {PrincipalID: pinned.PrincipalID, Class: pinned.Class, AuthorityRef: pinned.AuthorityRef, Scope: pinned.Scope, Active: false},
	} {
		if err := ValidateCurrentAuthority(binding, current, at); !errors.Is(err, ErrApprovalAuthorityStale) {
			t.Errorf("%s error = %v, want ErrApprovalAuthorityStale", name, err)
		}
	}
}

func TestPromotionLifecycleActionsNeverEditAnImmutableRevision(t *testing.T) {
	s := LifecycleSnapshot{RequestState: PromotionSimulated, RevisionImmutable: true}
	if CanEditProposal(s) {
		t.Fatal("an immutable simulated proposal exposed in-place edit")
	}
	if !CanSupersedePromotion(s) || !CanCancelPromotion(s) {
		t.Fatal("a pre-execution proposal should expose successor and cancellation actions")
	}
	committed := LifecycleSnapshot{RequestState: PromotionCommitted, ExecutionState: PromotionCommitted, RevisionImmutable: true, IrreversibleEffect: true}
	for _, action := range []PromotionAction{PromotionActionEdit, PromotionActionSupersede, PromotionActionWithdraw, PromotionActionCancel} {
		for _, available := range PromotionActionAvailability(committed) {
			if available.Action == action && available.Allowed {
				t.Fatalf("committed proposal exposed %s", action)
			}
		}
	}
	if CanCancelPromotion(LifecycleSnapshot{RequestState: PromotionExecuting, IrreversibleEffect: false, AtSafePoint: false}) {
		t.Fatal("executing proposal without a safe point exposed cancellation")
	}
	if CanSupersedePromotion(LifecycleSnapshot{RequestState: PromotionApproved, ExecutionState: PromotionExecuting}) ||
		CanWithdrawPromotion(LifecycleSnapshot{RequestState: PromotionApproved, ExecutionState: PromotionExecuting}) {
		t.Fatal("an executing proposal exposed pre-execution actions")
	}
	for _, available := range PromotionActionAvailability(LifecycleSnapshot{RequestState: "UNKNOWN"}) {
		if available.Allowed {
			t.Fatalf("unknown lifecycle state exposed %s", available.Action)
		}
	}
	for _, available := range PromotionActionAvailability(LifecycleSnapshot{RequestState: PromotionApproved, ExecutionState: "UNKNOWN"}) {
		if available.Allowed {
			t.Fatalf("unknown execution state exposed %s", available.Action)
		}
	}
	if CanSupersedePromotion(LifecycleSnapshot{RequestState: PromotionApproved, ExecutionState: PromotionExecutionRepairRequired}) {
		t.Fatal("repair-required execution exposed supersession")
	}
}

func TestPromotionLifecyclePassiveWaitMetadataIsTruthful(t *testing.T) {
	wake := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	metadata, err := NewPassiveWaitMetadata("AT_LOCAL_DATE", wake, "America/New_York", "us-federal", "waiting for effective date")
	if err != nil {
		t.Fatalf("NewPassiveWaitMetadata: %v", err)
	}
	if !metadata.Waiting || !metadata.Passive || metadata.WakeAt != wake || metadata.PollAfter <= 0 {
		t.Fatalf("metadata = %+v, want a passive scheduled wait", metadata)
	}
	if err := (PassiveWaitMetadata{Waiting: true, Passive: false, WakeKind: "AT_LOCAL_DATE", WakeAt: wake, Reason: "waiting"}).Validate(); !errors.Is(err, ErrPromotionWaitMetadata) {
		t.Fatalf("active wait metadata error = %v, want ErrPromotionWaitMetadata", err)
	}
	if err := (PassiveWaitMetadata{Waiting: false, Passive: true}).Validate(); !errors.Is(err, ErrPromotionWaitMetadata) {
		t.Fatalf("non-wait passive metadata error = %v, want ErrPromotionWaitMetadata", err)
	}
	if err := (PassiveWaitMetadata{Waiting: true, Passive: true, WakeKind: "AT_LOCAL_DATE", WakeAt: wake, Reason: "waiting"}).Validate(); !errors.Is(err, ErrPromotionWaitMetadata) {
		t.Fatalf("unbound wait metadata error = %v, want ErrPromotionWaitMetadata", err)
	}
}
