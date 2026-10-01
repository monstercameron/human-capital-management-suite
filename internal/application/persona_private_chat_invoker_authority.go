package application

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaPrivateChatInvokerAuthority = errors.New("application: current private chat invoker authority unavailable")

// PersonaChatAuthorityRequest names an immutable persona's exact pins and the
// chat tuple selected by the server. Current grants and chat access are always
// rechecked; these identifiers do not themselves confer authority.
type PersonaChatAuthorityRequest struct {
	Tenant                                                       values.TenantId
	InvokerID, Purpose, ConversationID, ThreadID, InvokingPostID string
	Pins                                                         []agentskills.SkillPin
	At                                                           time.Time
}

type PersonaChatInvokerAuthorityResolver interface {
	ResolvePersonaChatInvokerAuthority(context.Context, PersonaChatAuthorityRequest) (agentdelegation.UserAuthority, error)
}

type personaChatAuthorityTupleKey struct{}

func withPersonaChatAuthorityTuple(ctx context.Context, tenant, invoker, conversation, thread, post string) context.Context {
	return context.WithValue(ctx, personaChatAuthorityTupleKey{}, PersonaChatAuthorityRequest{Tenant: values.TenantId(tenant), InvokerID: invoker,
		Purpose: "persona-mention", ConversationID: conversation, ThreadID: thread, InvokingPostID: post})
}

func personaChatAuthorityResource(tenant, conversation, thread, post string) string {
	// Fixed string tuples have no marshal failure and preserve all boundaries.
	tuple, _ := json.Marshal([4]string{tenant, conversation, thread, post})
	return "chat.current:" + string(tuple)
}

// PersonaPrivateChatInvokerAuthoritySource projects chat and document search
// authority through their own policy owners, without substituting worker fields.
// Chat is the private-only owner; Scope also admits governed public channels.
type PersonaPrivateChatInvokerAuthoritySource struct {
	Gate  *agentgate.Gate
	Users PersonaPrivateChatScopeUserResolver
	Chat  agentgate.PrivateChatScopeAuthorizer
	Scope agentgate.PersonaChatScopeAuthorizer
}

func (s *PersonaPrivateChatInvokerAuthoritySource) ResolvePersonaChatInvokerAuthority(ctx context.Context, request PersonaChatAuthorityRequest) (agentdelegation.UserAuthority, error) {
	denied := agentdelegation.UserAuthority{UserID: request.InvokerID}
	if s == nil || s.Gate == nil || isNilPersonaOutputPort(s.Users) || (isNilPersonaOutputPort(s.Chat) && isNilPersonaOutputPort(s.Scope)) || ctx == nil || request.Tenant.Validate() != nil ||
		request.Purpose != "persona-mention" || request.At.IsZero() || len(request.Pins) == 0 || !required(request.ConversationID) || !required(request.ThreadID) || !required(request.InvokingPostID) {
		return denied, errPersonaPrivateChatInvokerAuthority
	}
	principal, err := verifiedHumanPrincipal(ctx, request.Tenant.String(), request.InvokerID, request.Purpose)
	if err != nil || request.At.Before(principal.IssuedAt()) || !request.At.Before(principal.ExpiresAt()) {
		return denied, errPersonaPrivateChatInvokerAuthority
	}
	user, err := s.Users.Resolve(ctx, principal, request.Purpose)
	if err != nil || user.Principal == nil || user.Principal.Fingerprint() != principal.Fingerprint() {
		return denied, errPersonaPrivateChatInvokerAuthority
	}
	organization, ok := currentOrganizationScope(principal, user.OrganizationScopes)
	if !ok {
		return denied, errPersonaPrivateChatInvokerAuthority
	}
	resource := personaChatAuthorityResource(request.Tenant.String(), request.ConversationID, request.ThreadID, request.InvokingPostID)
	projected := trust.SkillAuthorities{}
	for _, pin := range request.Pins {
		if _, duplicate := projected[pin.ID]; duplicate {
			return denied, errPersonaPrivateChatInvokerAuthority
		}
		req := agentgate.PrivateChatScopeRequest{User: user, Skill: pin, Purpose: request.Purpose, Tenant: request.Tenant,
			ConversationID: request.ConversationID, ThreadID: request.ThreadID, InvokingPostID: request.InvokingPostID, At: request.At}
		var projection agentgate.PrivateChatSkillAuthorization
		if !isNilPersonaOutputPort(s.Scope) {
			projection, err = s.Gate.ProjectPersonaChatSkillAuthorization(ctx, req, s.Scope)
		} else {
			switch pin.ID {
			case "persona.chat_reply":
				projection, err = s.Gate.ProjectPrivateChatSkillAuthorization(ctx, req, s.Chat)
			case personaPolicyHelperSkillID:
				projection, err = s.Gate.ProjectPrivateChatDocumentSearchAuthorization(ctx, req, s.Chat)
			default:
				return denied, errPersonaPrivateChatInvokerAuthority
			}
		}
		if err != nil {
			return denied, err
		}
		projected[pin.ID] = trust.SkillAuthority{Capabilities: []string{projection.Scope},
			Resources: []string{resource}, Purposes: []string{request.Purpose}}
	}
	return agentdelegation.UserAuthority{UserID: request.InvokerID, Active: true,
		Authority: authorityScopeFromSkills(principal, organization, projected, request.At), SkillAuthorities: trust.CloneSkillAuthorities(projected)}, nil
}
