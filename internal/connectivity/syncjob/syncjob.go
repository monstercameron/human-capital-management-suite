// Package syncjob owns the pure resumable SyncJob kernel (INTG-019). A
// SyncJob runs in full, incremental, delta or targeted mode against a
// source system and folds source items into local writes without losing
// its place, looping on its own echoes, inventing deletions, or dropping
// per-item failures.
//
// PlanBatch is pure and deterministic: the cursor is the whole resume
// state, so a restart replays from the returned cursor and never
// reprocesses an item. Unchanged fingerprints suppress writes. Source
// absence without a deletion tombstone (or without an explicit inferred-
// delete policy) yields an explicit absence candidate, never a silent
// deletion. Items whose origin is the local system are ownership-
// suppressed, which is what stops a bidirectional pair from oscillating.
//
// The kernel performs no I/O. Cursor persistence is the caller's
// responsibility through the CursorStore port; MemStore is the in-memory
// implementation used by tests and single-process runners.
package syncjob

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	// ErrInvalidJob reports a malformed job, item or batch (empty identity,
	// duplicate id, tombstone without policy).
	ErrInvalidJob = errors.New("syncjob: invalid job or item")
	// ErrUnknownMode reports a job mode outside the closed mode set.
	ErrUnknownMode = errors.New("syncjob: unknown sync mode")
	// ErrCursorMismatch reports a cursor that belongs to another job.
	ErrCursorMismatch = errors.New("syncjob: cursor belongs to another job")
)

// Mode is the closed set of resumable SyncJob modes. All four share the
// cursor contract; they differ only in which source slice the caller feeds
// each batch.
type Mode string

const (
	// ModeFull replays the whole source slice every run.
	ModeFull Mode = "FULL"
	// ModeIncremental replays items at or past the cursor watermark.
	ModeIncremental Mode = "INCREMENTAL"
	// ModeDelta replays caller-selected changed items.
	ModeDelta Mode = "DELTA"
	// ModeTargeted replays one explicit id set.
	ModeTargeted Mode = "TARGETED"
)

// Verdict reasons. Every processed item carries exactly one.
const (
	// ReasonNew marks a first-seen item.
	ReasonNew = "NEW"
	// ReasonChanged marks a fingerprint change.
	ReasonChanged = "CHANGED"
	// ReasonDelete marks an explicit tombstone application.
	ReasonDelete = "DELETE"
	// ReasonUnchanged marks a write suppressed by an equal fingerprint.
	ReasonUnchanged = "UNCHANGED"
	// ReasonLoopPrevented marks a local echo suppressed by ownership.
	ReasonLoopPrevented = "LOOP_PREVENTED"
	// ReasonAbsentCandidate marks a known id missing at the source.
	ReasonAbsentCandidate = "ABSENT_WITHOUT_POLICY"
)

// Job is one resumable sync definition.
type Job struct {
	// ID names the job; cursors bind to it.
	ID string
	// Mode selects the source slicing contract.
	Mode Mode
	// LocalSystem is the ownership identity of this side. Items whose
	// Origin equals LocalSystem are echoes and are never re-emitted.
	LocalSystem string
	// AllowInferredDelete permits absence to mean deletion. When false,
	// absence yields an explicit candidate instead.
	AllowInferredDelete bool
}

// SourceItem is one source row offered to a batch.
type SourceItem struct {
	// ID is the stable source identity.
	ID string
	// Fingerprint is the opaque content hash. Equal means unchanged.
	Fingerprint string
	// Origin names the system of record that last wrote the item.
	Origin string
	// Deleted marks an explicit source tombstone.
	Deleted bool
	// Err carries a per-item source failure, if the read of this item
	// failed. Failed items are recorded, never processed.
	Err string
}

// Cursor is the whole resume state. Watermark advances by processed items;
// Known maps every processed id to its last fingerprint.
type Cursor struct {
	JobID     string
	Watermark int64
	Processed int64
	Written   int64
	Skipped   int64
	Failed    int64
	Known     map[string]string
}

// Write is one local mutation the batch mints.
type Write struct {
	ID          string
	Fingerprint string
	Reason      string
}

// Skip is one item deliberately not written.
type Skip struct {
	ID     string
	Reason string
}

// Candidate is one known id missing at the source without deletion
// evidence. A human or policy decides; the kernel never deletes silently.
type Candidate struct {
	ID     string
	Reason string
}

// ItemFailure is one recorded per-item source failure.
type ItemFailure struct {
	ID    string
	Cause string
}

// Batch is the deterministic outcome of one PlanBatch call.
type Batch struct {
	Cursor     Cursor
	Writes     []Write
	Skips      []Skip
	Candidates []Candidate
	Failures   []ItemFailure
}

