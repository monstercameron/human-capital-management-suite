package runtime

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Refusal codes WF-RUN-002's fenced advancement path introduces. They are
// additive: [Advance] itself is unchanged and still refuses only on the
// instance version, which remains the whole concurrency story for a caller
// that holds no lease.
const (
	// CodeFenceRequired reports a fenced advancement whose fence is missing
	// or malformed. It is raised before any statement runs.
	CodeFenceRequired = "FENCE_REQUIRED"
	// CodeFenceRefused reports a fence the verifier rejected: a stale token, a
	// foreign lease line, or a lease the holder no longer holds. The
	// verifier's own typed refusal is wrapped, so a caller can still read
	// LEASE_LOST or FENCE_STALE off it with the lease package's CodeOf.
	CodeFenceRefused = "FENCE_REFUSED"
)

// Fence is the lease grant an advancement presents. It is a plain value this
// package compares nothing about itself: [FenceVerifier] owns the comparison,
// because the lease line lives in internal/workflow/lease and this package
// deliberately does not depend on it.
type Fence struct {
	ResourceKind string
	ResourceID   string
	LeaseID      uuid.UUID
	HolderID     string
	Token        uint64

	// At is the caller's own clock reading, handed to the verifier so that a
	// holder past its own lease window is refused. This package reads no
	// clock, here as everywhere else.
	At time.Time
}

func (f Fence) validate(instanceID uuid.UUID) error {
	id := instanceID.String()
	switch {
	case f.ResourceKind == "" || f.ResourceID == "":
		return refuse(CodeFenceRequired, id, "", "fence names no resource kind and id")
	case f.LeaseID == uuid.Nil:
		return refuse(CodeFenceRequired, id, "", "fence names no lease id")
	case f.HolderID == "":
		return refuse(CodeFenceRequired, id, "", "fence names no holder")
	case f.Token == 0:
		return refuse(CodeFenceRequired, id, "", "fence token must be at least 1; zero is never a minted token")
	case f.At.IsZero():
		return refuse(CodeFenceRequired, id, "",
			"fence carries no instant; a holder past its own lease window cannot be refused without one")
	}
	return nil
}

// FenceVerifier accepts or refuses one presented [Fence] inside the caller's
// transaction, before the advancement reads anything.
//
// This is a consumer-owned port: internal/workflow/lease's Manager implements
// it (through lease.Fenced), and nothing in this package knows how a fence is
// compared. A test supplies its own double.
type FenceVerifier interface {
	VerifyFence(ctx context.Context, ex Executor, tenantID uuid.UUID, fence Fence) error
}

// FencedAdvanceRequest is [AdvanceRequest] with the lease fence the caller is
// advancing under.
type FencedAdvanceRequest struct {
	Fence    Fence
	Verifier FenceVerifier
	Request  AdvanceRequest
}

// AdvanceFenced is [Advance] with a lease fence presented first.
//
// The ordering is the point (WF-RUN-002's RED clause: an expired or stale
// worker must not complete a node, write state or dispatch an effect). The
// fence is validated as a value and then verified against the durable lease
// before this function reads the instance, its node executions or its
// frontier -- so a refused advancement has issued no workflow_instance or
// workflow_node_execution statement at all, and has certainly dispatched no
// continuation, because every effect write a [ContinuationSink] performs
// happens inside [Advance], underneath the verified fence, in this same
// transaction.
//
// A caller that holds no lease keeps calling [Advance] directly; this is
// strictly additive.
func AdvanceFenced(ctx context.Context, tx Executor, req FencedAdvanceRequest) (ret0 AdvanceReceipt, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.advance_fenced", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if req.Verifier == nil {
		return AdvanceReceipt{}, refuse(CodeFenceRequired, req.Request.InstanceID.String(), req.Request.Outcome.NodeID,
			"no fence verifier supplied; a fenced advancement cannot check its own fence")
	}
	if err := req.Fence.validate(req.Request.InstanceID); err != nil {
		return AdvanceReceipt{}, err
	}
	if err := req.Verifier.VerifyFence(ctx, tx, req.Request.TenantID, req.Fence); err != nil {
		return AdvanceReceipt{}, wrap(CodeFenceRefused, req.Request.InstanceID.String(), req.Request.Outcome.NodeID, err,
			"fence token %d on %s %s presented by %s was refused",
			req.Fence.Token, req.Fence.ResourceKind, req.Fence.ResourceID, req.Fence.HolderID)
	}
	return Advance(ctx, tx, req.Request)
}
