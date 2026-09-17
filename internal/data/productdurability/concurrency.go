package productdurability

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ALIGN-036: mutable coordination rows carry a revision. Every write names
// the revision it saw; a write naming a stale revision loses with a typed
// refusal that names the current revision, so concurrent writers serialize
// instead of silently overwriting each other. Revisions start at 1 and
// advance by exactly 1 per write.

// Coordination errors.
var (
	ErrCoordinationInvalid  = errors.New("productdurability: coordination write is invalid")
	ErrCoordinationConflict = errors.New("productdurability: coordination row already exists")
	ErrStaleRevision        = errors.New("productdurability: coordination revision is stale")
)

// CoordinationRow is one mutable coordination fact with its revision.
type CoordinationRow struct {
	Tenant        values.TenantId `json:"tenant"`
	Key           string          `json:"key"`
	Revision      uint64          `json:"revision"`
	PayloadDigest string          `json:"payload_digest"`
	UpdatedAt     time.Time       `json:"updated_at"`
	UpdatedBy     string          `json:"updated_by"`
}

// Digest returns the content digest of the row.
func (r CoordinationRow) Digest() string {
	raw := strings.Join([]string{
		r.Tenant.String(), r.Key, fmt.Sprint(r.Revision),
		r.PayloadDigest, r.UpdatedAt.UTC().Format(time.RFC3339Nano), r.UpdatedBy,
	}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// StaleRevisionError names the current revision a stale writer missed.
type StaleRevisionError struct {
	Key     string
	Want    uint64
	Current uint64
}

func (e *StaleRevisionError) Error() string {
	return fmt.Sprintf("productdurability: %s at revision %d, current is %d", e.Key, e.Want, e.Current)
}

func (e *StaleRevisionError) Unwrap() error { return ErrStaleRevision }

// CoordinationStore holds versioned coordination rows. The zero value is
// not usable; build one with NewCoordinationStore.
type CoordinationStore struct {
	mu   sync.Mutex
	rows map[string]CoordinationRow
}

// NewCoordinationStore builds an empty store.
func NewCoordinationStore() *CoordinationStore {
	return &CoordinationStore{rows: make(map[string]CoordinationRow)}
}

func coordinationSlot(tenant values.TenantId, key string) string {
	return tenant.String() + "\x00" + key
}

// Create inserts a row at revision 1. An existing row is a conflict, never
// an overwrite.
func (s *CoordinationStore) Create(tenant values.TenantId, key, payloadDigest, updatedBy string, at time.Time) (CoordinationRow, error) {
	if err := tenant.Validate(); err != nil {
		return CoordinationRow{}, fmt.Errorf("%w: tenant: %v", ErrCoordinationInvalid, err)
	}
	if strings.TrimSpace(key) == "" || strings.TrimSpace(payloadDigest) == "" || strings.TrimSpace(updatedBy) == "" {
		return CoordinationRow{}, fmt.Errorf("%w: key, payload digest, and writer are required", ErrCoordinationInvalid)
	}
	if at.IsZero() {
		return CoordinationRow{}, fmt.Errorf("%w: update time is required", ErrCoordinationInvalid)
	}
	slot := coordinationSlot(tenant, key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[slot]; ok {
		return CoordinationRow{}, fmt.Errorf("%w: %s", ErrCoordinationConflict, key)
	}
	row := CoordinationRow{Tenant: tenant, Key: key, Revision: 1, PayloadDigest: payloadDigest, UpdatedAt: at.UTC(), UpdatedBy: updatedBy}
	s.rows[slot] = row
	return row, nil
}

// CompareAndSwap advances a row only when expected names its current
// revision. A stale writer receives the current revision and retries
// against fresh state instead of overwriting it.
func (s *CoordinationStore) CompareAndSwap(tenant values.TenantId, key string, expected uint64, payloadDigest, updatedBy string, at time.Time) (CoordinationRow, error) {
	if err := tenant.Validate(); err != nil {
		return CoordinationRow{}, fmt.Errorf("%w: tenant: %v", ErrCoordinationInvalid, err)
	}
	if expected == 0 {
		return CoordinationRow{}, fmt.Errorf("%w: expected revision must be positive", ErrCoordinationInvalid)
	}
	if strings.TrimSpace(payloadDigest) == "" || strings.TrimSpace(updatedBy) == "" {
		return CoordinationRow{}, fmt.Errorf("%w: payload digest and writer are required", ErrCoordinationInvalid)
	}
	if at.IsZero() {
		return CoordinationRow{}, fmt.Errorf("%w: update time is required", ErrCoordinationInvalid)
	}
	slot := coordinationSlot(tenant, key)
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.rows[slot]
	if !ok {
		return CoordinationRow{}, fmt.Errorf("%w: %s does not exist", ErrCoordinationInvalid, key)
	}
	if current.Revision != expected {
		return CoordinationRow{}, &StaleRevisionError{Key: key, Want: expected, Current: current.Revision}
	}
	next := CoordinationRow{Tenant: tenant, Key: key, Revision: current.Revision + 1, PayloadDigest: payloadDigest, UpdatedAt: at.UTC(), UpdatedBy: updatedBy}
	s.rows[slot] = next
	return next, nil
}

// Load returns the current row for a key, if any.
func (s *CoordinationStore) Load(tenant values.TenantId, key string) (CoordinationRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[coordinationSlot(tenant, key)]
	return row, ok
}
