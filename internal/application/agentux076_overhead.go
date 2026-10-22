package application

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AGENTUX-076 (AGENTUX-028): the owner's live run spent 21 s on 3.3 s of model
// time. Authority was verified in full at each of four runstate boundaries and
// again by the tool helpers, each time reading the persona, the facts, the pins,
// the invoker's grants and the delegation grant.
//
// A run now verifies in full where it matters and carries that verification
// across the bookkeeping boundaries between:
//
//   - in full when the run is claimed, and again immediately before its answer
//     is validated and written (the one effect that leaves the model's work);
//   - carried at the two model-call checkpoints and at the start of the
//     document search, where only a cheap probe runs: the invoker's revocation
//     epoch must be the one the full verification saw, and neither the human
//     session, the delegation grant nor the run's deadline may have run out.
//
// A change to the grant is therefore refused at the next boundary; a change to
// the installation, the person's membership or the stop controls is refused at
// the latest before the answer is written. Within one boundary the verified
// result was already shared (AGENTUX-028).

type agentUX076RunKey struct{}
type agentUX076CarryKey struct{}

// agentUX076Run is one run's record of the full verification its carried
// boundaries lean on.
type agentUX076Run struct {
	mu sync.Mutex
	// verified is set by a full verification and cleared by an invalidation.
	verified  bool
	epoch     uint64
	grantEnds time.Time
	// carrying is true from the start of a carried boundary until the probe has
	// run once; the verifications that follow inside the boundary reuse it.
	carrying bool
	probed   bool
	// policy is the T0 policy that the last full verification ran, so a tool
	// helper may rely on it only when it is the same policy.
	policy any
}

// agentUX076WithRun installs the run's verification record on ctx. A context
// without a run timing record carries nothing, and every boundary verifies in
// full as it did before.
func agentUX076WithRun(ctx context.Context) context.Context {
	if agentUXRunTimingFromContext(ctx) == nil {
		return ctx
	}
	if _, ok := ctx.Value(agentUX076RunKey{}).(*agentUX076Run); ok {
		return ctx
	}
	return context.WithValue(ctx, agentUX076RunKey{}, &agentUX076Run{})
}

func agentUX076RunFrom(ctx context.Context) *agentUX076Run {
	if ctx == nil {
		return nil
	}
	run, _ := ctx.Value(agentUX076RunKey{}).(*agentUX076Run)
	return run
}

// agentUX076CarryBoundary marks the runstate call made with the returned context
// as one that may carry the run's last full verification.
func agentUX076CarryBoundary(ctx context.Context) context.Context {
	if agentUX076RunFrom(ctx) == nil {
		return ctx
	}
	return context.WithValue(ctx, agentUX076CarryKey{}, true)
}

// agentUX076BeginBoundary is called by the runstate rechecker before it
// verifies. It reports whether this boundary carries the run's verification;
// when it does not, the cached verification is dropped so that the boundary
// verifies in full.
func agentUX076BeginBoundary(ctx context.Context) bool {
	run := agentUX076RunFrom(ctx)
	carry, _ := ctx.Value(agentUX076CarryKey{}).(bool)
	if run != nil && carry {
		run.mu.Lock()
		held := run.verified
		if held {
			run.carrying, run.probed = true, false
		}
		run.mu.Unlock()
		if held {
			return true
		}
	}
	agentUXSpeedInvalidateAuthority(ctx)
	if run != nil {
		run.mu.Lock()
		run.verified, run.carrying = false, false
		run.mu.Unlock()
	}
	return false
}

// agentUX076NoteVerified records what a full verification saw.
func agentUX076NoteVerified(ctx context.Context, policy any, epoch uint64, grantEnds time.Time) {
	if run := agentUX076RunFrom(ctx); run != nil {
		run.mu.Lock()
		run.verified, run.carrying, run.probed = true, false, false
		run.epoch, run.grantEnds, run.policy = epoch, grantEnds, policy
		run.mu.Unlock()
	}
}

