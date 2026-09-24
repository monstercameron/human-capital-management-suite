// Package achriskadapter connects the ACH scoring policy to direct-deposit
// change authorization while keeping the two domain packages independent.
package achriskadapter

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod/achrisk"
)

// MapAssessment converts immutable ACH assessment evidence into the
// paymethod authorization vocabulary, bound to the exact proposal and an
// explicit caller-selected freshness window.
func MapAssessment(assessment achrisk.RiskAssessment, proposalDigest string, validUntil time.Time) (paymethod.RiskDecision, error) {
	if err := assessment.Validate(); err != nil {
		return paymethod.RiskDecision{}, fmt.Errorf("achrisk adapter: invalid assessment: %w", err)
	}
	if !validDigest(proposalDigest) || validUntil.IsZero() || !assessment.EvaluatedAt.Before(validUntil) {
		return paymethod.RiskDecision{}, fmt.Errorf("achrisk adapter: proposal and future expiry are required")
	}
	decision := ""
	switch assessment.Decision {
	case achrisk.Alert:
		decision = paymethod.RiskAlert
	case achrisk.Hold:
		decision = paymethod.RiskHold
	case achrisk.Release:
		decision = paymethod.RiskRelease
	case achrisk.Reject:
		decision = paymethod.RiskReject
	default:
		return paymethod.RiskDecision{}, fmt.Errorf("achrisk adapter: unsupported decision %q", assessment.Decision)
	}
	return paymethod.RiskDecision{
		Decision: decision, RuleVersion: assessment.RuleVersion,
		AssessmentDigest: assessment.CanonicalDigest, ProposalDigest: proposalDigest,
		EvaluatedAt: assessment.EvaluatedAt, ValidUntil: validUntil.UTC(),
	}, nil
}

// AuthorizeDirectDepositChange evaluates ACH risk using the actual proposed
// bank-detail change, maps that assessment, then passes it to paymethod's
// activation gate. Expiry is explicit so callers own the risk freshness SLA.
func AuthorizeDirectDepositChange(policy achrisk.Policy, input achrisk.EvaluationInput, req paymethod.ChangeAuthorizationRequest, validUntil time.Time) (paymethod.AuthorizationDecision, achrisk.RiskAssessment, error) {
	change := req.BankDetailChange
	input.PaymentDestinationChange = &change
	input.DestinationChangeDigest = change.CanonicalDigest
	if input.TenantID != change.TenantID {
		return paymethod.AuthorizationDecision{}, achrisk.RiskAssessment{}, fmt.Errorf("achrisk adapter: tenant does not match bank-detail change")
	}
	assessment, err := policy.Evaluate(input)
	if err != nil {
		return paymethod.AuthorizationDecision{}, achrisk.RiskAssessment{}, err
	}
	if assessment.TenantID != change.TenantID || assessment.DestinationChangeDigest != change.CanonicalDigest {
		return paymethod.AuthorizationDecision{}, assessment, fmt.Errorf("achrisk adapter: assessment is not bound to this tenant and bank-detail change")
	}
	riskDecision, err := MapAssessment(assessment, req.Proposal.CanonicalDigest, validUntil)
	if err != nil {
		return paymethod.AuthorizationDecision{}, assessment, err
	}
	req.RiskDecision = riskDecision
	decision, err := paymethod.AuthorizeChange(req)
	return decision, assessment, err
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}
