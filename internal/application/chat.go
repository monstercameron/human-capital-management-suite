package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatadmission"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatappstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatauthority"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatrecordstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// unavailableChatAuthority keeps an enabled chat surface fail closed until a
// deployment supplies its current role and qualification reader. The chat
// service must never fall back to caller supplied role claims.
type unavailableChatAuthority struct{}

func (unavailableChatAuthority) Authorize(context.Context, chatcore.Principal, chatcore.Conversation, chatpolicy.Action, time.Time) (chatpolicy.Input, error) {
	return chatpolicy.Input{}, chatcore.ErrUnavailable
}

// ErrChatAuthorityUnavailable is returned when chat is enabled but no current
// governance reader was supplied. Mounting the routes with an authority that
// denies every call is a silently broken surface, so composition fails instead
// outside the local-dev profile.
var ErrChatAuthorityUnavailable = errors.New("application: chat authority facts are unavailable")

// ChatAdmissionConfig is the deployment override for the chat admission bounds.
// Only the fields a deployment sets are applied; the rest keep the composition
// defaults in defaultChatAdmissionConfig.
type ChatAdmissionConfig = chatadmission.Config

// ChatComposition is the chat boundary's own composition input. It lives with
// the chat composition rather than in the general serve options so the chat
// lanes and the policy freshness bound stay owned by chat.
type ChatComposition struct {
	// Admission overrides the chat admission bounds field by field.
	Admission ChatAdmissionConfig
	// AuthorityTTL bounds cached principal facts and channel policy. Zero means
	// DefaultChatAuthorityTTL.
	AuthorityTTL time.Duration
	// PollInterval is the durable-outbox fan-out period. Zero keeps 250ms.
	PollInterval time.Duration
}

// composedChat is the optional chat boundary owned by one serve composition.
// Its store has a separate pool and therefore a separate lifecycle from the
// core database pool carried by ServeInput.
type composedChat struct {
	service    chatcore.ConversationService
	extensions *ChatExtensions
	close      func()
}

// chatStreamPorts binds the live stream's read and authorization ports to the
// routed and audited service, the same decorator chain writes travel. Watching
// a conversation therefore passes route fencing and leaves the same audit
// evidence as sending to it; handing these ports the bare chat service would
// bypass both.
func chatStreamPorts(routed chatcore.ConversationService, membership chatMembershipResolver) (chatServiceReader, chatServiceStreamAuthorizer) {
	events, _ := membership.(chatDurableEventReader)
	return chatServiceReader{service: routed, membership: membership, events: events}, chatServiceStreamAuthorizer{service: routed, membership: membership}
}

func composeChat(ctx context.Context, cfg ServeConfig, now chatcore.Clock, facts ChatAuthorityFacts, corePool *pgxadapter.Pool, composition ...ChatComposition) (composedChat, error) {
	if !cfg.ChatEnabled {
		return composedChat{}, nil
	}
	var input ChatComposition
	if len(composition) > 0 {
		input = composition[0]
	}
	if facts == nil && cfg.Profile != ServeProfileLocalDev {
		// unavailableChatAuthority denies every call, so an enabled chat surface
		// composed without facts is a mounted route that can never answer.
		return composedChat{}, fmt.Errorf("%w in profile %q", ErrChatAuthorityUnavailable, cfg.Profile)
	}
	store, err := chatstore.New(ctx, chatstore.Config{DSN: cfg.ChatDatabaseURL, CoreDSN: cfg.DatabaseURL})
	if err != nil {
		return composedChat{}, fmt.Errorf("application: compose chat database: %w", err)
	}
	adapter := chatstore.NewAdapter(store)
	service := chatcore.NewService(adapter, now)
	apps := &chatapps.Service{Repo: chatappstore.NewChatStore(store), Secret: []byte(cfg.ChatCursorKey), Now: now}
	service.SetReferenceDirectory(chatReferenceDirectory{store: adapter, apps: apps})
	// One policy and grant authority over the chat database, shared by the
	// conversation authority and the company grant surface.
	policy := chatauthority.New(store)
	// The bounded authority cache is the policy freshness contract: steady-state
	// sends and stream rechecks read it instead of the core database, and every
	// revocation path invalidates the entries it affects.
	authorityCache := newChatAuthorityCache(input.AuthorityTTL, nil)
	if facts != nil {
		// Policy and grant authority use the same independent chat database
		// transaction boundary; authority never falls back to request claims.
		service.SetAuthority(newChatCurrentAuthority(facts, adapter, policy, authorityCache, apps.Repo))
	} else {
		service.SetAuthority(unavailableChatAuthority{})
	}
	records := &chatrecords.Service{Repo: chatrecordstore.New(store), Auth: ChatRecordAuthority{}, Clock: now}
	routed, err := composeChatRouting(ctx, &auditedChatService{ConversationService: service, records: records, atomicCore: true}, corePool)
	if err != nil {
		store.Close()
		return composedChat{}, fmt.Errorf("application: compose chat routing: %w", err)
	}
	grants := NewChatCompanyGrants(facts, policy)
	extensions := &ChatExtensions{
		Conversations: routed,
		Apps:          apps,
		Records:       records,
		Recipients:    &chatrecipient.Service{Conversations: routed, Repo: chatstore.NewRecipientStateStore(store)},
		Grants:        grants,
		TodoStore:     store,
	}
	apps.Authority = ChatAppAuthority{Conversations: routed}
	poll := input.PollInterval
	if poll <= 0 {
		poll = 250 * time.Millisecond
	}
	reader, authorizer := chatStreamPorts(routed, adapter)
	streamRuntime, err := NewChatStreamRuntime(ChatStreamRuntimeConfig{
		CursorKey: cfg.ChatCursorKey, Reader: reader, Authorizer: authorizer, PollInterval: poll,
		// QueueSize is comfortably above ReplayLimit plus one poll page: a
		// catch-up replay must not leave the queue with no room for the live
		// events the bridge publishes immediately afterwards.
		PageLimit: 100, QueueSize: 512, ReplayLimit: 100, CursorTTL: 15 * time.Minute,
		Budgets: mergeChatAdmissionConfig(defaultChatAdmissionConfig(), input.Admission),
	})
	if err != nil {
		if errors.Is(err, ErrChatStreamingDisabled) {
			grants.withRevocation(authorityCache, nil)
			return composedChat{service: &streamingChatService{ConversationService: routed, membership: adapter, authority: authorityCache}, extensions: extensions, close: store.Close}, nil
		}
		store.Close()
		return composedChat{}, fmt.Errorf("application: compose chat stream: %w", err)
	}
	// Grant revocation is event driven: revoking a cross-company grant drops the
	// cached authority and closes that tenant's live subscriptions.
	grants.withRevocation(authorityCache, streamRuntime)
	extensions.Admission = streamRuntime
	return composedChat{service: &streamingChatService{ConversationService: routed, runtime: streamRuntime, membership: adapter, authority: authorityCache}, extensions: extensions, close: store.Close}, nil
}
