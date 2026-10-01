package application

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
)

type personaPolicySearchInstallationFake struct {
	tenant, conversation, agent string
	scopes                      []string
}

func (f personaPolicySearchInstallationFake) InstalledScopes(_ context.Context, tenant, agent string) ([]string, error) {
	if tenant != f.tenant || agent != f.agent {
		return nil, ErrAgentNotInstalled
	}
	return append([]string(nil), f.scopes...), nil
}

type personaPolicySearchFilteredFake struct {
	tenant, reader string
	calls          int
}

func (f *personaPolicySearchFilteredFake) SearchLexicalFiltered(_ context.Context, tenant, _query, readerKind, reader string, _ documenthubstore.SearchFilters) (documenthubstore.FilteredSearchResult, error) {
	f.calls++
	if tenant != f.tenant || readerKind != "person" || reader != f.reader {
		return documenthubstore.FilteredSearchResult{}, nil
	}
	deployedAt := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	return documenthubstore.FilteredSearchResult{Hits: []documenthubstore.FilteredSearchHit{{
		DocumentID: "policy-remote-work", VersionID: "version-4", Title: "Remote Work Policy",
		Status: "deployed", Locale: "en-US", OwnerID: "people-ops", DeployedAt: deployedAt,
	}}, Total: 1}, nil
}

type personaPolicySearchHub31Fake struct {
	searcher     documentFilteredSearcher
	install      AgentInstallationAuthority
	conversation string
}

func (f personaPolicySearchHub31Fake) AgentSearchDocuments(ctx context.Context, tenant, conversation, agent, requester, query string, filters transportdocument.SearchFilters) (transportdocument.SearchResult, error) {
	if conversation != f.conversation {
		return transportdocument.SearchResult{}, ErrAgentNotInstalled
	}
	result, err := newDocumentAgentService(f.searcher, f.install).AgentSearchDocuments(ctx, tenant, agent, requester, query, DocumentSearchFilters{
		TeamID: filters.TeamID, ChannelID: filters.ChannelID,
	})
	if err != nil {
		return transportdocument.SearchResult{}, err
	}
	return transportSearchResultForTransport(result), nil
}

