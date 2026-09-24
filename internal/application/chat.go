package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
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
	intentapp "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// chatAppsIntentAdapter is chatapps.BusinessIntentPort's real production
// implementation (CHAT-045): a typed proposal from an installed chat agent
// becomes exactly the CreateIntentRequest an authenticated human would
// submit, through the same IntentService.CreateIntent the served intent
// transports call. The initiator is read from the caller's own authenticated
// context (trust.FromContext), never from anything the proposal claims about
// itself, so the kernel's ordinary RBAC-RT-003 authorization decides it: an
// agent installation cannot buy authority the acting principal does not
// already have. Proposals default to EXECUTION_MODE_SIMULATE, so an agent
// action enters normal workflow admission for review rather than committing
// on its own say-so.
type chatAppsIntentAdapter struct {
	intent *intentapp.IntentService
}

func (a chatAppsIntentAdapter) Propose(ctx context.Context, p chatapps.Proposal) (chatapps.ProposalReceipt, error) {
	if a.intent == nil {
		return chatapps.ProposalReceipt{}, chatcore.ErrUnavailable
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return chatapps.ProposalReceipt{}, chatcore.ErrUnavailable
	}
	initiatorKind := intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN
	switch principal.SubjectKind() {
	case trust.SubjectKindAgent:
		initiatorKind = intentsv1.InitiatorKind_INITIATOR_KIND_AGENT
	case trust.SubjectKindService:
		initiatorKind = intentsv1.InitiatorKind_INITIATOR_KIND_SERVICE
	case trust.SubjectKindIntegration:
		initiatorKind = intentsv1.InitiatorKind_INITIATOR_KIND_INTEGRATION
	}
	subjects := make([]*intentsv1.SubjectReference, 0, len(p.Subjects))
	for _, s := range p.Subjects {
		subjects = append(subjects, &intentsv1.SubjectReference{
			SubjectKind: s.Kind, SubjectId: s.ID, AuthorityDomain: s.AuthorityDomain,
		})
	}
	payload := p.RequestPayload
	if payload == nil {
		payload = map[string]any{}
	}
	raw, err := structpb.NewStruct(payload)
	if err != nil {
		return chatapps.ProposalReceipt{}, fmt.Errorf("application: encode chat-app proposal payload: %w", err)
	}
	wire, err := proto.Marshal(raw)
	if err != nil {
		return chatapps.ProposalReceipt{}, fmt.Errorf("application: marshal chat-app proposal payload: %w", err)
	}
	req := &intentsv1.CreateIntentRequest{
		IdempotencyKey: p.IdempotencyKey,
		Definition:     &intentsv1.DefinitionReference{IntentTypeId: p.IntentType, Version: p.SchemaVersion},
		Initiator: &intentsv1.PrincipalReference{
			PrincipalId: principal.Subject(), Kind: initiatorKind, IdentityAssuranceRef: principal.EvidenceID(),
		},
		Subjects: subjects,
		Request: &intentsv1.TypedPayload{
			Schema: &intentsv1.SchemaReference{
				SchemaId: p.SchemaID, Version: p.SchemaVersion, ProtobufFullName: p.ProtoFullName,
			},
			ProtobufWireBytes: wire,
		},
		ExecutionMode: intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE,
	}
	resp, err := a.intent.CreateIntent(ctx, req)
	if err != nil {
		return chatapps.ProposalReceipt{}, err
	}
	return chatapps.ProposalReceipt{
		IntentID: resp.GetIntent().GetIntentId(),
		Status:   resp.GetIntent().GetLifecycle().GetRequest().String(),
	}, nil
}

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
	// StreamRecheckInterval bounds how long a live subscription can outlive a
	// principal's current role or worker status when no revocation event exists.
	// Zero uses DefaultChatStreamRecheckInterval.
	StreamRecheckInterval time.Duration
}

// DefaultChatStreamRecheckInterval caps idle stream authority staleness. Session
// revocation is checked on each recheck; role and worker facts also expire from
// the authority cache after DefaultChatAuthorityTTL.
const DefaultChatStreamRecheckInterval = 5 * time.Second

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

func composeChat(ctx context.Context, cfg ServeConfig, now chatcore.Clock, facts ChatAuthorityFacts, corePool *pgxadapter.Pool, intentService *intentapp.IntentService, composition ...ChatComposition) (composedChat, error) {
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
	if intentService != nil {
		// CHAT-045: an agent's proposed HCM action is carried through the real
		// application/intent path (internal/intent/app.IntentService.CreateIntent),
		// under the caller's own authenticated context, not a same-package fake.
		apps.Intent = chatAppsIntentAdapter{intent: intentService}
	}
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
	recheck := input.StreamRecheckInterval
	if recheck <= 0 || recheck > DefaultChatStreamRecheckInterval {
		recheck = DefaultChatStreamRecheckInterval
	}
	streamRuntime, err := NewChatStreamRuntime(ChatStreamRuntimeConfig{
		CursorKey: cfg.ChatCursorKey, Reader: reader, Authorizer: authorizer, PollInterval: poll,
		RecheckInterval: recheck,
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
