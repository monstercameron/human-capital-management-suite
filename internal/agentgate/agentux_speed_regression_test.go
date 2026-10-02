package agentgate

import (
	"context"
	"testing"
)

func TestTodo_AGENTUX_SPEED_R8_ChatReplyDiscoveryNeedsGrantAndCapabilityOnly(t *testing.T) {
	gate, req, _, grants := privateChatFixture(t)
	record := gate.skills.List()[0]
	capabilityKey := record.ResolvedOperations[0].Capability.Definition.Key()
	recorder := &recordingPDP{decision: CapabilityDecision{Capability: capabilityKey, Allowed: true}}
	gate.pdp = recorder
	discovery := DiscoveryRequest{User: req.User, Purpose: req.Purpose, At: req.At}
	got, err := gate.DiscoverGranted(context.Background(), discovery)
	if err != nil || len(got) != 1 || got[0].Definition.Key() != record.Definition.Key() {
		t.Fatalf("grant-and-capability discovery = %+v, %v", got, err)
	}
	if len(recorder.calls) != 1 || len(recorder.calls[0].Subjects) != 0 || len(recorder.calls[0].Fields) != 0 {
		t.Fatalf("discovery invented record subjects or fields: %+v", recorder.calls)
	}
	grants.grants = nil
	got, err = gate.DiscoverGranted(context.Background(), discovery)
	if err != nil || len(got) != 0 {
		t.Fatalf("missing grant discovery = %+v, %v", got, err)
	}
	grants.grants = []SkillGrant{{ID: "grant-persona-reply", Tenant: req.Tenant, Skill: record.Definition.Key(), Roles: []string{"member"}, Population: "members", OrganizationScopes: []string{"org-a"}, Purposes: []string{req.Purpose}}}
	gate.pdp = &recordingPDP{decision: CapabilityDecision{Capability: capabilityKey, Allowed: false}}
	got, err = gate.DiscoverGranted(context.Background(), discovery)
	if err != nil || len(got) != 0 {
		t.Fatalf("missing capability discovery = %+v, %v", got, err)
	}
}
