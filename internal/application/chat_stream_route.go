package application

import (
	"context"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type chatRouteEpochResolver interface {
	RouteEpoch(context.Context, string, string) (uint64, error)
}

func chatRouteEpoch(ctx context.Context, service chatcore.ConversationService, tenantID, conversationID string) (uint64, error) {
	resolver, ok := service.(chatRouteEpochResolver)
	if !ok {
		// Direct service fixtures and legacy compositions have no route
		// directory. Zero is the explicit unregistered-route epoch.
		return 0, nil
	}
	return resolver.RouteEpoch(ctx, tenantID, conversationID)
}
