package datasource

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Limits shared by every adapter. A connector that needs tighter bounds
// documents them; none may exceed these.
const (
	// DefaultLimit applies when Query.Limit is zero.
	DefaultLimit = 100
	// MaxLimit caps a single query.
	MaxLimit = 1000
	// MaxWriteBatch caps the documents of a single Write.
	MaxWriteBatch = 500
)

// Query is one read against a collection.
type Query struct {
	// Collection names the collection to read.
	Collection string
	// Predicate is a canonical-JSON object the adapter turns into its
	// native filter; empty matches every document.
	Predicate string
	// Limit caps the documents returned; zero selects DefaultLimit.
	Limit int
}

// Validate rejects empty collections and over-cap limits. Predicate JSON
// itself is parsed by the adapter, which reports ErrInvalidQuery.
func (q Query) Validate() error {
	if strings.TrimSpace(q.Collection) == "" {
		return fmt.Errorf("%w: collection is empty", ErrInvalidQuery)
	}
	if q.Limit < 0 || q.Limit > MaxLimit {
		return fmt.Errorf("%w: %d (max %d)", ErrLimitExceeded, q.Limit, MaxLimit)
	}
	return nil
}

// EffectiveLimit resolves the zero-limit default after Validate.
func (q Query) EffectiveLimit() int {
	if q.Limit <= 0 {
		return DefaultLimit
	}
	return q.Limit
}

// Document is one opaque record: canonical JSON bytes whose keys sort
// ascending, so adapters and tests compare bytes, not object graphs.
type Document []byte

// Write is one insert batch against a single collection.
type Write struct {
	// Collection must belong to the source's WritableCollections.
	Collection string
	// Documents holds at least one canonical-JSON document.
	Documents []Document
}

// Validate rejects empty collections, empty batches and over-cap
// batches. Document bytes are parsed by the adapter.
func (w Write) Validate() error {
	if strings.TrimSpace(w.Collection) == "" {
		return fmt.Errorf("%w: collection is empty", ErrInvalidWrite)
	}
	if len(w.Documents) == 0 {
		return fmt.Errorf("%w: no documents", ErrInvalidWrite)
	}
	if len(w.Documents) > MaxWriteBatch {
		return fmt.Errorf("%w: %d documents (max %d)", ErrBatchTooLarge, len(w.Documents), MaxWriteBatch)
	}
	return nil
}

// Connector is one open, tenant-bound data source. Every method is safe
// for concurrent use; Close releases the underlying connection.
type Connector interface {
	// Source returns the registry key this connector was opened for.
	Source() SourceID
	// AdapterID names the adapter that built this connector.
	AdapterID() string
	// Query reads up to Limit documents from a collection.
	Query(ctx context.Context, q Query) ([]Document, error)
	// Write inserts one batch; it fails with ErrReadOnly on a
	// read-only source and ErrCollectionNotWritable outside the
	// declared set.
	Write(ctx context.Context, w Write) error
	// Close releases the connection.
	Close(ctx context.Context) error
}

// Registry holds adapters and sources and opens tenant-bound connectors.
// The zero value is not usable; build one with NewRegistry.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]AdapterFactory
	sources  map[SourceID]Source
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[string]AdapterFactory),
		sources:  make(map[SourceID]Source),
	}
}

// RegisterAdapter names one factory. Duplicate registrations are refused
// so two packages can never fight over an adapter id silently.
func (r *Registry) RegisterAdapter(id string, factory AdapterFactory) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: adapter id is empty", ErrInvalidSource)
	}
	if factory == nil {
		return fmt.Errorf("%w: factory for %q is nil", ErrInvalidSource, id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.adapters[id]; dup {
		return fmt.Errorf("%w: %q", ErrDuplicateAdapter, id)
	}
	r.adapters[id] = factory
	return nil
}

// AddSource validates and registers one source. Unknown adapters are
// refused here, at registration, rather than at first Open.
func (r *Registry) AddSource(src Source) error {
	if err := src.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.adapters[src.AdapterID]; !ok {
		return fmt.Errorf("%w: %q", ErrUnknownAdapter, src.AdapterID)
	}
	key := src.SourceID()
	if _, dup := r.sources[key]; dup {
		return fmt.Errorf("%w: %s/%s", ErrDuplicateSource, key.Tenant, key.ID)
	}
	r.sources[key] = src
	return nil
}

// Open returns a tenant-bound connector for a registered source. A source
// registered under another tenant reports ErrUnknownSource, exactly like
// a source that was never registered: the registry offers no
// cross-tenant existence oracle.
func (r *Registry) Open(ctx context.Context, tenant, id string, deps OpenDeps) (Connector, error) {
	if deps.Secrets == nil {
		return nil, fmt.Errorf("%w: secrets port is nil", ErrInvalidSource)
	}
	if deps.Egress == nil {
		return nil, fmt.Errorf("%w: egress policy is nil", ErrInvalidSource)
	}
	r.mu.RLock()
	src, ok := r.sources[SourceID{Tenant: tenant, ID: id}]
	factory, fok := r.adapters[src.AdapterID]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s", ErrUnknownSource, tenant, id)
	}
	if !fok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownAdapter, src.AdapterID)
	}
	conn, err := factory(ctx, src, deps)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, fmt.Errorf("%w: adapter %q returned nil", ErrInvalidSource, src.AdapterID)
	}
	return conn, nil
}
