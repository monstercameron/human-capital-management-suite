package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
)

// tenantLister lists the tenants a sweep should check for due outbox work.
// pgxTenantLister (main.go) is the production implementation; tests supply
// their own, avoiding any dependency on a real database.
type tenantLister interface {
	ActiveTenants(ctx context.Context) ([]uuid.UUID, error)
}

// dispatcher claims and completes outbox work for one tenant.
// *outbox.Consumer satisfies this directly.
type dispatcher interface {
	Poll(ctx context.Context, tenant uuid.UUID) ([]outbox.Record, error)
	Ack(ctx context.Context, tenant uuid.UUID, outboxID uuid.UUID) error
	Fail(ctx context.Context, tenant uuid.UUID, outboxID uuid.UUID, cause error) error
}

// compensationDispatcher supplies the durable ordering fence needed by a
// compensation-aware queue consumer. The outbox.Consumer implements this
// with its tenant-scoped row status and existing lease-fenced Defer method.
type compensationDispatcher interface {
	EffectDelivered(context.Context, uuid.UUID, string) (bool, error)
	Defer(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time, string) error
}

type messageHandler func(context.Context, outbox.Record) error

type workerMessageKind string

const workerMessageKindOutbox workerMessageKind = "outbox_message"

// durableSpanProvider is the process provider supplied by the composition
// root. Keeping this as the narrow provider seam lets tests use the real
// provider while the worker remains independent of any exporter.
type durableSpanProvider interface {
	StartDurableAsyncSpan(context.Context, string, string, hcmotel.DurableAsyncContinuation, map[string]string, time.Time) (context.Context, hcmotel.ExecutionSpan, error)
}

// runOutboxLoopWithTelemetry is the worker loop with optional finite
// continuation spans. A nil provider preserves the legacy no-telemetry path.
func runOutboxLoopWithTelemetry(ctx context.Context, logger bootstrap.Logger, tenants tenantLister, disp dispatcher, pollInterval time.Duration, handler messageHandler, provider durableSpanProvider, now func() time.Time) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		didWork, err := sweepWithTelemetry(ctx, logger, tenants, disp, handler, provider, now)
		if err != nil {
			logger.Error("worker.sweep_failed", "error", err.Error())
		}
		if didWork {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(pollInterval):
		}
	}
}

// sweep dispatches one batch of due messages for every active tenant, and
// reports whether any tenant had work.
func sweep(ctx context.Context, logger bootstrap.Logger, tenants tenantLister, disp dispatcher) (bool, error) {
	return sweepWithHandler(ctx, logger, tenants, disp, legacyMessageHandler(logger))
}

func sweepWithHandler(ctx context.Context, logger bootstrap.Logger, tenants tenantLister, disp dispatcher, handler messageHandler) (bool, error) {
	return sweepWithTelemetry(ctx, logger, tenants, disp, handler, nil, nil)
}

