package intent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Operator, support, recovery and break-glass actions through governed
// intents (INTENT-022). Every material mutation — database surgery,
// workflow node manipulation, connector redrive, projection rebuild,
// failover, quarantine, tenant suspension, key rotation, break-glass —
// resolves a typed operational intent with just-in-time authority:
// a live grant, dual control where the blast radius demands it, a
// simulation except in a declared emergency, and an idempotency key so
// the action executes at most once. Emergency execution records its
// declared bypass reason and mandatory review; it never rewrites
// business truth, and this gate rewrites nothing itself — it authorizes.
//
// Diagnostic reads stay governed capabilities outside this gate; any
// material change or effect carries an intent instance and an outcome.

// ErrOperatorAction is the sentinel every operator-action refusal unwraps
// to. Classify with errors.Is; an authorized action is a receipt, never
// this error.
var ErrOperatorAction = errors.New("intent: operator action refused")

// OperationKind names one material operator mutation.
type OperationKind string

// Material operator mutations.
const (
	OperationDBSurgery         OperationKind = "DB_SURGERY"
	OperationNodeManipulation  OperationKind = "NODE_MANIPULATION"
	OperationConnectorRedrive  OperationKind = "CONNECTOR_REDRIVE"
	OperationProjectionRebuild OperationKind = "PROJECTION_REBUILD"
	OperationFailover          OperationKind = "FAILOVER"
	OperationQuarantine        OperationKind = "QUARANTINE"
	OperationTenantSuspension  OperationKind = "TENANT_SUSPENSION"
	OperationKeyRotation       OperationKind = "KEY_ROTATION"
	OperationBreakGlass        OperationKind = "BREAK_GLASS"
)

// Valid reports whether k is a declared material mutation.
func (k OperationKind) Valid() bool {
	switch k {
	case OperationDBSurgery, OperationNodeManipulation, OperationConnectorRedrive,
		OperationProjectionRebuild, OperationFailover, OperationQuarantine,
		OperationTenantSuspension, OperationKeyRotation, OperationBreakGlass:
		return true
	}
	return false
}

// DualControl reports whether k needs two distinct approvers. The set is
// the blast-radius set: actions one operator must never take alone.
func (k OperationKind) DualControl() bool {
	switch k {
	case OperationDBSurgery, OperationFailover, OperationTenantSuspension,
		OperationKeyRotation, OperationBreakGlass:
		return true
	}
	return false
}

// EmergencyBypass declares an unsimulated emergency execution with its
// reason and the instant its mandatory review falls due.
type EmergencyBypass struct {
	Reason   string
	ReviewBy values.Instant
}

// OperatorActionRequest is one action candidate with its authority.
type OperatorActionRequest struct {
	IntentInstanceID string
	Operation        OperationKind
	Tenant           values.TenantId
	Target           string
	IdempotencyKey   string
	Simulated        bool
	SimulationRef    string
	JITGrant         string
	JITExpires       values.Instant
	Approvers        []string
	Emergency        *EmergencyBypass
}

// OperatorActionReceipt authorizes exactly one action execution.
type OperatorActionReceipt struct {
	IntentInstanceID string
	Operation        OperationKind
	Tenant           values.TenantId
	Target           string
	IdempotencyKey   string
	Emergency        bool
	ReviewBy         values.Instant
	Digest           string
}

// AuthorizeOperatorAction gates one material mutation on its governed
// intent and live JIT authority. Malformed requests, missing intents,
// lapsed grants, solo dual-control actions, unsimulated routine work and
// reasonless emergencies are refused with a typed error.
func AuthorizeOperatorAction(req OperatorActionRequest, at values.Instant) (OperatorActionReceipt, error) {
	if !at.IsSet() {
		return OperatorActionReceipt{}, fmt.Errorf("%w: authorization instant is not set", ErrOperatorAction)
	}
	if strings.TrimSpace(req.IntentInstanceID) == "" {
		return OperatorActionReceipt{}, fmt.Errorf("%w: material changes carry an intent instance; side doors are refused", ErrOperatorAction)
	}
	if !req.Operation.Valid() {
		return OperatorActionReceipt{}, fmt.Errorf("%w: unknown operation %q", ErrOperatorAction, req.Operation)
	}
	if req.Tenant.Validate() != nil {
		return OperatorActionReceipt{}, fmt.Errorf("%w: %v", ErrOperatorAction, req.Tenant.Validate())
	}
	if strings.TrimSpace(req.Target) == "" {
		return OperatorActionReceipt{}, fmt.Errorf("%w: action scope target is required", ErrOperatorAction)
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return OperatorActionReceipt{}, fmt.Errorf("%w: idempotency key is required", ErrOperatorAction)
	}
	if strings.TrimSpace(req.JITGrant) == "" {
		return OperatorActionReceipt{}, fmt.Errorf("%w: just-in-time grant is required", ErrOperatorAction)
	}
	if !req.JITExpires.IsSet() || !req.JITExpires.After(at) {
		return OperatorActionReceipt{}, fmt.Errorf("%w: just-in-time grant is lapsed or missing expiry", ErrOperatorAction)
	}
	if req.Operation.DualControl() && distinctApprovers(req.Approvers) < 2 {
		return OperatorActionReceipt{}, fmt.Errorf("%w: %s needs two distinct approvers", ErrOperatorAction, req.Operation)
	}
	emergency := req.Emergency != nil
	if emergency {
		if strings.TrimSpace(req.Emergency.Reason) == "" {
			return OperatorActionReceipt{}, fmt.Errorf("%w: emergency execution declares its bypass reason", ErrOperatorAction)
		}
		if !req.Emergency.ReviewBy.IsSet() || !req.Emergency.ReviewBy.After(at) {
			return OperatorActionReceipt{}, fmt.Errorf("%w: emergency execution sets a future mandatory review", ErrOperatorAction)
		}
	} else {
		if !req.Simulated || strings.TrimSpace(req.SimulationRef) == "" {
			return OperatorActionReceipt{}, fmt.Errorf("%w: routine execution simulates first and cites the simulation", ErrOperatorAction)
		}
	}
	rec := OperatorActionReceipt{
		IntentInstanceID: req.IntentInstanceID, Operation: req.Operation,
		Tenant: req.Tenant, Target: req.Target, IdempotencyKey: req.IdempotencyKey,
		Emergency: emergency,
	}
	if emergency {
		rec.ReviewBy = req.Emergency.ReviewBy
	}
	rec.Digest = digestOperatorAction(rec)
	return rec, nil
}

func distinctApprovers(approvers []string) int {
	seen := map[string]bool{}
	for _, a := range approvers {
		if strings.TrimSpace(a) == "" {
			continue
		}
		seen[a] = true
	}
	return len(seen)
}

func digestOperatorAction(rec OperatorActionReceipt) string {
	review := ""
	if rec.ReviewBy.IsSet() {
		sec, nsec := rec.ReviewBy.Unix()
		review = fmt.Sprintf("%d.%09d", sec, nsec)
	}
	raw := strings.Join([]string{
		rec.IntentInstanceID, string(rec.Operation), string(rec.Tenant),
		rec.Target, rec.IdempotencyKey, fmt.Sprintf("%t", rec.Emergency), review,
	}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// OperatorActionSort orders receipts deterministically for evidence.
func OperatorActionSort(recs []OperatorActionReceipt) {
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].IntentInstanceID != recs[j].IntentInstanceID {
			return recs[i].IntentInstanceID < recs[j].IntentInstanceID
		}
		return recs[i].IdempotencyKey < recs[j].IdempotencyKey
	})
}
