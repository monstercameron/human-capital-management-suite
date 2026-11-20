package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
)

// Issue 3809, first slice: Chat events out to signed webhooks.
//
// An administrator allows a public channel to publish. Every message committed
// to an allowed channel is then offered to the company's active webhook
// subscriptions through the same managed journal the clock events use: the
// subscription's declared fields are the field mask, its grant is rechecked on
// every read, the projection is signed, and a replayed cursor is the same
// idempotent delivery rather than a second send. A direct message, a group, a
// private channel, a post addressed to a restricted audience, a deleted post
// and anything from before the allowance never reaches this code, because the
// store does not return it.

const (
	// ChatWebhookSchemaVersion is the payload schema both chat event kinds carry.
	ChatWebhookSchemaVersion = 1
	// ChatWebhookMaxBatch bounds one publish.
	ChatWebhookMaxBatch = chatstore.WebhookPostBatch
)

var (
	ErrChatWebhooksUnavailable = errors.New("chat webhooks: publisher is unavailable")
	ErrChatWebhookDelivery     = errors.New("chat webhooks: delivery failed")
)

// ChatWebhookStore is the durable allowance and the committed posts of allowed
// channels; *chatstore.Store implements it.
type ChatWebhookStore interface {
	AllowWebhookChannel(ctx context.Context, tenant, conversation, actor string) (bool, error)
	RemoveWebhookChannel(ctx context.Context, tenant, conversation, actor string) (bool, error)
	ListWebhookChannels(ctx context.Context, tenant string) ([]chatstore.WebhookChannel, error)
	ListWebhookPosts(ctx context.Context, tenant string, after int64, limit int) (chatstore.WebhookPostPage, error)
}

// ChatWebhookAdmins decides who may change which channels publish. It must
// fail closed: any error or doubt is a no.
type ChatWebhookAdmins interface {
	CanManageChatWebhooks(ctx context.Context, tenant, subject string) bool
}

// ChatWebhookSubscriptions supplies the company's current subscription
// revisions for the chat event kinds and their separate authorization grants.
type ChatWebhookSubscriptions interface {
	ActiveChatWebhookSubscriptions(ctx context.Context, tenant string) ([]subscription.EventSubscription, map[string]subscription.ScopeGrant, error)
}

// ChatWebhookEvent is a minimally disclosed, signed event projection.
type ChatWebhookEvent struct {
	Tenant        string                         `json:"tenant"`
	Sequence      int64                          `json:"sequence"`
	EventType     string                         `json:"event_type"`
	SchemaVersion int                            `json:"schema_version"`
	Payload       map[string]any                 `json:"payload"`
	Envelope      subscription.CanonicalEnvelope `json:"envelope"`
	Signature     subscription.SignedDelivery    `json:"signature"`
}

// ChatWebhookAttempt is one delivery attempt with the field-masked projection.
// The provider owns HTTP, destination trust and response parsing; it never sees
// the stored post.
type ChatWebhookAttempt struct {
	Delivery subscription.DeliveryAttempt
	Event    ChatWebhookEvent
}

// ChatWebhookProvider is the transport boundary for an outbound webhook.
type ChatWebhookProvider interface {
	DeliverChatEvent(context.Context, ChatWebhookAttempt) (subscription.ProviderReceipt, error)
}

// ChatWebhookDelivery records one subscription's projection and its journal result.
type ChatWebhookDelivery struct {
	SubscriptionID string                      `json:"subscription_id"`
	Revision       uint64                      `json:"revision"`
	Event          ChatWebhookEvent            `json:"event"`
	Result         subscription.DeliveryResult `json:"delivery"`
}

// ChatWebhookPage is the result of one publish. Through is the outbox position
// to resume from; it is only advanced once every delivery of the page was
// accepted or journaled as a duplicate. Skipped counts subscriptions that were
// not offered the page because their grant no longer covers them or their scope
// cannot be proven against a channel.
type ChatWebhookPage struct {
	Deliveries []ChatWebhookDelivery `json:"deliveries"`
	Through    int64                 `json:"through"`
	Skipped    int                   `json:"skipped"`
}

