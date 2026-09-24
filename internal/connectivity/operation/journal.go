package operation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// JournalEvent is the payload-free append-only record of a connector
// operation transition. Digests chain the records so a missing or replaced
// transition is detectable without retaining a provider payload or secret.
type JournalEvent struct {
	Sequence          uint64
	OperationSequence uint64
	OperationID       uuid.UUID
	TenantID          string
	Event             string
	From              State
	To                State
	AttemptID         uuid.UUID
	FenceToken        uint64
	CredentialLeaseID string
	RequestDigest     string
	ResponseDigest    string
	OccurredAt        time.Time
	PreviousDigest    string
	Digest            string
}

var (
	ErrRecoveryRequired   = errors.New("operation: recovery required")
	ErrCredentialRequired = errors.New("operation: dispatch credential lease is required")
	ErrCredentialRejected = errors.New("operation: dispatch credential lease rejected")
)

// CredentialRejectedCode is the stable machine-readable refusal code for
// CONN-RT-004. It is safe to expose because it names no credential material.
const CredentialRejectedCode = "CONN_RT_004_REJECTED"

// CredentialRejection identifies the first dispatch binding that failed. It
// deliberately contains state and version coordinates only; no credential
// material is ever included.
type CredentialRejection struct {
	Code    string
	Field   string
	State   State
	Version uint64
	Cause   error
}

func (e *CredentialRejection) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s: field=%s state=%s version=%d", e.Code, e.Field, e.State, e.Version)
}

func (e *CredentialRejection) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *CredentialRejection) Is(target error) bool { return target == ErrCredentialRejected }

// Journal returns the append-only operation history. A zero operation id
// returns the complete stream, which is useful to a recovery verifier.
func (j *MemoryJournal) Journal(ctx context.Context, tenant string, id uuid.UUID) ([]JournalEvent, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(tenant) == "" {
		return nil, fmt.Errorf("%w: tenant is required", ErrInvalid)
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	if id != uuid.Nil {
		if op, ok := j.operations[id]; !ok || op.TenantID != tenant {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
	}
	out := make([]JournalEvent, 0, len(j.events))
	for _, event := range j.events {
		if event.TenantID == tenant && (id == uuid.Nil || event.OperationID == id) {
			out = append(out, event)
		}
	}
	return out, nil
}

// VerifyJournal checks the global sequence and hash chain. It is intentionally
// independent of the operation map so it can validate a restored journal
// before workers are allowed to dispatch.
func (j *MemoryJournal) VerifyJournal(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	previous := ""
	for index, event := range j.events {
		if event.Sequence != uint64(index+1) {
			return fmt.Errorf("%w: journal sequence %d", ErrRecoveryRequired, event.Sequence)
		}
		if event.PreviousDigest != previous || event.Digest != journalDigest(event, previous) {
			return fmt.Errorf("%w: journal digest at sequence %d", ErrRecoveryRequired, event.Sequence)
		}
		previous = event.Digest
	}
	return nil
}

// Recover settles work left in a leased or sending state after a worker or
// process crash. A sending operation becomes ambiguous and must be observed;
// it is never silently put back into the queue where it could double-send.
func (j *MemoryJournal) Recover(ctx context.Context, at time.Time) ([]Operation, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if at.IsZero() {
		at = j.now().UTC()
	} else {
		at = at.UTC()
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	var recovered []Operation
	for id, op := range j.operations {
		switch op.State {
		case StateLeased:
			lease, ok := j.activeLeases[id]
			if !ok || !at.Before(lease.ExpiresAt) {
				delete(j.activeLeases, id)
				from := op.State
				op.State, op.UpdatedAt = StateQueued, at
				j.operations[id] = cloneOperation(op)
				j.appendJournalLocked(op, from, StateQueued, "LEASE_RECOVERED", uuid.Nil, lease.FenceToken, "", "", at)
				recovered = append(recovered, cloneOperation(op))
			}
		case StateSending:
			from := op.State
			op.State, op.ResponseClass, op.CompletionState, op.UpdatedAt = StateAmbiguous, ResponseAmbiguous, "PENDING", at
			attempt := Attempt{AttemptID: uuid.New(), OperationID: id, AttemptNumber: len(op.Attempts) + 1, RequestDigest: op.MappedPayloadDigest, ProviderResult: ResponseAmbiguous, RetryDisposition: RetryObservationNeeded, FenceToken: op.FenceToken, AttemptedAt: at, ReceivedAt: at}
			op.Attempts = append(op.Attempts, attempt)
			j.operations[id] = cloneOperation(op)
			delete(j.activeLeases, id)
			j.appendJournalLocked(op, from, StateAmbiguous, "SEND_RECOVERED_AMBIGUOUS", attempt.AttemptID, attempt.FenceToken, "", attempt.RequestDigest, at)
			recovered = append(recovered, cloneOperation(op))
		}
	}
	return recovered, nil
}

func (j *MemoryJournal) appendJournalLocked(op Operation, from, to State, kind string, attemptID uuid.UUID, fence uint64, credentialID, requestDigest string, at time.Time) {
	j.journalSequence++
	operationSequence := j.operationSeqs[op.OperationID] + 1
	j.operationSeqs[op.OperationID] = operationSequence
	event := JournalEvent{Sequence: j.journalSequence, OperationSequence: operationSequence, OperationID: op.OperationID, TenantID: op.TenantID, Event: kind, From: from, To: to, AttemptID: attemptID, FenceToken: fence, CredentialLeaseID: credentialID, RequestDigest: requestDigest, OccurredAt: at.UTC(), PreviousDigest: j.journalDigest}
	event.Digest = journalDigest(event, event.PreviousDigest)
	j.events = append(j.events, event)
	j.journalDigest = event.Digest
}

func journalDigest(event JournalEvent, previous string) string {
	input := strings.Join([]string{
		"connector-operation-journal/v1", previous, fmt.Sprint(event.Sequence), fmt.Sprint(event.OperationSequence), event.OperationID.String(), event.TenantID,
		event.Event, string(event.From), string(event.To), event.AttemptID.String(), fmt.Sprint(event.FenceToken), event.CredentialLeaseID, event.RequestDigest, event.OccurredAt.UTC().Format(time.RFC3339Nano),
	}, "\x00")
	sum := sha256.Sum256([]byte(input))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ChainJournalEvent assigns durable chain coordinates to an event that is
// being replayed from a process-local journal. Durable adapters use this when
// a fresh worker has reconstructed an operation from its execution snapshot:
// synthetic PLANNED/QUEUED events are skipped, and the next real transition
// must continue the persisted hash chain.
func ChainJournalEvent(event JournalEvent, sequence, operationSequence uint64, previous string) JournalEvent {
	event.Sequence = sequence
	event.OperationSequence = operationSequence
	event.PreviousDigest = previous
	event.Digest = journalDigest(event, previous)
	return event
}
