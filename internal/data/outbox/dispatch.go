// Dispatch journal: EVENT-004 proves queue outage, ambiguous publish,
// and recovery over effect identity.
//
// A publish that crosses a connection loss resolves to exactly one of
// committed, duplicate, or ambiguous. Ambiguous is never guessed: the
// dispatcher resumes from durable journal state and reconciles each
// ambiguous operation against the verified downstream outcome, so the
// business effect applies exactly once.
package outbox

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

// PublishOutcome is the identity-resolved result of one publish.
type PublishOutcome string

const (
	// PublishCommitted: the effect committed and applied exactly once.
	PublishCommitted PublishOutcome = "committed"
	// PublishDuplicate: the identity already committed; no second effect.
	PublishDuplicate PublishOutcome = "duplicate"
	// PublishAmbiguous: connection loss at the boundary; unknown.
	PublishAmbiguous PublishOutcome = "ambiguous"
)

// Sender transports one message. It reports whether the broker
// confirmed receipt; a false confirmation with an error means the
// send may or may not have happened.
type Sender func() (confirmed bool, err error)

// Reconciler verifies the downstream outcome for one effect identity:
// true when the effect is already present downstream.
type Reconciler func(effectIdentity string) (present bool, err error)

type dispatchState string

const (
	dispatchCommitted dispatchState = "committed"
	dispatchAmbiguous dispatchState = "ambiguous"
)

type dispatchEntry struct {
	state        dispatchState
	appliedCount int
}

// DispatchJournal is the durable publish-journal projection. It is safe
// for concurrent use; Snapshot/Restore carry the durable state a
// restarted dispatcher resumes from.
type DispatchJournal struct {
	mu      sync.Mutex
	entries map[string]*dispatchEntry
}

// NewDispatchJournal starts an empty journal.
func NewDispatchJournal() *DispatchJournal {
	return &DispatchJournal{entries: map[string]*dispatchEntry{}}
}

// Publish resolves one operation by effect identity.
func (j *DispatchJournal) Publish(effectIdentity string, send Sender, apply func() error) (PublishOutcome, error) {
	trimmed := strings.TrimSpace(effectIdentity)
	if trimmed == "" || trimmed != effectIdentity {
		return "", errors.New("outbox: effect identity is required exact, without padding")
	}
	if send == nil || apply == nil {
		return "", errors.New("outbox: sender and applier are required")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if entry, seen := j.entries[effectIdentity]; seen && entry.state == dispatchCommitted {
		return PublishDuplicate, nil
	}
	if entry, seen := j.entries[effectIdentity]; seen && entry.state == dispatchAmbiguous {
		return PublishAmbiguous, nil
	}
	confirmed, err := send()
	switch {
	case err == nil && confirmed:
		if err := apply(); err != nil {
			return "", err
		}
		j.entries[effectIdentity] = &dispatchEntry{state: dispatchCommitted, appliedCount: 1}
		return PublishCommitted, nil
	case err == nil && !confirmed:
		return "", errors.New("outbox: sender must confirm or fail, never stay silent")
	default:
		j.entries[effectIdentity] = &dispatchEntry{state: dispatchAmbiguous}
		return PublishAmbiguous, nil
	}
}

// Resolve reconciles one ambiguous operation against the verified
// downstream outcome. When the effect is already present downstream it
// commits without applying; otherwise it applies exactly once.
func (j *DispatchJournal) Resolve(effectIdentity string, reconcile Reconciler, apply func() error) error {
	if reconcile == nil || apply == nil {
		return errors.New("outbox: reconciler and applier are required")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	entry, seen := j.entries[effectIdentity]
	if !seen || entry.state != dispatchAmbiguous {
		return errors.New("outbox: only an ambiguous operation can resolve")
	}
	present, err := reconcile(effectIdentity)
	if err != nil {
		return err
	}
	if !present {
		if err := apply(); err != nil {
			return err
		}
		entry.appliedCount = 1
	}
	entry.state = dispatchCommitted
	return nil
}

// JournalSnapshot is the durable state a dispatcher resumes from.
type JournalSnapshot struct {
	Committed []string
	Ambiguous []string
}

// Snapshot exports the durable journal state.
func (j *DispatchJournal) Snapshot() JournalSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	snap := JournalSnapshot{}
	for identity, entry := range j.entries {
		if entry.state == dispatchCommitted {
			snap.Committed = append(snap.Committed, identity)
		} else {
			snap.Ambiguous = append(snap.Ambiguous, identity)
		}
	}
	sort.Strings(snap.Committed)
	sort.Strings(snap.Ambiguous)
	return snap
}

// Resume rebuilds a dispatcher from durable state and reconciles every
// ambiguous operation against the verified downstream outcome.
func Resume(snap JournalSnapshot, reconcile Reconciler, apply func(string) error) (*DispatchJournal, error) {
	if reconcile == nil || apply == nil {
		return nil, errors.New("outbox: reconciler and applier are required")
	}
	journal := NewDispatchJournal()
	for _, identity := range snap.Committed {
		journal.entries[identity] = &dispatchEntry{state: dispatchCommitted, appliedCount: 1}
	}
	for _, identity := range snap.Ambiguous {
		journal.entries[identity] = &dispatchEntry{state: dispatchAmbiguous}
	}
	for _, identity := range snap.Ambiguous {
		present, err := reconcile(identity)
		if err != nil {
			return nil, err
		}
		entry := journal.entries[identity]
		if !present {
			if err := apply(identity); err != nil {
				return nil, err
			}
			entry.appliedCount = 1
		}
		entry.state = dispatchCommitted
	}
	return journal, nil
}
