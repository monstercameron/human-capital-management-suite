// Ambiguous-commit recovery: LEDGER-014 proves ledger recovery after
// database interruption and ambiguous commit.
//
// Every commit resolves by transaction and idempotency identity to
// committed, not-committed or ambiguous. Connection loss at the commit
// boundary records a governed ambiguity record with observation
// evidence instead of guessing; resolution replays the verdict without
// appending a second business effect, and derived rows repair from the
// ledger without rewriting history.
package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// CommitState is the closed commit resolution.
type CommitState string

// The commit resolutions.
const (
	CommitCommitted    CommitState = "COMMITTED"
	CommitNotCommitted CommitState = "NOT_COMMITTED"
	CommitAmbiguous    CommitState = "AMBIGUOUS"
)

// CommitReceipt is one identity-resolved commit outcome.
type CommitReceipt struct {
	TxID            string
	State           CommitState
	Head            int64
	AppendedEffects int
	Record          string
}

// AmbiguityRecord is the governed record of one interrupted commit.
type AmbiguityRecord struct {
	TxID           string
	IdempotencyKey string
	Evidence       string
	Resolved       bool
}

// commitEntry is the journal row.
type commitEntry struct {
	key      string
	state    CommitState
	head     int64
	evidence string
	resolved bool
}

// CommitJournal is the pure commit-identity journal. It is safe for
// concurrent use.
type CommitJournal struct {
	mu      sync.Mutex
	entries map[string]*commitEntry
	heads   map[string]int64
}

// NewCommitJournal starts an empty journal.
func NewCommitJournal() *CommitJournal {
	return &CommitJournal{entries: map[string]*commitEntry{}, heads: map[string]int64{}}
}

// Begin records one transaction attempt under its idempotency key. A
// begun transaction replays its current verdict instead of double
// beginning.
func (j *CommitJournal) Begin(txID, idempotencyKey string) error {
	if strings.TrimSpace(txID) == "" || strings.TrimSpace(txID) != txID {
		return errors.New("ledger: transaction id is required exact, without padding")
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		return errors.New("ledger: idempotency key is required")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, seen := j.entries[txID]; seen {
		return nil
	}
	for _, entry := range j.entries {
		if entry.key == idempotencyKey {
			return fmt.Errorf("ledger: idempotency key %q already begun", idempotencyKey)
		}
	}
	j.entries[txID] = &commitEntry{key: idempotencyKey, state: CommitAmbiguous}
	return nil
}

// Commit appends one effect and advances the head.
func (j *CommitJournal) Commit(txID string, head int64) (CommitReceipt, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	entry, seen := j.entries[txID]
	if !seen {
		return CommitReceipt{}, fmt.Errorf("ledger: unknown transaction %s", txID)
	}
	if entry.state == CommitCommitted {
		if head != entry.head {
			return CommitReceipt{}, fmt.Errorf("ledger: head/event disagreement for %s", txID)
		}
		return CommitReceipt{TxID: txID, State: CommitCommitted, Head: entry.head, Record: ambiguityDigest(txID, entry)}, nil
	}
	if head <= j.heads[entry.key] {
		return CommitReceipt{}, fmt.Errorf("ledger: head/event disagreement for %s", txID)
	}
	entry.state = CommitCommitted
	entry.head = head
	j.heads[entry.key] = head
	return CommitReceipt{TxID: txID, State: CommitCommitted, Head: head, AppendedEffects: 1, Record: ambiguityDigest(txID, entry)}, nil
}

// Interrupt records connection loss at the commit boundary: the
// transaction becomes a governed ambiguity record, never a guess.
func (j *CommitJournal) Interrupt(txID, evidence string) (CommitReceipt, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	entry, seen := j.entries[txID]
	if !seen {
		return CommitReceipt{}, fmt.Errorf("ledger: unknown transaction %s", txID)
	}
	if strings.TrimSpace(evidence) == "" {
		return CommitReceipt{}, errors.New("ledger: interruption evidence is required")
	}
	entry.state = CommitAmbiguous
	entry.evidence = evidence
	return CommitReceipt{TxID: txID, State: CommitAmbiguous, Record: ambiguityDigest(txID, entry)}, nil
}

// Resolve settles one ambiguity against the observed head: the event
// made it (observed=true) or it did not. Resolution replays the
// verdict and appends no business effect.
func (j *CommitJournal) Resolve(txID string, observedHead int64, observed bool) (CommitReceipt, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	entry, seen := j.entries[txID]
	if !seen {
		return CommitReceipt{}, fmt.Errorf("ledger: unknown transaction %s", txID)
	}
	if entry.state == CommitCommitted {
		return CommitReceipt{TxID: txID, State: CommitCommitted, Head: entry.head, Record: ambiguityDigest(txID, entry)}, nil
	}
	if !entry.resolved && entry.state == CommitAmbiguous && entry.evidence == "" {
		return CommitReceipt{}, fmt.Errorf("ledger: ambiguity for %s lacks an interruption record", txID)
	}
	if observed {
		if observedHead <= j.heads[entry.key] {
			return CommitReceipt{}, fmt.Errorf("ledger: head/event disagreement for %s", txID)
		}
		entry.state = CommitCommitted
		entry.head = observedHead
		j.heads[entry.key] = observedHead
	} else {
		entry.state = CommitNotCommitted
	}
	entry.resolved = true
	return CommitReceipt{TxID: txID, State: entry.state, Head: entry.head, Record: ambiguityDigest(txID, entry)}, nil
}

// Ambiguity returns the governed record for one transaction.
func (j *CommitJournal) Ambiguity(txID string) (AmbiguityRecord, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	entry, seen := j.entries[txID]
	if !seen {
		return AmbiguityRecord{}, fmt.Errorf("ledger: unknown transaction %s", txID)
	}
	return AmbiguityRecord{TxID: txID, IdempotencyKey: entry.key, Evidence: entry.evidence, Resolved: entry.resolved}, nil
}

// RepairDerived rebuilds derived rows from ledger heads. It reports
// the repaired values without writing history: repair reads the
// ledger, never rewrites it.
func (j *CommitJournal) RepairDerived(derived map[string]uint64) map[string]uint64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make(map[string]uint64, len(derived))
	for row, value := range derived {
		best := value
		for _, head := range j.heads {
			if head >= 0 && uint64(head) > best {
				best = uint64(head)
			}
		}
		out[row] = best
	}
	return out
}

func ambiguityDigest(txID string, entry *commitEntry) string {
	parts := []string{"ledger014-commit", txID, entry.key, string(entry.state),
		fmt.Sprint(entry.head), entry.evidence, fmt.Sprint(entry.resolved)}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
