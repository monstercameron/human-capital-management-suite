package cache

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrKeyInvalid             = errors.New("cache: key invalid")
	ErrTenantRequired         = errors.New("cache: tenant required")
	ErrPolicyVersionRequired  = errors.New("cache: policy version required")
	ErrContentVersionRequired = errors.New("cache: content version required")
	ErrVersionRequired        = ErrPolicyVersionRequired
	ErrNamespaceRequired      = errors.New("cache: namespace required")
	ErrIDRequired             = errors.New("cache: id required")
	ErrTTLTooLarge            = errors.New("cache: ttl too large")
	ErrSizeTooLarge           = errors.New("cache: size too large")
)

const (
	DefaultMaxEntries = 1024
	DefaultTTL        = 5 * time.Minute
	MaxTTL            = 24 * time.Hour
	MaxEntriesHard    = 100000
)

type Config struct {
	MaxEntries int
	TTL        time.Duration
}

// Key is the complete identity of one cached value. Tenant, policy version,
// and content version are all part of the identity so an old value cannot be
// returned across either an authorization-policy or content rebuild.
//
// Key values should be made with NewKey. The methods on the adapters validate
// literal values too, so a zero value cannot be used to bypass the contract.
type Key struct {
	Tenant         string
	PolicyVersion  string
	ContentVersion string
	Namespace      string
	ID             string
}

// NewKey constructs a cache key and refuses an incomplete identity.
func NewKey(tenant, policyVersion, contentVersion, namespace, id string) (Key, error) {
	k := Key{
		Tenant: tenant, PolicyVersion: policyVersion, ContentVersion: contentVersion,
		Namespace: namespace, ID: id,
	}
	if err := k.Validate(); err != nil {
		return Key{}, err
	}
	return k, nil
}

// Validate reports whether a key contains every field required to address a
// tenant-safe, versioned value.
func (k Key) Validate() error {
	switch {
	case k.Tenant == "":
		return ErrTenantRequired
	case k.PolicyVersion == "":
		return ErrPolicyVersionRequired
	case k.ContentVersion == "":
		return ErrContentVersionRequired
	case k.Namespace == "":
		return ErrNamespaceRequired
	case k.ID == "":
		return ErrIDRequired
	}
	for _, field := range []string{k.Tenant, k.PolicyVersion, k.ContentVersion, k.Namespace, k.ID} {
		if field == "." || field == ".." || containsSeparator(field) {
			return ErrKeyInvalid
		}
	}
	return nil
}

func containsSeparator(s string) bool {
	for _, r := range s {
		if r == ':' || r == '/' || r == '\\' || r == 0 {
			return true
		}
	}
	return false
}

// String returns a deterministic opaque representation suitable for a
// Valkey key. It contains identity metadata only, never the cached value.
func (k Key) String() string {
	if k.Validate() != nil {
		return ""
	}
	return "hcm:" + hex.EncodeToString([]byte(k.Tenant)) + ":" +
		hex.EncodeToString([]byte(k.PolicyVersion)) + ":" +
		hex.EncodeToString([]byte(k.ContentVersion)) + ":" +
		hex.EncodeToString([]byte(k.Namespace)) + ":" +
		hex.EncodeToString([]byte(k.ID))
}

// Store is the provider-neutral cache contract. Backend errors are not part
// of the correctness path: Get treats them as misses and GetOrLoad computes
// from the authoritative loader. Only malformed keys are caller errors.
type Store[V any] interface {
	Set(key Key, value V) error
	Get(key Key, authorize func(V) bool) (V, bool)
	GetOrLoad(key Key, load func() (V, error), authorize func(V) bool) (V, error)
	Delete(key Key)
	Rebuild(RebuildScope)
	Clear()
	Len() int
}

// Backend is the small provider port used by a remote-compatible adapter.
// Implementations may be backed by Valkey, but this package intentionally
// contains no network client or serialization policy.
type Backend[V any] interface {
	Set(Key, V, time.Duration) error
	Get(Key) (V, bool, error)
	Delete(Key) error
	Rebuild(RebuildScope) error
}

// ValkeyPort is the provider-neutral shape a Valkey adapter can implement.
// It is a declaration only; this package does not open connections or speak
// a network protocol.
type ValkeyPort interface {
	Set(context.Context, string, []byte, time.Duration) error
	Get(context.Context, string) ([]byte, bool, error)
	Delete(context.Context, string) error
}

// RebuildScope selects entries to discard. Each non-empty selector matches
// independently (OR semantics), allowing a caller to invalidate a tenant,
// a policy version, or a content version with one operation.
type RebuildScope struct {
	Tenant         string
	PolicyVersion  string
	ContentVersion string
}

