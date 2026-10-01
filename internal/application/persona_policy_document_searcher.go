package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
)

// Candidate evaluation supplies this authority from its reviewed provision;
// production uses the current durable published profile and delegation policy.
type PersonaRuntimeToolPinAuthority interface {
	ResolvePersonaRuntimeToolPins(context.Context, agentrun.Record, runstate.Run) ([]agentskills.SkillPin, error)
}
type PersonaDocumentSearchScope struct{ ScopeID string }
type PersonaDocumentSearchScopeSource interface {
	ResolvePersonaDocumentSearchScope(context.Context, PersonaRunT0ToolInvocation) (PersonaDocumentSearchScope, error)
}
type personaDocumentSearchContextKey struct{}
type personaDocumentSearchContext struct {
	identity PersonaRunT0ToolInvocation
	source   PersonaDocumentSearchScopeSource
}

func (p *DatabasePersonaT0SkillPolicy) ResolvePersonaDocumentSearchScope(ctx context.Context, id PersonaRunT0ToolInvocation) (PersonaDocumentSearchScope, error) {
	if p == nil || ctx == nil {
		return PersonaDocumentSearchScope{}, errPersonaRuntimeTools
	}
	request, ok := ctx.Value(personaBackgroundAdmissionKey{}).(agentrun.Request)
	if !ok || request.Persona == nil || request.Source.TenantID != id.TenantID || request.Source.Key != id.InvocationID || request.Principal.InvokerID != id.InvokerID || request.Audience.ID != id.ConversationID || request.Context.ID != id.ThreadID || request.Persona.ID != id.PersonaID || request.Persona.Version != id.PersonaVersion || request.InstallationID != id.InstallationID || request.Agent.AgentID != id.AgentID {
		return PersonaDocumentSearchScope{}, errPersonaRuntimeTools
	}
	invocation := personaRunInvocation(request)
	store, err := p.grantStores.ForTenant(ctx, values.TenantId(id.TenantID))
	if err != nil || isNilPersonaOutputPort(store) {
		return PersonaDocumentSearchScope{}, errPersonaRuntimeTools
	}
	grant, err := store.Get(request.Principal.DelegatedCredentialRef)
	if err != nil {
		return PersonaDocumentSearchScope{}, errPersonaRuntimeTools
	}
	bindForegroundGrant(&invocation, grant)
	allowed, err := p.IsBoundT0Run(ctx, invocation)
	if err != nil || !allowed {
		return PersonaDocumentSearchScope{}, errPersonaRuntimeTools
	}
	reader, err := p.personas.ForTenant(ctx, values.TenantId(id.TenantID))
	if err != nil || isNilPersonaOutputPort(reader) {
		return PersonaDocumentSearchScope{}, errPersonaRuntimeTools
	}
	_, installation, err := reader.ReadCurrentPersonaAuthority(ctx, id.ConversationID, id.PersonaID)
	if err != nil || installation.InstallationID != id.InstallationID || !installation.ChannelPolicy.ConversationSearchAllowed {
		return PersonaDocumentSearchScope{}, errPersonaRuntimeTools
	}
	return PersonaDocumentSearchScope{ScopeID: id.ConversationID}, nil
}

type PersonaPolicyDocumentSearchHit struct {
	transportdocument.SearchHit
	Markdown       string `json:"Markdown,omitempty"`
	ContentDigest  string `json:"ContentDigest,omitempty"`
	Classification string `json:"Classification,omitempty"`
	PlacementID    string `json:"PlacementID,omitempty"`
	ScopeID        string `json:"ScopeID,omitempty"`
}
type PersonaPolicyDocumentSearchResult struct {
	Hits  []PersonaPolicyDocumentSearchHit
	Total int
}

type PersonaPolicyDocumentSearcher struct {
	fallback  PersonaRunT0DocumentSearcher
	documents *documenthubstore.Store
}

