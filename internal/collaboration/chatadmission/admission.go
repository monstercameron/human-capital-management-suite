// Package chatadmission provides bounded chat-owned admission lanes. Workflow
// capacity is deliberately absent: acquiring a chat lease can never consume a
// workflow worker, timer, or request token.
package chatadmission

import (
	"context"
	"errors"
	"strings"
	"sync"
)

var (
	ErrInvalidConfig  = errors.New("chatadmission: invalid config")
	ErrOverloaded     = errors.New("chatadmission: overloaded")
	ErrInvalidRequest = errors.New("chatadmission: invalid request")
	ErrReleased       = errors.New("chatadmission: lease already released")
)

type Lane string

const (
	LaneSend  Lane = "send"
	LaneWatch Lane = "watch"
	// LaneRead is the cheaper lane for bounded cursor reads and metadata
	// lookups. It is shed before a send but after derived work.
	LaneRead Lane = "read"
	// LaneDerived carries ephemeral and chat-derived work: suggestions,
	// integration event pulls and other work whose loss is invisible to the
	// canonical timeline. The spec sheds this lane first.
	LaneDerived Lane = "derived"
)

// Config bounds each chat lane. TenantConcurrent, ConversationConcurrent,
// SendConcurrent and WatchConcurrent are required. The read and derived lanes
// and their shed fractions default when left zero so an existing composition
// keeps working without declaring them.
type Config struct {
	TenantConcurrent       int
	ConversationConcurrent int
	SendConcurrent         int
	WatchConcurrent        int
	ReadConcurrent         int
	DerivedConcurrent      int
	// WatchTenantConcurrent and WatchConversationConcurrent bound live
	// subscriptions per tenant and per conversation. They are separate scopes
	// from TenantConcurrent and ConversationConcurrent because a subscription is
	// held for its whole lifetime while a unary request is held for
	// milliseconds. Counting watches in the pools that gate requests is what let
	// one browser tab with a churning watch fill a conversation's request budget
	// and shed every ListPosts on that conversation as unavailable. They default
	// to WatchConcurrent, which is the real ceiling on watches anyway.
	WatchTenantConcurrent       int
	WatchConversationConcurrent int
	// ReadShedFraction and DerivedShedFraction are the per-scope utilisation
	// points at which the read and derived lanes start refusing work. They
	// implement the spec's shed order: derived first, then reads, and only
	// then new sends with a retryable overload result.
	ReadShedFraction    float64
	DerivedShedFraction float64
}

type Request struct {
	TenantID, ConversationID string
	Lane                     Lane
}

// pool is one bounded scope. refs counts the leases that reserved it, which is
// what lets an idle per-tenant or per-conversation pool be evicted instead of
// accumulating one entry per conversation ever observed.
type pool struct {
	slots chan struct{}
	refs  int
}

func newPool(n int) *pool { return &pool{slots: make(chan struct{}, n)} }
func (p *pool) take(ctx context.Context) error {
	select {
	case p.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrOverloaded
	}
}
func (p *pool) give() {
	select {
	case <-p.slots:
	default:
	}
}
func (p *pool) saturation() float64 {
	if cap(p.slots) == 0 {
		return 1
	}
	return float64(len(p.slots)) / float64(cap(p.slots))
}

type Admission struct {
	mu                         sync.Mutex
	cfg                        Config
	tenants                    map[string]*pool
	conversations              map[string]*pool
	watchTenants               map[string]*pool
	watchConversations         map[string]*pool
	send, watch, read, derived *pool
}

func New(c Config) (*Admission, error) {
	if c.TenantConcurrent <= 0 || c.ConversationConcurrent <= 0 || c.SendConcurrent <= 0 || c.WatchConcurrent <= 0 {
		return nil, ErrInvalidConfig
	}
	if c.ReadConcurrent <= 0 {
		c.ReadConcurrent = c.SendConcurrent
	}
	if c.DerivedConcurrent <= 0 {
		c.DerivedConcurrent = (c.SendConcurrent + 1) / 2
	}
	if c.ReadShedFraction <= 0 || c.ReadShedFraction > 1 {
		c.ReadShedFraction = 0.9
	}
	if c.DerivedShedFraction <= 0 || c.DerivedShedFraction > 1 {
		c.DerivedShedFraction = 0.75
	}
	if c.DerivedShedFraction > c.ReadShedFraction {
		return nil, ErrInvalidConfig
	}
	if c.WatchTenantConcurrent <= 0 {
		c.WatchTenantConcurrent = c.WatchConcurrent
	}
	if c.WatchConversationConcurrent <= 0 {
		c.WatchConversationConcurrent = c.WatchConcurrent
	}
	return &Admission{
		cfg:                c,
		tenants:            make(map[string]*pool),
		conversations:      make(map[string]*pool),
		watchTenants:       make(map[string]*pool),
		watchConversations: make(map[string]*pool),
		send:               newPool(c.SendConcurrent),
		watch:              newPool(c.WatchConcurrent),
		read:               newPool(c.ReadConcurrent),
		derived:            newPool(c.DerivedConcurrent),
	}, nil
}

