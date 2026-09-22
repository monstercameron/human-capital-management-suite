package execute

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type fenceContextKey struct{}

// WithFence returns ctx carrying the WORKFLOW_INSTANCE lease fence a caller
// already holds for the instance it asks the driver to advance -- the timer
// scheduler, for example, claims the instance before dispatching fired ready
// work. Every advancement the driver performs under ctx presents this fence
// (stamped per call) instead of acquiring its own lease.
func WithFence(ctx context.Context, fence runtime.Fence) context.Context {
	return context.WithValue(ctx, fenceContextKey{}, fence)
}

func fenceFromContext(ctx context.Context) (runtime.Fence, bool) {
	fence, ok := ctx.Value(fenceContextKey{}).(runtime.Fence)
	return fence, ok
}

// InstanceLeaser acquires and releases the WORKFLOW_INSTANCE lease a
// caller-driven run advances under, so a request that holds no lease of its
// own (an approval decision, an intent execution) is still fenced against a
// concurrent or superseded replica.
type InstanceLeaser interface {
	AcquireInstance(ctx context.Context, ex runtime.Executor, tenantID, instanceID uuid.UUID, at time.Time) (runtime.Fence, error)
	ReleaseInstance(ctx context.Context, ex runtime.Executor, tenantID uuid.UUID, fence runtime.Fence, at time.Time) error
}

// currentFence is the fence this call advances under: a caller-held fence on
// ctx, otherwise the driver's static Options.Fence, otherwise none.
func (d *Driver) currentFence(ctx context.Context) *runtime.Fence {
	if fence, ok := fenceFromContext(ctx); ok {
		return &fence
	}
	if d.opts.Fence != nil {
		fence := *d.opts.Fence
		return &fence
	}
	return nil
}

// verifyFence checks the fence against the durable lease inside tx before the
// advancement runs any in-transaction step or reads workflow state, so a
// superseded holder never writes a domain effect.
func (d *Driver) verifyFence(ctx context.Context, tx runtime.Executor, tenantID, instanceID uuid.UUID, fence runtime.Fence) error {
	if d.opts.FenceVerifier == nil {
		return invalid("advancement presents a lease fence but the driver has no FenceVerifier")
	}
	if err := fence.ValidateForInstance(instanceID); err != nil {
		return fmt.Errorf("%w: %w", ErrFenceRefused, err)
	}
	if err := d.opts.FenceVerifier.VerifyFence(ctx, tx, tenantID, fence); err != nil {
		return fmt.Errorf("%w: %w", ErrFenceRefused, err)
	}
	return nil
}

// acquireInstanceLease returns ctx carrying a fence for instanceID and a
// release function. When ctx already carries a fence or no InstanceLeaser is
// configured it changes nothing and release is a no-op.
func (d *Driver) acquireInstanceLease(ctx context.Context, tenantID, instanceID uuid.UUID) (context.Context, func() error, error) {
	noop := func() error { return nil }
	if fence := d.currentFence(ctx); fence != nil {
		if d.opts.FenceVerifier == nil {
			return ctx, noop, invalid("advancement presents a lease fence but the driver has no FenceVerifier")
		}
		fence.At = d.opts.Clock().UTC()
		if err := fence.ValidateForInstance(instanceID); err != nil {
			return ctx, noop, fmt.Errorf("%w: %w", ErrFenceRefused, err)
		}
		return ctx, noop, nil
	}
	if d.opts.Leases == nil {
		return ctx, noop, nil
	}
	if tenantID == uuid.Nil || instanceID == uuid.Nil {
		return ctx, noop, invalid("instance lease needs tenant and instance identity")
	}
	var fence runtime.Fence
	if err := d.inTenantTx(ctx, tenantID, func(tx runtime.Executor) error {
		var err error
		fence, err = d.opts.Leases.AcquireInstance(ctx, tx, tenantID, instanceID, d.opts.Clock().UTC())
		if err != nil {
			return err
		}
		return fence.ValidateForInstance(instanceID)
	}); err != nil {
		return ctx, noop, fmt.Errorf("%w: acquire instance lease: %w", ErrFenceRefused, err)
	}
	release := func() error {
		return d.inTenantTx(context.WithoutCancel(ctx), tenantID, func(tx runtime.Executor) error {
			return d.opts.Leases.ReleaseInstance(ctx, tx, tenantID, fence, d.opts.Clock().UTC())
		})
	}
	return WithFence(ctx, fence), release, nil
}

func (d *Driver) inTenantTx(ctx context.Context, tenantID uuid.UUID, fn func(runtime.Executor) error) error {
	tx, err := d.opts.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// releasing joins a lease release error onto the run's own error without
// masking it.
func releasing(err error, release func() error) error {
	if rerr := release(); rerr != nil {
		return errors.Join(err, fmt.Errorf("workflow execute: release instance lease: %w", rerr))
	}
	return err
}
