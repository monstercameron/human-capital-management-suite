package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Business-level journey events: one structured line per journey operation
// (proposed, workflow started, decision recorded, intervention, edit,
// acknowledgement, note), so an operator can follow a promotion through its
// life from the logs alone. The engine's own lines (workflow.*) say what the
// runtime did; these say what a person did and what came of it.
//
// Correlation: the line's top-level correlation_id is the INTENT's
// correlation id -- the id the workflow instance and every engine line of
// its run carry -- so the business events and the engine lines of one
// promotion join on one value. request_id stays the current call's.

// journeyEvent logs one business event for intentID. err is the operation's
// result: nil logs at INFO with outcome "ok"; a refusal the caller can act on
// logs at WARN with its class; anything else logs at ERROR. Nothing
// caller-supplied (reasons, note bodies, pay amounts) is ever logged.
func (e *journeyEngine) journeyEvent(ctx context.Context, name, intentID string, err error, attrs ...slog.Attr) {
	if e == nil || e.events == nil {
		return
	}
	level, outcome := slog.LevelInfo, "ok"
	if err != nil {
		outcome = journeyErrorClass(err)
		level = slog.LevelWarn
		if outcome == "failed" {
			level = slog.LevelError
		}
	}
	fields := make([]slog.Attr, 0, len(attrs)+4)
	fields = append(fields, slog.String("outcome", outcome))
	if owned, ok := envelope.As(err); ok {
		// Stable owned reason codes explain pre-runtime failures without
		// leaking error messages, proposal contents or decision rationale.
		fields = append(fields, slog.String("error_code", owned.Code().String()), slog.String("reason_ref", owned.ReasonRef()))
	} else if err != nil {
		fields = append(fields, slog.String("error_code", observe.ErrorCode(err)))
	}
	if intentID != "" {
		fields = append(fields, slog.String("intent_id", intentID))
	}
	if principal, ok := trust.FromContext(ctx); ok && principal != nil {
		fields = append(fields, slog.String("tenant", principal.Tenant().String()))
		if corr := e.intentCorrelation(ctx, principal, intentID); corr != "" {
			ctx = logging.WithCorrelationID(ctx, corr)
		}
	}
	e.events.LogAttrs(ctx, level, name, append(fields, attrs...)...)
}

// intentCorrelation reads the correlation id the intent was created with.
// Logging must never change a business result, so a failed read yields ""
// and the line keeps the request's own correlation id.
func (e *journeyEngine) intentCorrelation(ctx context.Context, principal *trust.Principal, intentID string) string {
	if intentID == "" || e.svc == nil {
		return ""
	}
	inst, _, ownedErr := e.svc.loadInstance(ctx, principal.Tenant().String(), intentID)
	if ownedErr != nil {
		return ""
	}
	return inst.CorrelationID
}

// journeyErrorClass names a journey refusal by its port sentinel.
//
// A missing workflow-version release carries no port sentinel -- it travels
// as its own owned refusal so the reader is not told to retry -- so it is
// named from that reason rather than falling into the anonymous "failed".
func journeyErrorClass(err error) string {
	if owned, ok := envelope.As(err); ok && owned.ReasonRef() == reasonNoActiveWorkflowVersion {
		return "no_active_workflow_version"
	}
	switch {
	case errors.Is(err, workspace.ErrJourneyInput):
		return "invalid_input"
	case errors.Is(err, workspace.ErrDenied):
		return "denied"
	case errors.Is(err, workspace.ErrJourneyStage):
		return "wrong_stage"
	case errors.Is(err, workspace.ErrJourneyUnknown):
		return "not_found"
	case errors.Is(err, workspace.ErrJourneyActiveConflict):
		return "active_conflict"
	case errors.Is(err, workspace.ErrJourneyUnavailable):
		return "unavailable"
	}
	return "failed"
}
