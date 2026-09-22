// Package chatroutingadapter composes core route authority with the chat
// service. It contains no chat persistence and forwards all authorization and
// membership decisions to the chat owner.
package chatroutingadapter

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
)

type Service struct {
	chat.ConversationService
	directory     chatrouting.Directory
	cache         *chatrouting.RouteCache
	defaultShard  string
	policy        string
	policyVersion uint64
	now           func() time.Time
}

type Options struct {
	Directory              chatrouting.Directory
	Cache                  *chatrouting.RouteCache
	DefaultShard           string
	PlacementPolicy        string
	PlacementPolicyVersion uint64
	Now                    func() time.Time
}

func New(next chat.ConversationService, opts Options) (*Service, error) {
	if next == nil || opts.Directory == nil || opts.DefaultShard == "" {
		return nil, chatrouting.ErrInvalid
	}
	if opts.Cache == nil {
		opts.Cache = chatrouting.NewRouteCache(5 * time.Second)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{ConversationService: next, directory: opts.Directory, cache: opts.Cache, defaultShard: opts.DefaultShard, policy: opts.PlacementPolicy, policyVersion: opts.PlacementPolicyVersion, now: opts.Now}, nil
}

func stableConversationID(tenant, key string) string {
	if key == "" {
		return uuid.NewString()
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(tenant+"\x00"+key)).String()
}

func requestConversationID(r chat.CreateConversationRequest) string {
	if r.ConversationID != "" {
		return r.ConversationID
	}
	if r.IdempotencyKey == "" {
		return uuid.NewString()
	}
	refs := append([]chat.MemberRef(nil), r.Members...)
	if len(refs) == 0 {
		for _, id := range r.MemberIDs {
			refs = append(refs, chat.MemberRef{TenantID: r.TenantID, SubjectID: id})
		}
	}
	parts := make([]string, 0, len(refs))
	for _, m := range refs {
		parts = append(parts, strings.TrimSpace(m.TenantID)+"\x00"+strings.TrimSpace(m.SubjectID))
	}
	sort.Strings(parts)
	seed := r.TenantID + "\x00" + r.Principal.SubjectID + "\x00" + r.OwnerID + "\x00" + string(r.Kind) + "\x00" + strings.TrimSpace(r.Name) + "\x00" + r.IdempotencyKey + "\x00" + strings.Join(parts, "\x01")
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(seed)).String()
}

func (s *Service) CreateConversation(ctx context.Context, r chat.CreateConversationRequest) (chat.Conversation, error) {
	if preflight, ok := s.ConversationService.(interface {
		ValidateCreate(context.Context, chat.CreateConversationRequest) error
	}); ok {
		if err := preflight.ValidateCreate(ctx, r); err != nil {
			return chat.Conversation{}, err
		}
	}
	id := requestConversationID(r)
	r.ConversationID = id
	reserved, err := s.directory.Reserve(ctx, chatrouting.ReserveRequest{ConversationID: id, HostTenantID: r.TenantID, ShardID: s.defaultShard, PlacementPolicy: s.policy, PlacementPolicyVersion: s.policyVersion, IdempotencyKey: r.IdempotencyKey})
	if err != nil {
		return chat.Conversation{}, err
	}
	if reserved.State == chatrouting.StatePending {
		trustedCtx := chatrouting.WithWriteLease(ctx, chatrouting.WriteLease{Route: reserved, ExpiresAt: s.now().Add(5 * time.Minute)})
		created, createErr := s.ConversationService.CreateConversation(trustedCtx, r)
		if createErr != nil {
			return created, createErr
		}
		active, activateErr := s.directory.Activate(ctx, id, r.TenantID, reserved.Epoch)
		if activateErr != nil {
			return created, activateErr
		}
		if active.State != chatrouting.StateActive {
			return created, errors.New("chatroutingadapter: route did not activate")
		}
		return created, nil
	}
	trustedCtx := chatrouting.WithWriteLease(ctx, chatrouting.WriteLease{Route: reserved, ExpiresAt: s.now().Add(5 * time.Minute)})
	return s.ConversationService.CreateConversation(trustedCtx, r)
}

