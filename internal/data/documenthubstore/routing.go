// Document ID routing and shard epochs (HUB-002). Core owns a directory
// mapping each document ID to its tenant, shard and epoch, independent of
// document content storage: a route names where a document currently lives,
// never its title, body or grants. It exists so a document read can refuse
// a stale migrated copy or a route claimed by a foreign tenant before any
// content is touched, mirroring
// internal/collaboration/chatrouting's conversation directory for chat. A
// production Directory persists the same compare-and-swap transitions in
// the core database instead of memory; this package declares the port and a
// concurrency-safe reference implementation, and never imports chat.
package documenthubstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrRouteInvalid    = errors.New("document routing: invalid request")
	ErrRouteNotFound   = errors.New("document routing: route not found")
	ErrRouteTenant     = errors.New("document routing: tenant mismatch")
	ErrRouteStaleEpoch = errors.New("document routing: stale route epoch")
	ErrRouteExists     = errors.New("document routing: route already exists")
)

// DocumentRoute is core-owned placement metadata for one document: which
// tenant owns it, which shard currently holds its content, and the epoch
// every read must match. It carries no title, body or grant state.
type DocumentRoute struct {
	DocumentID string
	TenantID   string
	ShardID    string
	Epoch      uint64
}

// DocumentRouteDirectory is the durable route authority port. Register is
// idempotent under the supplied key; Reshard is conditional on the caller's
// expected epoch so a stale mover can never win a race against a completed
// migration.
type DocumentRouteDirectory interface {
	Register(ctx context.Context, documentID, tenantID, shardID, idempotencyKey string) (DocumentRoute, error)
	Lookup(ctx context.Context, documentID, tenantID string) (DocumentRoute, error)
	Reshard(ctx context.Context, documentID, tenantID, newShardID string, expectedEpoch uint64) (DocumentRoute, error)
}

// MemoryDocumentRoutes is a concurrency-safe reference implementation and a
// contract test double.
type MemoryDocumentRoutes struct {
	mu     sync.RWMutex
	routes map[string]DocumentRoute
	keys   map[string]string
}

func NewMemoryDocumentRoutes() *MemoryDocumentRoutes {
	return &MemoryDocumentRoutes{routes: make(map[string]DocumentRoute), keys: make(map[string]string)}
}

func (d *MemoryDocumentRoutes) Register(_ context.Context, documentID, tenantID, shardID, idempotencyKey string) (DocumentRoute, error) {
	if documentID == "" || tenantID == "" || shardID == "" || idempotencyKey == "" {
		return DocumentRoute{}, ErrRouteInvalid
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if existing, ok := d.routes[documentID]; ok {
		if d.keys[documentID] == idempotencyKey && existing.TenantID == tenantID {
			return existing, nil
		}
		return DocumentRoute{}, ErrRouteExists
	}
	r := DocumentRoute{DocumentID: documentID, TenantID: tenantID, ShardID: shardID, Epoch: 1}
	d.routes[documentID] = r
	d.keys[documentID] = idempotencyKey
	return r, nil
}

func (d *MemoryDocumentRoutes) Lookup(_ context.Context, documentID, tenantID string) (DocumentRoute, error) {
	if documentID == "" || tenantID == "" {
		return DocumentRoute{}, ErrRouteInvalid
	}
	d.mu.RLock()
	r, ok := d.routes[documentID]
	d.mu.RUnlock()
	if !ok {
		return DocumentRoute{}, ErrRouteNotFound
	}
	if r.TenantID != tenantID {
		return DocumentRoute{}, ErrRouteTenant
	}
	return r, nil
}

func (d *MemoryDocumentRoutes) Reshard(_ context.Context, documentID, tenantID, newShardID string, expectedEpoch uint64) (DocumentRoute, error) {
	if newShardID == "" {
		return DocumentRoute{}, ErrRouteInvalid
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.routes[documentID]
	if !ok {
		return DocumentRoute{}, ErrRouteNotFound
	}
	if r.TenantID != tenantID {
		return DocumentRoute{}, ErrRouteTenant
	}
	if r.Epoch != expectedEpoch {
		return DocumentRoute{}, fmt.Errorf("%w: have %d want %d", ErrRouteStaleEpoch, r.Epoch, expectedEpoch)
	}
	if r.ShardID == newShardID {
		return r, nil
	}
	r.ShardID = newShardID
	r.Epoch++
	d.routes[documentID] = r
	return r, nil
}

// RequireCurrentRoute refuses a document read whose caller resolved a route
// earlier than the directory's current epoch, or whose tenant does not own
// the document: a stale migrated copy or a foreign route must never reach
// document content.
func RequireCurrentRoute(ctx context.Context, dir DocumentRouteDirectory, route DocumentRoute, tenantID string) error {
	if dir == nil {
		return ErrRouteInvalid
	}
	current, err := dir.Lookup(ctx, route.DocumentID, tenantID)
	if err != nil {
		return err
	}
	if current.Epoch != route.Epoch || current.ShardID != route.ShardID {
		return ErrRouteStaleEpoch
	}
	return nil
}