// agentUX076InvalidateRun drops the run's verification and the cached authority
// before the next verification, which will then be in full.
func agentUX076InvalidateRun(ctx context.Context) {
	agentUXSpeedInvalidateAuthority(ctx)
	if run := agentUX076RunFrom(ctx); run != nil {
		run.mu.Lock()
		run.verified, run.carrying = false, false
		run.mu.Unlock()
	}
}

// agentUX076CarryHolds is asked when a verification is about to be answered from
// the cached result. Inside a boundary that does not carry, and for every ask
// after the first inside one that does, the result stands. The first ask of a
// carried boundary probes: the revocation epoch is still the one the full
// verification saw, and the human session, the grant and the run itself are
// still inside their time.
func (a *PersonaForegroundRunAuthority) agentUX076CarryHolds(ctx context.Context, request agentrun.Request) bool {
	run := agentUX076RunFrom(ctx)
	if run == nil {
		return true
	}
	run.mu.Lock()
	if !run.carrying || run.probed {
		run.mu.Unlock()
		return true
	}
	run.probed = true
	epoch, grantEnds, verified := run.epoch, run.grantEnds, run.verified
	run.mu.Unlock()
	if !verified {
		return false
	}
	now := a.now().UTC()
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || !now.Before(principal.ExpiresAt()) || (!grantEnds.IsZero() && !now.Before(grantEnds)) || (!request.Deadline.IsZero() && !now.Before(request.Deadline)) {
		return false
	}
	store, err := a.grants.ForTenant(ctx, values.TenantId(request.Source.TenantID))
	if err != nil || isNilPersonaOutputPort(store) {
		return false
	}
	return store.CurrentRevocationEpoch(values.TenantId(request.Source.TenantID), request.Principal.InvokerID) == epoch
}

// agentUX076ToolPolicyCovered reports whether the authority verification that
// just answered for this request already ran the given T0 policy on the same
// invocation, so a tool helper need not run it again.
func agentUX076ToolPolicyCovered(ctx context.Context, policy any, request agentrun.Request) bool {
	run := agentUX076RunFrom(ctx)
	if run == nil || policy == nil {
		return false
	}
	if _, cached := agentUXSpeedCachedAuthority(ctx, request); !cached {
		return false
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.verified && run.policy == policy
}

// agentUX076ResolveKey names one persona authority read inside one verification.
type agentUX076ResolveKey struct{}

type agentUX076ResolveMemo struct {
	mu       sync.Mutex
	resolver agentinvoke.AuthorityResolver
	request  agentinvoke.AdmissionRequest
	value    agentinvoke.Admission
	set      bool
}

// agentUX076SameResolver is true only for the very same resolver value; a
// resolver that cannot be compared is never taken for the same one.
func agentUX076SameResolver(a, b agentinvoke.AuthorityResolver) bool {
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	return ta != nil && ta == tb && ta.Comparable() && a == b
}

// agentUX076WithResolveMemo lets the two checks inside one verification that ask
// the persona authority the same question share the answer. The memo lives as
// long as the context it is put on, which is one verification.
func agentUX076WithResolveMemo(ctx context.Context) context.Context {
	return context.WithValue(ctx, agentUX076ResolveKey{}, &agentUX076ResolveMemo{})
}

// agentUX076Resolve asks the resolver, or answers from the memo when this
// verification already asked the same question.
func agentUX076Resolve(ctx context.Context, resolver agentinvoke.AuthorityResolver, request agentinvoke.AdmissionRequest) (agentinvoke.Admission, error) {
	memo, _ := ctx.Value(agentUX076ResolveKey{}).(*agentUX076ResolveMemo)
	if memo != nil {
		memo.mu.Lock()
		if memo.set && memo.request == request && agentUX076SameResolver(memo.resolver, resolver) {
			value := memo.value
			memo.mu.Unlock()
			return value, nil
		}
		memo.mu.Unlock()
	}
	value, err := resolver.Resolve(ctx, request)
	if err == nil && memo != nil {
		memo.mu.Lock()
		memo.resolver, memo.request, memo.value, memo.set = resolver, request, value, true
		memo.mu.Unlock()
	}
	return value, err
}
