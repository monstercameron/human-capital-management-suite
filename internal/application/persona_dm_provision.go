package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	errPersonaDMProvisionUnavailable = errors.New("application: persona DM provisioner unavailable")
	errPersonaDMProvisionInvalid     = errors.New("application: invalid persona DM provision request")
)

// personaDMConversationCreator is the mutation seam used to create a direct
// conversation. Keeping it narrower than ConversationService prevents the
// provisioner from acquiring unrelated chat powers.
type personaDMConversationCreator interface {
	CreateConversation(context.Context, chatcore.CreateConversationRequest) (chatcore.Conversation, error)
}

type personaDMPolicyStore interface {
	PutAudiencePolicy(context.Context, string, string, int64, chatstore.AudiencePolicy) (int64, error)
	CapturePersonaChannelPolicy(context.Context, string, string, string) (chatstore.PersonaChannelPolicySnapshot, error)
	PutPersonaChannelPolicy(context.Context, string, string, int64, chatstore.PersonaChannelPolicy) (int64, error)
}

// PersonaDMProvisioner finds or creates the server-owned direct conversation
// between an invoker and one configured persona. The persona is fixed at
// construction; callers cannot choose a persona or conversation ID.
type PersonaDMProvisioner struct {
	Creator  personaDMConversationCreator
	Resolver chatcore.PersonaDMResolver
	Persona  chatcore.MemberRef
	Policies personaDMPolicyStore
	// DisplayName is the agent's published name, which a new conversation is
	// named after (AGENTUX-030). Unset, the conversation is created without a
	// name of its own, like a conversation between two people, and the page
	// names it from the agent's directory entry. It is never derived from the
	// agent's identifier.
	DisplayName string
}

// NewPersonaDMProvisioner constructs a tenant-bound provisioner. Creator and
// Resolver should be backed by the same chat service and durable store.
func NewPersonaDMProvisioner(creator personaDMConversationCreator, resolver chatcore.PersonaDMResolver, persona chatcore.MemberRef) (PersonaDMProvisioner, error) {
	if creator == nil || resolver == nil || strings.TrimSpace(persona.TenantID) == "" || strings.TrimSpace(persona.SubjectID) == "" {
		return PersonaDMProvisioner{}, errPersonaDMProvisionUnavailable
	}
	return PersonaDMProvisioner{Creator: creator, Resolver: resolver, Persona: persona}, nil
}