// sweepWithTelemetry dispatches each claimed record through one finite
// continuation span. The span is ended immediately around the actual handler
// call, before the lease acknowledgement/failure operation.
func sweepWithTelemetry(ctx context.Context, logger bootstrap.Logger, tenants tenantLister, disp dispatcher, handler messageHandler, provider durableSpanProvider, now func() time.Time) (bool, error) {
	if handler == nil {
		handler = legacyMessageHandler(logger)
	}
	ids, err := tenants.ActiveTenants(ctx)
	if err != nil {
		return false, fmt.Errorf("list tenants: %w", err)
	}

	didWork := false
	for _, tenant := range ids {
		batch, err := disp.Poll(ctx, tenant)
		if err != nil {
			logger.Error("worker.poll_failed", "tenant", tenant.String(), "error", err.Error())
			continue
		}
		for _, msg := range batch {
			didWork = true
			if original, compensation := outbox.IdentityCompensationLink(msg); compensation {
				aware, ok := disp.(compensationDispatcher)
				if !ok {
					cause := errors.New("worker: compensation requires a durable ordering fence")
					logger.Error("worker.compensation_fence_unavailable", "outbox_id", msg.OutboxID.String())
					if failErr := failClaim(ctx, disp, msg, cause); failErr != nil {
						logger.Error("worker.fail_failed", "outbox_id", msg.OutboxID.String(), "error", failErr.Error())
					}
					continue
				}
				applied, lookupErr := aware.EffectDelivered(ctx, msg.Tenant, original)
				if lookupErr != nil {
					logger.Error("worker.compensation_original_lookup_failed", "outbox_id", msg.OutboxID.String(), "error", lookupErr.Error())
					if failErr := failClaim(ctx, disp, msg, lookupErr); failErr != nil {
						logger.Error("worker.fail_failed", "outbox_id", msg.OutboxID.String(), "error", failErr.Error())
					}
					continue
				}
				if !applied {
					clock := now
					if clock == nil {
						clock = time.Now
					}
					deferErr := aware.Defer(ctx, msg.Tenant, msg.OutboxID, msg.LeaseToken, clock().Add(outbox.CompensationHoldDelay), "original effect has not been delivered")
					if deferErr != nil {
						logger.Error("worker.compensation_defer_failed", "outbox_id", msg.OutboxID.String(), "error", deferErr.Error())
					}
					continue
				}
			}
			handlerCtx, span, traced := startDispatchSpan(ctx, provider, msg, now)
			err := handler(handlerCtx, msg)
			if traced {
				if err != nil {
					span.End("failure", true)
				} else {
					span.End("success", false)
				}
			}
			if err != nil {
				logger.Error("worker.dispatch_failed", "outbox_id", msg.OutboxID.String(), "error", err.Error())
				if failErr := failClaim(ctx, disp, msg, err); failErr != nil {
					logger.Error("worker.fail_failed", "outbox_id", msg.OutboxID.String(), "error", failErr.Error())
				}
				continue
			}
			if err := ackClaim(ctx, disp, msg); err != nil {
				logger.Error("worker.ack_failed", "outbox_id", msg.OutboxID.String(), "error", err.Error())
			}
		}
	}
	return didWork, nil
}

func startDispatchSpan(ctx context.Context, provider durableSpanProvider, msg outbox.Record, now func() time.Time) (context.Context, hcmotel.ExecutionSpan, bool) {
	if provider == nil || msg.Causal == nil {
		return ctx, hcmotel.ExecutionSpan{}, false
	}
	c := hcmotel.DurableAsyncContinuation{
		CorrelationID: msg.Causal.CorrelationID, CausationID: msg.Causal.CausationID,
		LogicalOperationID: msg.Causal.LogicalOperationID, AttemptID: msg.Causal.AttemptID,
	}
	if link := msg.Causal.TraceLink; link != nil {
		c.TraceLink = &hcmotel.TraceLinkMetadata{TraceID: link.TraceID, SpanID: link.SpanID, TraceFlags: link.TraceFlags, TraceState: link.TraceState, ExpiresAt: link.ExpiresAt}
	}
	clock := time.Now
	if now != nil {
		clock = now
	}
	spanCtx, span, err := provider.StartDurableAsyncSpan(ctx, "worker", "hcmnext.queue.deliver", c, map[string]string{
		"message_kind": string(workerMessageKindOutbox),
	}, clock())
	if err != nil {
		// Telemetry metadata is optional operational context. Invalid or stale
		// metadata must never alter the business dispatch path.
		return ctx, hcmotel.ExecutionSpan{}, false
	}
	return spanCtx, span, true
}

func legacyMessageHandler(logger bootstrap.Logger) messageHandler {
	return func(_ context.Context, msg outbox.Record) error {
		return dispatch(logger, msg)
	}
}

type fencedDispatcher interface {
	AckClaim(context.Context, outbox.Record) error
	FailClaim(context.Context, outbox.Record, error) error
}

func ackClaim(ctx context.Context, disp dispatcher, msg outbox.Record) error {
	if fenced, ok := disp.(fencedDispatcher); ok {
		return fenced.AckClaim(ctx, msg)
	}
	return disp.Ack(ctx, msg.Tenant, msg.OutboxID)
}

func failClaim(ctx context.Context, disp dispatcher, msg outbox.Record, cause error) error {
	if fenced, ok := disp.(fencedDispatcher); ok {
		return fenced.FailClaim(ctx, msg, cause)
	}
	return disp.Fail(ctx, msg.Tenant, msg.OutboxID, cause)
}

// dispatch is the semantic delivery role's outbox boundary. The durable
// outbox lease and acknowledgement remain here; provider adapters are supplied
// behind internal/connectivity/delivery and never enter this command package.
func dispatch(logger bootstrap.Logger, msg outbox.Record) error {
	logger.Info("worker.messaging_intent_dispatched", "effect", msg.EffectIdentity, "schema", msg.SchemaRef, "bytes", len(msg.Payload))
	return nil
}