// PlanBatch folds one source slice into writes, skips, candidates and
// failures and returns the advanced cursor. Restarting from the returned
// cursor replays nothing already decided.
func PlanBatch(job Job, cursor Cursor, items []SourceItem) (Batch, error) {
	if strings.TrimSpace(job.ID) == "" || strings.TrimSpace(job.LocalSystem) == "" {
		return Batch{}, fmt.Errorf("%w: job needs an id and a local system", ErrInvalidJob)
	}
	switch job.Mode {
	case ModeFull, ModeIncremental, ModeDelta, ModeTargeted:
	default:
		return Batch{}, fmt.Errorf("%w: %q", ErrUnknownMode, string(job.Mode))
	}
	if cursor.JobID != "" && cursor.JobID != job.ID {
		return Batch{}, fmt.Errorf("%w: cursor of %q cannot resume %q", ErrCursorMismatch, cursor.JobID, job.ID)
	}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			return Batch{}, fmt.Errorf("%w: item with empty id", ErrInvalidJob)
		}
		if seen[id] {
			return Batch{}, fmt.Errorf("%w: duplicate item %q in one batch", ErrInvalidJob, id)
		}
		seen[id] = true
	}
	known := make(map[string]string, len(cursor.Known)+len(items))
	for id, fingerprint := range cursor.Known {
		known[id] = fingerprint
	}
	batch := Batch{}
	batch.Cursor = Cursor{JobID: job.ID, Watermark: cursor.Watermark, Known: known,
		Processed: cursor.Processed, Written: cursor.Written, Skipped: cursor.Skipped, Failed: cursor.Failed}
	present := make(map[string]bool, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		present[id] = true
		batch.Cursor.Processed++
		batch.Cursor.Watermark++
		if item.Err != "" {
			batch.Failures = append(batch.Failures, ItemFailure{ID: id, Cause: item.Err})
			batch.Cursor.Failed++
			continue
		}
		if strings.TrimSpace(item.Origin) != "" && item.Origin == job.LocalSystem {
			batch.Skips = append(batch.Skips, Skip{ID: id, Reason: ReasonLoopPrevented})
			batch.Cursor.Skipped++
			known[id] = item.Fingerprint
			continue
		}
		if item.Deleted {
			if !job.AllowInferredDelete {
				return Batch{}, fmt.Errorf("%w: tombstone for %q without inferred-delete policy", ErrInvalidJob, id)
			}
			batch.Writes = append(batch.Writes, Write{ID: id, Fingerprint: item.Fingerprint, Reason: ReasonDelete})
			batch.Cursor.Written++
			delete(known, id)
			continue
		}
		previous, ok := known[id]
		switch {
		case !ok:
			batch.Writes = append(batch.Writes, Write{ID: id, Fingerprint: item.Fingerprint, Reason: ReasonNew})
			batch.Cursor.Written++
			known[id] = item.Fingerprint
		case previous != item.Fingerprint:
			batch.Writes = append(batch.Writes, Write{ID: id, Fingerprint: item.Fingerprint, Reason: ReasonChanged})
			batch.Cursor.Written++
			known[id] = item.Fingerprint
		default:
			batch.Skips = append(batch.Skips, Skip{ID: id, Reason: ReasonUnchanged})
			batch.Cursor.Skipped++
		}
	}
	for id := range known {
		if !present[id] {
			batch.Candidates = append(batch.Candidates, Candidate{ID: id, Reason: ReasonAbsentCandidate})
		}
	}
	sort.Slice(batch.Writes, func(i, j int) bool { return batch.Writes[i].ID < batch.Writes[j].ID })
	sort.Slice(batch.Skips, func(i, j int) bool { return batch.Skips[i].ID < batch.Skips[j].ID })
	sort.Slice(batch.Candidates, func(i, j int) bool { return batch.Candidates[i].ID < batch.Candidates[j].ID })
	sort.Slice(batch.Failures, func(i, j int) bool { return batch.Failures[i].ID < batch.Failures[j].ID })
	return batch, nil
}

// CursorStore is the persistence port for resume cursors.
type CursorStore interface {
	Save(cursor Cursor)
	Load(jobID string) (Cursor, bool)
}

// MemStore is the in-memory CursorStore.
type MemStore struct {
	cursors map[string]Cursor
}

// NewMemStore constructs an empty in-memory cursor store.
func NewMemStore() *MemStore { return &MemStore{cursors: make(map[string]Cursor)} }

// Save persists a copy of the cursor.
func (s *MemStore) Save(cursor Cursor) {
	if s == nil {
		return
	}
	known := make(map[string]string, len(cursor.Known))
	for id, fingerprint := range cursor.Known {
		known[id] = fingerprint
	}
	cursor.Known = known
	s.cursors[cursor.JobID] = cursor
}

// Load returns a copy of the stored cursor for one job.
func (s *MemStore) Load(jobID string) (Cursor, bool) {
	if s == nil {
		return Cursor{}, false
	}
	cursor, ok := s.cursors[jobID]
	if !ok {
		return Cursor{}, false
	}
	known := make(map[string]string, len(cursor.Known))
	for id, fingerprint := range cursor.Known {
		known[id] = fingerprint
	}
	cursor.Known = known
	return cursor, true
}
