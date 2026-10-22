//go:build !(js && wasm)

// CHATBUG-014: server-side only. Planned writes belong to the intent engine,
// which the browser client must not carry.

package compensation

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const BasePayField = "compensation.base_pay"

var (
	ErrPromotionCompensationInvalid  = errors.New("compensation: promotion mutation is invalid")
	ErrPromotionCompensationCurrency = errors.New("compensation: promotion currency mismatch")
	ErrPromotionCompensationOverlap  = errors.New("compensation: promotion effective interval overlaps a future revision")
	ErrPromotionCompensationNoChange = errors.New("compensation: promotion does not change base pay")
)

// PromotionMutation is the Compensation-owned base-pay write. The interval is
// explicit and exact; no currency conversion or bonus component is inferred.
type PromotionMutation struct {
	Tenant             values.TenantId
	ProposalRevisionID string
	ProposalDigest     string
	ActorPrincipalID   string
	AuthorityDecision  string
	WorkerID           string
	PackageID          string
	ComponentID        string
	ResourceKey        values.ResourceKey
	Subject            intent.SubjectReference
	Effective          values.EffectiveInterval
	ExpectedRevision   values.RevisionToken
	CurrentBasePay     values.Money
	TargetBasePay      values.Money
	ExistingIntervals  []values.EffectiveInterval
}

func (m PromotionMutation) Validate() error {
	if err := m.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrPromotionCompensationInvalid, err)
	}
	for name, value := range map[string]string{"proposal_revision_id": m.ProposalRevisionID, "proposal_digest": m.ProposalDigest,
		"actor_principal_id": m.ActorPrincipalID, "authority_decision": m.AuthorityDecision, "worker_id": m.WorkerID,
		"package_id": m.PackageID, "component_id": m.ComponentID} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrPromotionCompensationInvalid, name)
		}
	}
	if err := m.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrPromotionCompensationInvalid, err)
	}
	if m.Subject.Kind != "worker" {
		return fmt.Errorf("%w: subject kind %q is not a worker", ErrPromotionCompensationInvalid, m.Subject.Kind)
	}
	if m.Subject.SubjectID != m.WorkerID {
		return fmt.Errorf("%w: subject is not the worker", ErrPromotionCompensationInvalid)
	}
	if err := m.ResourceKey.Validate(); err != nil || m.ResourceKey.Tenant != m.Tenant {
		return fmt.Errorf("%w: resource key is outside tenant", ErrPromotionCompensationInvalid)
	}
	if err := m.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrPromotionCompensationInvalid, err)
	}
	if !m.ExpectedRevision.IsSpecified() {
		return fmt.Errorf("%w: expected revision is required", ErrPromotionCompensationInvalid)
	}
	if err := m.CurrentBasePay.Validate(); err != nil {
		return fmt.Errorf("%w: current base pay: %v", ErrPromotionCompensationInvalid, err)
	}
	if err := m.TargetBasePay.Validate(); err != nil {
		return fmt.Errorf("%w: target base pay: %v", ErrPromotionCompensationInvalid, err)
	}
	if m.CurrentBasePay.Currency() != m.TargetBasePay.Currency() {
		return ErrPromotionCompensationCurrency
	}
	if string(m.CurrentBasePay.Canonical()) == string(m.TargetBasePay.Canonical()) {
		return ErrPromotionCompensationNoChange
	}
	for i, existing := range m.ExistingIntervals {
		overlaps, err := m.Effective.Overlaps(existing)
		if err != nil {
			return fmt.Errorf("%w: interval %d: %v", ErrPromotionCompensationInvalid, i, err)
		}
		if overlaps {
			return fmt.Errorf("%w: interval %d", ErrPromotionCompensationOverlap, i)
		}
	}
	return nil
}

func (m PromotionMutation) PlannedWrites() ([]intent.PlannedWrite, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return []intent.PlannedWrite{{Subject: m.Subject, ResourceKey: m.ResourceKey, FieldPath: BasePayField,
		CurrentCanonicalText: m.CurrentBasePay.String(), ProposedCanonicalText: m.TargetBasePay.String(),
		SourceAuthorityDecision: m.AuthorityDecision, ExpectedRevision: m.ExpectedRevision}}, nil
}

func (m PromotionMutation) Explain() string {
	return fmt.Sprintf("compensation promotion proposal=%s worker=%s package=%s component=%s effective=%s currency=%s",
		m.ProposalRevisionID, m.WorkerID, m.PackageID, m.ComponentID, m.Effective, m.TargetBasePay.Currency())
}

func ExplainPromotion(m PromotionMutation) string { return m.Explain() }
