package trust

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// INTAPI-002 revocation and replay enforcement. A machine access token is
// admitted at most once per lifetime: the first presentation records its
// identifier, a second presentation is a replay, and a revoked client or
// session fails closed even for a never-before-seen token. Signature,
// issuer, audience, expiry and the fifteen-minute lifetime cap are checked
// before the source is consulted, so a forgery never reaches it.

var (
	// ErrTokenReplayed refuses the second presentation of a token
	// identifier within its lifetime.
	ErrTokenReplayed = errors.New("trust: token identifier was already used")
	// ErrClientRevoked refuses tokens of a suspended, revoked or expired
	// machine-client registration.
	ErrClientRevoked = errors.New("trust: machine client is revoked, suspended or expired")
	// ErrSessionRevoked refuses tokens whose session ended server-side.
	ErrSessionRevoked = errors.New("trust: session has been revoked")
)

// RevocationQuery is one admission decision the source must make.
type RevocationQuery struct {
	Tenant    string
	ClientID  string
	TokenID   string
	Session   string
	ExpiresAt time.Time
}

// RevocationSource admits one token use. It refuses revoked clients and
// sessions, records the token identifier, and refuses its replay. It must
// be safe for concurrent use; two concurrent first presentations of one
// identifier admit exactly one. A nil source means no revocation
// information, which only the development path uses.
type RevocationSource interface {
	CheckRevocation(ctx context.Context, q RevocationQuery) error
}

// MemoryRevocationSource is the in-memory RevocationSource for tests and
// single-replica deployments. Production serves the durable truststore
// implementation so a replica restart cannot re-admit a spent identifier.
type MemoryRevocationSource struct {
	mu              sync.Mutex
	revokedClients  map[string]bool
	revokedSessions map[string]bool
	seen            map[string]time.Time
	now             func() time.Time
}

// NewMemoryRevocationSource returns an empty source. now may be nil.
func NewMemoryRevocationSource(now func() time.Time) *MemoryRevocationSource {
	if now == nil {
		now = time.Now
	}
	return &MemoryRevocationSource{
		revokedClients:  make(map[string]bool),
		revokedSessions: make(map[string]bool),
		seen:            make(map[string]time.Time),
		now:             now,
	}
}

// RevokeClient marks a client revoked. RevokeSession marks a session revoked.
func (s *MemoryRevocationSource) RevokeClient(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revokedClients[clientID] = true
}

// RevokeSession marks a session revoked.
func (s *MemoryRevocationSource) RevokeSession(session string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revokedSessions[session] = true
}

// CheckRevocation implements [RevocationSource].
func (s *MemoryRevocationSource) CheckRevocation(_ context.Context, q RevocationQuery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	if s.revokedClients[q.ClientID] {
		return fmt.Errorf("%w: %s", ErrClientRevoked, q.ClientID)
	}
	if s.revokedSessions[q.Session] {
		return fmt.Errorf("%w: %s", ErrSessionRevoked, q.Session)
	}
	if q.TokenID == "" {
		return fmt.Errorf("%w: missing identifier", ErrTokenReplayed)
	}
	if exp, ok := s.seen[q.TokenID]; ok && now.Before(exp) {
		return fmt.Errorf("%w: %s", ErrTokenReplayed, q.TokenID)
	}
	s.seen[q.TokenID] = q.ExpiresAt.UTC()
	for id, exp := range s.seen {
		if !now.Before(exp) {
			delete(s.seen, id)
		}
	}
	return nil
}
