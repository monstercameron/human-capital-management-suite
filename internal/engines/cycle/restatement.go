package cycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PriorCycleClose is the immutable basis of a correction. A restatement may
// reference a close, but can never rewrite it.
type PriorCycleClose struct {
	TenantID            string
	CycleID             string
	CycleRevisionDigest string
	CloseResultDigest   string
	CloseEvidenceRef    string
	ClosedAt            time.Time
	Sequence            uint64
}

type RestatementImpact struct {
	ResultID              string
	PopulationID          string
	PriorResultDigest     string
	CorrectedResultDigest string
}

type RestatementApproval struct {
	ApprovalID          string
	DecisionDigest      string
	DecisionEvidenceRef string
	ApprovedAt          time.Time
}

type RestatementCompensation struct {
	CompensationID string
	Required       bool
	Planned        bool
	PlanDigest     string
	Completed      bool
	ResultDigest   string
	EvidenceRef    string
}

type ReconciliationOutcome string

const (
	ReconciliationNoChange            ReconciliationOutcome = "NO_CHANGE"
	ReconciliationPendingCompensation ReconciliationOutcome = "PENDING_COMPENSATION"
	ReconciliationReconciled          ReconciliationOutcome = "RECONCILED"
)

// RestatementRequest is a pure append-only correction proposal. It contains
// references and digests, never mutable prior-cycle result contents.
type RestatementRequest struct {
	RestatementID                    string
	Revision                         uint64
	CorrectionAt                     time.Time
	Prior                            PriorCycleClose
	AffectedPopulationManifestDigest string
	AffectedResultManifestDigest     string
	Impacts                          []RestatementImpact
	Approvals                        []RestatementApproval
	Compensations                    []RestatementCompensation
	Outcome                          ReconciliationOutcome
}

type CycleRestatement struct {
	RestatementID                    string
	Revision                         uint64
	CorrectionAt                     time.Time
	Prior                            PriorCycleClose
	AffectedPopulationManifestDigest string
	AffectedResultManifestDigest     string
	Impacts                          []RestatementImpact
	Approvals                        []RestatementApproval
	Compensations                    []RestatementCompensation
	Outcome                          ReconciliationOutcome
	Digest                           string
}

var (
	ErrRestatementInvalid      = errors.New("cycle: restatement request is invalid")
	ErrRestatementPrior        = errors.New("cycle: prior close/result basis is invalid")
	ErrRestatementImpact       = errors.New("cycle: restatement impact is invalid")
	ErrRestatementApproval     = errors.New("cycle: restatement approval is required")
	ErrRestatementCompensation = errors.New("cycle: compensation requirement is invalid")
	ErrRestatementOutcome      = errors.New("cycle: reconciliation outcome is dishonest")
)

func validSHA256Digest(s string) bool {
	algorithm, encoded, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || algorithm != "sha256" || len(encoded) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(encoded)
	return err == nil
}

func (r RestatementRequest) Validate() error {
	if strings.TrimSpace(r.RestatementID) == "" || r.Revision == 0 || r.CorrectionAt.IsZero() {
		return ErrRestatementInvalid
	}
	if strings.TrimSpace(r.Prior.TenantID) == "" || strings.TrimSpace(r.Prior.CycleID) == "" || strings.TrimSpace(r.Prior.CloseEvidenceRef) == "" || !validSHA256Digest(r.Prior.CycleRevisionDigest) || !validSHA256Digest(r.Prior.CloseResultDigest) || r.Prior.ClosedAt.IsZero() || r.Prior.Sequence == 0 || r.CorrectionAt.Before(r.Prior.ClosedAt) {
		return ErrRestatementPrior
	}
	if !validSHA256Digest(r.AffectedPopulationManifestDigest) || !validSHA256Digest(r.AffectedResultManifestDigest) {
		return ErrRestatementImpact
	}
	if len(r.Impacts) == 0 {
		return ErrRestatementImpact
	}
	seenResults, seenPopulations := map[string]struct{}{}, map[string]struct{}{}
	for _, impact := range r.Impacts {
		if strings.TrimSpace(impact.ResultID) == "" || strings.TrimSpace(impact.PopulationID) == "" || !validSHA256Digest(impact.PriorResultDigest) || !validSHA256Digest(impact.CorrectedResultDigest) {
			return ErrRestatementImpact
		}
		if impact.PriorResultDigest == impact.CorrectedResultDigest && r.Outcome != ReconciliationNoChange {
			return ErrRestatementImpact
		}
		if impact.PriorResultDigest != impact.CorrectedResultDigest && r.Outcome == ReconciliationNoChange {
			return ErrRestatementOutcome
		}
		if _, ok := seenResults[impact.ResultID]; ok {
			return ErrRestatementImpact
		}
		seenResults[impact.ResultID] = struct{}{}
		seenPopulations[impact.PopulationID] = struct{}{}
	}
	if len(r.Approvals) == 0 {
		return ErrRestatementApproval
	}
	seenApprovals := map[string]struct{}{}
	for _, approval := range r.Approvals {
		if strings.TrimSpace(approval.ApprovalID) == "" || !validSHA256Digest(approval.DecisionDigest) || strings.TrimSpace(approval.DecisionEvidenceRef) == "" || approval.ApprovedAt.IsZero() || approval.ApprovedAt.After(r.CorrectionAt) {
			return ErrRestatementApproval
		}
		if _, ok := seenApprovals[approval.ApprovalID]; ok {
			return ErrRestatementApproval
		}
		seenApprovals[approval.ApprovalID] = struct{}{}
	}
	if len(r.Compensations) == 0 {
		return ErrRestatementCompensation
	}
	seenCompensations := map[string]struct{}{}
	pending := false
	for _, compensation := range r.Compensations {
		if strings.TrimSpace(compensation.CompensationID) == "" {
			return ErrRestatementCompensation
		}
		if _, ok := seenCompensations[compensation.CompensationID]; ok {
			return ErrRestatementCompensation
		}
		seenCompensations[compensation.CompensationID] = struct{}{}
		if compensation.Planned && !validSHA256Digest(compensation.PlanDigest) {
			return ErrRestatementCompensation
		}
		if compensation.Completed && (!compensation.Planned || !validSHA256Digest(compensation.ResultDigest) || strings.TrimSpace(compensation.EvidenceRef) == "") {
			return ErrRestatementCompensation
		}
		if compensation.Required && !compensation.Completed {
			pending = true
		}
	}
	switch r.Outcome {
	case ReconciliationNoChange:
		// Equal prior/corrected digests explicitly record a reviewed no-op.
	case ReconciliationPendingCompensation:
		if !pending {
			return ErrRestatementOutcome
		}
	case ReconciliationReconciled:
		if pending {
			return ErrRestatementOutcome
		}
	default:
		return ErrRestatementOutcome
	}
	return nil
}

