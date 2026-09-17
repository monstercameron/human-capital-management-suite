// Package settlement observes rail acceptance and settlement as distinct
// evidence. Provider acceptance is never settlement: only an authoritative
// rail observation advances an instruction, and every refusal carries the
// offending field without creating a side effect.
package settlement

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	// ErrObservationRejected is the SETTLE-005 typed refusal: the observation
	// conflates acceptance with settlement, names funding as a kind, advances
	// out of order, or lacks the identity and evidence a rail fact requires.
	ErrObservationRejected = errors.New("SETTLE_005_REJECTED")
)

// ObservationKind is the closed SETTLE-005 rail vocabulary. FUNDING is
// deliberately absent: funding is a requirement, never rail evidence.
type ObservationKind string

const (
	ObservationAcceptance    ObservationKind = "ACCEPTANCE"
	ObservationSettlement    ObservationKind = "SETTLEMENT"
	ObservationRejection     ObservationKind = "REJECTION"
	ObservationReturn        ObservationKind = "RETURN"
	ObservationIndeterminate ObservationKind = "INDETERMINATE"
)

func (k ObservationKind) valid() bool {
	switch k {
	case ObservationAcceptance, ObservationSettlement, ObservationRejection,
		ObservationReturn, ObservationIndeterminate:
		return true
	default:
		return false
	}
}

// EvidenceState is the closed SETTLE-005 outcome vocabulary. Acceptance and
// settlement are distinct states; UNKNOWN marks ambiguous rail evidence that
// advanced nothing.
type EvidenceState string

const (
	EvidencePending  EvidenceState = "PENDING"
	EvidenceAccepted EvidenceState = "ACCEPTED"
	EvidenceSettled  EvidenceState = "SETTLED"
	EvidenceRejected EvidenceState = "REJECTED"
	EvidenceReturned EvidenceState = "RETURNED"
	EvidenceUnknown  EvidenceState = "UNKNOWN"
)

// ObservationError reports the offending field and state without creating an
// authoritative side effect.
type ObservationError struct {
	Code   string
	Field  string
	State  string
	Reason string
	Cause  error
}

func (e *ObservationError) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s: %s", e.Code, e.Field, e.State, e.Reason)
}

// Is reports the typed SETTLE-005 boundary and any wrapped cause.
func (e *ObservationError) Is(target error) bool {
	return target == ErrObservationRejected || target == e.Cause
}

// Unwrap returns the wrapped cause, if any.
func (e *ObservationError) Unwrap() error { return e.Cause }

func observeRefusal(field, state, reason string, cause error) error {
	return &ObservationError{Code: ErrObservationRejected.Error(), Field: field, State: state, Reason: reason, Cause: cause}
}

// RailObservation is one rail fact about one instruction. ObservedAt is an
// injected reference time: this contract never reads a clock.
type RailObservation struct {
	InstructionKey string
	InstructionID  string
	Kind           ObservationKind
	Authoritative  bool
	Source         string
	ProviderRef    string
	ObservedAt     time.Time
}

// ObservationOutcome is the typed answer to one observation: the resulting
// evidence state, whether the observation advanced it, and how many rail
// facts the tracker holds for the instruction.
type ObservationOutcome struct {
	Key        string
	State      EvidenceState
	Advanced   bool
	Sightings  int
	ObservedAt time.Time
}

// ObservationTracker holds per-instruction evidence states. It is pure memory
// with a mutex: no database, no clock, no network, no provider call.
type ObservationTracker struct {
	mu            sync.Mutex
	states        map[string]EvidenceState
	sightings     map[string]int
	indeterminate map[string]bool
}

// NewObservationTracker builds an empty evidence tracker.
func NewObservationTracker() *ObservationTracker {
	return &ObservationTracker{
		states:        map[string]EvidenceState{},
		sightings:     map[string]int{},
		indeterminate: map[string]bool{},
	}
}

// State reads the current evidence for one instruction key. Keys never
// observed read PENDING; keys seen only through ambiguous evidence read
// UNKNOWN.
func (t *ObservationTracker) State(key string) EvidenceState {
	t.mu.Lock()
	defer t.mu.Unlock()
	if state, ok := t.states[key]; ok {
		return state
	}
	if t.indeterminate[key] {
		return EvidenceUnknown
	}
	return EvidencePending
}

