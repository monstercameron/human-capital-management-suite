// Package workerlifecyclestore retains onboarding owner inputs and emitted
// child state. It never derives requirement satisfaction from worker presence.
package workerlifecyclestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workerlifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalid  = errors.New("workerlifecyclestore: invalid owner snapshot")
	ErrNotFound = errors.New("workerlifecyclestore: onboarding snapshot not found")
	ErrConflict = errors.New("workerlifecyclestore: revision conflict")
)

// Snapshot contains accepted owner inputs, not a caller-supplied readiness
// answer. DisplayName is read from current workforce and is never persisted here.
type Snapshot struct {
	Request        workerlifecycle.ResolutionRequest
	Tracker        workerlifecycle.OnboardingTracker
	WorkerRevision values.RevisionToken
	ChannelID      string
	Revision       uint64
	DisplayName    string `json:"-"`
}

// Validate checks the full plan and tracker linkage as well as fact provenance.
// Each observation must name evidence pinned by its requirement; a valid entity
// reference alone cannot satisfy arbitrary requirements.
func (s Snapshot) Validate() error {
	if s.Revision == 0 || s.WorkerRevision.Validate() != nil || !s.WorkerRevision.IsSpecified() || strings.TrimSpace(s.ChannelID) != s.ChannelID || s.ChannelID == "" || s.Request.Plan.CanonicalDigest == "" || s.Request.Plan.Event != workerlifecycle.EventStart || s.Request.Plan.Validate() != nil || s.Tracker.Validate() != nil || s.Tracker.PlanDigest != s.Request.Plan.CanonicalDigest || s.Tracker.EventDate != s.Request.Plan.EventDate || s.Tracker.Completion != s.Request.Plan.Completion {
		return ErrInvalid
	}
	readiness, err := workerlifecycle.ResolveOnboardingReadiness(s.Request)
	if err != nil || readiness.Validate() != nil {
		return ErrInvalid
	}
	requirements := map[string]workerlifecycle.Requirement{}
	for _, r := range s.Request.Plan.Requirements {
		requirements[r.ID] = r
	}
	for _, f := range s.Request.Facts {
		found := false
		for _, ref := range requirements[f.RequirementID].Evidence {
			if ref == f.Evidence {
				found = true
			}
		}
		if !found {
			return ErrInvalid
		}
	}
	expected, err := workerlifecycle.NewOnboardingTracker(s.Request.Plan, readiness)
	if err != nil || len(expected.Children) != len(s.Tracker.Children) {
		return ErrInvalid
	}
	for i, child := range s.Tracker.Children {
		template := expected.Children[i]
		if child.ChildID != template.ChildID || child.IntentType != template.IntentType || child.IntentVersion != template.IntentVersion || child.ScopeDigest != template.ScopeDigest || !reflect.DeepEqual(child.DependsOn, template.DependsOn) {
			return ErrInvalid
		}
		if child.State == workerlifecycle.StatusChildObserved && child.ObservationRef.Tenant != s.Request.Plan.Worker.Tenant {
			return ErrInvalid
		}
	}
	return nil
}

type Store struct {
	db         dbport.Beginner
	tenantUUID func(values.TenantId) uuid.UUID
}

func New(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID) (*Store, error) {
	if db == nil || tenantUUID == nil {
		return nil, ErrInvalid
	}
	return &Store{db: db, tenantUUID: tenantUUID}, nil
}

func (s *Store) begin(ctx context.Context, worker values.EntityRef) (dbport.Tx, uuid.UUID, error) {
	if s == nil || s.db == nil || s.tenantUUID == nil || ctx == nil || worker.Validate() != nil || worker.Kind != "worker" {
		return nil, uuid.Nil, ErrInvalid
	}
	tenant := s.tenantUUID(worker.Tenant)
	if tenant == uuid.Nil {
		return nil, uuid.Nil, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, uuid.Nil, err
	}
	if err = tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return nil, uuid.Nil, err
	}
	return tx, tenant, nil
}

// Put is for the accepted lifecycle owner. It uses optimistic concurrency and
// binds the immutable workforce row's exact revision and employment.
func (s *Store) Put(ctx context.Context, snapshot Snapshot, expectedRevision uint64) error {
	if snapshot.Validate() != nil || expectedRevision >= uint64(1<<63-1) || snapshot.Revision != expectedRevision+1 {
		return ErrInvalid
	}
	worker := snapshot.Request.Plan.Worker
	tx, tenant, err := s.begin(ctx, worker)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = currentWorker(ctx, tx, tenant, &snapshot); err != nil {
		return err
	}
	raw, err := encode(snapshot)
	if err != nil || len(raw) > 1048576 {
		return ErrInvalid
	}
	var count int64
	if expectedRevision == 0 {
		count, err = tx.Exec(ctx, `INSERT INTO worker_onboarding_snapshot(tenant_id,worker_id,snapshot_revision,snapshot_payload,payload_digest) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, tenant, worker.Id, snapshot.Revision, raw, digest(raw))
	} else {
		count, err = tx.Exec(ctx, `UPDATE worker_onboarding_snapshot SET snapshot_revision=$3,snapshot_payload=$4,payload_digest=$5 WHERE tenant_id=$1 AND worker_id=$2 AND snapshot_revision=$6`, tenant, worker.Id, snapshot.Revision, raw, digest(raw), expectedRevision)
	}
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return tx.Commit(ctx)
}

// Get refuses stale workforce pins and corrupt snapshots. Both reads share the
// tenant transaction; workforce rows are protected by forbid_mutation.
func (s *Store) Get(ctx context.Context, worker values.EntityRef) (Snapshot, error) {
	tx, tenant, err := s.begin(ctx, worker)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var raw []byte
	var persistedDigest string
	var revision uint64
	err = tx.QueryRow(ctx, `SELECT snapshot_revision,snapshot_payload,payload_digest FROM worker_onboarding_snapshot WHERE tenant_id=$1 AND worker_id=$2`, tenant, worker.Id).Scan(&revision, &raw, &persistedDigest)
	if errors.Is(err, dbport.ErrNoRows) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, err
	}
	var out Snapshot
	if len(raw) > 1048576 || decode(raw, &out) != nil || out.Revision != revision || out.Request.Plan.Worker != worker || out.Validate() != nil {
		return Snapshot{}, ErrInvalid
	}
	// jsonb normalizes encoding. Re-encode the typed value before checking the
	// digest created at write time, rather than hashing PostgreSQL's whitespace.
	canonical, err := encode(out)
	if err != nil || digest(canonical) != persistedDigest {
		return Snapshot{}, ErrInvalid
	}
	if err = currentWorker(ctx, tx, tenant, &out); err != nil {
		return Snapshot{}, err
	}
	return out, nil
}

func currentWorker(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, snapshot *Snapshot) error {
	worker := snapshot.Request.Plan.Worker
	row, found, err := (workforce.Store{}).Get(ctx, tx, tenant, worker.Id)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	revision, err := values.NewSequenceRevision(row.RevisionStream, row.RevisionSequence)
	if err != nil || !revision.Equal(snapshot.WorkerRevision) || row.EmploymentID != snapshot.Request.Plan.Employment.Id {
		return ErrConflict
	}
	snapshot.DisplayName = row.PreferredName
	if snapshot.DisplayName == "" {
		snapshot.DisplayName = row.LegalName
	}
	if strings.TrimSpace(snapshot.DisplayName) == "" {
		return ErrInvalid
	}
	return nil
}

func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
