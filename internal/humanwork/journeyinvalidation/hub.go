// Package journeyinvalidation is REV-091-03's live delivery path for
// PROMOUX-011's promotion invalidations: a tenant-scoped, in-process hub that
// the journey engine feeds after a promotion transition has committed, and
// that every open WatchPromotionInvalidations stream reads from.
//
// # What travels where
//
// The engine calls [Hub.Publish] with a [Committed] record only after the
// transaction that changed the journey has committed, and only once
// internal/data/promotioninvalidation.NextSequence has given that transition
// its durable tenant-wide position. The record names nothing but the tenant,
// the journey's intent id and that position; it is never sent to anybody as
// it is.
//
// Each subscription filters every record by its own viewer's authority before
// anything reaches the wire (see [Subscription.Next]): the journey must still
// be visible to that viewer through the engine's own read, and the resulting
// hint must pass productquery.EmitInvalidation's authorization check through
// internal/domains/promotion.SubscriberSequencer, which is also what numbers
// the messages. A record the viewer may not see is dropped without touching
// that viewer's sequence, so the numbers a viewer receives never reveal how
// many transitions they were not told about.
//
// # What this package does not promise
//
// The hub is in-process. A transition committed by another process (a
// separately deployed scheduler, for example) reaches this hub only if that
// process publishes into it; a client that misses a hint for any reason --
// a dropped connection, a lagging subscription, a restart -- converges on its
// next reconnect, whose catch-up is an authoritative re-read, never a replay.
package journeyinvalidation

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Bounds. They are constants rather than options a caller chooses per
// request, for the same reason the journey watch's are: a feed whose buffer
// and population are chosen by its reader is not bounded.
const (
	// DefaultBuffer is how many committed records one subscription may hold
	// before it is declared lagging. A browser that stops reading for this
	// many transitions is better served by one authoritative re-read on
	// reconnect than by a replay of every hint it missed.
	DefaultBuffer = 64
	// DefaultMaxSubscribersPerTenant caps the open subscriptions one tenant
	// may hold in this process.
	DefaultMaxSubscribersPerTenant = 256
	// MaxAfterSequence bounds a resuming client's claimed position, so the
	// offset added to every later sequence can never overflow.
	MaxAfterSequence = uint64(1) << 62
)

var (
	// ErrInvalid means a subscribe request or a committed record is
	// incomplete or malformed.
	ErrInvalid = errors.New("journeyinvalidation: invalid input")
	// ErrLagged means the subscription fell further behind than its buffer
	// and some records were not kept. The stream should end so the client
	// reconnects and re-reads authoritatively.
	ErrLagged = errors.New("journeyinvalidation: subscription lagged behind its buffer")
	// ErrRevoked means the viewer's authority to keep receiving hints ended
	// while the subscription was open.
	ErrRevoked = errors.New("journeyinvalidation: subscription authority revoked")
	// ErrClosed means the subscription or its hub was closed.
	ErrClosed = errors.New("journeyinvalidation: subscription closed")
	// ErrTooManySubscribers means the tenant already holds the maximum
	// number of open subscriptions.
	ErrTooManySubscribers = errors.New("journeyinvalidation: too many subscriptions for this tenant")
)

// Committed is one durable promotion transition, recorded after its
// transaction committed. It is an internal record, never a wire message.
type Committed struct {
	Tenant   values.TenantId
	IntentID string
	// Revision is the transition's durable tenant-wide position from
	// internal/data/promotioninvalidation.NextSequence. It becomes the
	// emitted item's revision; it is never a subscriber's sequence number.
	Revision uint64
}

// Validate reports whether c is complete. The zero value fails.
func (c Committed) Validate() error {
	if err := c.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant", ErrInvalid)
	}
	if c.IntentID == "" || len(c.IntentID) > 128 {
		return fmt.Errorf("%w: intent id", ErrInvalid)
	}
	if c.Revision == 0 {
		return fmt.Errorf("%w: revision is unset", ErrInvalid)
	}
	return nil
}

// Inspector is the engine read a subscription uses to decide whether its
// viewer may see a journey. It is workspace.JourneyEngine.Inspect; a refusal
// or an unknown journey means "not visible".
type Inspector interface {
	Inspect(ctx context.Context, intentID string) (workspace.JourneyDetail, error)
}

// Options bounds a hub.
type Options struct {
	Buffer                  int
	MaxSubscribersPerTenant int
	// Now supplies the evaluation instant for authorization. Nil means
	// time.Now.
	Now func() time.Time
}

// Hub fans committed records out to the open subscriptions of the record's
// own tenant. The zero value is not usable; use [NewHub].
type Hub struct {
	buffer, maxPerTenant int
	now                  func() time.Time

	mu     sync.Mutex
	subs   map[values.TenantId]map[*Subscription]struct{}
	nextID uint64
	// published counts records accepted for fan-out, for tests and
	// observability only.
	published uint64
}

