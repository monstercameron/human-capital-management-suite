package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
)

const (
	personaPolicySearchOwner     = "documents"
	personaPolicySearchPurpose   = "persona-mention"
	personaPolicySearchScope     = "documents:search"
	personaPolicySearchDataClass = "POLICY_DOCUMENT"
)

var errPersonaPolicySearchRegistration = errors.New("application: policy document search registration unavailable")

// bindPersonaPolicySearchSkill publishes the capability only when its real
// HUB-031 implementation is available, then pins the SkillDefinition derived
// from that capability. The returned pin must be copied into the draft Policy
// Helper starter and its golden; callers must not supply a digest.
func bindPersonaPolicySearchSkill(caps *capability.Registry, skills *agentskills.Registry, searcher PersonaRunT0DocumentSearcher) (agentskills.SkillPin, error) {
	if caps == nil || skills == nil || searcher == nil {
		return agentskills.SkillPin{}, errPersonaPolicySearchRegistration
	}
	definition := personaPolicySearchCapabilityDefinition()
	if err := caps.Register(definition, personaPolicySearchHandler{searcher: searcher}.handle); err != nil {
		return agentskills.SkillPin{}, fmt.Errorf("%w: register capability: %v", errPersonaPolicySearchRegistration, err)
	}
	if err := skills.Publish(personaPolicySearchSkillDefinition()); err != nil {
		return agentskills.SkillPin{}, fmt.Errorf("%w: publish skill: %v", errPersonaPolicySearchRegistration, err)
	}
	pin, err := skills.Pin(agentskills.SkillKey{ID: personaPolicyHelperSkillID, Version: 1})
	if err != nil {
		return agentskills.SkillPin{}, fmt.Errorf("%w: derive skill pin: %v", errPersonaPolicySearchRegistration, err)
	}
	return pin, nil
}

func personaPolicySearchCapabilityDefinition() capability.Definition {
	return capability.Definition{
		ID: personaDocumentSearchCapabilityID, Version: personaDocumentSearchCapabilityVersion, OwnerDomain: personaPolicySearchOwner,
		RequestSchema:  capability.SchemaRef{SchemaID: "hcmnext.document.v1.AgentSearchDocumentsRequest", Version: 1, ProtobufFullName: "hcmnext.document.v1.AgentSearchDocumentsRequest"},
		ResponseSchema: capability.SchemaRef{SchemaID: "hcmnext.persona.v1.PolicyDocumentSearchResult", Version: 1, ProtobufFullName: "google.protobuf.Struct"},
		ErrorSchema:    capability.SchemaRef{SchemaID: "google.rpc.Status", Version: 1, ProtobufFullName: "google.rpc.Status"},
		EffectClass:    capability.EffectReadOnly,
		ReadData:       capability.DataDomainFieldSet{DataDomains: []string{"policy_document"}},
		RiskClass:      "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AgentEligible: true,
		AuthZScopeRef: personaPolicySearchScope, LegalBasisRef: "legal.p1a.observation-only.v1",
		EntitlementRef: "entitlement.pilot.p1a.v1", SLOClassRef: "slo.interactive.p95-2s.v1",
		TestRef: "test:TestTodo_AGENTP_023_PolicySearchSkill",
	}
}

func personaPolicySearchSkillDefinition() agentskills.SkillDefinition {
	return agentskills.SkillDefinition{
		ID: personaPolicyHelperSkillID, Version: 1, Owner: personaPolicySearchOwner,
		Description:    "Search deployed tenant policy documents the requesting user and this installation may read. Returns exact document-version citations.",
		InputSchema:    personaDocumentSearchSchema,
		OutputSchema:   json.RawMessage(`{"type":"object","properties":{"Hits":{"type":"array","items":{"type":"object","properties":{"DocumentID":{"type":"string"},"VersionID":{"type":"string"},"Title":{"type":"string"},"Status":{"type":"string"},"Locale":{"type":"string"},"OwnerID":{"type":"string"},"DeployedAt":{"type":"string","format":"date-time"},"Score":{"type":"integer"},"MatchedTerms":{"type":"integer"},"Markdown":{"type":"string"},"ContentDigest":{"type":"string"},"Classification":{"type":"string"},"PlacementID":{"type":"string"},"ScopeID":{"type":"string"}},"required":["DocumentID","VersionID","Title","Status","Locale","OwnerID","DeployedAt","Score","MatchedTerms"],"additionalProperties":false}},"Total":{"type":"integer"}},"required":["Hits","Total"],"additionalProperties":false}`),
		Operations:     []agentskills.OperationRef{{Kind: agentskills.OperationCapability, Capability: capability.Key{ID: personaDocumentSearchCapabilityID, Version: personaDocumentSearchCapabilityVersion}}},
		SideEffectTier: agentskills.TierT0, RequiredPurposes: []string{personaPolicySearchPurpose},
		DataClassesRead: []string{personaPolicySearchDataClass}, IdempotencyRule: "read-only", CostClass: "LOW",
		EvalRefs: []string{"AGENTP-021.policy_helper"},
	}
}

type personaPolicySearchHandler struct {
	searcher PersonaRunT0DocumentSearcher
}

func (h personaPolicySearchHandler) handle(ctx context.Context, payload any) (any, error) {
	call, ok := payload.(personaDocumentSearchCall)
	if ctx == nil || !ok || h.searcher == nil || call.TenantID.Validate() != nil ||
		!personaPolicySearchValue(call.ConversationID, 128) || !personaPolicySearchValue(call.AgentID, 128) ||
		!personaPolicySearchValue(call.InvokerID, 128) || !personaPolicySearchValue(call.Query, 500) ||
		strings.TrimSpace(call.Query) != call.Query || !personaPolicySearchFiltersValid(call.Filters) {
		return nil, errPersonaPolicySearchRegistration
	}
	if _, ok := ctx.Value(personaDocumentSearchContextKey{}).(personaDocumentSearchContext); ok {
		if scoped, ok := h.searcher.(interface {
			SearchPersonaPolicyDocuments(context.Context, personaDocumentSearchCall) (PersonaPolicyDocumentSearchResult, error)
		}); ok {
			return scoped.SearchPersonaPolicyDocuments(ctx, call)
		}
		return nil, errPersonaPolicySearchRegistration
	}
	result, err := h.searcher.AgentSearchDocuments(ctx, call.TenantID.String(), call.ConversationID, call.AgentID, call.InvokerID, call.Query, call.Filters)
	if err != nil {
		return nil, fmt.Errorf("%w: HUB-031 search refused", errPersonaPolicySearchRegistration)
	}
	return result, nil
}

func personaPolicySearchFiltersValid(filters transportdocument.SearchFilters) bool {
	return filters.Status == "" && filters.OwnerID == "" && filters.Locale == "" && filters.DateFrom.IsZero() && filters.DateTo.IsZero() &&
		!(filters.TeamID != "" && filters.ChannelID != "") && len(filters.TeamID) <= 128 && len(filters.ChannelID) <= 128
}

func personaPolicySearchValue(value string, max int) bool {
	return strings.TrimSpace(value) != "" && value == strings.TrimSpace(value) && len(value) <= max
}
