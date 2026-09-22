package execution

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// WF-REV-013: the served promotion's downstream sync consumer.
//
// The promotion's advance transaction enqueues one distribution leg per
// downstream projection ("payroll:"+proposal, "iam:"+proposal); the
// providers apply them and the step observations verify them. Published
// legs cannot be withdrawn, so when a run is reversed the downstream
// learns of it only through a compensating leg ("reversal:"+original).
// This consumer applies original legs and reverses compensated ones
// through one shared [outbox.CompensatingConsumer]: a duplicate leg runs
// once, and a compensation that arrives before its leg waits for it.
//
// The projection below is the consumer's own downstream bookkeeping -
// which legs applied, which reversed, in what order - standing in for the
// provider-side sync projections the observations read. It fails closed:
// a reversal for a leg that never applied, a second application of one
// leg, or a fenced run is an error, never a silent second effect.

// DownstreamSyncConsumer consumes the served promotion's distribution
// legs with compensation idempotency.
type DownstreamSyncConsumer struct {
	compensating *outbox.CompensatingConsumer

	mu       sync.Mutex
	applied  map[string]outbox.Record
	reversed map[string]string
	sequence []string
}

// NewDownstreamSyncConsumer builds the consumer over one group policy
// and store. Route every leg for the group through it; see
// [outbox.NewCompensatingConsumer] for why the group is not shared.
func NewDownstreamSyncConsumer(policy outbox.GroupPolicy, store outbox.CheckpointStore) (*DownstreamSyncConsumer, error) {
	compensating, err := outbox.NewCompensatingConsumer(policy, store)
	if err != nil {
		return nil, err
	}
	return &DownstreamSyncConsumer{
		compensating: compensating,
		applied:      make(map[string]outbox.Record),
		reversed:     make(map[string]string),
	}, nil
}

// Group exposes the underlying consumer group for checkpoints, poison
// inspection and repair.
func (c *DownstreamSyncConsumer) Group() *outbox.ConsumerGroup {
	return c.compensating.Group()
}

// Consume applies one distribution leg: an original leg applies, a
// compensating leg reverses the leg it names once that leg applied, and
// a compensating leg that arrives first waits for it.
func (c *DownstreamSyncConsumer) Consume(ctx context.Context, rec outbox.Record) (result outbox.ApplyOutcome, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.downstream.consume", nil)
	op.Set(observe.KeyTenant, rec.Tenant.String())
	defer func() { observe.Done(op, retErr) }()
	return c.compensating.Dispatch(ctx, rec, outbox.IdentityCompensationLink, c.handle)
}

func (c *DownstreamSyncConsumer) handle(_ context.Context, rec outbox.Record, fencing outbox.Fencing) error {
	if !fencing.AllowExternalEffects {
		return errors.New("platform execution: downstream sync never applies fenced effects")
	}
	original, isCompensation := outbox.IdentityCompensationLink(rec)
	c.mu.Lock()
	defer c.mu.Unlock()
	if isCompensation {
		if _, ok := c.applied[original]; !ok {
			return fmt.Errorf("platform execution: reversal %s names no applied leg", rec.EffectIdentity)
		}
		if _, duplicate := c.reversed[original]; duplicate {
			return fmt.Errorf("platform execution: leg %s is already reversed", original)
		}
		c.reversed[original] = rec.EffectIdentity
		c.sequence = append(c.sequence, "reverse:"+original)
		return nil
	}
	if _, duplicate := c.applied[rec.EffectIdentity]; duplicate {
		return fmt.Errorf("platform execution: leg %s is already applied", rec.EffectIdentity)
	}
	c.applied[rec.EffectIdentity] = rec
	c.sequence = append(c.sequence, "apply:"+rec.EffectIdentity)
	return nil
}

// Applied reports whether one leg applied.
func (c *DownstreamSyncConsumer) Applied(identity string) (outbox.Record, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	leg, ok := c.applied[identity]
	return leg, ok
}

// Reversed reports the compensation that reversed one leg, if any.
func (c *DownstreamSyncConsumer) Reversed(original string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	compensation, ok := c.reversed[original]
	return compensation, ok
}

// Sequence returns the apply/reverse order the consumer ran, oldest first.
func (c *DownstreamSyncConsumer) Sequence() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.sequence...)
}

// Held lists the compensations still waiting for one original leg.
func (c *DownstreamSyncConsumer) Held(tenant uuid.UUID, original string) []outbox.Record {
	return c.compensating.HeldCompensations(tenant, original)
}
