package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatFilterIdentity struct {
	facts    ChatAuthorityFacts
	personas personaReferenceLookup
	now      func() time.Time
}

func (s chatFilterIdentity) FilterInput(ctx context.Context, p chat.Principal) (chatfilter.Input, error) {
	verified, ok := trust.FromContext(ctx)
	if !ok || verified.Subject() != p.SubjectID || string(verified.Tenant()) != p.TenantID {
		return chatfilter.Input{}, chatfilter.ErrDenied
	}
	base := chatfilter.Input{Tenant: p.TenantID, Subject: p.SubjectID, Agent: verified.SubjectKind() == trust.SubjectKindAgent}
	at := time.Now().UTC()
	if s.now != nil {
		at = s.now()
	}
	if at.Before(verified.IssuedAt()) || !at.Before(verified.ExpiresAt()) {
		return chatfilter.Input{}, chatfilter.ErrDenied
	}
	current, err := newChatAuthoritySource(s.facts).Resolve(ctx, p.TenantID, p.SubjectID, at)
	if err != nil {
		// Posting authority has already admitted the actor. Missing role facts
		// cannot grant an exemption, but must not bypass or disable filtering.
		return base, nil
	}
	base.Roles = current.Roles
	return base, nil
}

// NewChatFilterService composes persistence and current permission authority.
// The permission port is supplied by the chat moderation authority lane.
func NewChatFilterService(store *chatstore.Store, authority chatfilter.Authority, now func() time.Time) *chatfilter.Service {
	repo := chatstore.NewFilterStore(store)
	return &chatfilter.Service{Store: repo, Registry: chatfilter.NewRegistry(), Authority: authority, Delivery: repo, Now: now}
}
func newChatFilterPolicy(store *chatstore.Store, facts ChatAuthorityFacts, now func() time.Time) *chat.FilterContentPolicy {
	return &chat.FilterContentPolicy{Filters: NewChatFilterService(store, nil, now), Identity: chatFilterIdentity{facts: facts, now: now}, AuthorIdentity: chatFilterIdentity{facts: facts, now: now}, Conversations: chatstore.NewAdapter(store)}
}

func chatFilterBoundary(service *chat.Service) chat.ConversationService {
	return chat.WithFilterMetadata(service)
}

func (s chatFilterIdentity) FilterAuthorInput(ctx context.Context, post chat.Post) (chatfilter.Input, error) {
	input := chatfilter.Input{Tenant: post.AuthorHomeTenantID, Subject: post.AuthorID}
	actor, ok := trust.FromContext(ctx)
	if !ok || actor == nil {
		return input, chatfilter.ErrDenied
	}
	if actor.Tenant().String() != post.AuthorHomeTenantID || post.AuthorHomeTenantID != post.TenantID {
		return input, nil
	}
	at := time.Now()
	if s.now != nil {
		at = s.now()
	}
	if s.personas != nil {
		facts, err := s.personas.LookupPersonaReference(ctx, post.TenantID, post.ConversationID, post.AuthorID)
		input.Agent = err == nil && currentPersonaReference(facts, post.TenantID, post.ConversationID, post.AuthorID, at)
	}
	if actor.Subject() == post.AuthorID {
		author, err := s.FilterInput(ctx, chat.Principal{TenantID: post.AuthorHomeTenantID, SubjectID: post.AuthorID})
		author.Agent = author.Agent || input.Agent
		return author, err
	}
	facts := s.facts
	if cached, ok := facts.(cachedChatFacts); ok {
		facts = cached.inner
	}
	source, ok := facts.(chatstateCandidateSource)
	if !ok {
		return input, nil
	}
	candidates, err := source.ChannelStatusCandidates(ctx, post.TenantID, at)
	if err != nil {
		return input, err
	}
	for _, candidate := range candidates {
		if candidate.Tenant == post.AuthorHomeTenantID && candidate.ID == post.AuthorID && candidate.Current(at) {
			input.Roles = candidate.Roles
			break
		}
	}
	return input, nil
}