// ChatWebhooks composes the allowance, the projection and the delivery journal.
type ChatWebhooks struct {
	Store         ChatWebhookStore
	Admins        ChatWebhookAdmins
	Subscriptions ChatWebhookSubscriptions
	Signers       ChatWebhookSigners
	Journal       *subscription.DeliveryJournal
	Provider      ChatWebhookProvider
	Now           func() time.Time
}

func (w *ChatWebhooks) now() time.Time {
	if w != nil && w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

// ChatWebhookSchemas returns a fresh schema registry for the two chat event
// kinds. Message text is classified confidential so a subscription must name it
// to receive it.
func ChatWebhookSchemas() *subscription.SchemaRegistry {
	r := subscription.NewSchemaRegistry()
	for kind, fields := range chatWebhookSchemaFields() {
		s, err := subscription.NewEventSchema(kind, ChatWebhookSchemaVersion, fields)
		if err != nil || r.Register(s) != nil {
			return nil
		}
	}
	return r
}

func chatWebhookSchemaFields() map[subscription.EventKind][]subscription.SchemaField {
	field := func(name string, typ subscription.FieldType, class subscription.FieldClassification) subscription.SchemaField {
		return subscription.SchemaField{Name: name, Type: typ, Classification: class}
	}
	c, i := subscription.ClassificationConfidential, subscription.ClassificationInternal
	return map[subscription.EventKind][]subscription.SchemaField{
		subscription.EventChatMessagePosted: {
			field("conversation_id", subscription.TypeRef, i), field("conversation_name", subscription.TypeString, i),
			field("post_id", subscription.TypeRef, i), field("author_id", subscription.TypeRef, c),
			field("thread_id", subscription.TypeRef, i), field("body", subscription.TypeString, c),
			field("sequence", subscription.TypeInteger, i), field("posted_at", subscription.TypeInstant, i),
		},
		subscription.EventChatApprovalDecided: {
			field("conversation_id", subscription.TypeRef, i), field("approval_id", subscription.TypeRef, i),
			field("decision", subscription.TypeString, i), field("decided_by", subscription.TypeRef, c),
			field("decided_at", subscription.TypeInstant, i),
		},
	}
}

// VerifyChatWebhookEvent checks that a delivered payload still matches the
// signed projected digest before a consumer uses it.
func VerifyChatWebhookEvent(event ChatWebhookEvent, signer *subscription.CredentialRing, at time.Time) error {
	if signer == nil || event.Tenant != event.Envelope.Tenant || event.Sequence <= 0 ||
		uint64(event.Sequence) != event.Envelope.Sequence || event.EventType != string(event.Envelope.Kind) ||
		event.SchemaVersion != event.Envelope.SchemaVersion {
		return errors.New("chat webhooks: invalid event")
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil || subscription.DigestValue(string(payload)) != event.Envelope.PayloadDigest {
		return errors.New("chat webhooks: payload does not match its digest")
	}
	if err := event.Envelope.Validate(); err != nil {
		return err
	}
	return signer.Verify(event.Signature, event.Envelope.Digest(), at)
}

// manage authorizes a change or a read of the allowance before anything is read
// or written. Only a signed-in human administrator of the same company passes.
func (w *ChatWebhooks) manage(ctx context.Context, p chat.Principal) (ChatWebhookStore, error) {
	if w == nil || w.Store == nil || w.Admins == nil {
		return nil, chat.ErrUnavailable
	}
	if err := chat.ValidateSavedOwner(ctx, p, p.TenantID); err != nil {
		return nil, err
	}
	if !w.Admins.CanManageChatWebhooks(ctx, p.TenantID, p.SubjectID) {
		return nil, chat.ErrPermissionDenied
	}
	return w.Store, nil
}

// AllowChannel lets an administrator publish a public channel's messages.
func (w *ChatWebhooks) AllowChannel(ctx context.Context, p chat.Principal, conversationID string) (bool, error) {
	store, err := w.manage(ctx, p)
	if err != nil {
		return false, err
	}
	return store.AllowWebhookChannel(ctx, p.TenantID, conversationID, p.SubjectID)
}

// RemoveChannel stops a channel publishing.
func (w *ChatWebhooks) RemoveChannel(ctx context.Context, p chat.Principal, conversationID string) (bool, error) {
	store, err := w.manage(ctx, p)
	if err != nil {
		return false, err
	}
	return store.RemoveWebhookChannel(ctx, p.TenantID, conversationID, p.SubjectID)
}

// Channels lists the channels an administrator allowed to publish.
func (w *ChatWebhooks) Channels(ctx context.Context, p chat.Principal) ([]chatstore.WebhookChannel, error) {
	store, err := w.manage(ctx, p)
	if err != nil {
		return nil, err
	}
	return store.ListWebhookChannels(ctx, p.TenantID)
}

// PublishMessages offers the committed posts of allowed channels after an outbox
// position to the company's active subscriptions. Each subscription gets its
// own field-masked, signed projection through the shared idempotent journal. A
// company with no active subscription is not an error: its page is consumed
// without a delivery, because a subscription only receives events from after it
// was made.
func (w *ChatWebhooks) PublishMessages(ctx context.Context, tenant string, after int64, limit int) (ChatWebhookPage, error) {
	if w == nil || w.Store == nil || w.Subscriptions == nil || w.Signers == nil || w.Journal == nil || w.Provider == nil ||
		strings.TrimSpace(tenant) == "" || after < 0 || limit <= 0 || limit > ChatWebhookMaxBatch {
		return ChatWebhookPage{}, ErrChatWebhooksUnavailable
	}
	revisions, grants, err := w.Subscriptions.ActiveChatWebhookSubscriptions(ctx, tenant)
	if err != nil {
		return ChatWebhookPage{}, err
	}
	page := ChatWebhookPage{Deliveries: []ChatWebhookDelivery{}, Through: after}
	eligible := make([]subscription.EventSubscription, 0, len(revisions))
	for _, revision := range revisions {
		if !chatWebhookEligible(tenant, revision, grants[revision.SubscriptionID]) {
			page.Skipped++
			continue
		}
		eligible = append(eligible, revision)
	}
	posts, err := w.Store.ListWebhookPosts(ctx, tenant, after, limit)
	if err != nil {
		return ChatWebhookPage{}, err
	}
	for _, post := range posts.Posts {
		for _, revision := range eligible {
			event, ok, err := w.project(tenant, post, revision, grants[revision.SubscriptionID])
			if err != nil {
				return ChatWebhookPage{}, err
			}
			if !ok {
				continue
			}
			request := subscription.DeliveryRequest{
				SubscriptionID: revision.SubscriptionID, SubscriptionRevision: revision.Revision,
				Envelope: event.Envelope, Destination: revision.DeliveryEndpointRef,
				OrderingKey:  strings.Join([]string{tenant, post.ConversationID}, "\x00"),
				OrderingMode: subscription.OrderingIndependent, Signature: event.Signature,
			}
			result, err := w.Journal.Deliver(ctx, chatWebhookJournalProvider{provider: w.Provider, event: event}, request)
			if err != nil {
				return ChatWebhookPage{}, fmt.Errorf("%w: subscription=%s sequence=%d: %v", ErrChatWebhookDelivery, revision.SubscriptionID, post.OutboxID, err)
			}
			page.Deliveries = append(page.Deliveries, ChatWebhookDelivery{SubscriptionID: revision.SubscriptionID, Revision: revision.Revision, Event: event, Result: result})
		}
	}
	page.Through = posts.Through
	return page, nil
}

// chatWebhookEligible is true for an active subscription of this company that
// asks for chat message events and whose current grant authorizes it. A
// subscription scoped to an organization or a population is refused: a channel
// carries no organization or population metadata to prove that scope against.
func chatWebhookEligible(tenant string, revision subscription.EventSubscription, grant subscription.ScopeGrant) bool {
	if revision.State != subscription.StateActive || revision.TenantScope != tenant ||
		revision.OrganizationScopeRef != "" || revision.PopulationScopeRef != "" {
		return false
	}
	wants := false
	for _, kind := range revision.EventKinds {
		wants = wants || kind == subscription.EventChatMessagePosted
	}
	if !wants {
		return false
	}
	_, err := subscription.Authorize(revision, grant)
	return err == nil
}

func (w *ChatWebhooks) project(tenant string, post chatstore.WebhookPost, revision subscription.EventSubscription, grant subscription.ScopeGrant) (ChatWebhookEvent, bool, error) {
	kind := subscription.EventChatMessagePosted
	payload := map[string]any{
		"conversation_id": post.ConversationID, "conversation_name": post.ConversationName,
		"post_id": post.PostID, "author_id": post.Author, "body": post.Body,
		"sequence": post.Sequence, "posted_at": post.PostedAt.UTC().Format(time.RFC3339Nano),
	}
	if post.ParentID != "" {
		payload["thread_id"] = post.ParentID
	}
	fieldDigests := make(map[string]string, len(payload))
	for name, value := range payload {
		b, _ := json.Marshal(value)
		fieldDigests[name] = subscription.DigestValue(string(b))
	}
	full, err := json.Marshal(payload)
	if err != nil {
		return ChatWebhookEvent{}, false, err
	}
	digest := subscription.EventDigest{TenantScope: tenant, Kind: kind, Digest: subscription.DigestValue(string(full)), FieldDigests: fieldDigests}
	matches, _, err := subscription.MatchAuthorized(digest, []subscription.EventSubscription{revision}, map[string]subscription.ScopeGrant{revision.SubscriptionID: grant})
	if err != nil || len(matches) == 0 {
		// A grant that no longer covers the subscription is a refusal, not a
		// reason to stop the others.
		return ChatWebhookEvent{}, false, nil
	}
	allowed := map[string]bool{}
	for _, name := range revision.DeclaredFields[kind] {
		allowed[name] = true
	}
	for name := range payload {
		if !allowed[name] {
			delete(payload, name)
		}
	}
	projected, err := json.Marshal(payload)
	if err != nil {
		return ChatWebhookEvent{}, false, err
	}
	env := subscription.CanonicalEnvelope{
		Tenant: tenant, Kind: kind, SchemaVersion: ChatWebhookSchemaVersion,
		SubjectRefs: []string{"chat-message:" + post.PostID}, EffectiveAt: post.PostedAt.UTC(), KnownAt: post.PostedAt.UTC(),
		PayloadDigest: subscription.DigestValue(string(projected)), ProvenanceRef: "chat-outbox:" + strconv.FormatInt(post.OutboxID, 10),
		Sequence: uint64(post.OutboxID),
	}
	if err := env.Validate(); err != nil {
		return ChatWebhookEvent{}, false, err
	}
	signer := w.Signers.SignerFor(revision.DeliveryEndpointRef)
	if signer == nil {
		return ChatWebhookEvent{}, false, nil
	}
	signed, err := signer.Sign(env.Digest(), w.now())
	if err != nil {
		return ChatWebhookEvent{}, false, err
	}
	return ChatWebhookEvent{Tenant: tenant, Sequence: post.OutboxID, EventType: string(kind), SchemaVersion: ChatWebhookSchemaVersion, Payload: payload, Envelope: env, Signature: signed}, true, nil
}

type chatWebhookJournalProvider struct {
	provider ChatWebhookProvider
	event    ChatWebhookEvent
}

func (p chatWebhookJournalProvider) Deliver(ctx context.Context, attempt subscription.DeliveryAttempt) (subscription.ProviderReceipt, error) {
	event := p.event
	event.Payload = make(map[string]any, len(p.event.Payload))
	for key, value := range p.event.Payload {
		event.Payload[key] = value
	}
	event.Envelope.SubjectRefs = append([]string(nil), p.event.Envelope.SubjectRefs...)
	return p.provider.DeliverChatEvent(ctx, ChatWebhookAttempt{Delivery: attempt, Event: event})
}

var _ ChatWebhookStore = (*chatstore.Store)(nil)

// ChatWebhookSigners returns the signing credentials of a delivery endpoint, or
// nil when the endpoint has none. An endpoint that cannot sign is not delivered to.
type ChatWebhookSigners interface {
	SignerFor(destination string) *subscription.CredentialRing
}
