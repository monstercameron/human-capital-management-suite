// Package chatstream owns the bounded, authorization-aware live conversation
// stream. It contains no transport or storage implementation.
package chatstream

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidConfig = errors.New("chatstream: invalid config")
	ErrInvalidCursor = errors.New("chatstream: invalid cursor")
	ErrExpiredCursor = errors.New("chatstream: expired cursor")
	ErrUnauthorized  = errors.New("chatstream: unauthorized")
	ErrBackpressure  = errors.New("chatstream: subscriber fell behind")
	ErrRevoked       = errors.New("chatstream: access revoked")
	ErrClosed        = errors.New("chatstream: closed")
)

type Event struct {
	TenantID        string
	ConversationID  string
	Sequence        uint64
	MembershipEpoch uint64
	Payload         []byte
}

type Page struct {
	Events       []Event
	NextSequence uint64
	Complete     bool
}

type Reader interface {
	Read(context.Context, ReadRequest) (Page, error)
}

type ReadRequest struct {
	TenantID, HomeTenantID, ConversationID, SubjectID string
	AfterSequence                                     uint64
	Limit                                             int
}

type Access struct {
	TenantID, HomeTenantID, ConversationID, SubjectID string
	MembershipEpoch                                   uint64
	RouteEpoch                                        uint64
	Sequence                                          uint64
}

type Authorizer interface {
	Authorize(context.Context, Access) error
}

type Config struct {
	Key         []byte
	Reader      Reader
	Authorizer  Authorizer
	QueueSize   int
	ReplayLimit int
	CursorTTL   time.Duration
	Clock       func() time.Time
	// RecheckInterval is the safety-net period for re-authorizing a live
	// subscription. Revocation is event driven (Revoke/RevokeTenant), so this
	// only has to catch a missed invalidation; it defaults to
	// DefaultRecheckInterval rather than hammering the authority every second.
	RecheckInterval time.Duration
}

// DefaultRecheckInterval is the safety-net re-authorization period used when
// Config.RecheckInterval is zero.
const DefaultRecheckInterval = 30 * time.Second

type hub struct {
	mu     sync.RWMutex
	config Config
	subs   map[string]map[*Subscription]struct{}
}

func New(c Config) (*Stream, error) {
	if len(c.Key) == 0 || c.Reader == nil || c.Authorizer == nil || c.QueueSize <= 0 || c.ReplayLimit <= 0 || c.CursorTTL <= 0 {
		return nil, ErrInvalidConfig
	}
	if c.Clock == nil {
		c.Clock = time.Now
	}
	if c.RecheckInterval <= 0 {
		c.RecheckInterval = DefaultRecheckInterval
	}
	c.Key = append([]byte(nil), c.Key...)
	return &Stream{h: &hub{config: c, subs: make(map[string]map[*Subscription]struct{})}}, nil
}

// Stream is a value-owned runtime. Multiple Streams can coexist without a
// package registry or shared mutable state.
type Stream struct{ h *hub }

// Bridge republishes committed events from a durable reader into the bounded
// live stream. The reader is intentionally pull based, keeping store cursors
// internal and leaving resumable client cursors signed by Stream.
type Bridge struct {
	Stream       *Stream
	Reader       Reader
	PollInterval time.Duration
	PageLimit    int
}

func (b Bridge) Watch(ctx context.Context, req WatchRequest) (*Subscription, error) {
	if b.Stream == nil || b.Reader == nil || b.PollInterval <= 0 || b.PageLimit <= 0 {
		return nil, ErrInvalidConfig
	}
	sub, err := b.Stream.Watch(ctx, req)
	if err != nil {
		return nil, err
	}
	go b.poll(ctx, sub, req)
	return sub, nil
}

func (b Bridge) poll(ctx context.Context, sub *Subscription, req WatchRequest) {
	ticker := time.NewTicker(b.PollInterval)
	defer ticker.Stop()
	after := sub.Sequence()
	for {
		page, err := b.Reader.Read(ctx, ReadRequest{TenantID: req.TenantID, HomeTenantID: req.HomeTenantID, SubjectID: req.SubjectID, ConversationID: req.ConversationID, AfterSequence: after, Limit: b.PageLimit})
		if err != nil {
			sub.close(err)
			return
		}
		for _, event := range page.Events {
			if err := b.Stream.Publish(ctx, event); err != nil {
				sub.close(err)
				return
			}
			if event.Sequence > after {
				after = event.Sequence
			}
		}
		// Durable readers may scan records that are invisible to this member.
		// Advance the internal watermark even when the page has no deliverable
		// events; the signed client cursor still advances only on consumption.
		if page.NextSequence > after {
			after = page.NextSequence
		}
		select {
		case <-ctx.Done():
			sub.close(ctx.Err())
			return
		case <-sub.done:
			return
		case <-ticker.C:
		}
	}
}

