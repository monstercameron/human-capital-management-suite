package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ALIGN-032: corrections and repairs travel governed routes. A correction
// names the exact proposal revision and digest it applies to; the router
// admits it onto the work-loop route for its kind only when it targets the
// current revision with intact content. A correction aimed at a superseded
// revision is routed to rebase instead of applied, and content that changed
// under a known revision is refused as tampering. Cross-tenant routing is
// never attempted.

// Correction kinds. The set is closed: a correction is a field amendment,
// a plan repair, or a withdrawal — anything else is not a correction.
const (
	CorrectionCorrectField = "correct_field"
	CorrectionRepairPlan   = "repair_plan"
	CorrectionWithdraw     = "withdraw"
)

// Correction routes. Rebase is the only route for a superseded revision:
// the requester must restate the correction against the current revision.
const (
	RouteCorrect  = "work_loop.correct"
	RouteRepair   = "work_loop.repair"
	RouteWithdraw = "work_loop.withdraw"
	RouteRebase   = "work_loop.rebase"
)

// Correction errors.
var (
	ErrCorrectionInvalid  = errors.New("app: correction request is invalid")
	ErrCorrectionTampered = errors.New("app: correction targets altered content")
)

// CorrectionRequest is one tenant-scoped correction or repair request.
type CorrectionRequest struct {
	Tenant             values.TenantId `json:"tenant"`
	IntentID           string          `json:"intent_id"`
	ProposalRevisionID string          `json:"proposal_revision_id"`
	ProposalDigest     string          `json:"proposal_digest"`
	Kind               string          `json:"kind"`
	Reason             string          `json:"reason"`
	RequestedBy        string          `json:"requested_by"`
	RequestedAt        values.Instant  `json:"requested_at"`
}

// CorrectionDecision is the governed routing outcome for one request.
type CorrectionDecision struct {
	Tenant             values.TenantId `json:"tenant"`
	IntentID           string          `json:"intent_id"`
	ProposalRevisionID string          `json:"proposal_revision_id"`
	ProposalDigest     string          `json:"proposal_digest"`
	Kind               string          `json:"kind"`
	Route              string          `json:"route"`
	Allowed            bool            `json:"allowed"`
	DecidedAt          values.Instant  `json:"decided_at"`
	Digest             string          `json:"digest"`
}

func (r CorrectionRequest) validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrCorrectionInvalid, err)
	}
	for _, req := range []struct{ field, value string }{
		{"intent_id", r.IntentID},
		{"proposal_revision_id", r.ProposalRevisionID},
		{"proposal_digest", r.ProposalDigest},
		{"requested_by", r.RequestedBy},
		{"reason", r.Reason},
	} {
		if strings.TrimSpace(req.value) == "" {
			return fmt.Errorf("%w: %s is required", ErrCorrectionInvalid, req.field)
		}
	}
	switch r.Kind {
	case CorrectionCorrectField, CorrectionRepairPlan, CorrectionWithdraw:
	default:
		return fmt.Errorf("%w: kind %q is not a governed correction", ErrCorrectionInvalid, r.Kind)
	}
	if err := r.RequestedAt.Validate(); err != nil {
		return fmt.Errorf("%w: requested_at: %v", ErrCorrectionInvalid, err)
	}
	return nil
}

// RouteCorrection routes one correction request against the current
// revision of its intent. The current tenant, revision and digest are
// authority inputs: they come from the durable intent state, never from
// the requester.
func RouteCorrection(req CorrectionRequest, currentTenant values.TenantId, currentRevisionID, currentDigest string, now values.Instant) (CorrectionDecision, error) {
	if err := req.validate(); err != nil {
		return CorrectionDecision{}, err
	}
	if err := currentTenant.Validate(); err != nil {
		return CorrectionDecision{}, fmt.Errorf("%w: current tenant: %v", ErrCorrectionInvalid, err)
	}
	if req.Tenant != currentTenant {
		return CorrectionDecision{}, fmt.Errorf("%w: correction crosses tenants", ErrCorrectionInvalid)
	}
	if strings.TrimSpace(currentRevisionID) == "" || strings.TrimSpace(currentDigest) == "" {
		return CorrectionDecision{}, fmt.Errorf("%w: current revision is incomplete", ErrCorrectionInvalid)
	}
	if err := now.Validate(); err != nil {
		return CorrectionDecision{}, fmt.Errorf("%w: decided_at: %v", ErrCorrectionInvalid, err)
	}
	decision := CorrectionDecision{
		Tenant: req.Tenant, IntentID: req.IntentID,
		ProposalRevisionID: req.ProposalRevisionID, ProposalDigest: req.ProposalDigest,
		Kind: req.Kind, DecidedAt: now,
	}
	switch {
	case req.ProposalRevisionID != currentRevisionID:
		// Superseded revision: the only governed route is rebase.
		decision.Route = RouteRebase
		decision.Allowed = false
	case req.ProposalDigest != currentDigest:
		// Known revision, altered content: refuse as tampering.
		return CorrectionDecision{}, fmt.Errorf("%w: revision %q", ErrCorrectionTampered, req.ProposalRevisionID)
	default:
		decision.Allowed = true
		switch req.Kind {
		case CorrectionCorrectField:
			decision.Route = RouteCorrect
		case CorrectionRepairPlan:
			decision.Route = RouteRepair
		case CorrectionWithdraw:
			decision.Route = RouteWithdraw
		}
	}
	decision.Digest = decision.computeDigest()
	return decision, nil
}

func (d CorrectionDecision) computeDigest() string {
	b, err := json.Marshal(struct {
		Tenant             string `json:"tenant"`
		IntentID           string `json:"intent_id"`
		ProposalRevisionID string `json:"proposal_revision_id"`
		ProposalDigest     string `json:"proposal_digest"`
		Kind               string `json:"kind"`
		Route              string `json:"route"`
		Allowed            bool   `json:"allowed"`
	}{d.Tenant.String(), d.IntentID, d.ProposalRevisionID, d.ProposalDigest, d.Kind, d.Route, d.Allowed})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyDecision reports whether the decision digest matches its content.
func (d CorrectionDecision) VerifyDecision() error {
	if d.Digest == "" || d.Digest != d.computeDigest() {
		return fmt.Errorf("%w: decision digest does not match its content", ErrCorrectionInvalid)
	}
	return nil
}

// ExplainCorrection returns a value-free description of the correction
// routes.
func ExplainCorrection() string {
	return "corrections route by kind onto work-loop correct, repair, or withdraw only against the current revision digest; superseded revisions rebase and altered content is refused"
}
