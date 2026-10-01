package app

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/crm"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CRM005CapabilityID is the application-owned execution seam for prospect
// conversion. It is published for discovery as an internal mutation, while
// the P1A gateway continues to refuse it; execution is entered by the
// governed application step below.
const CRM005CapabilityID = crm.CRM005IntentType

// CRM005ConversionInvocation is the trusted application-step input. The
// identity owner is a server-composed port, so callers provide an external
// identity address but cannot provide the link collection or canonical person
// as authority.
type CRM005ConversionInvocation struct {
	Proposal       crm.CandidateConversionProposal
	IdentityOwner  crm.IdentityLinkOwner
	Tenant         string
	Purpose        string
	ExternalSystem string
	ExternalID     string
	Fence          crm.ConversionCommitFence
	IdempotencyKey string
}

// CRM005ConversionService composes CRM's semantic executor with the
// recruiting writer owned by the application cell.
type CRM005ConversionService struct {
	store *crm.ConversionStore
}

// NewCRM005ConversionService returns a composed CRM-005 execution service.
func NewCRM005ConversionService(store *crm.ConversionStore) *CRM005ConversionService {
	return &CRM005ConversionService{store: store}
}

// Execute resolves the canonical person through MODEL-022 and then enters
// CRM's fenced, idempotent conversion executor. No role is written until both
// identity resolution and governed-intent authorization have succeeded.
func (s *CRM005ConversionService) Execute(ctx context.Context, in CRM005ConversionInvocation) (crm.ConversionResult, error) {
	if s == nil || s.store == nil {
		return crm.ConversionResult{}, fmt.Errorf("%w: CRM-005 service is unavailable", crm.ErrCRM005Rejected)
	}
	tenant := values.TenantId(in.Tenant)
	identity, err := crm.ResolveConversionIdentity(ctx, in.IdentityOwner, tenant, in.Purpose, in.ExternalSystem, in.ExternalID, in.Proposal.At)
	if err != nil {
		return crm.ConversionResult{}, err
	}
	return crm.ExecuteProspectConversionWithIdentity(ctx, in.Proposal, identity, s.store, s.store, in.Fence, in.IdempotencyKey)
}

// crm005CapabilityDefinition is deliberately an INTERNAL_MUTATION. Keeping
// the effect metadata honest means the P1A gateway refuses direct invocation;
// the app step is the only path that can supply the execution fence and
// governed identity owner.
func crm005CapabilityDefinition() capability.Definition {
	return capability.Definition{
		ID: CRM005CapabilityID, Version: 1, OwnerDomain: "recruiting",
		RequestSchema:  capability.SchemaRef{SchemaID: "hcmnext.recruiting.v1.CreateCandidateRequest", Version: 1, ProtobufFullName: "hcmnext.recruiting.v1.CreateCandidateRequest"},
		ResponseSchema: capability.SchemaRef{SchemaID: "hcmnext.recruiting.v1.CreateCandidateResponse", Version: 1, ProtobufFullName: "hcmnext.recruiting.v1.CreateCandidateResponse"},
		ErrorSchema:    capability.SchemaRef{SchemaID: "hcmnext.recruiting.v1.CreateCandidateError", Version: 1, ProtobufFullName: "hcmnext.recruiting.v1.CreateCandidateError"},
		EffectClass:    capability.EffectInternalMutation,
		ReadData:       capability.DataDomainFieldSet{DataDomains: []string{"prospect", "identity_link", "consent"}},
		WriteData:      capability.DataDomainFieldSet{DataDomains: []string{"candidate", "application", "identity_link"}},
		RiskClass:      "R3", IdempotencyPolicyRef: "idempotency.tenant-canonical-request.v1",
		AuthZScopeRef: "scope:recruiting.candidate_conversion", LegalBasisRef: "legal.recruiting.conversion.v1",
		EntitlementRef: "entitlement.pilot.p1b.v1", SLOClassRef: "slo.interactive.p95-2s.v1",
		TestRef: "conformance:CRM-005/served/v1",
	}
}