type WatchRequest struct {
	TenantID, HomeTenantID, SubjectID, ConversationID string
	MembershipEpoch                                   uint64
	// RouteEpoch comes from the current routing authority. Zero denotes a
	// conversation that predates route registration; it is still signed so a
	// later route registration invalidates its cursors.
	RouteEpoch uint64
	Cursor     string
	// AfterSequence is the plain starting position, for a client that knows the
	// last sequence it rendered but holds no signed cursor — a page that has
	// just reloaded, or one resuming after its cursor expired. It is ignored
	// when Cursor is set, and the caller is expected to refuse a request that
	// names both.
	AfterSequence uint64
}

type Subscription struct {
	h        *hub
	access   Access
	watchCtx context.Context
	queue    chan Event
	done     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	err      error
	closed   bool
	last     uint64
	cursor   string
}

type BackpressureError struct{ Cursor string }

func (e *BackpressureError) Error() string {
	return fmt.Sprintf("%v: catch-up cursor=%s", ErrBackpressure, e.Cursor)
}
func (e *BackpressureError) Unwrap() error { return ErrBackpressure }

type cursor struct {
	Version         int    `json:"v"`
	TenantID        string `json:"t"`
	HomeTenantID    string `json:"h"`
	SubjectID       string `json:"s"`
	ConversationID  string `json:"c"`
	MembershipEpoch uint64 `json:"m"`
	RouteEpoch      uint64 `json:"r"`
	Sequence        uint64 `json:"q"`
	ExpiresAt       int64  `json:"e"`
}

func (s *Stream) Watch(ctx context.Context, req WatchRequest) (*Subscription, error) {
	if req.HomeTenantID == "" {
		req.HomeTenantID = req.TenantID
	}
	if s == nil || s.h == nil || strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.HomeTenantID) == "" || strings.TrimSpace(req.SubjectID) == "" || strings.TrimSpace(req.ConversationID) == "" || req.MembershipEpoch == 0 {
		return nil, ErrUnauthorized
	}
	start := req.AfterSequence
	if req.Cursor != "" {
		c, err := s.h.decodeCursor(req.Cursor)
		if err != nil {
			return nil, err
		}
		// The epoch binding is deliberate and stays: a membership revision change
		// invalidates the cursor it was minted under. What the caller must do
		// with that refusal is drop the token and resubscribe with
		// AfterSequence, which is why it exists.
		if c.TenantID != req.TenantID || c.HomeTenantID != req.HomeTenantID || c.SubjectID != req.SubjectID || c.ConversationID != req.ConversationID || c.MembershipEpoch != req.MembershipEpoch || c.RouteEpoch != req.RouteEpoch {
			return nil, ErrInvalidCursor
		}
		start = c.Sequence
	}
	a := Access{TenantID: req.TenantID, HomeTenantID: req.HomeTenantID, SubjectID: req.SubjectID, ConversationID: req.ConversationID, MembershipEpoch: req.MembershipEpoch, RouteEpoch: req.RouteEpoch}
	if err := s.h.config.Authorizer.Authorize(ctx, a); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}
	// Replay must leave the queue room for live events. Clamping to QueueSize
	// let a catch-up page fill the queue exactly, and then the bridge's very
	// first poll published into a full queue and closed the brand-new
	// subscription with backpressure — a watch on any conversation with at least
	// QueueSize durable events died tens of milliseconds after it opened. Half
	// the queue is the reservation; a subscriber that wants more history pages
	// it with the cursor it is handed.
	limit := s.h.config.ReplayLimit
	if headroom := s.h.config.QueueSize / 2; limit > headroom {
		limit = headroom
	}
	if limit <= 0 {
		limit = 1
	}
	page, err := s.h.config.Reader.Read(ctx, ReadRequest{TenantID: req.TenantID, HomeTenantID: req.HomeTenantID, SubjectID: req.SubjectID, ConversationID: req.ConversationID, AfterSequence: start, Limit: limit})
	if err != nil {
		return nil, err
	}
	sub := &Subscription{h: s.h, access: a, watchCtx: ctx, queue: make(chan Event, s.h.config.QueueSize), done: make(chan struct{}), last: start}
	for _, event := range page.Events {
		if err := s.h.authorizeEvent(ctx, a, event); err != nil {
			return nil, fmt.Errorf("%w: replay sequence %d", ErrUnauthorized, event.Sequence)
		}
		sub.last = event.Sequence
		select {
		case sub.queue <- cloneEvent(event):
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			return nil, &BackpressureError{Cursor: s.h.encodeCursor(cursor{Version: 1, TenantID: a.TenantID, HomeTenantID: a.HomeTenantID, SubjectID: a.SubjectID, ConversationID: a.ConversationID, MembershipEpoch: a.MembershipEpoch, RouteEpoch: a.RouteEpoch, Sequence: start, ExpiresAt: s.h.config.Clock().Add(s.h.config.CursorTTL).UnixNano()})}
		}
	}
	sub.mu.Lock()
	sub.cursor = s.h.encodeCursor(cursor{Version: 1, TenantID: a.TenantID, HomeTenantID: a.HomeTenantID, SubjectID: a.SubjectID, ConversationID: a.ConversationID, MembershipEpoch: a.MembershipEpoch, RouteEpoch: a.RouteEpoch, Sequence: start, ExpiresAt: s.h.config.Clock().Add(s.h.config.CursorTTL).UnixNano()})
	sub.mu.Unlock()
	key := subscriptionKey(a.TenantID, a.ConversationID)
	s.h.mu.Lock()
	if s.h.subs[key] == nil {
		s.h.subs[key] = make(map[*Subscription]struct{})
	}
	s.h.subs[key][sub] = struct{}{}
	s.h.mu.Unlock()
	go func() {
		ticker := time.NewTicker(s.h.config.RecheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				sub.close(ErrRevoked)
				return
			case <-sub.done:
				return
			case <-ticker.C:
				if err := sub.h.config.Authorizer.Authorize(sub.watchCtx, sub.access); err != nil {
					sub.close(ErrRevoked)
					return
				}
			}
		}
	}()
	return sub, nil
}

