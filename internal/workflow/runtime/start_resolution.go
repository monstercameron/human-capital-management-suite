package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// ErrStartOutcomeUnresolved means a lost START response cannot be proven to
// have committed. In particular, an absent row is unresolved, not evidence
// that any replay or retry is safe.
var ErrStartOutcomeUnresolved = errors.New("workflow runtime: start outcome unresolved")

// StartResolution is the read-only result of proving that one logical START
// was durably committed. It describes current durable state; it is not the
// original StartReceipt, whose approval decision ids are not stored on the
// workflow instance.
type StartResolution struct {
	Instance        Instance
	SemanticVersion string
}

// ResolveStartOutcome checks the durable instance for one exact logical START.
// It performs only reads through tx and never calls Start, inserts, updates,
// or treats not-found as a definitive rollback.
func ResolveStartOutcome(ctx context.Context, tx dbport.Tx, req StartRequest, selection WorkflowSelection) (ret0 StartResolution, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.resolve_start_outcome", req, selection)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if tx == nil || req.Versions == nil || req.TenantID == uuid.Nil || req.StartIdempotencyKey == "" ||
		selection.WorkflowID == "" || selection.Plan == nil {
		return StartResolution{}, fmt.Errorf("%w: incomplete exact start identity", ErrStartOutcomeUnresolved)
	}
	if selection.Pin.CompiledPlanDigest == "" && selection.Pin.SemanticVersion == "" {
		return StartResolution{}, fmt.Errorf("%w: exact workflow pin is required", ErrStartOutcomeUnresolved)
	}
	pinned, err := version.Resolve(req.Versions, selection.WorkflowID, selection.Pin)
	if err != nil || pinned.CompiledPlanDigest != selection.Plan.Digest() ||
		(selection.Pin.CompiledPlanDigest != "" && pinned.CompiledPlanDigest != selection.Pin.CompiledPlanDigest) ||
		(selection.Pin.SemanticVersion != "" && pinned.SemanticVersion != selection.Pin.SemanticVersion) {
		return StartResolution{}, fmt.Errorf("%w: supplied workflow pin does not match selected plan", ErrStartOutcomeUnresolved)
	}
	instanceID := derivedStartInstanceID(req.TenantID, selection.WorkflowID, req.StartIdempotencyKey)
	existing, err := (Store{}).LoadInstance(ctx, tx, req.TenantID, instanceID)
	if err != nil {
		if CodeOf(err) == CodeInstanceNotFound {
			return StartResolution{}, fmt.Errorf("%w: durable start instance is absent", ErrStartOutcomeUnresolved)
		}
		return StartResolution{}, err
	}
	fingerprint := computeStartFingerprint(selection.WorkflowID, selection.Plan, req)
	if existing.CellID != req.CellID || existing.WorkflowID != selection.WorkflowID ||
		existing.CompiledPlanHash != selection.Plan.Digest() || existing.InputRef != fingerprint ||
		!equalUUIDPointer(existing.BusinessTransactionID, req.BusinessTransactionID) {
		return StartResolution{}, refuse(CodeStartConflict, instanceID.String(), "",
			"durable start does not match the exact workflow pin and proposal binding")
	}
	cv, err := version.Resolve(req.Versions, existing.WorkflowID, version.Pin{CompiledPlanDigest: existing.CompiledPlanHash})
	if err != nil {
		return StartResolution{}, wrap(CodeVersionResolutionFailed, instanceID.String(), "", err,
			"resolve compiled version for start outcome")
	}
	return StartResolution{Instance: existing, SemanticVersion: cv.SemanticVersion}, nil
}

func equalUUIDPointer(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
