package clockservice

import (
	"context"
	"errors"
	"testing"
	"time"
)

type workflowEventSourceFake struct {
	events []OutboxEvent
	err    error
}

func (f workflowEventSourceFake) ListEvents(context.Context, string, int64, int) ([]OutboxEvent, error) {
	return f.events, f.err
}

type workflowFake struct {
	starts, signals int
	err             error
	got             []WorkflowDispatchEvent
}

func (f *workflowFake) StartRun(_ context.Context, tenant, session, worker, assignment, observation string, at time.Time) error {
	f.starts++
	f.got = append(f.got, WorkflowDispatchEvent{TenantID: tenant, SessionID: session, WorkerRef: worker, AssignmentRef: assignment, ObservationID: observation, Signal: "OPENED", OccurredAt: at})
	return f.err
}
func (f *workflowFake) SignalRun(_ context.Context, tenant, session, signal, observation string, at time.Time) error {
	f.signals++
	f.got = append(f.got, WorkflowDispatchEvent{TenantID: tenant, SessionID: session, ObservationID: observation, Signal: signal, OccurredAt: at})
	return f.err
}

func TestDecodeWorkflowDispatchEvent(t *testing.T) {
	event := OutboxEvent{Sequence: 2, SchemaVersion: 1, EventType: "clock.punch.accepted", Payload: []byte(`{"tenant_id":"tenant-a","session_id":"s1","worker_ref":"w1","assignment_ref":"a1","observation_id":"o1","signal":"CLOSED","occurred_at":"2026-09-28T12:00:00Z"}`)}
	got, ok, err := DecodeWorkflowDispatchEvent(event)
	if err != nil || !ok {
		t.Fatalf("decode: got=%+v ok=%v err=%v", got, ok, err)
	}
	if got.Signal != "OUT_PUNCH" || got.ObservationID != "o1" {
		t.Fatalf("decoded=%+v", got)
	}
	missing := event
	missing.Payload = []byte(`{"session_id":"s1"}`)
	if _, ok, err := DecodeWorkflowDispatchEvent(missing); err == nil || ok {
		t.Fatalf("missing identity: ok=%v err=%v", ok, err)
	}
}

func TestDecodeWorkflowDispatchEvent_IgnoresRawSessionEvent(t *testing.T) {
	event := OutboxEvent{Sequence: 3, SchemaVersion: 1, EventType: "clock.session.opened", Payload: []byte(`{"session_id":"s1"}`)}
	if _, ok, err := DecodeWorkflowDispatchEvent(event); err != nil || ok {
		t.Fatalf("raw session event: ok=%v err=%v", ok, err)
	}
}

func TestWorkflowOutboxDispatcherDispatch(t *testing.T) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	source := workflowEventSourceFake{events: []OutboxEvent{{Sequence: 4, SchemaVersion: 1, EventType: "clock.punch.accepted", CreatedAt: at, Payload: []byte(`{"session_id":"s1","worker_ref":"w1","assignment_ref":"a1","observation_id":"o1","signal":"OPENED","occurred_at":"2026-09-28T12:00:00Z"}`)}}}
	wf := &workflowFake{}
	result, err := (WorkflowOutboxDispatcher{Source: source, Workflow: wf}).Dispatch(context.Background(), "tenant-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Cursor != 4 || result.Delivered != 1 || wf.starts != 1 || wf.signals != 0 {
		t.Fatalf("result=%+v starts=%d signals=%d", result, wf.starts, wf.signals)
	}
}

func TestWorkflowOutboxDispatcherStopsBeforeCursorOnFailure(t *testing.T) {
	wf := &workflowFake{err: errors.New("down")}
	_, err := (WorkflowOutboxDispatcher{Source: workflowEventSourceFake{events: []OutboxEvent{{Sequence: 1, SchemaVersion: 1, EventType: "clock.punch.accepted", Payload: []byte(`{"session_id":"s","worker_ref":"w","assignment_ref":"a","observation_id":"o","signal":"CLOSED","occurred_at":"2026-09-28T12:00:00Z"}`)}}}, Workflow: wf}).Dispatch(context.Background(), "t", 0)
	if err == nil || !errors.Is(err, wf.err) {
		t.Fatalf("err=%v", err)
	}
}
