package clockservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// WorkflowDispatchEvent is the authoritative observation needed to hand one
// committed clock event to the session workflow. It is decoded from the
// clock outbox; callers must not construct it from a request.
type WorkflowDispatchEvent struct {
	TenantID      string
	SessionID     string
	WorkerRef     string
	AssignmentRef string
	ObservationID string
	Signal        string
	OccurredAt    time.Time
}

// WorkflowEventSource reads the durable clock outbox in sequence order.
type WorkflowEventSource interface {
	ListEvents(context.Context, string, int64, int) ([]OutboxEvent, error)
}

// WorkflowDispatchResult reports the last inspected outbox sequence and the
// number of workflow calls made. A caller persists Cursor with its own
// durable delivery receipt; the bridge itself never invents a cursor store.
type WorkflowDispatchResult struct {
	Cursor    int64
	Delivered int
}

// WorkflowOutboxDispatcher delivers committed clock observations to the
// existing SessionWorkflow port. Replaying a page is safe when the workflow
// adapter derives its run and signal idempotency from the stable session and
// observation identities.
type WorkflowOutboxDispatcher struct {
	Source   WorkflowEventSource
	Workflow SessionWorkflow
	Limit    int
}

// Dispatch reads one bounded page after cursor and delivers accepted session
// events. It advances the returned cursor only after every delivery succeeds.
func (d WorkflowOutboxDispatcher) Dispatch(ctx context.Context, tenant string, cursor int64) (WorkflowDispatchResult, error) {
	if d.Source == nil || d.Workflow == nil || strings.TrimSpace(tenant) == "" || cursor < 0 {
		return WorkflowDispatchResult{}, ErrUnavailable
	}
	limit := d.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		return WorkflowDispatchResult{}, fmt.Errorf("clock workflow dispatch: limit %d exceeds 1000", limit)
	}
	events, err := d.Source.ListEvents(ctx, tenant, cursor, limit)
	if err != nil {
		return WorkflowDispatchResult{}, err
	}
	result := WorkflowDispatchResult{Cursor: cursor}
	for _, event := range events {
		if event.Sequence <= result.Cursor {
			return WorkflowDispatchResult{}, fmt.Errorf("clock workflow dispatch: outbox sequence regressed from %d to %d", result.Cursor, event.Sequence)
		}
		result.Cursor = event.Sequence
		decoded, ok, err := DecodeWorkflowDispatchEvent(event)
		if err != nil {
			return WorkflowDispatchResult{}, err
		}
		if !ok {
			continue
		}
		if decoded.TenantID == "" {
			decoded.TenantID = tenant
		}
		if decoded.TenantID != tenant {
			return WorkflowDispatchResult{}, fmt.Errorf("clock workflow dispatch sequence %d: tenant mismatch", event.Sequence)
		}
		if decoded.OccurredAt.IsZero() {
			return WorkflowDispatchResult{}, fmt.Errorf("clock workflow dispatch sequence %d: occurred_at is required", event.Sequence)
		}
		if decoded.Signal == "OPENED" {
			err = d.Workflow.StartRun(ctx, decoded.TenantID, decoded.SessionID, decoded.WorkerRef, decoded.AssignmentRef, decoded.ObservationID, decoded.OccurredAt)
		} else {
			if correlated, ok := d.Workflow.(interface {
				SignalRunForAssignment(context.Context, string, string, string, string, string, string, time.Time) error
			}); ok {
				err = correlated.SignalRunForAssignment(ctx, decoded.TenantID, decoded.SessionID, decoded.WorkerRef, decoded.AssignmentRef, decoded.Signal, decoded.ObservationID, decoded.OccurredAt)
			} else {
				err = d.Workflow.SignalRun(ctx, decoded.TenantID, decoded.SessionID, decoded.Signal, decoded.ObservationID, decoded.OccurredAt)
			}
		}
		if err != nil {
			return WorkflowDispatchResult{}, fmt.Errorf("clock workflow dispatch sequence %d: %w", event.Sequence, err)
		}
		result.Delivered++
	}
	return result, nil
}

type workflowDispatchPayload struct {
	TenantID      string    `json:"tenant_id"`
	SessionID     string    `json:"session_id"`
	WorkerRef     string    `json:"worker_ref"`
	AssignmentRef string    `json:"assignment_ref"`
	ObservationID string    `json:"observation_id"`
	ReceiptID     string    `json:"receipt_id"`
	Signal        string    `json:"signal"`
	EventType     string    `json:"event_type"`
	OccurredAt    time.Time `json:"occurred_at"`
	WorkerID      string    `json:"worker_id"`
}

