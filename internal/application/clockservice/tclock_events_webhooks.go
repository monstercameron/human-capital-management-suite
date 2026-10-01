package clockservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
)

var (
	ErrClockWebhooksUnavailable = errors.New("clock webhooks: publisher is unavailable")
	ErrClockWebhookDelivery     = errors.New("clock webhooks: delivery failed")
)

// ClockWebhookRequest selects one committed outbox page. Cursor replay is
// intentional: the delivery journal turns a repeated request into the same
// idempotent operation rather than a second external effect.
type ClockWebhookRequest struct {
	Tenant, Organization, Worker, Cursor string
	Limit                                int
}

// ClockWebhookAttempt is the generic subscription delivery attempt together
// with the already-authorized, field-masked clock projection. The subscription
// journal owns retries and idempotency; this adapter only supplies the
// domain-neutral envelope and the safe payload to the provider boundary.
type ClockWebhookAttempt struct {
	Delivery subscription.DeliveryAttempt
	Event    ClockEvent
}

// ClockWebhookProvider is the transport boundary for an outbound webhook.
// Implementations own HTTP, destination trust and response parsing; they do
// not receive the committed outbox row or any unprojected payload.
type ClockWebhookProvider interface {
	DeliverClockEvent(context.Context, ClockWebhookAttempt) (subscription.ProviderReceipt, error)
}

// ClockWebhookDelivery records one subscription-specific projection and its
// generic delivery-journal result.
type ClockWebhookDelivery struct {
	SubscriptionID string                      `json:"subscription_id"`
	Revision       uint64                      `json:"revision"`
	Event          ClockEvent                  `json:"event"`
	Result         subscription.DeliveryResult `json:"delivery"`
}

// ClockWebhookPage is the successful result of publishing one outbox page.
// NextCursor is only returned after every provider call in the page has been
// accepted or durably journaled as a duplicate.
type ClockWebhookPage struct {
	Deliveries []ClockWebhookDelivery `json:"deliveries"`
	NextCursor string                 `json:"next_cursor,omitempty"`
}

// ClockWebhookPublisher composes the clock projection with the managed
// subscription delivery journal. No clock-specific retry loop is kept here.
type ClockWebhookPublisher struct {
	feed     *ClockEventFeed
	journal  *subscription.DeliveryJournal
	provider ClockWebhookProvider
}

// NewClockWebhookPublisher binds a feed to the shared managed delivery
// journal and a provider adapter.
func NewClockWebhookPublisher(feed *ClockEventFeed, journal *subscription.DeliveryJournal, provider ClockWebhookProvider) (*ClockWebhookPublisher, error) {
	if feed == nil || journal == nil || provider == nil {
		return nil, ErrClockWebhooksUnavailable
	}
	return &ClockWebhookPublisher{feed: feed, journal: journal, provider: provider}, nil
}