// NewHub returns an empty hub.
func NewHub(options Options) *Hub {
	if options.Buffer <= 0 {
		options.Buffer = DefaultBuffer
	}
	if options.MaxSubscribersPerTenant <= 0 {
		options.MaxSubscribersPerTenant = DefaultMaxSubscribersPerTenant
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Hub{
		buffer: options.Buffer, maxPerTenant: options.MaxSubscribersPerTenant, now: options.Now,
		subs: make(map[values.TenantId]map[*Subscription]struct{}),
	}
}

// Publish hands one committed record to every open subscription of its
// tenant. It never blocks: a subscription whose buffer is full is marked
// lagging instead of making the committing request wait. An invalid record
// is dropped; Publish reports whether the record was accepted.
func (h *Hub) Publish(record Committed) bool {
	if h == nil || record.Validate() != nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.published++
	for sub := range h.subs[record.Tenant] {
		sub.offer(record)
	}
	return true
}

// Published reports how many records the hub has accepted.
func (h *Hub) Published() uint64 {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.published
}

// Subscribers reports how many subscriptions tenant holds open.
func (h *Hub) Subscribers(tenant values.TenantId) int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[tenant])
}

// SubscribeRequest is one viewer's subscription to one region.
type SubscribeRequest struct {
	// Principal is the admitted viewer. The subscription's tenant is always
	// this principal's tenant; nothing on the wire can choose another.
	Principal *trust.Principal
	// Region is the presentation region whose projection the hints name.
	Region promotion.Region
	// AfterSequence is the last sequence the client committed on a previous
	// connection. This subscription numbers its messages from the one after
	// it, so a reconnecting client's own ordering continues. It is only an
	// offset on the client's own numbering and grants nothing.
	AfterSequence uint64
	// Inspector decides journey visibility for this viewer.
	Inspector Inspector
	// Authorize, when set, is re-evaluated before every delivery; an error
	// ends the subscription with [ErrRevoked]. The transport uses it to
	// re-check the page access the subscription was opened under.
	Authorize func(context.Context) error
}

// Subscribe opens one subscription. The caller must Close it.
func (h *Hub) Subscribe(req SubscribeRequest) (*Subscription, error) {
	if h == nil {
		return nil, fmt.Errorf("%w: nil hub", ErrInvalid)
	}
	if req.Principal == nil || req.Principal.Tenant().Validate() != nil {
		return nil, fmt.Errorf("%w: principal", ErrInvalid)
	}
	if _, err := req.Region.Projection(); err != nil {
		return nil, fmt.Errorf("%w: region", ErrInvalid)
	}
	if req.Inspector == nil {
		return nil, fmt.Errorf("%w: inspector", ErrInvalid)
	}
	if req.AfterSequence > MaxAfterSequence {
		return nil, fmt.Errorf("%w: after sequence out of range", ErrInvalid)
	}
	tenant := req.Principal.Tenant()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs[tenant]) >= h.maxPerTenant {
		return nil, ErrTooManySubscribers
	}
	h.nextID++
	sub := &Subscription{
		hub:       h,
		key:       "subscription-" + strconv.FormatUint(h.nextID, 10),
		principal: req.Principal,
		region:    req.Region,
		base:      req.AfterSequence,
		inspector: req.Inspector,
		authorize: req.Authorize,
		now:       h.now,
		sequencer: promotion.NewSubscriberSequencer(),
		records:   make(chan Committed, h.buffer),
		lagged:    make(chan struct{}),
		closed:    make(chan struct{}),
	}
	if h.subs[tenant] == nil {
		h.subs[tenant] = make(map[*Subscription]struct{})
	}
	h.subs[tenant][sub] = struct{}{}
	return sub, nil
}

func (h *Hub) remove(sub *Subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tenant := sub.principal.Tenant()
	delete(h.subs[tenant], sub)
	if len(h.subs[tenant]) == 0 {
		delete(h.subs, tenant)
	}
}

// Subscription is one open, authority-filtered feed.
type Subscription struct {
	hub       *Hub
	key       string
	principal *trust.Principal
	region    promotion.Region
	base      uint64
	inspector Inspector
	authorize func(context.Context) error
	now       func() time.Time
	sequencer *promotion.SubscriberSequencer

	records    chan Committed
	lagOnce    sync.Once
	lagged     chan struct{}
	closeOnce  sync.Once
	closed     chan struct{}
	deliveries uint64
}

// offer queues record without blocking. The hub's lock is held, so offer
// races only with Close, which never closes records.
func (s *Subscription) offer(record Committed) {
	select {
	case <-s.closed:
		return
	default:
	}
	select {
	case s.records <- record:
	default:
		s.lagOnce.Do(func() { close(s.lagged) })
	}
}

// Close ends the subscription and removes it from its hub. It is safe to
// call more than once.
func (s *Subscription) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		close(s.closed)
		s.hub.remove(s)
	})
}

// Next blocks until a message this subscription's viewer is authorized to
// receive is ready, and returns its canonical productquery invalidation
// bytes. It returns ctx's error when ctx ends, [ErrLagged] when records were
// dropped for this subscription, [ErrRevoked] when Authorize fails, and
// [ErrClosed] after Close.
func (s *Subscription) Next(ctx context.Context) ([]byte, error) {
	if s == nil {
		return nil, ErrClosed
	}
	for {
		// A lag is reported before any further record is drained: the
		// buffer no longer holds every transition, so delivering what is
		// left would imply a completeness this subscription lost.
		select {
		case <-s.lagged:
			return nil, ErrLagged
		default:
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.closed:
			return nil, ErrClosed
		case <-s.lagged:
			return nil, ErrLagged
		case record := <-s.records:
			raw, ok, err := s.deliver(ctx, record)
			if err != nil {
				return nil, err
			}
			if ok {
				s.deliveries++
				return raw, nil
			}
		}
	}
}

// Deliveries reports how many messages Next has returned.
func (s *Subscription) Deliveries() uint64 {
	if s == nil {
		return 0
	}
	return s.deliveries
}