type Lease struct {
	a                          *Admission
	tenantKey, conversationKey string
	tenant, conversation       *pool
	// tenants and conversations are the scope maps this lease reserved in. A
	// watch reserves the subscription scopes, everything else the request
	// scopes, and the lease has to release into the same map it took from.
	tenants, conversations map[string]*pool
	lane                   *pool
	once                   sync.Once
}

func (a *Admission) lane(l Lane) *pool {
	switch l {
	case LaneSend:
		return a.send
	case LaneWatch:
		return a.watch
	case LaneRead:
		return a.read
	case LaneDerived:
		return a.derived
	}
	return nil
}

// shedFraction reports the scope utilisation at which lane work is refused
// before it competes with a send. A send or watch is never pre-shed: it fails
// only when its own bound is full.
func (a *Admission) shedFraction(l Lane) float64 {
	switch l {
	case LaneDerived:
		return a.cfg.DerivedShedFraction
	case LaneRead:
		return a.cfg.ReadShedFraction
	}
	return 0
}

func (a *Admission) reserveLocked(m map[string]*pool, key string, n int) *pool {
	p := m[key]
	if p == nil {
		p = newPool(n)
		m[key] = p
	}
	p.refs++
	return p
}

// releaseLocked drops one reservation and evicts the scope once no lease holds
// it. Without this the tenant and conversation maps grow for the life of the
// process.
func (a *Admission) releaseLocked(m map[string]*pool, key string, p *pool) {
	if p == nil {
		return
	}
	if p.refs > 0 {
		p.refs--
	}
	if p.refs == 0 && len(p.slots) == 0 && m[key] == p {
		delete(m, key)
	}
}

func (a *Admission) unreserve(l *Lease) {
	a.mu.Lock()
	a.releaseLocked(l.tenants, l.tenantKey, l.tenant)
	a.releaseLocked(l.conversations, l.conversationKey, l.conversation)
	a.mu.Unlock()
}

// scopes picks the per-tenant and per-conversation pools a lane reserves in,
// with the sizes for that scope.
func (a *Admission) scopes(l Lane) (tenants, conversations map[string]*pool, tenantSize, conversationSize int) {
	if l == LaneWatch {
		return a.watchTenants, a.watchConversations, a.cfg.WatchTenantConcurrent, a.cfg.WatchConversationConcurrent
	}
	return a.tenants, a.conversations, a.cfg.TenantConcurrent, a.cfg.ConversationConcurrent
}

func (a *Admission) Acquire(ctx context.Context, req Request) (*Lease, error) {
	if a == nil || strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.ConversationID) == "" {
		return nil, ErrInvalidRequest
	}
	lane := a.lane(req.Lane)
	if lane == nil {
		return nil, ErrInvalidRequest
	}
	tenants, conversations, tenantSize, conversationSize := a.scopes(req.Lane)
	l := &Lease{a: a, tenantKey: req.TenantID, conversationKey: req.TenantID + "\x00" + req.ConversationID, tenants: tenants, conversations: conversations, lane: lane}
	a.mu.Lock()
	l.tenant = a.reserveLocked(tenants, l.tenantKey, tenantSize)
	l.conversation = a.reserveLocked(conversations, l.conversationKey, conversationSize)
	shed := a.shedFraction(req.Lane)
	pressure := l.tenant.saturation()
	if c := l.conversation.saturation(); c > pressure {
		pressure = c
	}
	if s := lane.saturation(); s > pressure {
		pressure = s
	}
	a.mu.Unlock()
	if shed > 0 && pressure >= shed {
		a.unreserve(l)
		return nil, ErrOverloaded
	}
	if err := l.tenant.take(ctx); err != nil {
		a.unreserve(l)
		return nil, err
	}
	if err := l.conversation.take(ctx); err != nil {
		l.tenant.give()
		a.unreserve(l)
		return nil, err
	}
	if err := lane.take(ctx); err != nil {
		l.conversation.give()
		l.tenant.give()
		a.unreserve(l)
		return nil, err
	}
	return l, nil
}

func (l *Lease) Release() error {
	if l == nil || l.a == nil {
		return ErrReleased
	}
	released := false
	l.once.Do(func() {
		released = true
		l.lane.give()
		l.conversation.give()
		l.tenant.give()
		l.a.unreserve(l)
	})
	if !released {
		return ErrReleased
	}
	return nil
}

// Available reports chat-owned slots. Workflow capacity is intentionally not
// included and therefore cannot be reported as consumed by this package.
func (a *Admission) Available(req Request) int {
	if a == nil {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	lane := a.lane(req.Lane)
	if lane == nil {
		lane = a.send
	}
	return cap(lane.slots) - len(lane.slots)
}

// ScopeCounts reports how many per-tenant and per-conversation pools are
// currently retained. Both settle back to zero once every lease is released.
func (a *Admission) ScopeCounts() (tenants, conversations int) {
	if a == nil {
		return 0, 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.tenants) + len(a.watchTenants), len(a.conversations) + len(a.watchConversations)
}
