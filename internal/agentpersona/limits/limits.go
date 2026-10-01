package limits

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrInvalid           = errors.New("agentpersona limits: invalid request")
	ErrDenied            = errors.New("agentpersona limits: admission denied")
	ErrPolicyVersion     = errors.New("agentpersona limits: policy version mismatch")
	ErrReservationClosed = errors.New("agentpersona limits: reservation is closed")
	ErrSpendExceeds      = errors.New("agentpersona limits: settled spend exceeds reservation")
)

// DenialCode identifies a stable, client-safe reason for refusing a mention.
type DenialCode string

const (
	DenialInvokerRate        DenialCode = "INVOKER_PERSONA_RATE_CEILING"
	DenialInvokerConcurrency DenialCode = "INVOKER_PERSONA_CONCURRENCY_CEILING"
	DenialConversationRate   DenialCode = "CONVERSATION_RATE_CEILING"
	DenialPersonaSpend       DenialCode = "PERSONA_DAILY_SPEND_CEILING"
	DenialPolicyVersion      DenialCode = "POLICY_VERSION_STALE"
)

// Scope identifies the admission dimension that refused a mention.
type Scope string

const (
	ScopeInvoker      Scope = "INVOKER_PERSONA"
	ScopeConversation Scope = "CONVERSATION"
	ScopePersona      Scope = "PERSONA_TENANT"
	ScopePolicy       Scope = "POLICY"
)

// Denial is a typed, ephemeral-safe admission failure. RetryAfter is derived
// from the caller-supplied clock and never from wall-clock state in the type.
type Denial struct {
	Code          DenialCode
	Scope         Scope
	RetryAfter    time.Duration
	PolicyVersion string
	Limit         int64
	Observed      int64
}

func (d *Denial) Error() string {
	if d == nil {
		return "<nil>"
	}
	return fmt.Sprintf("agentpersona limits: %s (%s)", d.Code, d.Scope)
}

// Unwrap makes errors.Is(err, ErrDenied) stable for transport layers.
func (d *Denial) Unwrap() error { return ErrDenied }

// RateLimit bounds admissions in a fixed UTC window.
type RateLimit struct {
	Max    int64
	Window time.Duration
}

// Policy defines all persona mention ceilings. Version is required and binds
// every reservation to the policy that admitted it.
type Policy struct {
	Version                 string
	InvokerPerPersona       RateLimit
	InvokerConcurrency      int64
	Conversation            RateLimit
	PersonaDailySpendMicros int64
}

func (p Policy) valid() bool {
	return p.Version != "" && p.InvokerPerPersona.Max > 0 && p.InvokerPerPersona.Window > 0 &&
		p.InvokerConcurrency > 0 && p.Conversation.Max > 0 && p.Conversation.Window > 0 &&
		p.PersonaDailySpendMicros > 0
}

// Request identifies one mention and its estimated spend.
type Request struct {
	TenantID             string
	InvokerID            string
	ConversationID       string
	PersonaID            string
	PersonaVersion       uint64
	PolicyVersion        string
	EstimatedSpendMicros int64
}

func (r Request) valid() bool {
	return r.TenantID != "" && r.InvokerID != "" && r.ConversationID != "" && r.PersonaID != "" &&
		r.PersonaVersion > 0 && r.PolicyVersion != "" && r.EstimatedSpendMicros > 0
}

// Token is the opaque atomic-store handle for one admitted mention.
type Token struct {
	ID              string
	Key             string
	InvokerKey      string
	ConversationKey string
	PersonaKey      string
	PolicyVersion   string
	Estimate        int64
	At              time.Time
}

// Store is the atomic persistence seam. Reserve must check every ceiling and
// mutate all counters in one transaction. Settle releases unused spend and
// records actual spend; Release releases reserved spend while retaining rate
// admission history. Durable adapters must implement this contract atomically.
type Store interface {
	Reserve(context.Context, Policy, Request, time.Time) (Token, error)
	Settle(context.Context, Policy, Token, int64) error
	Release(context.Context, Policy, Token) error
}

// Service is the mention admission API consumed by persona orchestration.
type Service struct {
	clock  func() time.Time
	store  Store
	policy Policy
}

