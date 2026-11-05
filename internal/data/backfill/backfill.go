// Package backfill tracks derived-data backfills (WF-DATA-028): publishing
// a derived field, rollup, resolution or searchable field starts a resumable,
// throttled backfill; the element is BACKFILLING until complete and then
// READY, and consumers refuse anything that is not READY.
//
// The driver is pure and the Store port owns durability: a SQL adapter joins
// the caller's transaction exactly like projection.Apply, while MemoryStore
// proves the resume semantics in-process. Async fan-out after READY belongs
// to WF-DATA-027's propagation watermarks, not to this tracker.
package backfill

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	ErrInvalidElement  = errors.New("backfill: invalid element reference")
	ErrInvalidJob      = errors.New("backfill: invalid backfill job")
	ErrNoBackfill      = errors.New("backfill: element has no backfill job")
	ErrElementNotReady = errors.New("backfill: element is not READY")
	ErrCursorConflict  = errors.New("backfill: cursor moved under this driver")
	ErrInvalidBudget   = errors.New("backfill: invalid batch budget")
)

// maxBatchSize bounds one computed batch: a backfill that cannot bound its
// batches cannot be throttled.
const maxBatchSize = 100000

// ElementKind is the closed derived-element vocabulary.
type ElementKind string

const (
	KindDerivedField    ElementKind = "DERIVED_FIELD"
	KindRollup          ElementKind = "ROLLUP"
	KindResolution      ElementKind = "RESOLUTION"
	KindSearchableField ElementKind = "SEARCHABLE_FIELD"
)

func (k ElementKind) Valid() bool {
	switch k {
	case KindDerivedField, KindRollup, KindResolution, KindSearchableField:
		return true
	default:
		return false
	}
}

// ElementState is the closed backfill lifecycle: BACKFILLING until the last
// batch commits, then READY forever.
type ElementState string

const (
	StateBackfilling ElementState = "BACKFILLING"
	StateReady       ElementState = "READY"
)

// ElementRef pins one versioned derived element in one tenant.
type ElementRef struct {
	Tenant    string
	Kind      ElementKind
	Namespace string
	Name      string
	Version   uint32
}

func (e ElementRef) Validate() error {
	if strings.TrimSpace(e.Tenant) == "" || !e.Kind.Valid() || strings.TrimSpace(e.Namespace) == "" || strings.TrimSpace(e.Name) == "" || e.Version == 0 {
		return fmt.Errorf("%w: tenant, kind, namespace, name and positive version are required", ErrInvalidElement)
	}
	return nil
}

func (e ElementRef) key() string {
	return e.Tenant + "\x00" + string(e.Kind) + "\x00" + e.Namespace + "\x00" + e.Name + "\x00" + fmt.Sprintf("%d", e.Version)
}

// Job is one element's backfill progress: the committed cursor, the throttle
// batch size, and the lifecycle state.
type Job struct {
	Element ElementRef
	Cursor  string
	State   ElementState
	Batch   int
	Total   int64
}

// BatchBudget throttles one Run: at most MaxBatches batches and MaxRecords
// computed records before the driver yields.
type BatchBudget struct {
	MaxBatches int
	MaxRecords int
}

func (b BatchBudget) Validate() error {
	if b.MaxBatches < 1 || b.MaxRecords < 1 {
		return fmt.Errorf("%w: batches and records must be positive", ErrInvalidBudget)
	}
	return nil
}

// Batch is one computed unit of backfill work.
type Batch struct {
	NextCursor string
	Records    int
	Done       bool
}

// Computer derives backfill values for one cursor range. It must be a pure
// function of (element, cursor, limit): resume replays cursors, never values.
type Computer interface {
	ComputeBatch(ctx context.Context, element ElementRef, cursor string, limit int) (Batch, error)
}

// Store is the durable backfill ledger. Start and Advance are atomic:
// concurrent drivers serialize on the element, and a crash between batches
// leaves the last committed cursor behind.
type Store interface {
	Start(ctx context.Context, element ElementRef, batchSize int, totalEstimate int64) (Job, error)
	Load(ctx context.Context, element ElementRef) (Job, error)
	Advance(ctx context.Context, element ElementRef, expectedCursor, nextCursor string, done bool) (Job, error)
}

// MemoryStore is an in-process Store with compare-and-swap cursor semantics.
type MemoryStore struct {
	mu   sync.Mutex
	jobs map[string]Job
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{jobs: map[string]Job{}} }

