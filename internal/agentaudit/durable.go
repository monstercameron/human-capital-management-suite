package agentaudit

import (
	"fmt"
	"time"
)

// ValidateEntry applies the same admission checks as MemoryStore.Append and
// wraps failures in ErrInvalidEntry, for durable Store adapters.
func ValidateEntry(entry Entry) error {
	if err := entry.validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEntry, err)
	}
	return nil
}

// ValidateQuery applies the same viewer and kind checks as MemoryStore.Query.
func ValidateQuery(q Query) error {
	if err := q.Viewer.valid(); err != nil {
		return fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}
	for _, kind := range q.Kinds {
		if !kind.valid() {
			return fmt.Errorf("%w: invalid event kind %q", ErrInvalidEntry, kind)
		}
	}
	return nil
}

// SealRecord links an entry to the previous chain hash at the given sequence
// and returns the created record, exactly as MemoryStore.Append builds it.
func SealRecord(entry Entry, sequence uint64, prevHash string, recordedAt time.Time) Record {
	entry = cloneEntry(entry)
	record := Record{Entry: entry, Sequence: sequence, PrevHash: prevHash, RecordedAt: recordedAt.UTC(), Created: true}
	record.ChainHash = hashRecord(record)
	return record
}

// ProjectRecord redacts a record for a viewer using the query projection.
func ProjectRecord(record Record, viewer Viewer) View { return project(record, viewer) }

// SameEntry reports whether two entries have identical canonical bytes, the
// idempotent-retry comparison used by Append.
func SameEntry(a, b Entry) bool { return canonicalEntry(a) == canonicalEntry(b) }