// New validates the policy, clock and store. A clock is mandatory so callers
// cannot accidentally make admission decisions with hidden wall-clock state.
func New(policy Policy, clock func() time.Time, store Store) (*Service, error) {
	if !policy.valid() {
		return nil, fmt.Errorf("%w: policy ceilings and version are required", ErrInvalid)
	}
	if clock == nil || store == nil {
		return nil, fmt.Errorf("%w: clock and store are required", ErrInvalid)
	}
	return &Service{clock: clock, store: store, policy: policy}, nil
}

// Reserve atomically admits a mention before any model call begins.
func (s *Service) Reserve(ctx context.Context, req Request) (*Reservation, error) {
	if s == nil || !req.valid() {
		return nil, fmt.Errorf("%w: complete tenant, identity, version and positive estimate are required", ErrInvalid)
	}
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if req.PolicyVersion != s.policy.Version {
		return nil, &Denial{Code: DenialPolicyVersion, Scope: ScopePolicy, PolicyVersion: s.policy.Version}
	}
	token, err := s.store.Reserve(ctx, s.policy, req, s.clock().UTC())
	if err != nil {
		return nil, err
	}
	return &Reservation{service: s, token: token}, nil
}

// Reservation is a one-use handle for settling or refunding an admission.
type Reservation struct {
	service *Service
	token   Token
}

// Token returns the stable admission identifier for audit correlation.
func (r *Reservation) Token() Token {
	if r == nil {
		return Token{}
	}
	return r.token
}

// Settle records actual spend and refunds the unused estimate atomically.
func (r *Reservation) Settle(ctx context.Context, actualSpendMicros int64) error {
	if r == nil || r.service == nil {
		return ErrReservationClosed
	}
	if actualSpendMicros < 0 {
		return fmt.Errorf("%w: negative actual spend", ErrInvalid)
	}
	err := r.service.store.Settle(ctx, r.service.policy, r.token, actualSpendMicros)
	if err == nil {
		r.service = nil
	}
	return err
}

// Release refunds the complete reserved spend after work did not run.
func (r *Reservation) Release(ctx context.Context) error {
	if r == nil || r.service == nil {
		return ErrReservationClosed
	}
	err := r.service.store.Release(ctx, r.service.policy, r.token)
	if err == nil {
		r.service = nil
	}
	return err
}

// Refund releases the complete reserved spend after admitted work does not
// run. It is an explicit alias for Release for callers that model spend
// accounting as reservation/refund.
func (r *Reservation) Refund(ctx context.Context) error { return r.Release(ctx) }

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

type counter struct {
	count         int64
	active        int64
	usedSpend     int64
	reservedSpend int64
}

// MemoryStore is a race-safe process-local Store. It is intentionally not a
// durable production adapter; restart loses counters and outstanding tokens.
type MemoryStore struct {
	mu           sync.Mutex
	sequence     uint64
	closed       map[string]bool
	invoker      map[string]*counter
	conversation map[string]*counter
	persona      map[string]*counter
}

// NewMemoryStore creates an empty atomic store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{closed: make(map[string]bool), invoker: make(map[string]*counter), conversation: make(map[string]*counter), persona: make(map[string]*counter)}
}

