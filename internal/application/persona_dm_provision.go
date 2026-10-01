package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
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

// PersonaDMProvisioner finds or creates the server-owned direct conversation
// between an invoker and one configured persona. The persona is fixed at
// construction; callers cannot choose a persona or conversation ID.
type PersonaDMProvisioner struct {
	Creator  personaDMConversationCreator
	Resolver chatcore.PersonaDMResolver
	Persona  chatcore.MemberRef
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
		Kind: chatcore.Direct, Members: []chatcore.MemberRef{p.Persona},
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
	return resolved, nil
}