func (s *Stream) Publish(ctx context.Context, event Event) error {
	if s == nil || s.h == nil || event.TenantID == "" || event.ConversationID == "" || event.Sequence == 0 {
		return ErrInvalidConfig
	}
	key := subscriptionKey(event.TenantID, event.ConversationID)
	s.h.mu.RLock()
	subscribers := make([]*Subscription, 0, len(s.h.subs[key]))
	for sub := range s.h.subs[key] {
		subscribers = append(subscribers, sub)
	}
	s.h.mu.RUnlock()
	for _, sub := range subscribers {
		sub.deliver(ctx, event)
	}
	return nil
}

// Revoke closes every live subscription one principal holds on a conversation
// at or below membershipEpoch. It is the event-driven half of the revocation
// budget: membership removal calls it instead of waiting for the periodic
// recheck. An empty homeTenantID matches any home tenant.
func (s *Stream) Revoke(hostTenantID, homeTenantID, subjectID, conversationID string, membershipEpoch uint64) {
	s.revoke(hostTenantID, conversationID, func(a Access) bool {
		return a.SubjectID == subjectID && (homeTenantID == "" || a.HomeTenantID == homeTenantID) && membershipEpoch >= a.MembershipEpoch
	})
}

// RevokeTenant closes every live subscription a consumer tenant holds on a
// host conversation. Grant revocation removes a whole tenant's access at once
// and cannot enumerate the affected subjects.
func (s *Stream) RevokeTenant(hostTenantID, homeTenantID, conversationID string) {
	s.revoke(hostTenantID, conversationID, func(a Access) bool {
		return homeTenantID == "" || a.HomeTenantID == homeTenantID
	})
}

func (s *Stream) revoke(hostTenantID, conversationID string, match func(Access) bool) {
	if s == nil || s.h == nil {
		return
	}
	key := subscriptionKey(hostTenantID, conversationID)
	s.h.mu.RLock()
	subscribers := make([]*Subscription, 0, len(s.h.subs[key]))
	for sub := range s.h.subs[key] {
		if match(sub.access) {
			subscribers = append(subscribers, sub)
		}
	}
	s.h.mu.RUnlock()
	for _, sub := range subscribers {
		sub.close(ErrRevoked)
	}
}

// subscriptionKey scopes fan-out by host tenant as well as conversation, so a
// conversation identifier reused in another tenant can never reach this
// tenant's subscribers.
func subscriptionKey(tenantID, conversationID string) string {
	return tenantID + "\x00" + conversationID
}

