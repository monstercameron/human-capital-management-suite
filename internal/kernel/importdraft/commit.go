package importdraft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"sync"
)

// ErrCheckpointConflict is returned when a checkpoint was advanced by another
// committer since it was loaded.
var ErrCheckpointConflict = errors.New("importing: checkpoint was advanced by another committer")

// Item outcome statuses.
const (
	ItemCommitted = "COMMITTED"
	ItemFailed    = "FAILED"
	ItemNoOp      = "NO_OP"
	ItemError     = "ERROR"
	ItemConflict  = "CONFLICT"
)

// Reconciliation statuses an item committer reports.
const (
	ReconciliationMatched  = "MATCHED"
	ReconciliationMismatch = "MISMATCH"
)

// ItemRequest is one draft to commit.
type ItemRequest struct {
	Tenant           string
	ImportID         string
	SimulationDigest string
	IdempotencyKey   string
	Draft            BusinessIntentDraft
}

// ItemLineage is the evidence a committed item leaves.
type ItemLineage struct {
	TransactionRef       string
	ObservationRef       string
	ReconciliationStatus string
}

// ItemCommitter commits one draft through the governed transaction path. It
// must be idempotent on ItemRequest.IdempotencyKey.
type ItemCommitter interface {
	CommitItem(ctx context.Context, req ItemRequest) (ItemLineage, error)
}

// ItemState is one checkpointed item.
type ItemState struct {
	Status      string
	Lineage     ItemLineage
	FailureCode string
}

// Checkpoint is the durable progress of one import commit.
type Checkpoint struct {
	ImportID         string
	SimulationDigest string
	BatchDigest      string
	MappingDigest    string
	Version          uint64
	Items            map[string]ItemState
}

// CheckpointStore persists checkpoints under compare-and-set.
type CheckpointStore interface {
	Load(ctx context.Context, importID string) (Checkpoint, bool, error)
	// Save stores cp when the stored version equals expectedVersion (0 for a
	// new checkpoint), otherwise it returns ErrCheckpointConflict.
	Save(ctx context.Context, cp Checkpoint, expectedVersion uint64) error
}

// IdempotencyKey is the item key a commit presents for a row of a simulation.
func IdempotencyKey(simulationDigest, rowID string) string {
	sum := sha256.Sum256([]byte("hcmnext.dataops.import.item/v1\x00" + simulationDigest + "\x00" + rowID))
	return "import-item:" + hex.EncodeToString(sum[:16])
}

// MemoryCheckpoints is an in-process [CheckpointStore].
type MemoryCheckpoints struct {
	mu   sync.Mutex
	byID map[string]Checkpoint
}

// NewMemoryCheckpoints returns an empty store.
func NewMemoryCheckpoints() *MemoryCheckpoints {
	return &MemoryCheckpoints{byID: map[string]Checkpoint{}}
}

// Load implements CheckpointStore.
func (m *MemoryCheckpoints) Load(_ context.Context, importID string) (Checkpoint, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp, ok := m.byID[importID]
	cp.Items = maps.Clone(cp.Items)
	return cp, ok, nil
}

// Save implements CheckpointStore.
func (m *MemoryCheckpoints) Save(_ context.Context, cp Checkpoint, expectedVersion uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byID[cp.ImportID].Version != expectedVersion {
		return ErrCheckpointConflict
	}
	cp.Items = maps.Clone(cp.Items)
	m.byID[cp.ImportID] = cp
	return nil
}