func (s RebuildScope) matches(k Key) bool {
	return (s.Tenant != "" && k.Tenant == s.Tenant) ||
		(s.PolicyVersion != "" && k.PolicyVersion == s.PolicyVersion) ||
		(s.ContentVersion != "" && k.ContentVersion == s.ContentVersion)
}

func (c Config) normalized() Config {
	if c.MaxEntries <= 0 {
		c.MaxEntries = DefaultMaxEntries
	}
	if c.MaxEntries > MaxEntriesHard {
		c.MaxEntries = MaxEntriesHard
	}
	if c.TTL <= 0 {
		c.TTL = DefaultTTL
	}
	if c.TTL > MaxTTL {
		c.TTL = MaxTTL
	}
	return c
}

type entry[V any] struct {
	key       Key
	value     V
	expiresAt time.Time
}

type Cache[V any] struct {
	mu      sync.Mutex
	cfg     Config
	items   map[Key]*list.Element
	order   *list.List
	now     func() time.Time
	backend Backend[V]
}

// TenantReadCache binds a cache to one tenant and one pair of authorization
// and content versions. Use it for tenant-scoped reads whose loader remains
// the source of truth. The authorize callback is evaluated on every hit and
// after every load.
type TenantReadCache[V any] struct {
	cache          Store[V]
	tenant         string
	policyVersion  string
	contentVersion string
	namespace      string
}

// NewTenantReadCache binds a read adapter to a complete tenant/version
// identity. Invalid identity is rejected before any reads can occur.
func NewTenantReadCache[V any](cache Store[V], tenant, policyVersion, contentVersion, namespace string) (*TenantReadCache[V], error) {
	if cache == nil {
		return nil, ErrKeyInvalid
	}
	if _, err := NewKey(tenant, policyVersion, contentVersion, namespace, "identity-check"); err != nil {
		return nil, err
	}
	return &TenantReadCache[V]{cache: cache, tenant: tenant, policyVersion: policyVersion, contentVersion: contentVersion, namespace: namespace}, nil
}

// GetOrLoad reads one tenant-owned value, falling back to the authoritative
// loader on a miss. Caller authorization is mandatory and checked again for
// cached values by the underlying store.
func (r *TenantReadCache[V]) GetOrLoad(id string, load func() (V, error), authorize func(V) bool) (V, error) {
	var zero V
	if r == nil || r.cache == nil || authorize == nil {
		return zero, ErrKeyInvalid
	}
	key, err := NewKey(r.tenant, r.policyVersion, r.contentVersion, r.namespace, id)
	if err != nil {
		return zero, err
	}
	return r.cache.GetOrLoad(key, load, authorize)
}

func New[V any](cfg Config) *Cache[V] {
	cfg = cfg.normalized()
	return &Cache[V]{
		cfg:   cfg,
		items: make(map[Key]*list.Element),
		order: list.New(),
		now:   time.Now,
	}
}

// BuildKey is a named constructor retained for callers that prefer a
// verb-oriented API. The result remains typed; it is not an unvalidated
// stringly-typed cache address.
func BuildKey(tenant, policyVersion, contentVersion, namespace, id string) (Key, error) {
	return NewKey(tenant, policyVersion, contentVersion, namespace, id)
}

// Explain is an audit-safe description of a key. It intentionally omits any
// cached value and reports only whether the key has the required identity.
func Explain(key Key) string {
	if err := key.Validate(); err != nil {
		return "cache key invalid: " + err.Error()
	}
	digest := sha256.Sum256([]byte(key.String()))
	return fmt.Sprintf("cache key valid identity_digest=%x", digest[:8])
}

// Explain returns an audit-safe description of key and never includes a
// cached value.
func (c *Cache[V]) Explain(key Key) string { return Explain(key) }

// NewWithBackend creates a cache facade over a provider port. Provider
// failures are deliberately swallowed by the facade and treated as misses;
// the caller can always fall back to its authoritative loader.
func NewWithBackend[V any](cfg Config, backend Backend[V]) *Cache[V] {
	c := New[V](cfg)
	c.backend = backend
	return c
}

func (c *Cache[V]) Set(key Key, value V) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if c.backend != nil {
		_ = c.backend.Set(key, value, c.cfg.TTL)
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictExpiredLocked()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		e := el.Value.(*entry[V])
		e.value = value
		e.expiresAt = c.now().Add(c.cfg.TTL)
		return nil
	}
	if c.order.Len() >= c.cfg.MaxEntries {
		back := c.order.Back()
		if back != nil {
			ev := back.Value.(*entry[V])
			delete(c.items, ev.key)
			c.order.Remove(back)
		}
	}
	e := &entry[V]{key: key, value: value, expiresAt: c.now().Add(c.cfg.TTL)}
	el := c.order.PushFront(e)
	c.items[key] = el
	return nil
}

