// Package idempotency defines one lifecycle for retries across ingress and
// effect layers without binding the kernel to a database or provider.
package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

func Version() int { return 1 }

var (
	ErrInvalidRequest   = errors.New("idempotency: invalid request")
	ErrConflict         = errors.New("idempotency: canonical request conflict")
	ErrNotFound         = errors.New("idempotency: record not found")
	ErrExpired          = errors.New("idempotency: key expired and remains protected")
	ErrCannotComplete   = errors.New("idempotency: record cannot be completed")
	ErrRetentionInvalid = errors.New("idempotency: retention horizon is invalid")
)

// ExpiryMode declares what happens after the replay horizon.
type ExpiryMode string

const (
	RejectReuse ExpiryMode = "REJECT_REUSE"
	AllowReuse  ExpiryMode = "ALLOW_REUSE"
)

type State string

const (
	InProgress State = "IN_PROGRESS"
	Completed  State = "COMPLETED"
	Expired    State = "EXPIRED"
	Tombstone  State = "TOMBSTONE"
)

// Identity is the stable business identity. Layer is evidence metadata and
// never participates in the uniqueness key, so a worker change does not make a
// second logical effect.
type Identity struct {
	Tenant      string `json:"tenant"`
	Capability  string `json:"capability"`
	EffectScope string `json:"effect_scope"`
	Key         string `json:"key"`
	Principal   string `json:"principal,omitempty"`
}

// RetentionPolicy must cover the maximum permitted retry/redelivery horizon.
type RetentionPolicy struct {
	ExpiresAt time.Time  `json:"expires_at"`
	Mode      ExpiryMode `json:"mode"`
	Tombstone bool       `json:"tombstone"`
}

type Request struct {
	Identity  Identity
	Layer     string
	Canonical []byte
	Retention RetentionPolicy
	Now       time.Time
}

type Decision string

const (
	Reserved        Decision = "RESERVED"
	Replay          Decision = "REPLAY"
	InFlight        Decision = "IN_PROGRESS"
	Conflict        Decision = "CONFLICT"
	ExpiredDecision Decision = "EXPIRED"
)

