package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrPersonaRunEffectivePolicy indicates current tenant policy could not be proven.
var ErrPersonaRunEffectivePolicy = errors.New("application: current persona run policy unavailable")

// PersonaRunEffectivePolicyReader returns the single active tenant/entity
// policy as of an explicit server timestamp. Implementations scope by tenant.
type PersonaRunEffectivePolicyReader interface {
	CurrentPersonaRunPolicy(context.Context, uuid.UUID, string, time.Time) (agentstore.PersonaRunPolicy, error)
}

// PersonaRunEffectivePolicy carries the current, persisted ceilings and a
// server-created deadline for one persona chat run.
type PersonaRunEffectivePolicy struct {
	Budget       agentrun.Budget
	Deadline     time.Time
	PolicyDigest string
	Revision     int64
}

// PersonaRunEffectivePolicyResolver resolves a tenant/legal-entity policy at
// the server's current time. It has no caller-controlled budget or deadline.
type PersonaRunEffectivePolicyResolver struct {
	reader     PersonaRunEffectivePolicyReader
	tenantUUID func(values.TenantId) uuid.UUID
	now        func() time.Time
}

// NewPersonaRunEffectivePolicyResolver requires the current-policy store,
// canonical tenant mapper and trusted clock; absent dependencies fail closed.
func NewPersonaRunEffectivePolicyResolver(reader PersonaRunEffectivePolicyReader, tenantUUID func(values.TenantId) uuid.UUID, now func() time.Time) (*PersonaRunEffectivePolicyResolver, error) {
	if reader == nil || tenantUUID == nil || now == nil {
		return nil, fmt.Errorf("%w: policy reader, tenant mapper and clock are required", ErrPersonaRunEffectivePolicy)
	}
	return &PersonaRunEffectivePolicyResolver{reader: reader, tenantUUID: tenantUUID, now: now}, nil
}

// Resolve returns the exact active policy revision, bounded token/cost limits,
// and deadline computed from its maximum run duration.
func (r *PersonaRunEffectivePolicyResolver) Resolve(ctx context.Context, tenantID, legalEntityID string) (PersonaRunEffectivePolicy, error) {
	if r == nil || r.reader == nil || r.tenantUUID == nil || r.now == nil || ctx == nil || values.TenantId(tenantID).Validate() != nil ||
		strings.TrimSpace(legalEntityID) == "" || strings.TrimSpace(legalEntityID) != legalEntityID {
		return PersonaRunEffectivePolicy{}, fmt.Errorf("%w: tenant and legal entity are required", ErrPersonaRunEffectivePolicy)
	}
	tenant := values.TenantId(tenantID)
	tenantKey := r.tenantUUID(tenant)
	if tenantKey == uuid.Nil {
		return PersonaRunEffectivePolicy{}, fmt.Errorf("%w: tenant is not in the canonical directory", ErrPersonaRunEffectivePolicy)
	}
	now := r.now().UTC()
	if now.IsZero() {
		return PersonaRunEffectivePolicy{}, fmt.Errorf("%w: trusted clock returned zero", ErrPersonaRunEffectivePolicy)
	}
	policy, err := r.reader.CurrentPersonaRunPolicy(ctx, tenantKey, legalEntityID, now)
	if err != nil {
		return PersonaRunEffectivePolicy{}, fmt.Errorf("%w: read current policy: %w", ErrPersonaRunEffectivePolicy, err)
	}
	if policy.TenantID != tenantKey || policy.LegalEntityID != legalEntityID || policy.Revision <= 0 ||
		policy.EffectiveFrom.After(now) || (policy.EffectiveUntil != nil && !policy.EffectiveUntil.After(now)) ||
		policy.MaxCostMicros <= 0 || policy.MaxInputTokens <= 0 || policy.MaxOutputTokens <= 0 || policy.MaxRunDuration <= 0 {
		return PersonaRunEffectivePolicy{}, fmt.Errorf("%w: stored policy is invalid or not effective", ErrPersonaRunEffectivePolicy)
	}
	deadline := now.Add(policy.MaxRunDuration)
	if !deadline.After(now) {
		return PersonaRunEffectivePolicy{}, fmt.Errorf("%w: policy deadline overflowed", ErrPersonaRunEffectivePolicy)
	}
	identity := struct {
		TenantID        string        `json:"tenant_id"`
		LegalEntityID   string        `json:"legal_entity_id"`
		Revision        int64         `json:"revision"`
		EffectiveFrom   time.Time     `json:"effective_from"`
		EffectiveUntil  *time.Time    `json:"effective_until,omitempty"`
		MaxCostMicros   int64         `json:"max_cost_micros"`
		MaxInputTokens  int64         `json:"max_input_tokens"`
		MaxOutputTokens int64         `json:"max_output_tokens"`
		MaxRunDuration  time.Duration `json:"max_run_duration"`
	}{tenantID, legalEntityID, policy.Revision, policy.EffectiveFrom.UTC(), policy.EffectiveUntil, policy.MaxCostMicros, policy.MaxInputTokens, policy.MaxOutputTokens, policy.MaxRunDuration}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return PersonaRunEffectivePolicy{}, fmt.Errorf("%w: encode policy identity: %v", ErrPersonaRunEffectivePolicy, err)
	}
	digest := sha256.Sum256(encoded)
	return PersonaRunEffectivePolicy{
		Budget:   agentrun.Budget{MaxCostMicros: uint64(policy.MaxCostMicros), MaxInputTokens: uint64(policy.MaxInputTokens), MaxOutputTokens: uint64(policy.MaxOutputTokens)},
		Deadline: deadline, PolicyDigest: "sha256:" + hex.EncodeToString(digest[:]), Revision: policy.Revision,
	}, nil
}
