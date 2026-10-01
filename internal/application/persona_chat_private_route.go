package application

import (
	"context"
	"errors"
	"strings"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

var errPersonaChatPrivateRouteUnavailable = errors.New("application: persona chat private route unavailable")

// personaChatPrivateRoute decorates the routed chat service's ephemeral
// write. It resolves the invoker's server-owned persona DM before the routed
// service can acquire a lease, making a request hint unable to select storage.
type personaChatPrivateRoute struct {
	chatcore.ConversationService
	private  chatcore.EphemeralService
	resolver chatcore.PersonaDMResolver
}

func newPersonaChatPrivateRoute(service chatcore.ConversationService, resolver chatcore.PersonaDMResolver) (*personaChatPrivateRoute, error) {
	if service == nil || resolver == nil {
		return nil, errPersonaChatPrivateRouteUnavailable
	}
	private, ok := service.(chatcore.EphemeralService)
	if !ok || private == nil {
		return nil, errPersonaChatPrivateRouteUnavailable
	}
	return &personaChatPrivateRoute{ConversationService: service, private: private, resolver: resolver}, nil
}

// SendEphemeralPost resolves the canonical invoker DM before delegating to
// the routed service. The caller's DurableCopyConversationID is a hint only.
func (s *personaChatPrivateRoute) SendEphemeralPost(ctx context.Context, request chatcore.SendEphemeralPostRequest) (chatcore.EphemeralPost, error) {
	if s == nil || s.private == nil || s.resolver == nil {
		return chatcore.EphemeralPost{}, errPersonaChatPrivateRouteUnavailable
	}
	if strings.TrimSpace(request.TenantID) == "" || request.Principal.TenantID != request.TenantID || strings.TrimSpace(request.Principal.SubjectID) == "" {
		return chatcore.EphemeralPost{}, chatcore.ErrPermissionDenied
	}
	canonical, err := s.resolver.ResolvePersonaDM(ctx, request.Principal, request.TenantID)
	if err != nil {
		return chatcore.EphemeralPost{}, err
	}
	if strings.TrimSpace(canonical) == "" {
		return chatcore.EphemeralPost{}, chatcore.ErrPermissionDenied
	}
	request.DurableCopyConversationID = canonical
	return s.private.SendEphemeralPost(ctx, request)
}

var _ chatcore.ConversationService = (*personaChatPrivateRoute)(nil)
var _ chatcore.EphemeralService = (*personaChatPrivateRoute)(nil)
