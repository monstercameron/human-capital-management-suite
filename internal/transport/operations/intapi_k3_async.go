package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/streaming"
)

var (
	ErrAsyncInvalid  = errors.New("operations: invalid asynchronous operation")
	ErrAsyncConflict = errors.New("operations: asynchronous operation conflict")
	ErrAsyncNotFound = errors.New("operations: asynchronous operation not found")
)

// AsyncRequest is the uniform status-resource input used by imports, exports,
// evidence exports and report runs.
type AsyncRequest struct {
	TenantID       string
	Owner          string
	RequestType    string
	IdempotencyKey string
}

type AsyncRegistry struct {
	mu    sync.RWMutex
	now   func() time.Time
	items map[string]Record
	keys  map[string]string
}

func NewAsyncRegistry(now func() time.Time) *AsyncRegistry {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &AsyncRegistry{now: now, items: make(map[string]Record), keys: make(map[string]string)}
}

func (r *AsyncRegistry) Start(ctx context.Context, request AsyncRequest) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if strings.TrimSpace(request.TenantID) == "" || strings.TrimSpace(request.Owner) == "" || strings.TrimSpace(request.RequestType) == "" || strings.TrimSpace(request.IdempotencyKey) == "" {
		return Record{}, ErrAsyncInvalid
	}
	if r == nil {
		return Record{}, ErrAsyncInvalid
	}
	digest := asyncDigest(request)
	r.mu.Lock()
	defer r.mu.Unlock()
	key := request.TenantID + "\x00" + request.IdempotencyKey
	if id, ok := r.keys[key]; ok {
		return clone(r.items[request.TenantID+"\x00"+id]), nil
	}
	now := r.now().UTC()
	record := Record{OperationID: "operation-" + digest[:24], TenantID: request.TenantID, Owner: request.Owner, RequestType: request.RequestType, State: streaming.OperationPending, CreatedAt: now, UpdatedAt: now}
	if _, exists := r.items[request.TenantID+"\x00"+record.OperationID]; exists {
		return Record{}, ErrAsyncConflict
	}
	r.items[request.TenantID+"\x00"+record.OperationID] = record
	r.keys[key] = record.OperationID
	return clone(record), nil
}

func (r *AsyncRegistry) Get(ctx context.Context, tenant, id string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.items[tenant+"\x00"+id]
	if !ok {
		return Record{}, ErrAsyncNotFound
	}
	return clone(record), nil
}

func (r *AsyncRegistry) SetState(ctx context.Context, tenant, id string, state streaming.OperationState) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if state != streaming.OperationRunning && state != streaming.OperationSucceeded && state != streaming.OperationFailed && state != streaming.OperationCancelled {
		return Record{}, ErrAsyncInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := tenant + "\x00" + id
	record, ok := r.items[key]
	if !ok {
		return Record{}, ErrAsyncNotFound
	}
	if record.State.Terminal() {
		return Record{}, ErrAsyncConflict
	}
	record.State, record.UpdatedAt = state, r.now().UTC()
	r.items[key] = record
	return clone(record), nil
}

func (r *AsyncRegistry) Cancel(ctx context.Context, tenant, id string) (Record, error) {
	return r.SetState(ctx, tenant, id, streaming.OperationCancelled)
}

func asyncDigest(request AsyncRequest) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{request.TenantID, request.Owner, request.RequestType, request.IdempotencyKey}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (r AsyncRequest) String() string {
	return fmt.Sprintf("%s/%s/%s", r.TenantID, r.RequestType, r.IdempotencyKey)
}