func cloneImpacts(in []RestatementImpact) []RestatementImpact {
	return append([]RestatementImpact(nil), in...)
}
func cloneApprovals(in []RestatementApproval) []RestatementApproval {
	return append([]RestatementApproval(nil), in...)
}
func cloneCompensations(in []RestatementCompensation) []RestatementCompensation {
	return append([]RestatementCompensation(nil), in...)
}

// RestatePriorCycle validates and appends a correction record. No prior close
// or result is mutated, and no completed compensation is synthesized.
func RestatePriorCycle(request RestatementRequest) (CycleRestatement, error) {
	if err := request.Validate(); err != nil {
		return CycleRestatement{}, err
	}
	out := CycleRestatement{RestatementID: request.RestatementID, Revision: request.Revision, CorrectionAt: request.CorrectionAt.UTC(), Prior: request.Prior, AffectedPopulationManifestDigest: request.AffectedPopulationManifestDigest, AffectedResultManifestDigest: request.AffectedResultManifestDigest, Impacts: cloneImpacts(request.Impacts), Approvals: cloneApprovals(request.Approvals), Compensations: cloneCompensations(request.Compensations), Outcome: request.Outcome}
	b, err := out.canonicalBody()
	if err != nil {
		return CycleRestatement{}, fmt.Errorf("%w: canonical encoding: %v", ErrRestatementInvalid, err)
	}
	digest := sha256.Sum256(b)
	out.Digest = "sha256:" + hex.EncodeToString(digest[:])
	return out, nil
}

func (r CycleRestatement) CanonicalDigest() string { return r.Digest }

// Verify checks both the semantic contract and the seal of a previously
// constructed restatement. Callers loading durable records must verify them
// before treating their status or references as trustworthy.
func (r CycleRestatement) Verify() error {
	request := RestatementRequest{RestatementID: r.RestatementID, Revision: r.Revision, CorrectionAt: r.CorrectionAt, Prior: r.Prior, AffectedPopulationManifestDigest: r.AffectedPopulationManifestDigest, AffectedResultManifestDigest: r.AffectedResultManifestDigest, Impacts: cloneImpacts(r.Impacts), Approvals: cloneApprovals(r.Approvals), Compensations: cloneCompensations(r.Compensations), Outcome: r.Outcome}
	if err := request.Validate(); err != nil {
		return err
	}
	b, err := r.canonicalBody()
	if err != nil {
		return fmt.Errorf("%w: canonical encoding: %v", ErrRestatementInvalid, err)
	}
	digest := sha256.Sum256(b)
	if r.Digest != "sha256:"+hex.EncodeToString(digest[:]) {
		return ErrRestatementInvalid
	}
	return nil
}

func (r CycleRestatement) canonicalBody() ([]byte, error) {
	return json.Marshal(struct {
		RestatementID                    string
		Revision                         uint64
		CorrectionAt                     time.Time
		Prior                            PriorCycleClose
		AffectedPopulationManifestDigest string
		AffectedResultManifestDigest     string
		Impacts                          []RestatementImpact
		Approvals                        []RestatementApproval
		Compensations                    []RestatementCompensation
		Outcome                          ReconciliationOutcome
	}{r.RestatementID, r.Revision, r.CorrectionAt, r.Prior, r.AffectedPopulationManifestDigest, r.AffectedResultManifestDigest, r.Impacts, r.Approvals, r.Compensations, r.Outcome})
}

func (r CycleRestatement) Canonical() ([]byte, error) {
	b, err := r.canonicalBody()
	if err != nil {
		return nil, err
	}
	return b, nil
}

// DecodeRestatement reconstructs a sealed restatement from its canonical body
// and separately stored digest. Durable adapters use this to verify records
// after a process restart.
func DecodeRestatement(body []byte, digest string) (CycleRestatement, error) {
	var r CycleRestatement
	if err := json.Unmarshal(body, &r); err != nil {
		return CycleRestatement{}, fmt.Errorf("%w: decode canonical body: %v", ErrRestatementInvalid, err)
	}
	r.Digest = digest
	if err := r.Verify(); err != nil {
		return CycleRestatement{}, err
	}
	return r, nil
}