// Publish reads only committed outbox rows, rechecks current subscription
// authorization, applies each subscription's field mask, signs the projected
// envelope and delivers through the shared idempotent journal.
func (p *ClockWebhookPublisher) Publish(ctx context.Context, req ClockWebhookRequest) (ClockWebhookPage, error) {
	if p == nil || p.feed == nil || p.journal == nil || p.provider == nil || strings.TrimSpace(req.Tenant) == "" || req.Limit <= 0 || req.Limit > 1000 {
		return ClockWebhookPage{}, ErrClockWebhooksUnavailable
	}
	feed := p.feed
	if err := feed.authorizer.AuthorizeClockEvents(ctx, req.Tenant, req.Organization, req.Worker); err != nil {
		return ClockWebhookPage{}, fmt.Errorf("%w: %v", ErrClockScopeDenied, err)
	}
	cursor, err := feed.decodeCursor(req.Cursor, req.Tenant, req.Organization, req.Worker)
	if err != nil {
		return ClockWebhookPage{}, err
	}
	revisions, grants, err := feed.currentSubscriptions(ctx, req.Tenant)
	if err != nil {
		return ClockWebhookPage{}, err
	}
	if err := validateFeedSubscriptions(req.Tenant, revisions, grants); err != nil {
		return ClockWebhookPage{}, err
	}
	rows, scoped, err := feed.readWebhookRows(ctx, req, cursor.Sequence)
	if err != nil {
		return ClockWebhookPage{}, err
	}
	if err := validateOutboxRows(rows, req.Tenant, cursor.Sequence); err != nil {
		return ClockWebhookPage{}, err
	}

	page := ClockWebhookPage{Deliveries: make([]ClockWebhookDelivery, 0)}
	for i, row := range rows {
		var organization, worker string
		if len(scoped) > 0 {
			organization, worker = scoped[i].Organization, scoped[i].Worker
		}
		for _, revision := range revisions {
			grant, exists := grants[revision.SubscriptionID]
			if !exists {
				return ClockWebhookPage{}, ErrClockScopeDenied
			}
			event, ok, err := feed.project(req.Tenant, row, organization, worker, req.Organization, req.Worker, []subscription.EventSubscription{revision}, map[string]subscription.ScopeGrant{revision.SubscriptionID: grant})
			if err != nil {
				return ClockWebhookPage{}, err
			}
			if !ok {
				continue
			}
			request := subscription.DeliveryRequest{
				SubscriptionID:       revision.SubscriptionID,
				SubscriptionRevision: revision.Revision,
				Envelope:             event.Envelope,
				Destination:          revision.DeliveryEndpointRef,
				OrderingKey:          webhookOrderingKey(req.Tenant, organization, worker),
				OrderingMode:         subscription.OrderingIndependent,
				Signature:            event.Signature,
			}
			result, err := p.journal.Deliver(ctx, clockWebhookProvider{provider: p.provider, event: cloneClockEvent(event)}, request)
			if err != nil {
				return ClockWebhookPage{}, fmt.Errorf("%w: subscription=%s sequence=%d: %v", ErrClockWebhookDelivery, revision.SubscriptionID, row.Sequence, err)
			}
			page.Deliveries = append(page.Deliveries, ClockWebhookDelivery{SubscriptionID: revision.SubscriptionID, Revision: revision.Revision, Event: event, Result: result})
		}
		cursor.Sequence = row.Sequence
	}
	cursor.ExpiresAt = 0
	if cursor.Sequence > 0 {
		page.NextCursor, err = feed.IssueClockEventCursor(cursor)
	}
	return page, err
}

type clockWebhookProvider struct {
	provider ClockWebhookProvider
	event    ClockEvent
}

func (p clockWebhookProvider) Deliver(ctx context.Context, attempt subscription.DeliveryAttempt) (subscription.ProviderReceipt, error) {
	return p.provider.DeliverClockEvent(ctx, ClockWebhookAttempt{Delivery: attempt, Event: cloneClockEvent(p.event)})
}

func webhookOrderingKey(tenant, organization, worker string) string {
	return strings.Join([]string{tenant, organization, worker}, "\x00")
}

func cloneClockEvent(event ClockEvent) ClockEvent {
	payload := event.Payload
	event.Payload = make(map[string]any, len(event.Payload))
	for key, value := range payload {
		event.Payload[key] = value
	}
	event.Envelope.SubjectRefs = append([]string(nil), event.Envelope.SubjectRefs...)
	return event
}

func (f *ClockEventFeed) readWebhookRows(ctx context.Context, req ClockWebhookRequest, after int64) ([]OutboxEvent, []ScopedOutboxEvent, error) {
	if req.Organization != "" || req.Worker != "" {
		source, ok := f.source.(ScopedClockEventSource)
		if !ok {
			return nil, nil, fmt.Errorf("%w: trusted organization/worker metadata unavailable", ErrClockScopeDenied)
		}
		scoped, err := source.ListScopedEvents(ctx, req.Tenant, req.Organization, req.Worker, after, req.Limit)
		if err != nil {
			return nil, nil, err
		}
		if len(scoped) > req.Limit {
			return nil, nil, ErrClockEventInvalid
		}
		rows := make([]OutboxEvent, len(scoped))
		for i := range scoped {
			rows[i] = scoped[i].OutboxEvent
		}
		return rows, scoped, nil
	}
	rows, err := f.source.ListEvents(ctx, req.Tenant, after, req.Limit)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) > req.Limit {
		return nil, nil, ErrClockEventInvalid
	}
	return rows, nil, nil
}
