package chatrouting

import "context"

type CreateRequest struct {
	ConversationID, HostTenantID, ShardID, IdempotencyKey string
	PlacementPolicy                                       string
	PlacementPolicyVersion                                uint64
}

// ConversationCreator is implemented by the separate chat database owner.
// EnsureConversation must be idempotent by request idempotency key and must
// never access the core route directory.
type ConversationCreator interface {
	EnsureConversation(context.Context, CreateRequest) error
}

type ConversationReconciler interface {
	EnsureConversation(context.Context, CreateRequest) error
}

type CreateCoordinator struct {
	Directory Directory
	Chat      ConversationCreator
}

func (c CreateCoordinator) Create(ctx context.Context, req CreateRequest) (Route, error) {
	if c.Directory == nil || c.Chat == nil || req.ConversationID == "" || req.HostTenantID == "" || req.IdempotencyKey == "" {
		return Route{}, ErrInvalid
	}
	r, err := c.Directory.Reserve(ctx, ReserveRequest{ConversationID: req.ConversationID, HostTenantID: req.HostTenantID, ShardID: req.ShardID, PlacementPolicy: req.PlacementPolicy, PlacementPolicyVersion: req.PlacementPolicyVersion, IdempotencyKey: req.IdempotencyKey})
	if err != nil {
		return Route{}, err
	}
	if r.State == StateActive {
		return r, nil
	}
	if err = c.Chat.EnsureConversation(ctx, req); err != nil {
		return r, err
	}
	return c.Directory.Activate(ctx, r.ConversationID, r.HostTenantID, r.Epoch)
}

// Reconcile retries the chat side after an ambiguous core/chat boundary. It
// preserves the same idempotency key and activates only the reserved epoch.
func (c CreateCoordinator) Reconcile(ctx context.Context, req CreateRequest) (Route, error) {
	if c.Directory == nil || c.Chat == nil {
		return Route{}, ErrInvalid
	}
	r, err := c.Directory.Lookup(ctx, req.ConversationID, req.HostTenantID)
	if err != nil {
		return Route{}, err
	}
	if r.State == StateActive {
		return r, nil
	}
	if r.State != StatePending {
		return r, ErrMoveConflict
	}
	if err = c.Chat.EnsureConversation(ctx, req); err != nil {
		return r, err
	}
	return c.Directory.Activate(ctx, r.ConversationID, r.HostTenantID, r.Epoch)
}
