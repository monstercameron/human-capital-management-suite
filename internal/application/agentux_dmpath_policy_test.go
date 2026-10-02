package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
)

func TestTodo_AGENTUX_025_EffectivePolicyIntersectsCurrentCeiling(t *testing.T) {
	accepted := agentpersonastore.ChannelPolicy{
		PlacementClass: "ONE_TO_ONE_DM", MaxTier: "T2", AllowedDataClasses: []string{"PUBLIC", "INTERNAL", "POLICY_DOCUMENT"},
		AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationOneToOne}, ConversationSearchAllowed: true,
	}
	narrower := accepted
	narrower.MaxTier = "T0"
	narrower.AllowedDataClasses = []string{"PUBLIC", "INTERNAL"}
	narrower.ConversationSearchAllowed = false
	got := effectivePersonaChannelPolicy(accepted, narrower)
	if got.MaxTier != "T0" || got.ConversationSearchAllowed || len(got.AllowedDataClasses) != 2 {
		t.Fatalf("narrowed policy = %+v", got)
	}
	wider := accepted
	wider.MaxTier = "T3"
	wider.AllowedDataClasses = append(wider.AllowedDataClasses, "WORKFORCE")
	got = effectivePersonaChannelPolicy(accepted, wider)
	if got.MaxTier != accepted.MaxTier || !agentPersonaPolicyEqual(got, accepted) {
		t.Fatalf("current policy widened accepted copy: %+v", got)
	}
}