func (s *Subscription) Next(ctx context.Context) (Event, error) {
	if s.watchCtx.Err() != nil {
		s.close(ErrRevoked)
		return Event{}, ErrRevoked
	}
	s.mu.Lock()
	terminal, closed := s.err, s.closed
	s.mu.Unlock()
	if closed && (errors.Is(terminal, ErrRevoked) || errors.Is(terminal, ErrUnauthorized)) {
		return Event{}, terminal
	}
	// Drain already accepted events before observing terminal state. This keeps
	// durable/replayed order visible when a slow-consumer close races delivery.
	select {
	case event := <-s.queue:
		if err := s.h.authorizeEvent(s.watchCtx, s.access, event); err != nil {
			s.close(ErrRevoked)
			return Event{}, ErrRevoked
		}
		s.recordConsumed(event)
		return event, nil
	default:
	}
	select {
	case event := <-s.queue:
		if err := s.h.authorizeEvent(s.watchCtx, s.access, event); err != nil {
			s.close(ErrRevoked)
			return Event{}, ErrRevoked
		}
		s.recordConsumed(event)
		return event, nil
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.err != nil {
			return Event{}, s.err
		}
		return Event{}, ErrClosed
	case <-ctx.Done():
		return Event{}, ctx.Err()
	}
}
func (s *Subscription) recordConsumed(event Event) {
	s.mu.Lock()
	s.cursor = s.h.encodeCursor(cursor{Version: 1, TenantID: s.access.TenantID, HomeTenantID: s.access.HomeTenantID, SubjectID: s.access.SubjectID, ConversationID: s.access.ConversationID, MembershipEpoch: s.access.MembershipEpoch, RouteEpoch: s.access.RouteEpoch, Sequence: event.Sequence, ExpiresAt: s.h.config.Clock().Add(s.h.config.CursorTTL).UnixNano()})
	s.mu.Unlock()
}
func (s *Subscription) Cursor() string        { s.mu.Lock(); defer s.mu.Unlock(); return s.cursor }
func (s *Subscription) Sequence() uint64      { s.mu.Lock(); defer s.mu.Unlock(); return s.last }
func (s *Subscription) Done() <-chan struct{} { return s.done }
func (s *Subscription) Close()                { s.close(ErrClosed) }

func (s *Subscription) deliver(_ context.Context, event Event) {
	if event.TenantID != s.access.TenantID {
		return
	}
	if err := s.h.authorizeEvent(s.watchCtx, s.access, event); err != nil {
		s.close(ErrRevoked)
		return
	}
	s.mu.Lock()
	if s.closed || event.Sequence <= s.last {
		s.mu.Unlock()
		return
	}
	select {
	case s.queue <- cloneEvent(event):
		s.last = event.Sequence
	case <-s.done:
	case <-s.watchCtx.Done():
	default:
		token := s.cursor
		s.mu.Unlock()
		s.close(&BackpressureError{Cursor: token})
		return
	}
	s.mu.Unlock()
}

func (s *Subscription) close(err error) {
	s.once.Do(func() {
		s.mu.Lock()
		s.err = err
		s.closed = true
		if errors.Is(err, ErrRevoked) || errors.Is(err, ErrUnauthorized) {
			for {
				select {
				case <-s.queue:
				default:
					goto drained
				}
			}
		}
	drained:
		s.mu.Unlock()
		s.h.remove(s)
		close(s.done)
	})
}
func (h *hub) remove(s *Subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := subscriptionKey(s.access.TenantID, s.access.ConversationID)
	delete(h.subs[key], s)
	if len(h.subs[key]) == 0 {
		delete(h.subs, key)
	}
}
func (h *hub) authorizeEvent(ctx context.Context, a Access, e Event) error {
	a.Sequence = e.Sequence
	return h.config.Authorizer.Authorize(ctx, a)
}
func cloneEvent(e Event) Event { e.Payload = append([]byte(nil), e.Payload...); return e }

func (h *hub) encodeCursor(c cursor) string {
	raw, _ := json.Marshal(c)
	mac := hmac.New(sha256.New, h.config.Key)
	mac.Write(raw)
	return "cs1." + base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (h *hub) decodeCursor(token string) (cursor, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "cs1" {
		return cursor{}, ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return cursor{}, ErrInvalidCursor
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return cursor{}, ErrInvalidCursor
	}
	mac := hmac.New(sha256.New, h.config.Key)
	mac.Write(raw)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return cursor{}, ErrInvalidCursor
	}
	var c cursor
	if json.Unmarshal(raw, &c) != nil || c.Version != 1 || c.TenantID == "" || c.HomeTenantID == "" || c.SubjectID == "" || c.ConversationID == "" || c.MembershipEpoch == 0 || c.ExpiresAt <= 0 {
		return cursor{}, ErrInvalidCursor
	}
	if h.config.Clock().UnixNano() >= c.ExpiresAt {
		return cursor{}, ErrExpiredCursor
	}
	return c, nil
}
