package execute

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// RepairRecordStage is the closed vocabulary of durable repair-record stages.
//
// The ordering is the whole point. RepairStageClaimed is appended BEFORE the
// corrective effect is invoked, so its presence means "an attempt reached the
// effect boundary", never "the effect succeeded". A success-only record would
// only promise "usually not re-run"; claiming first is what makes "never
// re-run after a restart" true, at the cost of leaving a repair whose outcome
// nobody observed in an honestly indeterminate state.
type RepairRecordStage string

// Repair record stages.
const (
	// RepairStageClaimed is written before the effect port is called.
	RepairStageClaimed RepairRecordStage = "CLAIMED"
	// RepairStageExecuted is written once the provider accepted the redrive.
	// A restart resumes from here at observe and verify, without touching the
	// external system a second time.
	RepairStageExecuted RepairRecordStage = "EXECUTED"
	// RepairStageSettled is the terminal revalidation answer; a later replay
	// of the same plan returns it instead of re-deciding.
	RepairStageSettled RepairRecordStage = "SETTLED"
)

// RepairRecord is one durable fact about a tenant-scoped repair fence. It
// carries execution identities only -- references, states and digests -- never
// a business payload or a provider response body.
type RepairRecord struct {
	TenantID            uuid.UUID
	FenceKey            string
	Stage               RepairRecordStage
	FenceID             string
	PlanDigest          string
	OriginalSemanticKey string
	FailedEffectKey     string
	Status              RepairStatus
	Executed            bool
	ConsistencyState    string

	EffectRef            string
	EffectResultRef      string
	ObservationState     string
	ObservationDigest    string
	ObservationComplete  bool
	ReconciliationStatus string
	ReconciliationRoute  string

	RecordedAt time.Time
}

// RepairIdempotencyStore is the durable, tenant-scoped, append-only record of
// which repair fences already reached the effect boundary.
//
// internal/data/repairrecord is the PostgreSQL implementation
// (migrations/00311: RLS tenant_isolation, a forbid_mutation trigger, and only
// SELECT and INSERT granted); [MemoryRepairRecords] is the in-process one a
// kernel test composes.
type RepairIdempotencyStore interface {
	// LoadRepairRecords returns every record for one tenant-scoped fence,
	// ordered claimed, executed, settled.
	LoadRepairRecords(ctx context.Context, tenantID uuid.UUID, fenceKey string) ([]RepairRecord, error)
	// AppendRepairRecord appends one record and reports whether this call is
	// the one that wrote it. An implementation must decide that atomically --
	// a caller told false has lost a race and must never treat it as
	// permission to proceed.
	AppendRepairRecord(ctx context.Context, record RepairRecord) (bool, error)
}

// MemoryRepairRecords is a concurrency-safe in-process [RepairIdempotencyStore]
// for kernel tests. It is not durable: a composition that wants a repair to
// survive a restart must supply the PostgreSQL-backed store instead.
type MemoryRepairRecords struct {
	mu      sync.Mutex
	records map[string][]RepairRecord
}

// NewMemoryRepairRecords returns an empty in-process record store.
func NewMemoryRepairRecords() *MemoryRepairRecords {
	return &MemoryRepairRecords{records: make(map[string][]RepairRecord)}
}

func repairRecordKey(tenantID uuid.UUID, fenceKey string) string {
	return tenantID.String() + "\x1f" + strings.TrimSpace(fenceKey)
}

// LoadRepairRecords implements [RepairIdempotencyStore].
func (m *MemoryRepairRecords) LoadRepairRecords(_ context.Context, tenantID uuid.UUID, fenceKey string) ([]RepairRecord, error) {
	if err := validateRepairRecordScope(tenantID, fenceKey); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := m.records[repairRecordKey(tenantID, fenceKey)]
	return append([]RepairRecord(nil), stored...), nil
}

// AppendRepairRecord implements [RepairIdempotencyStore]. The lock is held
// across the read and the write, so the claim is one decision rather than a
// check followed by a write.
func (m *MemoryRepairRecords) AppendRepairRecord(_ context.Context, record RepairRecord) (bool, error) {
	if err := record.validate(); err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := repairRecordKey(record.TenantID, record.FenceKey)
	for _, existing := range m.records[key] {
		if existing.Stage == record.Stage {
			return false, nil
		}
	}
	m.records[key] = append(m.records[key], record)
	return true, nil
}

func validateRepairRecordScope(tenantID uuid.UUID, fenceKey string) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("workflow execute: repair record tenant is required")
	}
	if strings.TrimSpace(fenceKey) == "" {
		return fmt.Errorf("workflow execute: repair record fence key is required")
	}
	return nil
}

// Valid reports whether s is a declared stage.
func (s RepairRecordStage) Valid() bool {
	switch s {
	case RepairStageClaimed, RepairStageExecuted, RepairStageSettled:
		return true
	default:
		return false
	}
}

func (r RepairRecord) validate() error {
	if err := validateRepairRecordScope(r.TenantID, r.FenceKey); err != nil {
		return err
	}
	if !r.Stage.Valid() {
		return fmt.Errorf("workflow execute: repair record stage %q is not declared", r.Stage)
	}
	if strings.TrimSpace(string(r.Status)) == "" {
		return fmt.Errorf("workflow execute: repair record status is required")
	}
	if r.RecordedAt.IsZero() {
		return fmt.Errorf("workflow execute: repair record instant is required")
	}
	return nil
}

// findRepairStage returns the record for one stage, if it was written.
func findRepairStage(records []RepairRecord, stage RepairRecordStage) (RepairRecord, bool) {
	for _, record := range records {
		if record.Stage == stage {
			return record, true
		}
	}
	return RepairRecord{}, false
}

// effectOf rebuilds the accepted effect result an EXECUTED record pinned, so a
// restarted cell resumes at observe and verify without calling the provider
// again.
func (r RepairRecord) effectOf() RepairEffectResult {
	return RepairEffectResult{
		EffectKey: r.FailedEffectKey, EffectRef: r.EffectRef,
		Accepted: r.Executed, ResultRef: r.EffectResultRef,
	}
}

// observationOf rebuilds the observation a SETTLED record pinned.
func (r RepairRecord) observationOf() RepairObservation {
	return RepairObservation{
		Observed: r.ObservationState != "", Complete: r.ObservationComplete,
		Digest: r.ObservationDigest, State: r.ObservationState,
	}
}