// EnsurePersonaDM returns the unique active direct conversation for the
// invoker and configured persona, creating it atomically by deterministic
// identity when it does not exist. A successful create is re-resolved before
// returning so malformed or partial membership can never receive a reply.
func (p PersonaDMProvisioner) EnsurePersonaDM(ctx context.Context, principal chatcore.Principal, tenant string) (string, error) {
	if p.Creator == nil || p.Resolver == nil || strings.TrimSpace(tenant) == "" ||
		principal.TenantID != tenant || strings.TrimSpace(principal.SubjectID) == "" ||
		p.Persona.TenantID != tenant || strings.TrimSpace(p.Persona.SubjectID) == "" ||
		(principal.TenantID == p.Persona.TenantID && principal.SubjectID == p.Persona.SubjectID) {
		return "", errPersonaDMProvisionInvalid
	}

	conversationID, err := p.Resolver.ResolvePersonaDM(ctx, principal, tenant)
	if err == nil {
		if strings.TrimSpace(conversationID) == "" {
			return "", errPersonaDMProvisionUnavailable
		}
		if err := p.ensurePolicies(ctx, tenant, conversationID); err != nil {
			return "", err
		}
		return conversationID, nil
	}
	if !errors.Is(err, chatcore.ErrPermissionDenied) {
		return "", fmt.Errorf("%w: resolve: %v", errPersonaDMProvisionUnavailable, err)
	}

	deterministicID, err := chatcore.DirectPairConversationID(tenant, []chatcore.MemberRef{
		{TenantID: principal.TenantID, SubjectID: principal.SubjectID}, p.Persona,
	})
	if err != nil {
		return "", fmt.Errorf("%w: direct identity: %v", errPersonaDMProvisionUnavailable, err)
	}
	created, createErr := p.Creator.CreateConversation(ctx, chatcore.CreateConversationRequest{
		Principal: principal, TenantID: tenant, ConversationID: deterministicID,
		Kind: chatcore.Direct, Name: personaDMDisplayName(p.DisplayName), Members: []chatcore.MemberRef{p.Persona},
		IdempotencyKey: "persona-dm:" + deterministicID,
	})
	if createErr != nil && !errors.Is(createErr, chatcore.ErrAlreadyExists) && !errors.Is(createErr, chatcore.ErrConflict) {
		return "", fmt.Errorf("%w: create: %v", errPersonaDMProvisionUnavailable, createErr)
	}
	if strings.TrimSpace(created.ID) != "" && created.ID != deterministicID {
		return "", errPersonaDMProvisionUnavailable
	}

	resolved, resolveErr := p.Resolver.ResolvePersonaDM(ctx, principal, tenant)
	if resolveErr != nil {
		return "", fmt.Errorf("%w: verify: %v", errPersonaDMProvisionUnavailable, resolveErr)
	}
	if resolved != deterministicID {
		return "", errPersonaDMProvisionUnavailable
	}
	if err := p.ensurePolicies(ctx, tenant, resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

// personaDMPolicyPair is a policy store that writes the two policies of a
// direct conversation with an agent together.
type personaDMPolicyPair interface {
	EnsurePersonaDirectPolicies(context.Context, string, string, chatstore.AudiencePolicy, chatstore.PersonaChannelPolicy) (bool, bool, error)
}

func (p PersonaDMProvisioner) ensurePolicies(ctx context.Context, tenant, conversationID string) error {
	if p.Policies == nil {
		return nil
	}
	// AGENTUX-038: the audience policy and the one-to-one agent policy are
	// written in one transaction where the store can, so a conversation is never
	// left with the first and not the second (a question there was then refused
	// at admission until somebody repaired it by hand). Policies already there
	// are left alone, as below.
	if pair, ok := p.Policies.(personaDMPolicyPair); ok {
		if _, _, err := pair.EnsurePersonaDirectPolicies(ctx, tenant, conversationID, personaDMaudiencePolicy(), personaDMChannelPolicy()); err != nil {
			return fmt.Errorf("%w: direct conversation policies: %v", errPersonaDMProvisionUnavailable, err)
		}
		return nil
	}
	if _, err := p.Policies.PutAudiencePolicy(ctx, tenant, conversationID, 0, personaDMaudiencePolicy()); err != nil && !errors.Is(err, chatstore.ErrAudiencePolicyConflict) {
		return fmt.Errorf("%w: direct audience policy: %v", errPersonaDMProvisionUnavailable, err)
	}
	ceiling := personaDMChannelPolicy()
	current, err := p.Policies.CapturePersonaChannelPolicy(ctx, tenant, conversationID, "")
	if err == nil {
		if !reflect.DeepEqual(current.Policy, ceiling) {
			return nil
		}
		return nil
	}
	if !errors.Is(err, dbport.ErrNoRows) && !errors.Is(err, chatstore.ErrAudienceEligibilityUnavailable) {
		return fmt.Errorf("%w: direct channel policy: %v", errPersonaDMProvisionUnavailable, err)
	}
	if _, err := p.Policies.PutPersonaChannelPolicy(ctx, tenant, conversationID, 0, ceiling); err != nil && !errors.Is(err, chatstore.ErrAudiencePolicyConflict) {
		return fmt.Errorf("%w: direct channel policy: %v", errPersonaDMProvisionUnavailable, err)
	}
	return nil
}

func personaDMaudiencePolicy() chatstore.AudiencePolicy {
	return chatstore.AudiencePolicy{RoleMode: 1, Classification: "INTERNAL"}
}

func personaDMChannelPolicy() chatstore.PersonaChannelPolicy {
	return chatstore.PersonaChannelPolicy{
		MaxTier: "T3", PlacementClass: "ONE_TO_ONE_DM", AlwaysPrivate: true, ConversationSearchAllowed: true,
		AllowedDataClasses:    []string{"PUBLIC", "INTERNAL", "POLICY_DOCUMENT", "WORKFORCE", "SCHEDULE"},
		AllowedChannelClasses: []string{"ONE_TO_ONE"},
	}
}

// personaDMDisplayName is the name a new direct conversation with an agent is
// given: the agent's published name, with the spaces around it and inside it
// tidied. An identifier used to be turned into a name here ("policy-helper"
// read well; "673214ec-4402-…" did not), which is how a conversation came to
// be titled with its agent's identifier (AGENTUX-030).
func personaDMDisplayName(published string) string {
	return strings.Join(strings.Fields(published), " ")
}
