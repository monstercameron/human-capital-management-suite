package cba

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payinput"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CheckoffAction is an evidenced authorization or revocation instrument.
type CheckoffAction string

const (
	CheckoffAuthorize CheckoffAction = "AUTHORIZE"
	CheckoffRevoke    CheckoffAction = "REVOKE"
)

// CheckoffState is the complete resolution vocabulary. Unknown policy evidence
// is deliberately distinct from a legal conclusion.
type CheckoffState string

const (
	CheckoffAuthorized    CheckoffState = "AUTHORIZED"
	CheckoffRevoked       CheckoffState = "REVOKED"
	CheckoffAgencyFeeOnly CheckoffState = "AGENCY_FEE_ONLY"
	CheckoffProhibited    CheckoffState = "PROHIBITED_BY_JURISDICTION"
	CheckoffUnknown       CheckoffState = "UNKNOWN"
)

var ErrInvalidCheckoff = errors.New("cba: invalid dues checkoff")

// CheckoffAuthorizationRevision binds an instrument to a CBA membership and an
// effective window. Each semantic change is represented by a new revision.
type CheckoffAuthorizationRevision struct {
	ID, AuthorizationID, Revision                string
	WorkerID, MembershipID, UnitID               string
	Action                                       CheckoffAction
	InstrumentRef, EvidenceRef, SupersedesDigest string
	EffectiveFrom, EffectiveTo                   time.Time
	KnownFrom, KnownTo                           time.Time
}

func (r CheckoffAuthorizationRevision) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.AuthorizationID) == "" || strings.TrimSpace(r.Revision) == "" ||
		strings.TrimSpace(r.WorkerID) == "" || strings.TrimSpace(r.MembershipID) == "" || strings.TrimSpace(r.UnitID) == "" ||
		strings.TrimSpace(r.InstrumentRef) == "" || strings.TrimSpace(r.EvidenceRef) == "" || r.EffectiveFrom.IsZero() || r.KnownFrom.IsZero() ||
		(!r.EffectiveTo.IsZero() && !r.EffectiveTo.After(r.EffectiveFrom)) ||
		(!r.KnownTo.IsZero() && !r.KnownTo.After(r.KnownFrom)) ||
		(r.Action != CheckoffAuthorize && r.Action != CheckoffRevoke) {
		return ErrInvalidCheckoff
	}
	revision, err := strconv.ParseUint(r.Revision, 10, 64)
	if err != nil || revision == 0 || (revision == 1) != (r.SupersedesDigest == "") {
		return ErrInvalidCheckoff
	}
	return nil
}

func (r CheckoffAuthorizationRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	w := canonicalbytes.New("hcmnext.domains.cba.CheckoffAuthorizationRevision", 1).
		String("id", r.ID).String("authorization", r.AuthorizationID).String("revision", r.Revision).
		String("worker", r.WorkerID).String("membership", r.MembershipID).String("unit", r.UnitID).
		String("action", string(r.Action)).String("instrument", r.InstrumentRef).String("evidence", r.EvidenceRef).
		String("supersedes_digest", r.SupersedesDigest).
		String("effective_from", r.EffectiveFrom.UTC().Format(time.RFC3339Nano)).
		String("effective_to", r.EffectiveTo.UTC().Format(time.RFC3339Nano)).
		String("known_from", r.KnownFrom.UTC().Format(time.RFC3339Nano)).
		String("known_to", r.KnownTo.UTC().Format(time.RFC3339Nano))
	b, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(b), nil
}

// CheckoffPolicyEvidence is supplied by an authorized jurisdiction-policy
// source. This package contains no agency-fee or deduction law assumptions.
type CheckoffPolicyEvidence struct {
	State                                          CheckoffState
	Jurisdiction, PolicyRef, EvidenceRef           string
	WorkerID, MembershipID, JurisdictionBindingRef string
	TenantID                                       string
}

func (p CheckoffPolicyEvidence) validFor(tenantID, workerID, membershipID string) bool {
	return strings.TrimSpace(p.Jurisdiction) != "" && strings.TrimSpace(p.PolicyRef) != "" &&
		strings.TrimSpace(p.EvidenceRef) != "" && strings.TrimSpace(p.JurisdictionBindingRef) != "" &&
		p.TenantID == tenantID && p.WorkerID == workerID && p.MembershipID == membershipID &&
		(strings.TrimSpace(workerID) != "" && strings.TrimSpace(membershipID) != "") &&
		(p.State == CheckoffAuthorized || p.State == CheckoffAgencyFeeOnly || p.State == CheckoffProhibited)
}

type CheckoffResolution struct {
	State                          CheckoffState
	AuthorizationDigest            string
	PolicyRef, PolicyEvidenceRef   string
	HistoryRef, HistoryEvidenceRef string
	workerID, membershipID         string
	tenantID                       string
}

// CheckoffAuthorizationHistory is a complete, tenant-scoped history snapshot.
// The caller must load it from an authoritative CBA source. The pure domain
// validates its contiguous revision chain and snapshot evidence before use.
type CheckoffAuthorizationHistory struct {
	TenantID, AuthorizationID, CurrentRevision string
	HeadDigest, HistoryRef, EvidenceRef        string
	Revisions                                  []CheckoffAuthorizationRevision
}

