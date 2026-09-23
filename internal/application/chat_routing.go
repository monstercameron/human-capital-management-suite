package application

import (
	"context"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatroutingadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatroutestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// composeChatRouting injects the current core credential pool into the route
// directory. The chat store keeps its own pool; no chat query can reach this
// database through a join.
func composeChatRouting(ctx context.Context, service chatcore.ConversationService, corePool *pgxadapter.Pool) (chatcore.ConversationService, error) {
	if corePool == nil {
		return nil, chatrouting.ErrInvalid
	}
	directory, err := chatroutestore.New(corePool)
	if err != nil {
		return nil, err
	}
	if err = directory.Migrate(ctx); err != nil {
		return nil, err
	}
	routed, err := chatroutingadapter.New(service, chatroutingadapter.Options{Directory: directory, DefaultShard: "chat-default", PlacementPolicy: "tenant-default", PlacementPolicyVersion: 1})
	if err != nil {
		return nil, err
	}
	return routed, nil
}
