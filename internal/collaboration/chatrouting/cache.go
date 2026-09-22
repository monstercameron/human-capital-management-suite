package chatrouting

import (
	"context"
	"sync"
	"time"
)

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
	items map[string]cacheItem
}
type cacheItem struct {
	route   Route
	expires time.Time
}

func NewRouteCache(ttl time.Duration) *RouteCache {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	return &RouteCache{ttl: ttl, now: time.Now, items: make(map[string]cacheItem)}
}

func (c *RouteCache) Resolve(ctx context.Context, d Directory, id, tenant string) (WriteLease, error) {
	now := c.now()
	c.mu.Lock()
	item, ok := c.items[id]
	if ok && item.expires.After(now) && item.route.HostTenantID == tenant {
		c.mu.Unlock()
		return WriteLease{Route: item.route, ExpiresAt: item.expires}, nil
	}
	c.mu.Unlock()
	r, err := d.Lookup(ctx, id, tenant)
	if err != nil {
		return WriteLease{}, err
	}
	expires := now.Add(c.ttl)
	c.mu.Lock()
	c.items[id] = cacheItem{route: r, expires: expires}
	c.mu.Unlock()
	return WriteLease{Route: r, ExpiresAt: expires}, nil
}

func (c *RouteCache) Invalidate(id string, epoch uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[id]
	if ok && item.route.Epoch <= epoch {
		delete(c.items, id)
	}
}

func (c *RouteCache) InvalidateAll() {
	c.mu.Lock()
	c.items = make(map[string]cacheItem)
	c.mu.Unlock()
}

// CheckWrite performs the authoritative pre-commit fence. A cache may route
// an existing request during its freshness window, but it cannot authorize a
// write after a move or lifecycle transition.
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
