package chatroutingadapter

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
)

// RouteEpoch returns the current route fence for a conversation. Epoch zero is
// reserved for legacy conversations that are not present in the route
// directory; signing that value ensures a later route registration invalidates
// any cursor minted before the registration.
func (s *Service) RouteEpoch(ctx context.Context, tenantID, conversationID string) (uint64, error) {
	if s == nil || s.directory == nil || tenantID == "" || conversationID == "" {
		return 0, chatrouting.ErrInvalid
	}
	route, err := s.directory.Lookup(ctx, conversationID, tenantID)
	if errors.Is(err, chatrouting.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return route.Epoch, nil
}
