package application

import (
	"context"
	"errors"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

type personaCurrentChannelPolicySource interface {
	CapturePersonaChannelPolicy(context.Context, string, string, string) (chatstore.PersonaChannelPolicySnapshot, error)
}

func currentPersonaChannelPolicy(ctx context.Context, source personaCurrentChannelPolicySource, tenant, conversation string, accepted agentpersonastore.ChannelPolicy) (agentpersonastore.ChannelPolicy, bool, error) {
	if source == nil {
		return accepted, false, nil
	}
	snapshot, err := source.CapturePersonaChannelPolicy(ctx, tenant, conversation, "")
	if err != nil {
		return agentpersonastore.ChannelPolicy{}, true, err
	}
	if snapshot.TenantID != tenant || snapshot.ConversationID != conversation || snapshot.PolicyRevision <= 0 {
		return agentpersonastore.ChannelPolicy{}, true, errors.New("application: current persona channel policy is invalid")
	}
	return effectivePersonaChannelPolicy(accepted, agentPersonaPolicyFromChat(snapshot.Policy)), true, nil
}

func personaPolicySourceFromAuthority(authority any) personaCurrentChannelPolicySource {
	if source, ok := authority.(personaCurrentChannelPolicySource); ok {
		return source
	}
	adapter, ok := authority.(*PersonaAuthorityAdapter)
	if !ok || adapter == nil {
		return nil
	}
	source, ok := adapter.Personas.(*DatabasePersonaAuthoritySource)
	if !ok || source == nil {
		return nil
	}
	policy, _ := source.Audience.(personaCurrentChannelPolicySource)
	return policy
}

func personaPolicySourceFromAudienceFloor(authority any) personaCurrentChannelPolicySource {
	adapter, ok := authority.(*PersonaAudienceFloorAdapter)
	if !ok || adapter == nil {
		return nil
	}
	policy, _ := adapter.ChannelPolicy.(personaCurrentChannelPolicySource)
	return policy
}

func agentPersonaPolicyFromChat(policy chatstore.PersonaChannelPolicy) agentpersonastore.ChannelPolicy {
	classes := make([]agentpersonastore.ConversationClass, 0, len(policy.AllowedChannelClasses))
	for _, class := range policy.AllowedChannelClasses {
		classes = append(classes, agentpersonastore.ConversationClass(class))
	}
	return agentpersonastore.ChannelPolicy{
		PlacementClass:            policy.PlacementClass,
		MaxTier:                   policy.MaxTier,
		AllowedDataClasses:        slices.Clone(policy.AllowedDataClasses),
		AlwaysPrivate:             policy.AlwaysPrivate,
		ConversationSearchAllowed: policy.ConversationSearchAllowed,
		AllowedChannelClasses:     classes,
		AllowExternalMembers:      policy.AllowExternalMembers,
		AllowCrossCompanyMembers:  policy.AllowCrossCompanyMembers,
	}
}

func effectivePersonaChannelPolicy(accepted, current agentpersonastore.ChannelPolicy) agentpersonastore.ChannelPolicy {
	out := accepted
	out.AlwaysPrivate = accepted.AlwaysPrivate || current.AlwaysPrivate
	out.ConversationSearchAllowed = accepted.ConversationSearchAllowed && current.ConversationSearchAllowed
	out.AllowExternalMembers = accepted.AllowExternalMembers && current.AllowExternalMembers
	out.AllowCrossCompanyMembers = accepted.AllowCrossCompanyMembers && current.AllowCrossCompanyMembers
	out.AllowedDataClasses = intersectStrings(accepted.AllowedDataClasses, current.AllowedDataClasses)
	out.AllowedChannelClasses = intersectConversationClasses(accepted.AllowedChannelClasses, current.AllowedChannelClasses)
	if tierRank(current.MaxTier) < tierRank(accepted.MaxTier) {
		out.MaxTier = current.MaxTier
	}
	if current.PlacementClass != accepted.PlacementClass && current.PlacementClass != "ANY_INTERNAL" {
		out.PlacementClass = current.PlacementClass
	}
	return out
}

func agentPersonaPolicyEqual(a, b agentpersonastore.ChannelPolicy) bool {
	return a.PlacementClass == b.PlacementClass && a.MaxTier == b.MaxTier && a.AlwaysPrivate == b.AlwaysPrivate &&
		a.ConversationSearchAllowed == b.ConversationSearchAllowed && a.AllowExternalMembers == b.AllowExternalMembers &&
		a.AllowCrossCompanyMembers == b.AllowCrossCompanyMembers && sameAgentUXDMPathStringSet(a.AllowedDataClasses, b.AllowedDataClasses) &&
		sameConversationClassSet(a.AllowedChannelClasses, b.AllowedChannelClasses)
}

func intersectStrings(a, b []string) []string {
	out := make([]string, 0, len(a))
	for _, value := range a {
		if slices.Contains(b, value) && !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	return out
}

func intersectConversationClasses(a, b []agentpersonastore.ConversationClass) []agentpersonastore.ConversationClass {
	out := make([]agentpersonastore.ConversationClass, 0, len(a))
	for _, value := range a {
		if slices.Contains(b, value) && !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	return out
}

func sameAgentUXDMPathStringSet(a, b []string) bool {
	return len(intersectStrings(a, b)) == len(a) && len(a) == len(b)
}

func sameConversationClassSet(a, b []agentpersonastore.ConversationClass) bool {
	return len(intersectConversationClasses(a, b)) == len(a) && len(a) == len(b)
}

func tierRank(tier string) int {
	switch tier {
	case "T0":
		return 0
	case "T1":
		return 1
	case "T2":
		return 2
	case "T3":
		return 3
	case "T4":
		return 4
	default:
		return -1
	}
}
