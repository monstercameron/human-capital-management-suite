package projectworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Resumable migrations deliberately keep the write fence in the workflow
// value. A caller can persist this value after every batch and resume it after
// a crash without ever exposing a mixed configuration epoch.
type MigrationPhase string

const (
	MigrationPhaseMigrating MigrationPhase = "MIGRATING"
	MigrationPhaseActivated MigrationPhase = "ACTIVATED"
	MigrationPhaseAbandoned MigrationPhase = "ABANDONED"
	MaxMigrationBatchSize                  = 100
)

var (
	ErrMigrationFence       = errors.New("projectworkflow: migration write fence is active")
	ErrMigrationCheckpoint  = errors.New("projectworkflow: invalid migration checkpoint")
	ErrMigrationNotComplete = errors.New("projectworkflow: migration has unapplied task checkpoints")
)

type ResumableMigration struct {
	OperationID    string
	TenantID       string
	ProjectID      string
	CurrentVersion uint64
	PendingVersion uint64
	PlanDigest     string
	Epoch          uint64
	NextTask       int
	AppliedTaskIDs []string
	TaskSnapshots  []TaskSnapshot
	LastBatchToken string
	Phase          MigrationPhase
	Preview        MigrationPreview
}

// NewResumableMigration validates the complete plan once. It does not mutate
// tasks or publish a version; callers must checkpoint batches and activate
// only after every task has been acknowledged.
func NewResumableMigration(tenantID, projectID string, currentVersion, pendingVersion uint64, current, pending Config, tasks []TaskSnapshot, mappings MigrationMappings) (ResumableMigration, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(projectID) == "" || currentVersion == 0 || pendingVersion <= currentVersion {
		return ResumableMigration{}, ErrMigrationCheckpoint
	}
	ordered := append([]TaskSnapshot(nil), tasks...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	preview, err := PreviewMigration(MigrationRequest{Current: current, Pending: pending, Tasks: ordered, StatusMappings: mappings.Statuses, FieldMappings: mappings.Fields})
	if err != nil {
		return ResumableMigration{}, err
	}
	currentDigest := digestConfig(current)
	pendingDigest := digestConfig(pending)
	plan := DigestMigrationPlan(currentDigest, pendingDigest, currentVersion, pendingVersion, mappings, preview, ordered)
	operation := sha256.Sum256([]byte(strings.Join([]string{tenantID, projectID, fmt.Sprint(currentVersion), fmt.Sprint(pendingVersion), plan}, "\x00")))
	return ResumableMigration{
		OperationID: operationID(operation[:]), TenantID: tenantID, ProjectID: projectID,
		CurrentVersion: currentVersion, PendingVersion: pendingVersion, PlanDigest: plan,
		Epoch: 1, Phase: MigrationPhaseMigrating, Preview: preview, TaskSnapshots: ordered,
	}, nil
}

// ApplyBatch advances one bounded checkpoint. Repeating the same batch token
// is an idempotent acknowledgement and returns the already advanced value.
func (m ResumableMigration) ApplyBatch(expectedEpoch uint64, batchSize int, batchToken string) (ResumableMigration, error) {
	if m.Phase != MigrationPhaseMigrating || m.OperationID == "" || m.Epoch == 0 || expectedEpoch == 0 || batchSize < 1 || batchSize > MaxMigrationBatchSize || strings.TrimSpace(batchToken) == "" || !m.validCheckpoint() {
		return ResumableMigration{}, ErrMigrationCheckpoint
	}
	if m.LastBatchToken == batchToken {
		return m, nil
	}
	if expectedEpoch != m.Epoch {
		return ResumableMigration{}, ErrMigrationCheckpoint
	}
	result := m
	result.AppliedTaskIDs = append([]string(nil), m.AppliedTaskIDs...)
	end := m.NextTask + batchSize
	if end > len(m.TaskSnapshots) {
		end = len(m.TaskSnapshots)
	}
	for _, task := range m.TaskSnapshots[m.NextTask:end] {
		result.AppliedTaskIDs = append(result.AppliedTaskIDs, task.ID)
	}
	result.NextTask = end
	result.Epoch++
	result.LastBatchToken = batchToken
	return result, nil
}

// Activate is the only transition that opens writes for the pending epoch.
// It is atomic at the value boundary; persistence adapters should store this
// value and the published configuration in one transaction.
func (m ResumableMigration) Activate(expectedEpoch uint64) (ResumableMigration, error) {
	if m.Phase != MigrationPhaseMigrating || expectedEpoch == 0 || expectedEpoch != m.Epoch || !m.validCheckpoint() {
		return ResumableMigration{}, ErrMigrationCheckpoint
	}
	if m.NextTask != len(m.TaskSnapshots) || len(m.AppliedTaskIDs) != len(m.TaskSnapshots) {
		return ResumableMigration{}, ErrMigrationNotComplete
	}
	result := m
	result.Phase = MigrationPhaseActivated
	result.Epoch++
	return result, nil
}

// AllowsWrite is intentionally false for the whole project while the
// operation is MIGRATING. A task write using the old or pending version must
// not cross the fence.
func (m ResumableMigration) AllowsWrite(configVersion uint64) bool {
	return m.Phase == MigrationPhaseActivated && configVersion == m.PendingVersion
}

func (m ResumableMigration) Tasks() []TaskSnapshot {
	return append([]TaskSnapshot(nil), m.TaskSnapshots...)
}

func (m ResumableMigration) validCheckpoint() bool {
	if m.NextTask < 0 || m.NextTask > len(m.TaskSnapshots) || len(m.AppliedTaskIDs) != m.NextTask {
		return false
	}
	for i, task := range m.TaskSnapshots {
		if task.ID == "" || (i < m.NextTask && m.AppliedTaskIDs[i] != task.ID) {
			return false
		}
	}
	return true
}

func digestConfig(c Config) string {
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func operationID(raw []byte) string {
	return hex.EncodeToString(raw)
}