// routeContext resolves a conversation's route and returns a context carrying
// the write lease the chat shard's fence demands.
//
// Every conversation-scoped write goes through here, not only the four this
// adapter started with. chatstore's routeFence refuses an unleased write to any
// conversation the route authority has placed, so an uncovered write method is
// not a missing optimisation: it is a method that can never succeed once the
// conversation is routed, which is exactly how DeletePost, EditPost, the
// reaction, pin and membership writes and UpdateConversation came to fail
// every time.
//
// A conversation the directory does not know is passed through unleased on
// purpose. The store records no shard for it and fences it on route state
// alone, so requiring a lease here would refuse writes the store itself
// accepts — including every conversation created before this adapter existed.
func (s *Service) routeContext(ctx context.Context, tenant, conversation string) (context.Context, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(conversation) == "" {
		// The inner service owns request validation and says so in its own
		// vocabulary; resolving a route for an empty identifier would answer
		// with a routing error instead.
		return ctx, nil
	}
	lease, err := s.cache.Resolve(ctx, s.directory, conversation, tenant)
	if err != nil {
		if errors.Is(err, chatrouting.ErrNotFound) {
			return ctx, nil
		}
		return nil, err
	}
	if err = chatrouting.CheckWrite(ctx, s.directory, lease, tenant, s.now()); err != nil {
		s.cache.Invalidate(conversation, lease.Route.Epoch)
		// A fence refusal is a placement condition, not a bad request: the same
		// call succeeds against the current placement. Naming it as the chat
		// contract's retryable unavailable is what lets the transport classify
		// it instead of logging an unclassified internal failure.
		return nil, fmt.Errorf("%w: %w", chat.ErrUnavailable, err)
	}
	return chatrouting.WithWriteLease(ctx, lease), nil
}

// leaseWrite runs one conversation-scoped write under its route lease. It is a
// free function rather than a method because Go methods cannot be generic, and
// the alternative — one hand-written resolve/check/lease block per write — is
// how the coverage gap opened in the first place.
func leaseWrite[T any](ctx context.Context, s *Service, tenant, conversation string, fn func(context.Context) (T, error)) (T, error) {
	leased, err := s.routeContext(ctx, tenant, conversation)
	if err != nil {
		var zero T
		return zero, err
	}
	return fn(leased)
}

func (s *Service) SendPost(ctx context.Context, r chat.SendPostRequest) (chat.Post, error) {
	return leaseWrite(ctx, s, r.TenantID, r.ConversationID, func(c context.Context) (chat.Post, error) {
		return s.ConversationService.SendPost(c, r)
	})
}

func (s *Service) UpdateConversation(ctx context.Context, r chat.UpdateConversationRequest) (chat.Conversation, error) {
	return leaseWrite(ctx, s, r.Conversation.TenantID, r.Conversation.ID, func(c context.Context) (chat.Conversation, error) {
		return s.ConversationService.UpdateConversation(c, r)
	})
}

func (s *Service) AddMembership(ctx context.Context, r chat.AddMembershipRequest) (chat.Membership, error) {
	return leaseWrite(ctx, s, r.Membership.TenantID, r.Membership.ConversationID, func(c context.Context) (chat.Membership, error) {
		return s.ConversationService.AddMembership(c, r)
	})
}

func (s *Service) RemoveMembership(ctx context.Context, r chat.RemoveMembershipRequest) (chat.Membership, error) {
	return leaseWrite(ctx, s, r.TenantID, r.ConversationID, func(c context.Context) (chat.Membership, error) {
		return s.ConversationService.RemoveMembership(c, r)
	})
}

func (s *Service) EditPost(ctx context.Context, r chat.EditPostRequest) (chat.Post, error) {
	return leaseWrite(ctx, s, r.TenantID, r.ConversationID, func(c context.Context) (chat.Post, error) {
		return s.ConversationService.EditPost(c, r)
	})
}

func (s *Service) DeletePost(ctx context.Context, r chat.DeletePostRequest) (chat.Post, error) {
	return leaseWrite(ctx, s, r.TenantID, r.ConversationID, func(c context.Context) (chat.Post, error) {
		return s.ConversationService.DeletePost(c, r)
	})
}