func (h CheckoffAuthorizationHistory) Validate() error {
	if strings.TrimSpace(h.TenantID) == "" || strings.TrimSpace(h.AuthorizationID) == "" || strings.TrimSpace(h.HeadDigest) == "" || strings.TrimSpace(h.HistoryRef) == "" || strings.TrimSpace(h.EvidenceRef) == "" || len(h.Revisions) == 0 {
		return ErrInvalidCheckoff
	}
	revisions := append([]CheckoffAuthorizationRevision(nil), h.Revisions...)
	sort.Slice(revisions, func(i, j int) bool {
		a, _ := strconv.ParseUint(revisions[i].Revision, 10, 64)
		b, _ := strconv.ParseUint(revisions[j].Revision, 10, 64)
		return a < b
	})
	for i, r := range revisions {
		if err := r.Validate(); err != nil || r.AuthorizationID != h.AuthorizationID {
			return ErrInvalidCheckoff
		}
		if i > 0 {
			prev := revisions[i-1]
			previousNumber, _ := strconv.ParseUint(prev.Revision, 10, 64)
			currentNumber, _ := strconv.ParseUint(r.Revision, 10, 64)
			previousDigest, err := prev.Digest()
			if err != nil || currentNumber != previousNumber+1 || r.ID == prev.ID || r.SupersedesDigest != previousDigest || r.WorkerID != prev.WorkerID || r.MembershipID != prev.MembershipID || r.UnitID != prev.UnitID || r.KnownFrom.Before(prev.KnownFrom) {
				return ErrInvalidCheckoff
			}
		}
	}
	if revisions[0].Revision != "1" || revisions[len(revisions)-1].Revision != h.CurrentRevision {
		return ErrInvalidCheckoff
	}
	headDigest, err := revisions[len(revisions)-1].Digest()
	if err != nil || headDigest != h.HeadDigest {
		return ErrInvalidCheckoff
	}
	return nil
}

// ResolveCheckoff fails closed on absent or mismatched membership and policy
// evidence. EffectiveAt and knownAt are supplied by the caller for deterministic
// bitemporal evaluation.
func ResolveCheckoff(history CheckoffAuthorizationHistory, m MembershipRevision, membershipTenantID string, policy CheckoffPolicyEvidence, effectiveAt, knownAt time.Time) (CheckoffResolution, error) {
	if err := history.Validate(); err != nil {
		return CheckoffResolution{}, err
	}
	if effectiveAt.IsZero() || knownAt.IsZero() {
		return CheckoffResolution{}, fmt.Errorf("%w: effective and known evaluation times required", ErrInvalidCheckoff)
	}
	var selected *CheckoffAuthorizationRevision
	for i := range history.Revisions {
		r := &history.Revisions[i]
		if knownAt.Before(r.KnownFrom) || (!r.KnownTo.IsZero() && !knownAt.Before(r.KnownTo)) || effectiveAt.Before(r.EffectiveFrom) {
			continue
		}
		if selected == nil || r.EffectiveFrom.After(selected.EffectiveFrom) || (r.EffectiveFrom.Equal(selected.EffectiveFrom) && revisionNumber(r.Revision) > revisionNumber(selected.Revision)) {
			selected = r
		}
	}
	if selected == nil {
		return CheckoffResolution{State: CheckoffUnknown}, nil
	}
	r := *selected
	digest, err := r.Digest()
	if err != nil {
		return CheckoffResolution{}, err
	}
	out := CheckoffResolution{State: CheckoffUnknown, AuthorizationDigest: digest, HistoryRef: history.HistoryRef, HistoryEvidenceRef: history.EvidenceRef, workerID: r.WorkerID, membershipID: r.MembershipID, tenantID: history.TenantID}
	if !r.EffectiveTo.IsZero() && !effectiveAt.Before(r.EffectiveTo) {
		return out, nil
	}
	if err := m.Validate(); err != nil {
		return out, nil
	}
	if membershipTenantID != history.TenantID || r.WorkerID != m.WorkerID || r.MembershipID != m.MembershipID || r.UnitID != m.UnitID || !m.active(effectiveAt, knownAt) ||
		effectiveAt.Before(r.EffectiveFrom) {
		return out, nil
	}
	if r.Action == CheckoffRevoke {
		out.State = CheckoffRevoked
		return out, nil
	}
	if !policy.validFor(history.TenantID, r.WorkerID, r.MembershipID) {
		return out, nil
	}
	out.PolicyRef, out.PolicyEvidenceRef = policy.PolicyRef, policy.EvidenceRef
	out.State = policy.State
	return out, nil
}

func revisionNumber(revision string) uint64 { n, _ := strconv.ParseUint(revision, 10, 64); return n }

// BuildCheckoffDeduction projects only an evaluated active authorization into
// one bounded pay-input assignment. Worker and membership come from the
// evaluated revision so callers cannot substitute another worker.
func BuildCheckoffDeduction(resolution CheckoffResolution, definition payinput.Definition, unionRef, periodID string, amount values.Decimal, effective values.EffectiveInterval) (payinput.CheckoffDeduction, error) {
	if resolution.State != CheckoffAuthorized || resolution.workerID == "" || resolution.membershipID == "" || resolution.AuthorizationDigest == "" {
		return payinput.CheckoffDeduction{}, ErrInvalidCheckoff
	}
	return payinput.NewCheckoffDeduction(definition, payinput.CheckoffDeductionSource{
		State: payinput.CheckoffAuthorizationActive, WorkerRef: resolution.workerID,
		MembershipID: resolution.membershipID, AuthorizationDigest: resolution.AuthorizationDigest, TenantID: resolution.tenantID,
		UnionRef: unionRef, PeriodID: periodID, Amount: amount, Effective: effective,
	})
}
