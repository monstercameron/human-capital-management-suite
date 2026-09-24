package chatrouting

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"sync"
	"time"
)

const writeLeaseVersion uint32 = 1

type leaseContextKey struct{}

func WithWriteLease(ctx context.Context, lease WriteLease) context.Context {
	return context.WithValue(ctx, leaseContextKey{}, lease)
}

func WriteLeaseFromContext(ctx context.Context) (WriteLease, bool) {
	l, ok := ctx.Value(leaseContextKey{}).(WriteLease)
	return l, ok
}

// RouteCache serves only unexpired route snapshots. Invalidation is explicit
// and epoch-aware, so an old invalidation cannot evict a newer route.
type RouteCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	now   func() time.Time
	key   []byte
	items map[string]cacheItem
}
type cacheItem struct {
	lease WriteLease
}

func cloneLease(l WriteLease) WriteLease {
	l.Signature = append([]byte(nil), l.Signature...)
	return l
}

func NewRouteCache(ttl time.Duration) *RouteCache {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// Without entropy the process cannot issue authentic leases. Fail during
		// construction instead of quietly falling back to unsigned authority.
		panic("chatrouting: unable to initialize route lease signer: " + err.Error())
	}
	return &RouteCache{ttl: ttl, now: time.Now, key: key, items: make(map[string]cacheItem)}
}

// IssueLease signs a versioned snapshot for the supplied route and expiry.
// The cache signer is process-local; leases are consumed by the chat store on
// the same request path and the durable shard fence remains authoritative.
func (c *RouteCache) IssueLease(route Route, expiresAt time.Time) (WriteLease, error) {
	if c == nil || len(c.key) < 32 || route.Validate() != nil || !expiresAt.After(c.now()) {
		return WriteLease{}, ErrLeaseInvalid
	}
	l := WriteLease{Route: route, ExpiresAt: expiresAt, Version: writeLeaseVersion}
	l.Signature = c.sign(l)
	return l, nil
}

func (c *RouteCache) sign(l WriteLease) []byte {
	body, _ := json.Marshal(struct {
		Version   uint32
		Route     Route
		ExpiresAt int64
	}{l.Version, l.Route, l.ExpiresAt.UTC().UnixNano()})
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(body)
	return mac.Sum(nil)
}

// VerifyLease rejects unknown lease versions and any route, expiry, or
// signature that no longer matches the signed snapshot.
func (c *RouteCache) VerifyLease(l WriteLease) error {
	if c == nil || len(c.key) < 32 || l.Version != writeLeaseVersion || l.Route.Validate() != nil || l.ExpiresAt.IsZero() || len(l.Signature) != sha256.Size {
		return ErrLeaseInvalid
	}
	if !hmac.Equal(l.Signature, c.sign(l)) {
		return ErrLeaseInvalid
	}
	return nil
}

func (c *RouteCache) Resolve(ctx context.Context, d Directory, id, tenant string) (WriteLease, error) {
	now := c.now()
	c.mu.Lock()
	item, ok := c.items[id]
	if ok && item.lease.ExpiresAt.After(now) && item.lease.Route.HostTenantID == tenant {
		c.mu.Unlock()
		return cloneLease(item.lease), nil
	}
	c.mu.Unlock()
	r, err := d.Lookup(ctx, id, tenant)
	if err != nil {
		return WriteLease{}, err
	}
	lease, err := c.IssueLease(r, now.Add(c.ttl))
	if err != nil {
		return WriteLease{}, err
	}
	c.mu.Lock()
	c.items[id] = cacheItem{lease: cloneLease(lease)}
	c.mu.Unlock()
	return lease, nil
}

func (c *RouteCache) Invalidate(id string, epoch uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[id]
	if ok && item.lease.Route.Epoch <= epoch {
		delete(c.items, id)
	}
}

func (c *RouteCache) InvalidateAll() {
	c.mu.Lock()
	c.items = make(map[string]cacheItem)
	c.mu.Unlock()
}

// CheckWrite checks the live directory against a lease snapshot. It does not
// verify lease authenticity; serving callers should use RouteCache.CheckWrite.
//
// Deprecated: use (*RouteCache).CheckWrite to validate the lease signature.
func CheckWrite(ctx context.Context, d Directory, lease WriteLease, tenant string, now time.Time) error {
	if err := lease.Validate(now); err != nil {
		return err
	}
	if lease.Route.HostTenantID != tenant {
		return ErrTenant
	}
	r, err := d.Lookup(ctx, lease.Route.ConversationID, tenant)
	if err != nil {
		return err
	}
	if r.Epoch != lease.Route.Epoch || r.State != StateActive || r.ShardID != lease.Route.ShardID {
		return ErrStaleEpoch
	}
	return nil
}

// CheckWrite verifies the signed lease before checking the live directory.
// The chat store repeats its route epoch check inside the message transaction,
// making a move between this check and commit fail before the post commits.
func (c *RouteCache) CheckWrite(ctx context.Context, d Directory, lease WriteLease, tenant string, now time.Time) error {
	if err := c.VerifyLease(lease); err != nil {
		return err
	}
	if err := lease.Validate(now); err != nil {
		return err
	}
	return CheckWrite(ctx, d, lease, tenant, now)
}
