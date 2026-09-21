package application

// Served work-queue write ports (PROMO-EXEC-004).
//
// The WorkService read surface (ListWorkItems, GetWorkItem) has always been
// composed; its write surface (Claim, Release, Complete, DecideApproval) was
// left unwired because the ports had no production driver behind them. This
// file supplies that driver: thin adapters over the pool this composition
// opened, calling the workitem domain store inside caller-owned,
// tenant-scoped transactions. Handlers keep calling ports, never business
// logic (ARCH-GO-023): every rule -- membership, compare-and-swap,
// separation of duties -- lives in the store and its ports, not here.
//
// Authority is PROMOUX-003's SeparationAuthority: the same sibling-lock rule
// the approval kernel enforces, so a decision through the queue and a
// decision through the journey are held to one separation policy. The session
// port accepts the verified caller's own session reference, which the
// transport fills from the credential it just verified: no served revocation
// registry exists for bearer session refs, so the credential's own
// verification (signature and expiry, on every call) is the live session
// check. Wiring a durable revocation manager is an explicit non-goal here.
//
// Approvals complete only by decision: Complete on an approval-kind item is
// refused, so the decision (and its SoD recheck) cannot be bypassed with a
// plain completion.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	stepsapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
)

var (
	_ transporthumanwork.Claims      = (*servedWorkQueueWrites)(nil)
	_ transporthumanwork.Completions = (*servedWorkQueueWrites)(nil)
	_ transporthumanwork.Decisions   = (*servedWorkQueueWrites)(nil)
)

// servedWorkQueueWrites drives the workitem store for the served WorkService.
// State lives on this value, which ComposeServe owns; there is no package
// mutable registry.
type servedWorkQueueWrites struct {
	pool *pgxadapter.Pool
	now  func() time.Time
}

func newServedWorkQueueWrites(pool *pgxadapter.Pool, now func() time.Time) *servedWorkQueueWrites {
	if now == nil {
		now = time.Now
	}
	return &servedWorkQueueWrites{pool: pool, now: now}
}

// workQueueSession is the served session port: the reference must be present,
// and it is always the verified caller's own, because the transport fills it
// from the credential it authenticated this call with rather than from
// anything the caller asserted.
type workQueueSession struct{}

func (workQueueSession) CheckRevocation(_ context.Context, ref string, _ time.Time) error {
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("application: work queue: a session reference is required")
	}
	return nil
}

// inTx runs fn inside one tenant-scoped transaction.
func (w *servedWorkQueueWrites) inTx(ctx context.Context, tenant string, fn func(tx dbport.Tx) error) error {
	if w == nil || w.pool == nil {
		return fmt.Errorf("application: work queue: no database is composed")
	}
	tenantID := pgstore.TenantID(tenant)
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("application: work queue: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return fmt.Errorf("application: work queue: scope tenant: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("application: work queue: commit transaction: %w", err)
	}
	return nil
}

func parseWorkItemID(workItemID string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(workItemID))
	if err != nil {
		return uuid.Nil, fmt.Errorf("application: work queue: work item %q is not a UUID: %w", workItemID, err)
	}
	return id, nil
}

// Claim implements transporthumanwork.Claims.
func (w *servedWorkQueueWrites) Claim(ctx context.Context, tenant, workItemID, principal string, expectedVersion uint64, claimExpiresAt, now time.Time, meta workitem.TransitionMeta) (workitem.WorkItem, error) {
	id, err := parseWorkItemID(workItemID)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	var claimed workitem.WorkItem
	if err := w.inTx(ctx, tenant, func(tx dbport.Tx) error {
		var err error
		claimed, err = (workitem.Store{}).ClaimCurrent(ctx, tx, workitem.ClaimCurrentInput{
			TenantID: pgstore.TenantID(tenant), WorkItemID: id, ExpectedVersion: int64(expectedVersion),
			ClaimantPrincipalID: principal, ClaimExpiresAt: claimExpiresAt, Now: now, Meta: meta,
		})
		return err
	}); err != nil {
		return workitem.WorkItem{}, err
	}
	return claimed, nil
}

// Release implements transporthumanwork.Claims.
func (w *servedWorkQueueWrites) Release(ctx context.Context, tenant, workItemID, principal string, expectedVersion uint64, now time.Time, meta workitem.TransitionMeta) (workitem.WorkItem, error) {
	id, err := parseWorkItemID(workItemID)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	var released workitem.WorkItem
	if err := w.inTx(ctx, tenant, func(tx dbport.Tx) error {
		var err error
		released, err = (workitem.Store{}).Release(ctx, tx, workitem.ReleaseInput{
			TenantID: pgstore.TenantID(tenant), WorkItemID: id, ExpectedVersion: int64(expectedVersion),
			ReleasingPrincipalID: principal, Now: now, Meta: meta,
		})
		return err
	}); err != nil {
		return workitem.WorkItem{}, err
	}
	return released, nil
}

