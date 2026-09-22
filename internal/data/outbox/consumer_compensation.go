package outbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// WF-REV-013: every outbox consumer handles compensating events
// idempotently.
//
// Published events cannot be withdrawn, so a downstream consumer only ever
// learns of a reversal through a compensating event. A compensating event
// is its own outbox row (its own effect identity, so redelivery dedupes on
// it exactly like an original) that reverses one original effect:
//
//   - it applies exactly once, no matter how often it is redelivered;
//   - delivered before its original, it is held - never applied, never
//     failed - until the original arrives and applies;
//   - it never crosses tenants: a compensation releases only when an
//     original with the same identity applied under the same tenant.
//
// The wire link is the house reversal vocabulary the provider hand-off
// already uses (internal/connectivity/payrollsim ReversalKeyPrefix,
// internal/connectivity/providerdelivery ReversalIdempotencyPrefix): a
// compensation carries effect identity "reversal:"+original, so it can
// never be confused with (or replayed as) the original intake. Producers
// mint compensations under that shape; [IdentityCompensationLink] reads
// it back. A custom [CompensationLink] may name the reversal target any
// other way, but the exactly-once and hold contract below is unchanged.
//
// Correlation is partition-free: a compensation may arrive on a different
// ordering key than its original and still release. The durable rule stays
// what [ConsumerGroup] already enforces - one identity, one application
// per partition - plus a process-held observation that an original
// applied. Across a restart the held buffer is empty by construction; the
// same at-least-once redelivery that replays any un-checkpointed message
// replays the compensation (held again, or applied at once when its
// original is already recorded under its partition) and the original
// (observed again, which drains what it releases).

// CompensationPrefix prefixes an original effect identity to form its
// compensation's identity, mirroring the provider reversal keys.
const CompensationPrefix = "reversal:"

// ApplyCompensationHeld reports a compensating event buffered until its
// original applies. It is neither a failure nor a duplicate: the consumer
// holds the row and the next observation of the original drains it.
const ApplyCompensationHeld ApplyOutcome = "COMPENSATION_HELD"

// ErrInvalidCompensation reports a compensating event that names no
// original (an empty reversal target) or names itself.
var ErrInvalidCompensation = errors.New("outbox: invalid compensating event")

// CompensationLink classifies one record: it reports the original effect
// identity a compensating record reverses, or ok=false for an original.
// It must be pure: Dispatch may call it on redeliveries.
type CompensationLink func(rec Record) (reverses string, ok bool)

// IdentityCompensationLink links compensations minted as
// "reversal:"+original, the canonical producer shape. An identity without
// the prefix is an original; "reversal:" with an empty remainder is a
// compensation the dispatcher rejects as invalid.
func IdentityCompensationLink(rec Record) (string, bool) {
	if !strings.HasPrefix(rec.EffectIdentity, CompensationPrefix) {
		return "", false
	}
	return strings.TrimPrefix(rec.EffectIdentity, CompensationPrefix), true
}

// CompensatingConsumer is one [ConsumerGroup] plus the compensation
// contract: originals dispatch through the group unchanged, while
// compensating events apply exactly once and wait for their original.
// All durable state (applied identities, checkpoints, poison) stays in
// the group's CheckpointStore; this wrapper holds only the ephemeral
// wait: which compensations are held for which original, and which
// originals this process already saw applied. At-least-once redelivery
// rebuilds both after a restart, so a held compensation is never lost,
// only re-held until its original is observed again.
type CompensatingConsumer struct {
	groupName string
	group     *ConsumerGroup
	store     CheckpointStore

	mu sync.Mutex
	// held buffers compensating records by the original they reverse,
	// keyed scopeKey(group, tenant, original). Entries leave only by a
	// drain that settles them (applied, fenced as duplicate, or
	// poison-isolated).
	held map[string][]Record
	// observed remembers originals this process saw applied, keyed
	// scopeKey(group, tenant, original), so a compensation on another
	// partition still correlates without partition affinity.
	observed map[string]bool
}

func scopeKey(group string, tenant uuid.UUID, identity string) string {
	return group + "\x00" + tenant.String() + "\x00" + identity
}

// NewCompensatingConsumer builds the compensation-aware consumer over one
// group policy and store, binding them exactly like [NewConsumerGroup]
// plus [ConsumerGroup.Bind]. Route every dispatch for the group through
// the returned consumer (plain originals through [CompensatingConsumer.Group]
// when no link applies); a second group over the same store would split
// the ephemeral attempt accounting.
func NewCompensatingConsumer(policy GroupPolicy, store CheckpointStore) (*CompensatingConsumer, error) {
	if store == nil {
		return nil, fmt.Errorf("outbox: NewCompensatingConsumer: %w", ErrInvalidGroupPolicy)
	}
	group, err := NewConsumerGroup(policy)
	if err != nil {
		return nil, err
	}
	group.Bind(store)
	return &CompensatingConsumer{
		groupName: policy.Group,
		group:     group,
		store:     store,
		held:      make(map[string][]Record),
		observed:  make(map[string]bool),
	}, nil
}

