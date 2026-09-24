// Package chatrouting owns the core conversation directory and its fencing
// protocol. It deliberately has no dependency on chat message or membership
// storage.
package chatrouting

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrInvalid       = errors.New("chatrouting: invalid request")
	ErrNotFound      = errors.New("chatrouting: route not found")
	ErrTenant        = errors.New("chatrouting: tenant mismatch")
	ErrStaleEpoch    = errors.New("chatrouting: stale route epoch")
	ErrNotWritable   = errors.New("chatrouting: route is not writable")
	ErrAlreadyExists = errors.New("chatrouting: route already exists")
	ErrMoveConflict  = errors.New("chatrouting: move conflict")
	ErrLeaseInvalid  = errors.New("chatrouting: invalid write lease")
)

// State is the core-owned lifecycle of a conversation route.
type State string

const (
	StatePending State = "PENDING"
	StateActive  State = "ACTIVE"
	StateMoving  State = "MOVING"
)

// Route is intentionally placement metadata only. It must never grow message,
// membership, unread, search, or attachment fields.
type Route struct {
	ConversationID       string
	HostTenantID         string
	ShardID              string
	TargetShardID        string // populated only while MOVING
	Epoch                uint64
	State                State
	PlacementPolicy      string
	PlacementPolicyVer   uint64
	CreateIdempotencyKey string
}

func (r Route) validate() error {
	if r.ConversationID == "" || r.HostTenantID == "" || r.ShardID == "" || r.Epoch == 0 || r.State == "" {
		return ErrInvalid
	}
	if r.State != StatePending && r.State != StateActive && r.State != StateMoving {
		return ErrInvalid
	}
	if r.State == StateMoving && r.TargetShardID == "" {
		return ErrInvalid
	}
	return nil
}

// Validate checks that a route contains only a usable placement record.
func (r Route) Validate() error { return r.validate() }

type ReserveRequest struct {
	ConversationID, HostTenantID, ShardID string
	PlacementPolicy                       string
	PlacementPolicyVersion                uint64
	IdempotencyKey                        string
}

// Directory is the durable route authority port. Implementations must make
// reserve, activate and cutover conditional on the expected epoch.
type Directory interface {
	Reserve(context.Context, ReserveRequest) (Route, error)
	Lookup(context.Context, string, string) (Route, error)
	Activate(context.Context, string, string, uint64) (Route, error)
	BeginMove(context.Context, string, string, uint64, string) (MovePlan, error)
	Cutover(context.Context, string, string, uint64) (Route, error)
	AbortMove(context.Context, string, string, uint64) (Route, error)
}

type MovePlan struct {
	ConversationID string
	HostTenantID   string
	FromShard      string
	ToShard        string
	SourceEpoch    uint64
	MoveEpoch      uint64
}

// MemoryDirectory is a concurrency-safe reference implementation and a useful
// contract test double. A production implementation can persist the same CAS
// transitions in the core database.
type MemoryDirectory struct {
	mu     sync.RWMutex
	routes map[string]Route
}

func NewMemoryDirectory() *MemoryDirectory { return &MemoryDirectory{routes: make(map[string]Route)} }