// startIfClaimed moves a CLAIMED item to IN_PROGRESS inside the caller's
// transaction, chaining the version forward. The lifecycle completes only
// from IN_PROGRESS, and the journey kernel moves claim, start and complete
// atomically the same way; the queue ports do the same so a caller holding
// a claim can finish the action it claimed for in one RPC. Anything not
// CLAIMED or IN_PROGRESS is left for the store to refuse.
func startIfClaimed(ctx context.Context, tx dbport.Tx, tenantID, id uuid.UUID, expectedVersion int64, now time.Time, principal string) (int64, error) {
	current, err := (workitem.Store{}).Load(ctx, tx, tenantID, id)
	if err != nil {
		return 0, err
	}
	if current.Status != workitem.StatusClaimed {
		return expectedVersion, nil
	}
	started, err := (workitem.Store{}).Start(ctx, tx, tenantID, id, expectedVersion, now, workitem.TransitionMeta{
		ActorPrincipalID: principal, Reason: "workitem.started_via_endpoint", At: now,
	})
	if err != nil {
		return 0, err
	}
	return started.ItemVersion, nil
}

// Complete implements transporthumanwork.Completions. Approval-kind items
// are refused: an approval completes only through DecideApproval, where the
// decision and its separation-of-duties recheck are recorded.
func (w *servedWorkQueueWrites) Complete(ctx context.Context, tenant, workItemID, sessionRef, principal string, expectedVersion uint64, completedOutputDigest string, now time.Time, meta workitem.TransitionMeta) (workitem.WorkItem, error) {
	id, err := parseWorkItemID(workItemID)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	var completed workitem.WorkItem
	if err := w.inTx(ctx, tenant, func(tx dbport.Tx) error {
		current, err := (workitem.Store{}).Load(ctx, tx, pgstore.TenantID(tenant), id)
		if err != nil {
			return err
		}
		if current.Kind == workitem.KindApproval {
			return &workitem.Error{Code: workitem.CodeIllegalTransition, WorkItemID: id.String(),
				Detail: "an approval completes only through DecideApproval, never a plain completion"}
		}
		version, err := startIfClaimed(ctx, tx, pgstore.TenantID(tenant), id, int64(expectedVersion), now, principal)
		if err != nil {
			return err
		}
		completed, err = (workitem.Store{}).CompleteWithAuthorityRecheck(ctx, tx, workitem.CompleteWithAuthorityRecheckInput{
			CompleteInput: workitem.CompleteInput{
				TenantID: pgstore.TenantID(tenant), WorkItemID: id, ExpectedVersion: version,
				CompletedBy: principal, CompletedOutputDigest: completedOutputDigest, Now: now, Meta: meta,
			},
			SessionRef: sessionRef, Session: workQueueSession{}, Authority: stepsapproval.SeparationAuthority{},
		})
		return err
	}); err != nil {
		return workitem.WorkItem{}, err
	}
	return completed, nil
}

// Decide implements transporthumanwork.Decisions.
func (w *servedWorkQueueWrites) Decide(ctx context.Context, tenant, workItemID, sessionRef, principal string, expectedVersion uint64, proposalRevisionRef string, decision workitem.ApprovalDecision, reasonRef string, now time.Time, meta workitem.TransitionMeta) (workitem.WorkItem, error) {
	id, err := parseWorkItemID(workItemID)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	var decided workitem.WorkItem
	if err := w.inTx(ctx, tenant, func(tx dbport.Tx) error {
		version, err := startIfClaimed(ctx, tx, pgstore.TenantID(tenant), id, int64(expectedVersion), now, principal)
		if err != nil {
			return err
		}
		decided, err = (workitem.Store{}).DecideApproval(ctx, tx, workitem.DecideApprovalInput{
			TenantID: pgstore.TenantID(tenant), WorkItemID: id, ExpectedVersion: version,
			ProposalRevisionRef: proposalRevisionRef, Decision: decision, ReasonRef: reasonRef,
			DecidingPrincipal: principal, SessionRef: sessionRef,
			Session: workQueueSession{}, Authority: stepsapproval.SeparationAuthority{},
			Now: now, Meta: meta,
		})
		return err
	}); err != nil {
		return workitem.WorkItem{}, err
	}
	return decided, nil
}
