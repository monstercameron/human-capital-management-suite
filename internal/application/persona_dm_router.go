package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

var (
	errPersonaDMRouterUnavailable = errors.New("application: persona DM router unavailable")
	errPersonaDMInvocation        = errors.New("application: invalid persona invocation context")
)

type personaDMInvocationKey struct{}

// WithPersonaDMInvocation binds the server-created on-behalf-of invocation
// to the context used while delivering its private result. The router checks
// this complete binding before selecting a persona-specific DM.
func WithPersonaDMInvocation(ctx context.Context, invocation agentinvoke.RunRequest) context.Context {
	return context.WithValue(ctx, personaDMInvocationKey{}, invocation)
}

func personaDMInvocationFromContext(ctx context.Context) (agentinvoke.RunRequest, bool) {
	invocation, ok := ctx.Value(personaDMInvocationKey{}).(agentinvoke.RunRequest)
	return invocation, ok && validPersonaDMInvocation(invocation)
}

// PersonaDMRouter routes each validated persona invocation to a fixed,
// server-configured resolver. Its resolver map is copied at construction and
// is never selected from request text, principal roles, or a DM hint.
type PersonaDMRouter struct {
	resolvers map[string]chatcore.PersonaDMResolver
}

// NewPersonaDMRouter constructs a multi-persona resolver from server-owned
// bindings. Empty or duplicate persona IDs are rejected.
func NewPersonaDMRouter(bindings map[string]chatcore.PersonaDMResolver) (PersonaDMRouter, error) {
	if len(bindings) == 0 {
		return PersonaDMRouter{}, errPersonaDMRouterUnavailable
	}
	copyOf := make(map[string]chatcore.PersonaDMResolver, len(bindings))
	for personaID, resolver := range bindings {
		if strings.TrimSpace(personaID) == "" || resolver == nil {
			return PersonaDMRouter{}, errPersonaDMRouterUnavailable
		}
		if _, exists := copyOf[personaID]; exists {
			return PersonaDMRouter{}, errPersonaDMRouterUnavailable
		}
		copyOf[personaID] = resolver
	}
	return PersonaDMRouter{resolvers: copyOf}, nil
}

// ResolvePersonaDM selects the DM only when the context carries a valid
// on-behalf-of invocation whose invoker and tenant match the authenticated
// principal. The configured persona binding then performs the authoritative
// chat membership lookup.
func (r PersonaDMRouter) ResolvePersonaDM(ctx context.Context, principal chatcore.Principal, tenant string) (string, error) {
	if len(r.resolvers) == 0 || strings.TrimSpace(tenant) == "" || principal.TenantID != tenant || principal.SubjectID == "" {
		return "", errPersonaDMRouterUnavailable
	}
	invocation, ok := personaDMInvocationFromContext(ctx)
	if !ok || invocation.TenantID != tenant || invocation.InvokerID != principal.SubjectID || invocation.Mode != agentinvoke.OnBehalfOf {
		return "", errPersonaDMInvocation
	}
	resolver, ok := r.resolvers[invocation.PersonaID]
	if !ok {
		return "", chatcore.ErrPermissionDenied
	}
	conversationID, err := resolver.ResolvePersonaDM(ctx, principal, tenant)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errPersonaDMRouterUnavailable, err)
	}
	if strings.TrimSpace(conversationID) == "" {
		return "", errPersonaDMRouterUnavailable
	}
	return conversationID, nil
}

func validPersonaDMInvocation(invocation agentinvoke.RunRequest) bool {
	if invocation.Mode != agentinvoke.OnBehalfOf || strings.TrimSpace(invocation.InvocationID) == "" ||
		strings.TrimSpace(invocation.TenantID) == "" || strings.TrimSpace(invocation.InvokerID) == "" ||
		strings.TrimSpace(invocation.PersonaID) == "" || strings.TrimSpace(invocation.InstallationID) == "" ||
		strings.TrimSpace(invocation.Grant.ID) == "" || invocation.Grant.UserID != invocation.InvokerID ||
		invocation.Grant.TenantID != invocation.TenantID || invocation.Actor.Validate() != nil {
		return false
	}
	return invocation.Actor.UserID == invocation.InvokerID && invocation.Actor.PersonaID == invocation.PersonaID &&
		invocation.Actor.InstallationID == invocation.InstallationID && invocation.Actor.InvocationID == invocation.InvocationID
}

var _ chatcore.PersonaDMResolver = PersonaDMRouter{}