// Observe records one rail fact. Non-authoritative and indeterminate facts
// are recorded without advancing the state; only an authoritative rail
// observation moves an instruction forward, and only in evidence order.
func (t *ObservationTracker) Observe(obs RailObservation) (ObservationOutcome, error) {
	if !obs.Kind.valid() {
		return ObservationOutcome{}, observeRefusal("kind", string(EvidencePending), fmt.Sprintf("observation kind %q is not rail evidence", obs.Kind), nil)
	}
	if strings.TrimSpace(obs.InstructionKey) == "" {
		return ObservationOutcome{}, observeRefusal("instruction_key", string(EvidencePending), "instruction key is required", nil)
	}
	if strings.TrimSpace(obs.Source) == "" {
		return ObservationOutcome{}, observeRefusal("source", string(EvidencePending), "rail source is required", nil)
	}
	if obs.ObservedAt.IsZero() {
		return ObservationOutcome{}, observeRefusal("observed_at", string(EvidencePending), "reference time is required", nil)
	}
	if obs.Kind != ObservationIndeterminate && strings.TrimSpace(obs.ProviderRef) == "" {
		return ObservationOutcome{}, observeRefusal("provider_ref", string(EvidencePending), "provider evidence is required", nil)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	current, tracked := t.states[obs.InstructionKey]
	if !tracked {
		current = EvidencePending
		if t.indeterminate[obs.InstructionKey] {
			current = EvidenceUnknown
		}
	}
	record := func(state EvidenceState, advanced bool) ObservationOutcome {
		t.sightings[obs.InstructionKey]++
		return ObservationOutcome{
			Key: obs.InstructionKey, State: state, Advanced: advanced,
			Sightings: t.sightings[obs.InstructionKey], ObservedAt: obs.ObservedAt.UTC(),
		}
	}
	if obs.Kind == ObservationIndeterminate {
		if !tracked {
			t.indeterminate[obs.InstructionKey] = true
			return record(EvidenceUnknown, false), nil
		}
		return record(current, false), nil
	}
	if !obs.Authoritative {
		return record(current, false), nil
	}
	switch obs.Kind {
	case ObservationAcceptance:
		switch current {
		case EvidencePending, EvidenceUnknown:
			t.states[obs.InstructionKey] = EvidenceAccepted
			delete(t.indeterminate, obs.InstructionKey)
			return record(EvidenceAccepted, true), nil
		case EvidenceAccepted:
			return record(EvidenceAccepted, false), nil
		default:
			return ObservationOutcome{}, observeRefusal("state", string(current), "acceptance cannot advance a terminal instruction", nil)
		}
	case ObservationSettlement:
		switch current {
		case EvidenceAccepted:
			t.states[obs.InstructionKey] = EvidenceSettled
			return record(EvidenceSettled, true), nil
		case EvidenceSettled:
			return record(EvidenceSettled, false), nil
		case EvidencePending, EvidenceUnknown:
			return ObservationOutcome{}, observeRefusal("state", string(current), "settlement requires prior acceptance: acceptance is not settlement", nil)
		default:
			return ObservationOutcome{}, observeRefusal("state", string(current), "settlement cannot advance a terminal instruction", nil)
		}
	case ObservationRejection:
		switch current {
		case EvidencePending, EvidenceUnknown, EvidenceAccepted:
			t.states[obs.InstructionKey] = EvidenceRejected
			delete(t.indeterminate, obs.InstructionKey)
			return record(EvidenceRejected, true), nil
		default:
			return ObservationOutcome{}, observeRefusal("state", string(current), "rejection cannot advance a terminal instruction", nil)
		}
	case ObservationReturn:
		switch current {
		case EvidenceAccepted, EvidenceSettled:
			t.states[obs.InstructionKey] = EvidenceReturned
			return record(EvidenceReturned, true), nil
		default:
			return ObservationOutcome{}, observeRefusal("state", string(current), "return requires an accepted or settled instruction", nil)
		}
	default:
		return ObservationOutcome{}, observeRefusal("kind", string(current), "observation kind is not rail evidence", nil)
	}
}