// Start publishes a backfill job. Republishing is idempotent: a BACKFILLING
// job keeps its cursor (resume, never restart) and a READY job stays READY.
func (s *MemoryStore) Start(_ context.Context, element ElementRef, batchSize int, totalEstimate int64) (Job, error) {
	if err := element.Validate(); err != nil {
		return Job{}, err
	}
	if batchSize < 1 || batchSize > maxBatchSize || totalEstimate < 0 {
		return Job{}, fmt.Errorf("%w: batch size within 1..%d and non-negative total are required", ErrInvalidJob, maxBatchSize)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.jobs[element.key()]; ok {
		return job, nil
	}
	job := Job{Element: element, Cursor: "offset:0", State: StateBackfilling, Batch: batchSize, Total: totalEstimate}
	s.jobs[element.key()] = job
	return job, nil
}

// Load returns the recorded job, or ErrNoBackfill when nothing published.
func (s *MemoryStore) Load(_ context.Context, element ElementRef) (Job, error) {
	if err := element.Validate(); err != nil {
		return Job{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[element.key()]
	if !ok {
		return Job{}, fmt.Errorf("%w: %s/%s", ErrNoBackfill, element.Namespace, element.Name)
	}
	return job, nil
}

// Advance commits one computed batch under a cursor compare-and-swap. A done
// batch flips the job READY; anything else stays BACKFILLING at nextCursor.
func (s *MemoryStore) Advance(_ context.Context, element ElementRef, expectedCursor, nextCursor string, done bool) (Job, error) {
	if err := element.Validate(); err != nil {
		return Job{}, err
	}
	if strings.TrimSpace(nextCursor) == "" {
		return Job{}, fmt.Errorf("%w: next cursor is required", ErrInvalidJob)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[element.key()]
	if !ok {
		return Job{}, fmt.Errorf("%w: %s/%s", ErrNoBackfill, element.Namespace, element.Name)
	}
	if job.State == StateReady {
		return job, nil
	}
	if job.Cursor != expectedCursor {
		return Job{}, fmt.Errorf("%w: at %s, driver advanced from %s", ErrCursorConflict, job.Cursor, expectedCursor)
	}
	job.Cursor = nextCursor
	if done {
		job.State = StateReady
	}
	s.jobs[element.key()] = job
	return job, nil
}

// RequireReady is the consumer gate the workflow compiler, screens and
// queries call: only a READY element may be used. An unpublished element is
// refused as never computed, never admitted.
func RequireReady(ctx context.Context, store Store, element ElementRef) error {
	if store == nil {
		return fmt.Errorf("%w: store is required", ErrInvalidJob)
	}
	job, err := store.Load(ctx, element)
	if err != nil {
		return err
	}
	if job.State != StateReady {
		return fmt.Errorf("%w: %s/%s is %s", ErrElementNotReady, element.Namespace, element.Name, job.State)
	}
	return nil
}

// Run drives one element's backfill within budget: compute batches from the
// committed cursor and advance after each. A computer failure aborts the run
// without advancing, so the next run resumes from the last commit. A READY
// element is a no-op that computes nothing.
func Run(ctx context.Context, store Store, computer Computer, element ElementRef, budget BatchBudget) (Job, error) {
	if store == nil || computer == nil {
		return Job{}, fmt.Errorf("%w: store and computer are required", ErrInvalidJob)
	}
	if err := budget.Validate(); err != nil {
		return Job{}, err
	}
	job, err := store.Load(ctx, element)
	if err != nil {
		return Job{}, err
	}
	if job.State == StateReady {
		return job, nil
	}
	records := 0
	for batch := 0; batch < budget.MaxBatches; batch++ {
		if err := ctx.Err(); err != nil {
			return job, err
		}
		computed, err := computer.ComputeBatch(ctx, element, job.Cursor, job.Batch)
		if err != nil {
			return job, err
		}
		if computed.Records < 0 || records+computed.Records > budget.MaxRecords {
			return job, fmt.Errorf("%w: batch overruns the record budget", ErrInvalidBudget)
		}
		records += computed.Records
		job, err = store.Advance(ctx, element, job.Cursor, computed.NextCursor, computed.Done)
		if err != nil {
			return job, err
		}
		if computed.Done {
			return job, nil
		}
		if records >= budget.MaxRecords {
			return job, nil
		}
	}
	return job, nil
}