func (d *MemoryDirectory) Reserve(_ context.Context, req ReserveRequest) (Route, error) {
	if req.ConversationID == "" || req.HostTenantID == "" || req.ShardID == "" || req.IdempotencyKey == "" {
		return Route{}, ErrInvalid
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if existing, ok := d.routes[req.ConversationID]; ok {
		if existing.CreateIdempotencyKey == req.IdempotencyKey && existing.HostTenantID == req.HostTenantID {
			return existing, nil
		}
		return Route{}, ErrAlreadyExists
	}
	r := Route{ConversationID: req.ConversationID, HostTenantID: req.HostTenantID, ShardID: req.ShardID, Epoch: 1, State: StatePending, PlacementPolicy: req.PlacementPolicy, PlacementPolicyVer: req.PlacementPolicyVersion, CreateIdempotencyKey: req.IdempotencyKey}
	d.routes[r.ConversationID] = r
	return r, nil
}

func (d *MemoryDirectory) Lookup(_ context.Context, conversationID, tenantID string) (Route, error) {
	if conversationID == "" || tenantID == "" {
		return Route{}, ErrInvalid
	}
	d.mu.RLock()
	r, ok := d.routes[conversationID]
	d.mu.RUnlock()
	if !ok {
		return Route{}, ErrNotFound
	}
	if r.HostTenantID != tenantID {
		return Route{}, ErrTenant
	}
	return r, nil
}

func (d *MemoryDirectory) Activate(_ context.Context, id, tenant string, expectedEpoch uint64) (Route, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.routes[id]
	if !ok {
		return Route{}, ErrNotFound
	}
	if r.HostTenantID != tenant {
		return Route{}, ErrTenant
	}
	if r.Epoch != expectedEpoch {
		return Route{}, fmt.Errorf("%w: have %d want %d", ErrStaleEpoch, r.Epoch, expectedEpoch)
	}
	if r.State == StateActive {
		return r, nil
	}
	if r.State != StatePending {
		return Route{}, ErrMoveConflict
	}
	r.State = StateActive
	d.routes[id] = r
	return r, nil
}

func (d *MemoryDirectory) BeginMove(_ context.Context, id, tenant string, expectedEpoch uint64, target string) (MovePlan, error) {
	if target == "" {
		return MovePlan{}, ErrInvalid
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.routes[id]
	if !ok {
		return MovePlan{}, ErrNotFound
	}
	if r.HostTenantID != tenant {
		return MovePlan{}, ErrTenant
	}
	if r.Epoch != expectedEpoch {
		return MovePlan{}, ErrStaleEpoch
	}
	if r.State != StateActive || r.ShardID == target {
		return MovePlan{}, ErrMoveConflict
	}
	r.State, r.TargetShardID, r.Epoch = StateMoving, target, r.Epoch+1
	d.routes[id] = r
	return MovePlan{ConversationID: id, HostTenantID: tenant, FromShard: r.ShardID, ToShard: target, SourceEpoch: expectedEpoch, MoveEpoch: r.Epoch}, nil
}

func (d *MemoryDirectory) Cutover(_ context.Context, id, tenant string, moveEpoch uint64) (Route, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.routes[id]
	if !ok {
		return Route{}, ErrNotFound
	}
	if r.HostTenantID != tenant {
		return Route{}, ErrTenant
	}
	if r.Epoch != moveEpoch {
		return Route{}, ErrStaleEpoch
	}
	if r.State != StateMoving || r.TargetShardID == "" {
		return Route{}, ErrMoveConflict
	}
	r.ShardID, r.TargetShardID, r.State, r.Epoch = r.TargetShardID, "", StateActive, r.Epoch+1
	d.routes[id] = r
	return r, nil
}

func (d *MemoryDirectory) AbortMove(_ context.Context, id, tenant string, moveEpoch uint64) (Route, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.routes[id]
	if !ok {
		return Route{}, ErrNotFound
	}
	if r.HostTenantID != tenant {
		return Route{}, ErrTenant
	}
	if r.Epoch != moveEpoch {
		return Route{}, ErrStaleEpoch
	}
	if r.State != StateMoving {
		return Route{}, ErrMoveConflict
	}
	r.TargetShardID, r.State, r.Epoch = "", StateActive, r.Epoch+1
	d.routes[id] = r
	return r, nil
}

// WriteLease is a route snapshot that callers must pass to chat writes. The
// epoch is checked again by the chat shard immediately before committing.
type WriteLease struct {
	Route     Route
	ExpiresAt time.Time
	Version   uint32
	Signature []byte
}

func (l WriteLease) Validate(now time.Time) error {
	if l.Route.State != StateActive {
		return ErrNotWritable
	}
	if !l.ExpiresAt.After(now) {
		return ErrStaleEpoch
	}
	return nil
}