func (s *Service) AddReaction(ctx context.Context, r chat.AddReactionRequest) (chat.Reaction, error) {
	return leaseWrite(ctx, s, r.Reaction.TenantID, r.Reaction.ConversationID, func(c context.Context) (chat.Reaction, error) {
		return s.ConversationService.AddReaction(c, r)
	})
}

func (s *Service) RemoveReaction(ctx context.Context, r chat.RemoveReactionRequest) error {
	_, err := leaseWrite(ctx, s, r.TenantID, r.ConversationID, func(c context.Context) (struct{}, error) {
		return struct{}{}, s.ConversationService.RemoveReaction(c, r)
	})
	return err
}

func (s *Service) PinPost(ctx context.Context, r chat.PinPostRequest) (chat.Pin, error) {
	return leaseWrite(ctx, s, r.Pin.TenantID, r.Pin.ConversationID, func(c context.Context) (chat.Pin, error) {
		return s.ConversationService.PinPost(c, r)
	})
}

func (s *Service) UnpinPost(ctx context.Context, r chat.UnpinPostRequest) error {
	_, err := leaseWrite(ctx, s, r.TenantID, r.ConversationID, func(c context.Context) (struct{}, error) {
		return struct{}{}, s.ConversationService.UnpinPost(c, r)
	})
	return err
}

// UpdateReadState and UpdatePreferences are per-subject state on a conversation.
// The store does not fence them today, so they would work unleased; they are
// leased anyway because whether a row is fenced is the store's decision to
// change, and a caller that arrives with a stale placement should be told so
// once, here, rather than in whichever write the store fences next.
func (s *Service) UpdateReadState(ctx context.Context, r chat.UpdateReadStateRequest) (chat.ReadState, error) {
	return leaseWrite(ctx, s, r.ReadState.TenantID, r.ReadState.ConversationID, func(c context.Context) (chat.ReadState, error) {
		return s.ConversationService.UpdateReadState(c, r)
	})
}

func (s *Service) UpdatePreferences(ctx context.Context, r chat.UpdatePreferencesRequest) (chat.NotificationPreferences, error) {
	return leaseWrite(ctx, s, r.Preferences.TenantID, r.Preferences.ConversationID, func(c context.Context) (chat.NotificationPreferences, error) {
		return s.ConversationService.UpdatePreferences(c, r)
	})
}

func (s *Service) referenceService() (chat.ReferenceService, error) {
	v, ok := s.ConversationService.(chat.ReferenceService)
	if !ok {
		return nil, chat.ErrUnavailable
	}
	return v, nil
}

func (s *Service) SuggestReferences(ctx context.Context, r chat.SuggestReferencesRequest) ([]chat.ReferenceCandidate, error) {
	v, err := s.referenceService()
	if err != nil {
		return nil, err
	}
	return v.SuggestReferences(ctx, r)
}

func (s *Service) SendPostWithReferences(ctx context.Context, r chat.SendPostWithReferencesRequest) (chat.Post, error) {
	v, err := s.referenceService()
	if err != nil {
		return chat.Post{}, err
	}
	return leaseWrite(ctx, s, r.TenantID, r.ConversationID, func(c context.Context) (chat.Post, error) {
		return v.SendPostWithReferences(c, r)
	})
}

func (s *Service) CreateShareLink(ctx context.Context, p chat.Principal, tenant, conversation, post string) (chat.ConversationLink, error) {
	v, err := s.referenceService()
	if err != nil {
		return chat.ConversationLink{}, err
	}
	return v.CreateShareLink(ctx, p, tenant, conversation, post)
}

func (s *Service) ResolveShareLink(ctx context.Context, p chat.Principal, token string) (chat.Conversation, *chat.Post, error) {
	v, err := s.referenceService()
	if err != nil {
		return chat.Conversation{}, nil, err
	}
	return v.ResolveShareLink(ctx, p, token)
}

func (s *Service) ForwardPost(ctx context.Context, r chat.ForwardPostRequest) (chat.Post, error) {
	tenant := r.DestinationTenantID
	if tenant == "" {
		tenant = r.Principal.TenantID
	}
	v, err := s.referenceService()
	if err != nil {
		return chat.Post{}, err
	}
	return leaseWrite(ctx, s, tenant, r.DestinationConversationID, func(c context.Context) (chat.Post, error) {
		return v.ForwardPost(c, r)
	})
}