// NewPersonaPolicyDocumentSearcher keeps native app calls on their existing
// owner and routes locally authorized persona calls through real placements.
func NewPersonaPolicyDocumentSearcher(fallback PersonaRunT0DocumentSearcher, documents *documenthubstore.Store) (*PersonaPolicyDocumentSearcher, error) {
	if isNilPersonaOutputPort(fallback) || documents == nil {
		return nil, errPersonaRuntimeTools
	}
	return &PersonaPolicyDocumentSearcher{fallback: fallback, documents: documents}, nil
}
func (s *PersonaPolicyDocumentSearcher) AgentSearchDocuments(ctx context.Context, tenant, conversation, agent, invoker, query string, filters transportdocument.SearchFilters) (transportdocument.SearchResult, error) {
	return s.fallback.AgentSearchDocuments(ctx, tenant, conversation, agent, invoker, query, filters)
}
func (s *PersonaPolicyDocumentSearcher) SearchPersonaPolicyDocuments(ctx context.Context, call personaDocumentSearchCall) (PersonaPolicyDocumentSearchResult, error) {
	if s == nil || ctx == nil || s.documents == nil {
		return PersonaPolicyDocumentSearchResult{}, errPersonaRuntimeTools
	}
	binding, ok := ctx.Value(personaDocumentSearchContextKey{}).(personaDocumentSearchContext)
	if !ok || isNilPersonaOutputPort(binding.source) || binding.identity.TenantID != call.TenantID.String() || binding.identity.ConversationID != call.ConversationID || binding.identity.InvokerID != call.InvokerID || binding.identity.AgentID != call.AgentID {
		return PersonaPolicyDocumentSearchResult{}, errPersonaRuntimeTools
	}
	scope, err := binding.source.ResolvePersonaDocumentSearchScope(ctx, binding.identity)
	if err != nil || scope.ScopeID != call.ConversationID || call.Filters.TeamID != "" && call.Filters.TeamID != scope.ScopeID || call.Filters.ChannelID != "" && call.Filters.ChannelID != scope.ScopeID {
		return PersonaPolicyDocumentSearchResult{}, errPersonaRuntimeTools
	}
	hits, err := s.documents.SearchOfficialPlacementLexical(ctx, call.TenantID.String(), scope.ScopeID, call.Query, "person", call.InvokerID)
	if err != nil {
		return PersonaPolicyDocumentSearchResult{}, errPersonaRuntimeTools
	}
	result := PersonaPolicyDocumentSearchResult{Hits: []PersonaPolicyDocumentSearchHit{}}
	if len(hits) > 5 {
		hits = hits[:5]
	}
	for _, hit := range hits {
		placement, err := s.documents.CurrentPlacement(ctx, call.TenantID.String(), hit.DocumentID, "placement", scope.ScopeID)
		if err != nil || !placement.IsOfficialPlacement() || placement.VersionID != hit.VersionID {
			return PersonaPolicyDocumentSearchResult{}, errPersonaRuntimeTools
		}
		version, err := s.documents.ReadVersion(ctx, call.TenantID.String(), hit.DocumentID, hit.VersionID, "person", call.InvokerID)
		if err != nil || len(version.Markdown) == 0 || len(version.Markdown) > 10000 {
			return PersonaPolicyDocumentSearchResult{}, errPersonaRuntimeTools
		}
		result.Hits = append(result.Hits, PersonaPolicyDocumentSearchHit{SearchHit: transportdocument.SearchHit{DocumentID: hit.DocumentID, VersionID: hit.VersionID, Title: hit.Title, Locale: version.Locale, OwnerID: hit.OwnerID, Status: "deployed", DeployedAt: placement.EffectiveAt, Score: hit.Score, MatchedTerms: hit.MatchedTerms}, Markdown: version.Markdown, ContentDigest: personaRunT0ToolOutputDigest([]byte(version.Markdown)), Classification: version.Classification, PlacementID: placement.ID, ScopeID: scope.ScopeID})
	}
	result.Total = len(result.Hits)
	return result, nil
}