// Record stores hashes and references, not the canonical request bytes.
type Record struct {
	Identity      Identity   `json:"identity"`
	RequestDigest string     `json:"request_digest"`
	State         State      `json:"state"`
	ExpiryMode    ExpiryMode `json:"expiry_mode"`
	Layer         string     `json:"layer"`
	ExecutionRef  string     `json:"execution_ref,omitempty"`
	ResultRef     string     `json:"result_ref,omitempty"`
	EffectRef     string     `json:"effect_ref,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     time.Time  `json:"expires_at"`
	ReplayCount   int        `json:"replay_count"`
	Tombstone     bool       `json:"tombstone"`
}

type Resolution struct {
	Decision Decision
	Record   Record
}

type Registry struct {
	mu      sync.Mutex
	records map[string]Record
	store   Store
}

func NewRegistry() *Registry { return &Registry{records: make(map[string]Record)} }

// Store is the durable boundary used by shared ingress and effect adapters.
// The pure registry remains useful for bounded tests and local decisions;
// production composition can inject a tenant-scoped durable implementation.
type Store interface {
	Reserve(Request) (Resolution, error)
	Complete(Identity, string, string, string, time.Time) (Record, error)
	Lookup(Identity) (Record, error)
}

// TenantExpirer is the explicit tenant-scoped reclamation capability exposed
// by durable stores. A caller must choose the tenant and cutoff under its
// retention authority; the registry never discovers tenants or reads a clock.
type TenantExpirer interface {
	ExpireTenant(string, time.Time) (int64, error)
}

// NewRegistryWithStore binds lifecycle calls to a durable store. The store
// must provide atomic Reserve semantics across processes.
func NewRegistryWithStore(store Store) *Registry {
	return &Registry{store: store}
}

// NewRegistryWithRecord creates an isolated decision kernel initialized from
// one persisted row. It lets storage adapters reuse exactly the pure
// transition semantics while holding their database's identity lock.
func NewRegistryWithRecord(record Record) *Registry {
	return &Registry{records: map[string]Record{identityKey(record.Identity): record}}
}

// Reserve binds a canonical request before an effect begins.
func (r *Registry) Reserve(req Request) (Resolution, error) {
	if r != nil && r.store != nil {
		return r.store.Reserve(req)
	}
	if r == nil || strings.TrimSpace(req.Identity.Tenant) == "" || strings.TrimSpace(req.Identity.Capability) == "" || strings.TrimSpace(req.Identity.EffectScope) == "" || strings.TrimSpace(req.Identity.Key) == "" || len(req.Canonical) == 0 || req.Now.IsZero() {
		return Resolution{}, ErrInvalidRequest
	}
	if req.Retention.Mode != RejectReuse && req.Retention.Mode != AllowReuse {
		return Resolution{}, ErrRetentionInvalid
	}
	digest := hashBytes(req.Canonical)
	mapKey := identityKey(req.Identity)
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.records[mapKey]; ok {
		if old.RequestDigest != digest {
			return Resolution{Decision: Conflict, Record: old}, ErrConflict
		}
		if old.State == Tombstone || old.State == Expired {
			return Resolution{Decision: ExpiredDecision, Record: old}, ErrExpired
		}
		if !req.Now.Before(old.ExpiresAt) {
			if old.Tombstone || req.Retention.Tombstone || req.Retention.Mode == RejectReuse {
				old.State = Expired
				if old.Tombstone || req.Retention.Tombstone {
					old.State = Tombstone
					old.ResultRef = ""
					old.EffectRef = ""
					old.ExecutionRef = ""
				}
				r.records[mapKey] = old
				return Resolution{Decision: ExpiredDecision, Record: old}, ErrExpired
			}
			delete(r.records, mapKey)
		} else {
			old.ReplayCount++
			r.records[mapKey] = old
			if old.State == InProgress {
				return Resolution{Decision: InFlight, Record: old}, nil
			}
			if old.State == Completed {
				return Resolution{Decision: Replay, Record: old}, nil
			}
		}
	}
	if req.Retention.ExpiresAt.IsZero() || !req.Retention.ExpiresAt.After(req.Now) {
		return Resolution{}, ErrRetentionInvalid
	}
	execution := "execution://" + digestText(mapKey+"|"+digest)
	record := Record{Identity: req.Identity, RequestDigest: digest, State: InProgress, ExpiryMode: req.Retention.Mode, Layer: req.Layer, ExecutionRef: execution, CreatedAt: req.Now.UTC(), ExpiresAt: req.Retention.ExpiresAt.UTC(), Tombstone: req.Retention.Tombstone}
	r.records[mapKey] = record
	return Resolution{Decision: Reserved, Record: record}, nil
}

// Complete records the single business result/effect identity.
func (r *Registry) Complete(identity Identity, requestDigest, resultRef, effectRef string, now time.Time) (Record, error) {
	if r != nil && r.store != nil {
		return r.store.Complete(identity, requestDigest, resultRef, effectRef, now)
	}
	if r == nil || strings.TrimSpace(requestDigest) == "" || strings.TrimSpace(resultRef) == "" || strings.TrimSpace(effectRef) == "" || now.IsZero() {
		return Record{}, ErrInvalidRequest
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	mapKey := identityKey(identity)
	record, ok := r.records[mapKey]
	if !ok {
		return Record{}, ErrNotFound
	}
	if record.RequestDigest != requestDigest {
		return Record{}, ErrConflict
	}
	if !now.Before(record.ExpiresAt) || record.State == Expired || record.State == Tombstone {
		return Record{}, ErrExpired
	}
	if record.State == Completed {
		return record, nil
	}
	if record.State != InProgress {
		return Record{}, ErrCannotComplete
	}
	record.State = Completed
	record.ResultRef = resultRef
	record.EffectRef = effectRef
	r.records[mapKey] = record
	return record, nil
}

// Lookup returns a copy without allowing callers to mutate lifecycle state.
func (r *Registry) Lookup(identity Identity) (Record, error) {
	if r != nil && r.store != nil {
		return r.store.Lookup(identity)
	}
	if r == nil {
		return Record{}, ErrNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[identityKey(identity)]
	if !ok {
		return Record{}, ErrNotFound
	}
	return record, nil
}

// Expire advances records past their declared horizon. Tombstones retain only
// the minimum identity/hash protection and never resurrect an effect.
func (r *Registry) Expire(now time.Time) int {
	if r == nil || now.IsZero() {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for key, record := range r.records {
		if now.Before(record.ExpiresAt) || record.State == Tombstone {
			continue
		}
		count++
		if record.Tombstone {
			record.State = Tombstone
			record.ExecutionRef = ""
			record.ResultRef = ""
			record.EffectRef = ""
			r.records[key] = record
		} else {
			record.State = Expired
			r.records[key] = record
		}
	}
	return count
}

// ExpireTenant invokes the durable store's tenant-scoped reclamation path.
// In-memory registries continue to use Expire(now), which sweeps their own
// bounded test state.
func (r *Registry) ExpireTenant(tenant string, cutoff time.Time) (int64, error) {
	if r == nil || r.store == nil || strings.TrimSpace(tenant) == "" || cutoff.IsZero() {
		return 0, ErrInvalidRequest
	}
	expirer, ok := r.store.(TenantExpirer)
	if !ok {
		return 0, ErrInvalidRequest
	}
	return expirer.ExpireTenant(tenant, cutoff.UTC())
}

func Explain(record Record) string {
	return fmt.Sprintf("tenant=%s capability=%s effect=%s state=%s replay_count=%d tombstone=%t", record.Identity.Tenant, record.Identity.Capability, record.Identity.EffectScope, record.State, record.ReplayCount, record.Tombstone)
}

func identityKey(identity Identity) string {
	parts := []string{identity.Tenant, identity.Capability, identity.EffectScope, identity.Key, identity.Principal}
	for i := range parts {
		parts[i] = fmt.Sprintf("%d:%s", len(parts[i]), parts[i])
	}
	return strings.Join([]string{parts[0], parts[1], parts[2], parts[3], parts[4]}, "|")
}

func hashBytes(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func digestText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
