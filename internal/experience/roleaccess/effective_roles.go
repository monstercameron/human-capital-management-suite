package roleaccess

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DefaultResolverTTL is the short server-side cache lifetime for resolved
// role sets. It is far below any credential lifetime, so a revocation takes
// effect at most this late even if the change notification is lost; the
// explicit invalidation on assignment change is what normally makes it
// immediate.
const DefaultResolverTTL = 30 * time.Second

// ContainsRole reports whether roles holds role, comparing case-insensitively
// after the same normalization [NormalizeRoleIDs] applies. Enforcement points
// use this over the resolved set instead of reading credential claims.
func ContainsRole(roles []string, role string) bool {
	want := strings.ToLower(strings.TrimSpace(role))
	if want == "" {
		return false
	}
	for _, have := range roles {
		if strings.ToLower(strings.TrimSpace(have)) == want {
			return true
		}
	}
	return false
}

// HasEffectiveRole reports whether subject holds role under snapshot: the
// durable assignment when one exists, else the admitted credential roles.
// A durable assignment wholly replaces the admitted claims; admitted roles
// never widen it.
func HasEffectiveRole(snapshot Snapshot, subject string, admitted []string, role string) bool {
	return ContainsRole(AssignedRoles(snapshot, subject, admitted), role)
}

// Resolver caches one principal's server-side role set for a short TTL. The
// cached value is the durable assignment when the snapshot carries one for
// the subject, else the admitted credential roles supplied at resolve time
// (the rollout fallback for principals that are not assigned workers).
// Assignment changes invalidate entries explicitly; the TTL only bounds
// staleness when a notification is lost.
type Resolver struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[resolverKey]resolverEntry
}

type resolverKey struct {
	tenant  values.TenantId
	subject string
}

type resolverEntry struct {
	roles     []string
	expiresAt time.Time
}

// NewResolver returns a Resolver caching role sets for ttl. A non-positive
// ttl disables caching (every Resolve loads). A nil clock reads [time.Now].
func NewResolver(ttl time.Duration, now func() time.Time) *Resolver {
	if now == nil {
		now = time.Now
	}
	return &Resolver{ttl: ttl, now: now, entries: make(map[resolverKey]resolverEntry)}
}

// Resolve returns the effective roles for subject in tenant: the durable
// assignment when the store carries one, else admitted. Resolutions are
// cached per tenant and subject until the TTL elapses or Invalidate removes
// them. A snapshot load failure is returned as an error with no caching, so
// a transient outage never poisons the cache.
func (r *Resolver) Resolve(ctx context.Context, store Store, tenant values.TenantId, scope, subject string, admitted []string) ([]string, error) {
	key := resolverKey{tenant: tenant, subject: strings.ToLower(strings.TrimSpace(subject))}
	if r != nil {
		r.mu.Lock()
		if entry, ok := r.entries[key]; ok && r.now().Before(entry.expiresAt) {
			roles := append([]string(nil), entry.roles...)
			r.mu.Unlock()
			return roles, nil
		}
		r.mu.Unlock()
	}
	snapshot, err := store.Load(ctx, tenant, scope)
	if err != nil {
		return nil, err
	}
	roles := AssignedRoles(snapshot, subject, admitted)
	if r != nil && r.ttl > 0 {
		r.mu.Lock()
		r.entries[key] = resolverEntry{roles: append([]string(nil), roles...), expiresAt: r.now().Add(r.ttl)}
		r.mu.Unlock()
	}
	return roles, nil
}

// Invalidate drops the cached role set for subject in tenant, so the next
// Resolve reloads it. Call it after every durable assignment change for that
// subject.
func (r *Resolver) Invalidate(tenant values.TenantId, subject string) {
	if r == nil {
		return
	}
	key := resolverKey{tenant: tenant, subject: strings.ToLower(strings.TrimSpace(subject))}
	r.mu.Lock()
	delete(r.entries, key)
	r.mu.Unlock()
}

// InvalidateTenant drops every cached role set for tenant. Call it when a
// change cannot name its affected subjects (for example a role definition
// change that alters which assignments are meaningful).
func (r *Resolver) InvalidateTenant(tenant values.TenantId) {
	if r == nil {
		return
	}
	r.mu.Lock()
	for key := range r.entries {
		if key.tenant == tenant {
			delete(r.entries, key)
		}
	}
	r.mu.Unlock()
}

// HasAdministratorRole reports whether roles holds an HCM administration
// role (see [IsAdministratorRole]). Enforcement points use this over the
// resolved set instead of reading credential claims.
func HasAdministratorRole(roles []string) bool {
	for _, role := range roles {
		if IsAdministratorRole(role) {
			return true
		}
	}
	return false
}
