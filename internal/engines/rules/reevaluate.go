package rules

// Material rule re-evaluation before execution (RULE-004): an approved
// plan re-runs its rules against the current table version and inputs at
// execution time. The same material result confirms the plan; moved
// inputs require re-approval even when the tier holds; a moved result
// invalidates the plan for replanning. Every verdict cites the original
// rule version alongside the current one, so the historical approval
// explanation never loses its version.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Re-evaluation verdicts.
const (
	VerdictConfirmed          = "CONFIRMED"
	VerdictReapprovalRequired = "REAPPROVAL_REQUIRED"
	VerdictInvalidated        = "INVALIDATED"
)

var (
	// ErrPlanTampered reports an approved plan whose stored input digest
	// does not reproduce from its inputs.
	ErrPlanTampered = errors.New("rules: approved plan inputs do not reproduce their digest")
	// ErrPlanNotApproved reports re-evaluation of a plan with no approval
	// binding.
	ErrPlanNotApproved = errors.New("rules: plan carries no approval binding")
)

// ApprovedPlan is the frozen approval-time record.
type ApprovedPlan struct {
	Input          PromotionApprovalInput
	InputDigest    string
	Tier           ApprovalTier
	MatchedRowID   string
	TableID        string
	TableVersion   string
	TableDigest    string
	ApprovalDigest string
}

// Reevaluation is one execution-time verdict with both rule versions
// cited as evidence.
type Reevaluation struct {
	Verdict             string
	Tier                ApprovalTier
	MatchedRowID        string
	OriginalTableID     string
	OriginalTableVer    string
	OriginalTableDigest string
	CurrentTableID      string
	CurrentTableVer     string
	CurrentTableDigest  string
	MovedInput          bool
	MovedTable          bool
	Evidence            string
}

// InputDigest mints the canonical digest over one threshold input set. The
// served recorder freezes it beside the inputs so ReevaluatePromotionApproval
// can reproduce it and prove the stored inputs were not tampered with.
func InputDigest(in PromotionApprovalInput) (string, error) {
	return inputDigest(in)
}

func inputDigest(in PromotionApprovalInput) (string, error) {
	if err := in.Validate(); err != nil {
		return "", err
	}
	parts := []string{
		"promotion-input", string(in.IncreasePercent.Canonical()),
		string(in.BandPosition), string(in.BudgetAuthority), fmt.Sprintf("%v", in.GradeChange),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// NewApprovedPlan freezes the approval-time record for one decision the
// caller just evaluated: it re-resolves the tier and matched row against
// table so the stored record always cites the exact version it was decided
// against, and mints the input digest ReevaluatePromotionApproval later
// reproduces to prove the stored inputs were not tampered with. An empty
// approval digest is not an approval at all and is refused outright.
func NewApprovedPlan(table Table, in PromotionApprovalInput, approvalDigest string) (ApprovedPlan, error) {
	if strings.TrimSpace(approvalDigest) == "" {
		return ApprovedPlan{}, ErrPlanNotApproved
	}
	decision, err := EvaluatePromotionApproval(table, in)
	if err != nil {
		return ApprovedPlan{}, err
	}
	digest, err := inputDigest(in)
	if err != nil {
		return ApprovedPlan{}, err
	}
	return ApprovedPlan{
		Input: in, InputDigest: digest, Tier: decision.Tier, MatchedRowID: decision.MatchedRowID,
		TableID: decision.TableID, TableVersion: decision.TableVersion, TableDigest: decision.TableDigest,
		ApprovalDigest: approvalDigest,
	}, nil
}

// ReevaluatePromotionApproval re-runs the approval rules for one approved
// plan against the current table and inputs.
func ReevaluatePromotionApproval(table Table, approved ApprovedPlan, current PromotionApprovalInput) (Reevaluation, error) {
	if approved.ApprovalDigest == "" {
		return Reevaluation{}, ErrPlanNotApproved
	}
	reproduced, err := inputDigest(approved.Input)
	if err != nil {
		return Reevaluation{}, err
	}
	if reproduced != approved.InputDigest {
		return Reevaluation{}, fmt.Errorf("%w: stored %s reproduces %s", ErrPlanTampered, approved.InputDigest, reproduced)
	}
	currentDigest, err := inputDigest(current)
	if err != nil {
		return Reevaluation{}, err
	}
	decision, err := EvaluatePromotionApproval(table, current)
	if err != nil {
		return Reevaluation{}, err
	}
	verdict := Reevaluation{
		Tier: decision.Tier, MatchedRowID: decision.MatchedRowID,
		OriginalTableID: approved.TableID, OriginalTableVer: approved.TableVersion,
		OriginalTableDigest: approved.TableDigest,
		CurrentTableID:      decision.TableID, CurrentTableVer: decision.TableVersion,
		CurrentTableDigest: decision.TableDigest,
		MovedInput:         currentDigest != approved.InputDigest,
		MovedTable:         decision.TableDigest != approved.TableDigest,
		Evidence:           approved.ApprovalDigest,
	}
	switch {
	case decision.Tier == ApprovalTierUnknownBlocked:
		verdict.Verdict = VerdictInvalidated
	case decision.Tier != approved.Tier || decision.MatchedRowID != approved.MatchedRowID:
		verdict.Verdict = VerdictInvalidated
	case verdict.MovedInput:
		// The material inputs moved: the old approval set cannot stay
		// valid on its own, even when the tier holds.
		verdict.Verdict = VerdictReapprovalRequired
	default:
		verdict.Verdict = VerdictConfirmed
	}
	return verdict, nil
}