// Group exposes the underlying group for checkpoints, poison inspection
// and repair ([ConsumerGroup.CommitCheckpoint], [ConsumerGroup.Poisoned],
// [ConsumerGroup.RequeueExpired]) and for plain original dispatches.
func (c *CompensatingConsumer) Group() *ConsumerGroup { return c.group }

// Dispatch applies one record under the compensation contract. Originals
// run through the group and, once applied (or observed already applied),
// drain whatever they release. Compensations run at once when their
// original is recorded, and hold otherwise. A held compensation is not a
// failure: failing it would return the row to the queue only to hold it
// again, so the consumer keeps it and reports [ApplyCompensationHeld].
func (c *CompensatingConsumer) Dispatch(ctx context.Context, rec Record, link CompensationLink, handler EffectHandler) (ApplyOutcome, error) {
	if rec.EffectIdentity == "" {
		return "", fmt.Errorf("outbox: compensating Dispatch: %w", ErrInvalidRecord)
	}
	if link == nil || handler == nil {
		return "", fmt.Errorf("outbox: compensating Dispatch: %w", ErrInvalidGroupPolicy)
	}
	reverses, isCompensation := link(rec)
	if !isCompensation {
		return c.dispatchOriginal(ctx, rec, handler)
	}
	if reverses == "" || reverses == rec.EffectIdentity {
		return "", fmt.Errorf("outbox: compensating Dispatch %s: %w", rec.EffectIdentity, ErrInvalidCompensation)
	}
	return c.dispatchCompensation(ctx, rec, reverses, handler)
}

// dispatchOriginal runs one original through the group. An applied
// original (now or earlier) releases its held compensations through the
// current handler; a failed or poisoned one releases nothing, so a
// compensation never reverses an effect that never applied.
func (c *CompensatingConsumer) dispatchOriginal(ctx context.Context, rec Record, handler EffectHandler) (ApplyOutcome, error) {
	outcome, err := c.group.Dispatch(ctx, rec, handler)
	if err != nil {
		return outcome, err
	}
	if outcome != ApplyApplied && outcome != ApplyDuplicateFenced {
		return outcome, nil
	}
	c.observeOriginal(rec.Tenant, rec.EffectIdentity)
	c.drainHeld(ctx, rec.Tenant, rec.EffectIdentity, handler)
	return outcome, nil
}

// dispatchCompensation runs one compensation at once when its original is
// recorded - through the group, so its own fencing, attempts and poison
// match an original's - and holds it otherwise. Redeliveries of a held
// compensation re-hold without duplicating the buffer.
func (c *CompensatingConsumer) dispatchCompensation(ctx context.Context, rec Record, reverses string, handler EffectHandler) (ApplyOutcome, error) {
	if c.originalApplied(rec.Tenant, partitionOf(rec), reverses) {
		return c.group.Dispatch(ctx, rec, handler)
	}
	c.appendHeld(scopeKey(c.groupName, rec.Tenant, reverses), rec)
	return ApplyCompensationHeld, nil
}

// originalApplied reports whether the original applied: durably under the
// compensation's partition, or observed by this process under any
// partition. The store read is advisory - the group remains the authority
// that fences the actual run - so a stale observation can only cause one
// fenced duplicate, never a second application.
func (c *CompensatingConsumer) originalApplied(tenant uuid.UUID, partition, original string) bool {
	if c.store.Applied(c.groupName, partition, original) {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.observed[scopeKey(c.groupName, tenant, original)]
}

func (c *CompensatingConsumer) observeOriginal(tenant uuid.UUID, original string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.observed[scopeKey(c.groupName, tenant, original)] = true
}

func (c *CompensatingConsumer) appendHeld(key string, rec Record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, queued := range c.held[key] {
		if queued.EffectIdentity == rec.EffectIdentity {
			return
		}
	}
	c.held[key] = append(c.held[key], rec)
}

// drainHeld settles one batch of compensations the original releases,
// each through the group (which fences, retries and poisons it exactly
// like a direct delivery). Only the batch present when the drain starts
// runs: transient failures re-hold for the next observation instead of
// spinning, and concurrent arrivals wait for the next drain. Settled
// compensations - applied, fenced as duplicates of a concurrent run, or
// poison-isolated - leave the buffer.
func (c *CompensatingConsumer) drainHeld(ctx context.Context, tenant uuid.UUID, original string, handler EffectHandler) {
	key := scopeKey(c.groupName, tenant, original)
	c.mu.Lock()
	batch := c.held[key]
	delete(c.held, key)
	c.mu.Unlock()
	for _, rec := range batch {
		outcome, err := c.group.Dispatch(ctx, rec, handler)
		if err != nil || outcome == ApplyTransientFailed {
			c.appendHeld(key, rec)
		}
	}
}

// HeldCompensations lists the compensations currently held for one
// original under one tenant: the wait the consumer still owes. The
// returned slice is a copy.
func (c *CompensatingConsumer) HeldCompensations(tenant uuid.UUID, original string) []Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Record(nil), c.held[scopeKey(c.groupName, tenant, original)]...)
}
