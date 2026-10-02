package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
)

func TestAgentUXSearch_Skill_Security_Integration(t *testing.T) {
	ctx, s, call, members := agentUXSearchFixture(t)
	evidence := app.NewMemoryEvidenceSink()
	caps, _, err := newAgentCapabilities(ownWorkerReader{}, evidence, func() time.Time { return time.Unix(1, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	skills, err := newAgentSkills(caps)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := bindPersonaPolicySearchSkill(caps, skills, s)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := BindPersonaWorkspaceSearchSkill(caps, skills, s)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("workspace skill pin: %s", pin.Digest)
	if pin.ID != personaWorkspaceSearchSkillID || !personaWorkspacePinHasSearchOperation(skills, pin) || personaRunT0PinHasDocumentSearchOperation(skills, pin) || !personaRunT0PinHasDocumentSearchOperation(skills, policy) {
		t.Fatal("workspace skill altered Policy Helper or escaped T0")
	}
	// Meaning search being absent is not unavailability (keywords answer); an
	// unreadable workspace directory is, and must never read as "no documents".
	members.err = errors.New("directory unavailable")
	result, err := capability.NewGateway(caps, evidence).Invoke(ctx, capability.InvokeRequest{Capability: capability.Key{ID: personaWorkspaceSearchCapabilityID, Version: 1}, Payload: call, Authorization: capability.Authorization{Decision: capability.Allow, Scopes: []string{personaPolicySearchScope}, SubjectRef: call.InvokerID, Tenant: call.TenantID.String()}})
	if err != nil {
		t.Fatal(err)
	}
	unavailable, ok := result.Response.(PersonaPolicyDocumentSearchResult)
	if !ok || unavailable.Unavailable != workspaceSearchUnavailableMessage || len(unavailable.Hits) != 0 {
		t.Fatalf("unavailability mislabeled: %#v", result.Response)
	}
	if _, err := capability.NewGateway(caps, evidence).Invoke(context.Background(), capability.InvokeRequest{Capability: capability.Key{ID: personaWorkspaceSearchCapabilityID, Version: 1}, Payload: call, Authorization: capability.Authorization{Decision: capability.Allow, Scopes: []string{personaPolicySearchScope}, SubjectRef: call.InvokerID, Tenant: call.TenantID.String()}}); err == nil {
		t.Fatal("unbound workspace call allowed")
	}
}