func (m *MemoryStore) Reserve(ctx context.Context, p Policy, r Request, at time.Time) (Token, error) {
	if err := contextErr(ctx); err != nil {
		return Token{}, err
	}
	if m == nil || !p.valid() || !r.valid() || r.PolicyVersion != p.Version || at.IsZero() {
		return Token{}, fmt.Errorf("%w: invalid policy, request or admission time", ErrInvalid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	invokerKey := r.TenantID + "\x00" + r.InvokerID + "\x00" + r.PersonaID + "\x00" + fmt.Sprint(r.PersonaVersion)
	conversationKey := r.TenantID + "\x00" + r.ConversationID
	personaKey := r.TenantID + "\x00" + r.PersonaID
	inv := m.invoker[windowKey(invokerKey, at, p.InvokerPerPersona.Window)]
	if inv == nil {
		inv = &counter{}
		m.invoker[windowKey(invokerKey, at, p.InvokerPerPersona.Window)] = inv
	}
	if inv.count >= p.InvokerPerPersona.Max {
		return Token{}, deny(DenialInvokerRate, ScopeInvoker, p.Version, p.InvokerPerPersona.Max, inv.count, windowRetry(at, p.InvokerPerPersona.Window))
	}
	if inv.active >= p.InvokerConcurrency {
		return Token{}, deny(DenialInvokerConcurrency, ScopeInvoker, p.Version, p.InvokerConcurrency, inv.active, windowRetry(at, p.InvokerPerPersona.Window))
	}
	conv := m.conversation[windowKey(conversationKey, at, p.Conversation.Window)]
	if conv == nil {
		conv = &counter{}
		m.conversation[windowKey(conversationKey, at, p.Conversation.Window)] = conv
	}
	if conv.count >= p.Conversation.Max {
		return Token{}, deny(DenialConversationRate, ScopeConversation, p.Version, p.Conversation.Max, conv.count, windowRetry(at, p.Conversation.Window))
	}
	dayKey := windowKey(personaKey, at, 24*time.Hour)
	person := m.persona[dayKey]
	if person == nil {
		person = &counter{}
		m.persona[dayKey] = person
	}
	if person.usedSpend+person.reservedSpend+r.EstimatedSpendMicros > p.PersonaDailySpendMicros {
		return Token{}, deny(DenialPersonaSpend, ScopePersona, p.Version, p.PersonaDailySpendMicros, person.usedSpend+person.reservedSpend, dayRetry(at))
	}
	m.sequence++
	token := Token{ID: fmt.Sprintf("persona-reservation-%08d", m.sequence), Key: dayKey + "\x00" + fmt.Sprint(m.sequence), InvokerKey: windowKey(invokerKey, at, p.InvokerPerPersona.Window), ConversationKey: windowKey(conversationKey, at, p.Conversation.Window), PersonaKey: dayKey, PolicyVersion: p.Version, Estimate: r.EstimatedSpendMicros, At: at.UTC()}
	inv.count++
	inv.active++
	conv.count++
	person.reservedSpend += r.EstimatedSpendMicros
	return token, nil
}

func deny(code DenialCode, scope Scope, version string, limit, observed int64, retry time.Duration) error {
	return &Denial{Code: code, Scope: scope, PolicyVersion: version, Limit: limit, Observed: observed, RetryAfter: retry}
}

func (m *MemoryStore) Settle(ctx context.Context, p Policy, token Token, actual int64) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if actual < 0 {
		return fmt.Errorf("%w: negative actual spend", ErrInvalid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.openToken(p, token); err != nil {
		return err
	}
	if actual > token.Estimate {
		return ErrSpendExceeds
	}
	m.persona[token.PersonaKey].reservedSpend -= token.Estimate
	m.persona[token.PersonaKey].usedSpend += actual
	m.invoker[token.InvokerKey].active--
	m.closed[token.Key] = true
	return nil
}

func (m *MemoryStore) Release(ctx context.Context, p Policy, token Token) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.openToken(p, token); err != nil {
		return err
	}
	m.persona[token.PersonaKey].reservedSpend -= token.Estimate
	m.invoker[token.InvokerKey].active--
	m.closed[token.Key] = true
	return nil
}

func (m *MemoryStore) openToken(p Policy, token Token) error {
	if m == nil || token.ID == "" || token.PolicyVersion != p.Version {
		return ErrPolicyVersion
	}
	if m.closed[token.Key] {
		return ErrReservationClosed
	}
	if _, ok := m.persona[token.PersonaKey]; !ok {
		return ErrReservationClosed
	}
	return nil
}

func windowKey(key string, at time.Time, window time.Duration) string {
	seconds := at.UTC().UnixNano() / int64(window)
	return fmt.Sprintf("%s\x00%d", key, seconds)
}

func windowRetry(at time.Time, window time.Duration) time.Duration {
	next := time.Unix(0, (at.UTC().UnixNano()/int64(window)+1)*int64(window)).UTC()
	return next.Sub(at.UTC())
}

func dayRetry(at time.Time) time.Duration {
	utc := at.UTC()
	next := time.Date(utc.Year(), utc.Month(), utc.Day()+1, 0, 0, 0, 0, time.UTC)
	return next.Sub(utc)
}