// TestTodo_AGENTP_023_PolicySearchSkill binds a pin only after the real HUB-031
// operation is registered, then exercises its gateway handler and requester
// authorization boundary.
func TestTodo_AGENTP_023_PolicySearchSkill(t *testing.T) {
	evidence := app.NewMemoryEvidenceSink()
	reader := ownWorkerReader{}
	caps, _, err := newAgentCapabilities(reader, evidence, func() time.Time { return time.Unix(1, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	skills, err := newAgentSkills(caps)
	if err != nil {
		t.Fatal(err)
	}
	key := agentskills.SkillKey{ID: personaPolicyHelperSkillID, Version: 1}
	if _, ok := skills.Lookup(key); ok {
		t.Fatal("unbound policy search skill was published before its implementation")
	}
	filtered := &personaPolicySearchFilteredFake{tenant: "tenant-a", reader: "alice"}
	searcher := personaPolicySearchHub31Fake{
		searcher:     filtered,
		install:      personaPolicySearchInstallationFake{tenant: "tenant-a", conversation: "room-a", agent: "agent-a", scopes: []string{DocumentSearchScope}},
		conversation: "room-a",
	}
	pin, err := bindPersonaPolicySearchSkill(caps, skills, searcher)
	if err != nil {
		t.Fatalf("bind Policy Helper search: %v", err)
	}
	if pin.ID != personaPolicyHelperSkillID || pin.Version != 1 || pin.Digest == "" {
		t.Fatalf("derived Policy Helper pin = %+v", pin)
	}
	starter, ok := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	if !ok || !slices.Contains(starter.SkillPins, pin) {
		t.Fatalf("Policy Helper starter pin = %+v, registry-derived pin = %+v", starter.SkillPins, pin)
	}
	record, err := skills.ResolvePin(pin)
	if err != nil {
		t.Fatalf("exact policy pin did not resolve: %v", err)
	}
	if record.Definition.SideEffectTier != agentskills.TierT0 || len(record.ResolvedOperations) != 1 ||
		!record.ResolvedOperations[0].HasCapability || record.ResolvedOperations[0].Capability.Definition.ID != personaDocumentSearchCapabilityID ||
		record.ResolvedOperations[0].Capability.Definition.AuthZScopeRef != personaPolicySearchScope {
		t.Fatalf("resolved skill is not the governed T0 document search: %+v", record)
	}

	gateway := capability.NewGateway(caps, evidence)
	call := personaDocumentSearchCall{TenantID: values.TenantId("tenant-a"), ConversationID: "room-a", AgentID: "agent-a", InvokerID: "alice", Query: "remote work"}
	invoke := func(payload personaDocumentSearchCall, tenant string, decision capability.AuthorizationDecision) (transportdocument.SearchResult, error) {
		result, invokeErr := gateway.Invoke(context.Background(), capability.InvokeRequest{
			Capability: capability.Key{ID: personaDocumentSearchCapabilityID, Version: personaDocumentSearchCapabilityVersion},
			Payload:    payload,
			Authorization: capability.Authorization{Decision: decision, Scopes: []string{personaPolicySearchScope},
				Tenant: tenant, SubjectRef: payload.InvokerID},
		})
		if invokeErr != nil {
			return transportdocument.SearchResult{}, invokeErr
		}
		response, ok := result.Response.(transportdocument.SearchResult)
		if !ok {
			return transportdocument.SearchResult{}, errors.New("unexpected Policy Helper response type")
		}
		return response, nil
	}

	if _, err := invoke(call, "tenant-a", capability.Deny); err == nil || filtered.calls != 0 {
		t.Fatalf("denied gateway call leaked or searched: err=%v store calls=%d", err, filtered.calls)
	}
	foreignTenantCall := call
	foreignTenantCall.TenantID = values.TenantId("tenant-b")
	if _, err := invoke(foreignTenantCall, "tenant-b", capability.Allow); err == nil || filtered.calls != 0 {
		t.Fatalf("foreign tenant reached the document store: err=%v store calls=%d", err, filtered.calls)
	}
	call.InvokerID = "stranger"
	result, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
		Capability: capability.Key{ID: personaDocumentSearchCapabilityID, Version: personaDocumentSearchCapabilityVersion},
		Payload:    call,
		Authorization: capability.Authorization{Decision: capability.Allow, Scopes: []string{personaPolicySearchScope},
			Tenant: "tenant-a", SubjectRef: "stranger"},
	})
	if err != nil {
		t.Fatalf("authorized installation search for ungranted requester: %v", err)
	}
	ungranted, ok := result.Response.(transportdocument.SearchResult)
	if !ok || len(ungranted.Hits) != 0 || ungranted.Total != 0 {
		t.Fatalf("ungranted requester saw policy data or counts: %+v", result.Response)
	}
	call.InvokerID = "alice"
	result, err = gateway.Invoke(context.Background(), capability.InvokeRequest{
		Capability: capability.Key{ID: personaDocumentSearchCapabilityID, Version: personaDocumentSearchCapabilityVersion},
		Payload:    call,
		Authorization: capability.Authorization{Decision: capability.Allow, Scopes: []string{personaPolicySearchScope},
			Tenant: "tenant-a", SubjectRef: "alice"},
	})
	if err != nil {
		t.Fatalf("authorized search: %v", err)
	}
	authorized, ok := result.Response.(transportdocument.SearchResult)
	if !ok || len(authorized.Hits) != 1 || authorized.Hits[0].VersionID != "version-4" || authorized.Hits[0].Status != "deployed" || authorized.Hits[0].DeployedAt.IsZero() {
		t.Fatalf("authorized result omitted exact citation: %+v", result.Response)
	}
}
