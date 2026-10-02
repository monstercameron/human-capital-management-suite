package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
)

type agentUXSearchLegacyPolicy struct{ bindings map[string]PersonaT0SkillPin }

func (p agentUXSearchLegacyPolicy) ResolveAndAuthorize(_ context.Context, _ agentrun.Record, _ runstate.Run, _ PersonaRunT0ToolInvocation, skill string) (PersonaT0SkillPin, error) {
	binding, ok := p.bindings[skill]
	if !ok {
		return binding, errPersonaRunT0Tool
	}
	return binding, nil
}
func (agentUXSearchLegacyPolicy) ResolvePersonaDocumentSearchScope(_ context.Context, id PersonaRunT0ToolInvocation) (PersonaDocumentSearchScope, error) {
	return PersonaDocumentSearchScope{ScopeID: id.ConversationID, WorkspaceSearchAllowed: true}, nil
}

type agentUXSearchLegacyGateway struct {
	request capability.InvokeRequest
	calls   int
}

func (g *agentUXSearchLegacyGateway) Invoke(_ context.Context, r capability.InvokeRequest) (capability.InvokeResult, error) {
	g.request = r
	g.calls++
	return capability.InvokeResult{Response: PersonaPolicyDocumentSearchResult{Hits: []PersonaPolicyDocumentSearchHit{}, Unavailable: workspaceSearchUnavailableMessage}}, nil
}

func TestAgentUXSearch_T0SealedTool_Security_Integration(t *testing.T) {
	e, p, _, journal, record, run := personaRunT0ToolFixture(t)
	_, s, _, _ := agentUXSearchFixture(t)
	caps, _, err := newAgentCapabilities(ownWorkerReader{}, app.NewMemoryEvidenceSink(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	skills, err := newAgentSkills(caps)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := BindPersonaWorkspaceSearchSkill(caps, skills, s)
	if err != nil {
		t.Fatal(err)
	}
	binding := p.binding
	binding.Pin = pin
	e.t0.catalog = agentUXSearchRuntimeCatalog{base: e.t0.catalog, workspace: skills}
	e.t0.bindings[personaT0BindingKey(binding.Invocation, pin)] = binding
	e.policy = agentUXSearchLegacyPolicy{bindings: map[string]PersonaT0SkillPin{personaPolicyHelperSkillID: p.binding, personaWorkspaceSearchSkillID: binding}}
	gateway := &agentUXSearchLegacyGateway{}
	e.gateway = gateway
	expected, _ := json.Marshal(PersonaPolicyDocumentSearchResult{Hits: []PersonaPolicyDocumentSearchHit{}, Unavailable: workspaceSearchUnavailableMessage})
	journal.digest = personaRunT0ToolOutputDigest(expected)
	ctx := personaRunT0ToolHumanContext(t)
	schemas, err := e.ToolSchemas(ctx, record, run)
	if err != nil || len(schemas) != 2 || schemas[1].Name != personaWorkspaceSearchTool {
		t.Fatalf("missing workspace schema: %+v %v", schemas, err)
	}
	output, ref, digest, err := e.Execute(ctx, record, run, agentmodel.ToolProposal{ID: "workspace", Name: personaWorkspaceSearchTool, Arguments: json.RawMessage(`{"query":"list policies"}`)})
	if err != nil || ref == "" || digest != personaRunT0ToolOutputDigest(output) || journal.calls != 1 {
		t.Fatalf("result not sealed: %s %s %v", output, ref, err)
	}
	call, ok := gateway.request.Payload.(personaDocumentSearchCall)
	if !ok || call.Scope != personaWorkspaceSearchScope || call.TenantID.String() != record.Request.Source.TenantID || gateway.request.Capability.ID != personaWorkspaceSearchCapabilityID {
		t.Fatalf("workspace call not bound: %+v", gateway.request)
	}
	if _, _, _, err := e.Execute(ctx, record, run, agentmodel.ToolProposal{ID: "forged", Name: personaWorkspaceSearchTool, Arguments: json.RawMessage(`{"query":"list policies","tenant_id":"foreign"}`)}); !errors.Is(err, errPersonaRunT0Tool) || gateway.calls != 1 {
		t.Fatalf("forged argument reached search: %v", err)
	}
}