type workflowSessionPayload struct {
	Tenant      string `json:"Tenant"`
	Worker      string `json:"Worker"`
	Assignment  string `json:"Assignment"`
	SessionID   string `json:"SessionID"`
	LastOutcome *struct {
		Kind string `json:"Kind"`
	} `json:"LastOutcome"`
}

// DecodeWorkflowDispatchEvent decodes the canonical accepted-observation
// outbox shape. Other clock rows remain audit data and are safely skipped;
// they must never be treated as workflow dispatch input.
func DecodeWorkflowDispatchEvent(event OutboxEvent) (WorkflowDispatchEvent, bool, error) {
	if event.Sequence <= 0 {
		return WorkflowDispatchEvent{}, false, errors.New("clock workflow dispatch: invalid outbox event")
	}
	eventType := strings.ToLower(strings.TrimSpace(event.EventType))
	// Only the observation acceptance event carries the authoritative worker,
	// assignment, observation, and occurred-time facts needed to start or
	// signal a run. Raw session events are retained for audit but are not a
	// workflow dispatch stream; advancing past them is safe because they have
	// no dispatch identity.
	workflowEvent := eventType == "clock.punch.accepted"
	if !workflowEvent {
		return WorkflowDispatchEvent{}, false, nil
	}
	if event.SchemaVersion != 1 {
		return WorkflowDispatchEvent{}, false, errors.New("clock workflow dispatch: invalid accepted-event schema")
	}
	var payload workflowDispatchPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return WorkflowDispatchEvent{}, false, fmt.Errorf("clock workflow dispatch: decode sequence %d: %w", event.Sequence, err)
	}
	payload.EventType = strings.ToLower(strings.TrimSpace(payload.EventType))
	if payload.ObservationID == "" {
		payload.ObservationID = payload.ReceiptID
	}
	if payload.WorkerRef == "" {
		payload.WorkerRef = payload.WorkerID
	}
	signal := strings.TrimSpace(payload.Signal)
	if signal == "" {
		signal = strings.ToUpper(strings.TrimSpace(payload.EventType))
	}
	if signal == "" {
		var session workflowSessionPayload
		if err := json.Unmarshal(event.Payload, &session); err == nil && session.SessionID != "" && session.LastOutcome != nil {
			payload.TenantID, payload.SessionID = session.Tenant, session.SessionID
			payload.WorkerRef, payload.AssignmentRef = session.Worker, session.Assignment
			signal = session.LastOutcome.Kind
		}
	}
	if payload.SessionID == "" || payload.ObservationID == "" || signal == "" || payload.WorkerRef == "" || payload.AssignmentRef == "" {
		return WorkflowDispatchEvent{}, false, errors.New("clock workflow dispatch: workflow event is missing authoritative identity")
	}
	if signal == "IN" || signal == "OPENED" || eventType == "clock.session.opened" {
		signal = "OPENED"
	}
	switch signal {
	case "OPENED", "BREAK_START", "BREAK_END", "JOB_TRANSFER", "TRAVEL_TRANSFER", "OUT_PUNCH":
	case "OUT":
		signal = "OUT_PUNCH"
	case "ON_BREAK":
		signal = "BREAK_START"
	case "RESUMED":
		signal = "BREAK_END"
	case "TRANSFER_JOB":
		signal = "JOB_TRANSFER"
	case "TRANSFER_TRAVEL":
		signal = "TRAVEL_TRANSFER"
	case "CLOSED", "AUTO_CLOSED":
		signal = "OUT_PUNCH"
	default:
		return WorkflowDispatchEvent{}, false, fmt.Errorf("clock workflow dispatch: unsupported session signal %q", signal)
	}
	if payload.OccurredAt.IsZero() {
		return WorkflowDispatchEvent{}, false, errors.New("clock workflow dispatch: occurred_at is required")
	}
	return WorkflowDispatchEvent{TenantID: payload.TenantID, SessionID: payload.SessionID, WorkerRef: payload.WorkerRef, AssignmentRef: payload.AssignmentRef, ObservationID: payload.ObservationID, Signal: signal, OccurredAt: payload.OccurredAt}, true, nil
}