func (c *Cache[V]) Get(key Key, authorize func(V) bool) (V, bool) {
	var zero V
	if key.Validate() != nil {
		return zero, false
	}
	if c.backend != nil {
		value, ok, err := c.backend.Get(key)
		if err != nil || !ok {
			return zero, false
		}
		if authorize == nil || !authorize(value) {
			_ = c.backend.Delete(key)
			return zero, false
		}
		return value, true
	}
	c.mu.Lock()
	el, ok := c.items[key]
	if !ok {
		c.mu.Unlock()
		return zero, false
	}
	e := el.Value.(*entry[V])
	if !c.now().Before(e.expiresAt) {
		delete(c.items, key)
		c.order.Remove(el)
		c.mu.Unlock()
		return zero, false
	}
	value := e.value
	c.mu.Unlock()
	// Authorization is deliberately evaluated after lookup and immediately
	// before returning. A denied value is indistinguishable from a miss and is
	// evicted so a later request cannot reuse the denied value.
	if authorize == nil || !authorize(value) {
		c.mu.Lock()
		if current, present := c.items[key]; present && current == el {
			delete(c.items, key)
			c.order.Remove(el)
		}
		c.mu.Unlock()
		return zero, false
	}
	// Re-check presence after authorization: a concurrent Delete/Clear must
	// not make a denied or removed entry become recently used again.
	c.mu.Lock()
	if current, present := c.items[key]; !present || current != el {
		c.mu.Unlock()
		return zero, false
	}
	if !c.now().Before(e.expiresAt) {
		delete(c.items, key)
		c.order.Remove(el)
		c.mu.Unlock()
		return zero, false
	}
	c.order.MoveToFront(el)
	c.mu.Unlock()
	return value, true
}

func (c *Cache[V]) GetOrLoad(key Key, load func() (V, error), authorize func(V) bool) (V, error) {
	var zero V
	if err := key.Validate(); err != nil {
		return zero, err
	}
	if v, ok := c.Get(key, authorize); ok {
		return v, nil
	}
	if load == nil {
		return zero, nil
	}
	v, err := load()
	if err != nil {
		return zero, err
	}
	if authorize == nil || !authorize(v) {
		return zero, nil
	}
	// The loader is the correctness-bearing source. A provider failure while
	// recording its result must not turn a successful authoritative computation
	// into an error.
	_ = c.Set(key, v)
	return v, nil
}

func (c *Cache[V]) Delete(key Key) {
	if key.Validate() != nil {
		return
	}
	if c.backend != nil {
		_ = c.backend.Delete(key)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		delete(c.items, key)
		c.order.Remove(el)
	}
}

func (c *Cache[V]) Clear() {
	if c.backend != nil {
		if clearable, ok := c.backend.(interface{ Clear() error }); ok {
			_ = clearable.Clear()
		}
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[Key]*list.Element)
	c.order.Init()
}

// Rebuild drops every local entry matching any non-empty tenant or version
// selector. It is intentionally idempotent and safe during concurrent reads.
func (c *Cache[V]) Rebuild(scope RebuildScope) {
	if scope.Tenant == "" && scope.PolicyVersion == "" && scope.ContentVersion == "" {
		return
	}
	if c.backend != nil {
		_ = c.backend.Rebuild(scope)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for el := c.order.Back(); el != nil; {
		previous := el.Prev()
		key := el.Value.(*entry[V]).key
		if scope.matches(key) {
			delete(c.items, key)
			c.order.Remove(el)
		}
		el = previous
	}
}

// RebuildTenant drops all entries belonging to tenant.
func (c *Cache[V]) RebuildTenant(tenant string) { c.Rebuild(RebuildScope{Tenant: tenant}) }

// RebuildVersion drops all entries carrying either supplied version. Empty
// arguments are ignored, so callers may invalidate only policy or content.
func (c *Cache[V]) RebuildVersion(policyVersion, contentVersion string) {
	c.Rebuild(RebuildScope{PolicyVersion: policyVersion, ContentVersion: contentVersion})
}

func (c *Cache[V]) Len() int {
	if c.backend != nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictExpiredLocked()
	return c.order.Len()
}

func (c *Cache[V]) evictExpiredLocked() {
	now := c.now()
	for key, el := range c.items {
		e := el.Value.(*entry[V])
		if !now.Before(e.expiresAt) {
			delete(c.items, key)
			c.order.Remove(el)
		}
	}
}
