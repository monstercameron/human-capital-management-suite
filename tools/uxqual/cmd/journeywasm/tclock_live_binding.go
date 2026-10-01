package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/taskmux"
)

// clockSelfReasonRefPrefix starts the reason reference of a refusal that is a
// decision about the worker. It mirrors transport/timeclock.SelfClockReasonRefPrefix;
// the wasm client cannot import that package, so a test on each side pins the
// same literal.
const clockSelfReasonRefPrefix = "time.clock.self."

// clockLiveBinding is the browser adapter for the authenticated worker-self
// clock. It speaks the workspace gRPC tunnel every other page uses, so the page
// needs no extra network permission. Identity, tenant and authorization are
// supplied by the session bearer and the server; the browser sends no worker
// reference.
type clockLiveBinding struct {
	mu       sync.Mutex
	cfg      journeyclient.Config
	client   timev1.WorkerClockServiceClient
	keys     map[string]string
	revision uint64
	busy     bool
	receipt  string
	lastErr  string
}

// newClockLiveBinding creates a binding whose calls ride the given tunnel
// connection.
func newClockLiveBinding(cfg journeyclient.Config, conn grpc.ClientConnInterface) *clockLiveBinding {
	binding := &clockLiveBinding{cfg: cfg, keys: make(map[string]string)}
	if conn != nil {
		binding.client = timev1.NewWorkerClockServiceClient(conn)
	}
	return binding
}

var _ productclient.ClockProjectionReader = (*clockLiveBinding)(nil)

// clockActionWire is the workflow receipt of one accepted clock action.
type clockActionWire struct {
	ReceiptID           string
	WorkflowInstanceRef string
	PublishedPlanRef    string
	WorkflowID          string
	WorkflowTraceID     string
	WorkflowNodeID      string
	WorkflowAttempt     int
	WorkflowVersion     int64
	Status              clockSelfStatusWire
}

type clockSelfStatusWire struct {
	WorkerLabel    string
	ScheduleLabel  string
	StatusLabel    string
	LastEventLabel string
	StatusCode     string
	Revision       uint64
}

func (b *clockLiveBinding) rpcContext(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, journeyclient.AuthorizationHeader, journeyclient.BearerScheme+b.cfg.Bearer)
}

// ReadClockProjection reads the current authenticated worker projection and
// maps it to the product renderer's fail-closed presentation contract.
func (b *clockLiveBinding) ReadClockProjection(ctx context.Context) (productui.ClockProjection, error) {
	if b == nil || b.client == nil {
		return productui.ClockProjection{}, errors.New("clock self service is unavailable")
	}
	response, err := b.client.GetSelfClock(b.rpcContext(ctx), &timev1.GetSelfClockRequest{})
	if err != nil {
		// A decision about this worker is a page state that names its reason,
		// not a failed read: the projection carries no data and no action.
		if decided, ok := clockUnavailableProjection(err); ok {
			return decided, nil
		}
		return productui.ClockProjection{}, err
	}
	wire := clockSelfStatusWire{WorkerLabel: response.GetWorkerLabel(), ScheduleLabel: response.GetScheduleLabel(), StatusLabel: response.GetStatusLabel(), LastEventLabel: response.GetLastEventLabel(), StatusCode: response.GetStatusCode(), Revision: response.GetRevision()}
	if wire.Revision == 0 || strings.TrimSpace(wire.WorkerLabel) == "" || strings.TrimSpace(wire.ScheduleLabel) == "" || strings.TrimSpace(wire.StatusLabel) == "" || strings.TrimSpace(wire.LastEventLabel) == "" {
		return productui.ClockProjection{}, errors.New("clock self service returned an incomplete projection")
	}
	b.mu.Lock()
	b.revision = wire.Revision
	receipt := b.receipt
	b.mu.Unlock()
	return productui.ClockProjection{State: productui.ClockProjectionReady, WorkerLabel: wire.WorkerLabel, ScheduleLabel: wire.ScheduleLabel, StatusLabel: wire.StatusLabel, LastEventLabel: wire.LastEventLabel, ReceiptTraceLabel: receipt, Phase: clockPhaseFromCode(wire.StatusCode)}, nil
}

func (b *clockLiveBinding) setActionBusy(busy bool, diagnostic string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.busy, b.lastErr = busy, diagnostic
	b.mu.Unlock()
}

// ClockRevision returns the server revision most recently read. Callers must
// bind actions to this revision instead of inventing one in the browser.
func (b *clockLiveBinding) ClockRevision() uint64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.revision
}

// ClockActionState reports the current bounded action state for UI busy and
// diagnostic rendering. Error text is intentionally generic and localized by
// the caller; transport details never become user-facing content.
func (b *clockLiveBinding) ClockActionState() (busy bool, receipt, diagnostic string) {
	if b == nil {
		return false, "", ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.busy, b.receipt, b.lastErr
}

// SubmitClockAction queues one non-blocking action. KeepExisting prevents a
// double click from creating a second request for the same revision.
func (b *clockLiveBinding) SubmitClockAction(parent context.Context, tasks *taskmux.Scheduler, action string, done func(clockActionWire, error)) (*taskmux.Handle, error) {
	if b == nil || tasks == nil || done == nil {
		return nil, errors.New("clock self action is unavailable")
	}
	revision := b.ClockRevision()
	b.mu.Lock()
	b.busy = true
	b.lastErr = ""
	b.mu.Unlock()
	handle, err := tasks.Submit(parent, taskmux.Spec{Key: fmt.Sprintf("clock-self:%s:%d", action, revision), Priority: taskmux.Interactive, Duplicate: taskmux.KeepExisting}, func(ctx context.Context) error {
		result, err := b.ExecuteClockAction(ctx, action, revision)
		b.mu.Lock()
		b.busy = false
		if err != nil {
			b.lastErr = "clock action could not be completed"
		} else {
			b.receipt = result.WorkflowTraceID
			b.revision = result.Status.Revision
		}
		b.mu.Unlock()
		done(result, err)
		return err
	})
	if err != nil {
		b.mu.Lock()
		b.busy = false
		b.lastErr = "clock action could not be queued"
		b.mu.Unlock()
	}
	return handle, err
}

// ExecuteClockAction sends one revision-bound, idempotent action and checks
// that the response contains a genuine workflow receipt before it is exposed.
func (b *clockLiveBinding) ExecuteClockAction(ctx context.Context, action string, revision uint64) (clockActionWire, error) {
	if b == nil || b.client == nil || !validClockSelfAction(action) || revision == 0 {
		return clockActionWire{}, errors.New("clock self action is unavailable")
	}
	key, err := b.idempotencyKey(action, revision)
	if err != nil {
		return clockActionWire{}, err
	}
	request := &timev1.ExecuteSelfClockActionRequest{Action: timev1.ExecuteSelfClockActionRequest_ACTION_IN, ExpectedRevision: revision, IdempotencyKey: key}
	if action == "out" {
		request.Action = timev1.ExecuteSelfClockActionRequest_ACTION_OUT
	} else if action == "start_break" {
		request.Action = timev1.ExecuteSelfClockActionRequest_ACTION_START_BREAK
	} else if action == "end_break" {
		request.Action = timev1.ExecuteSelfClockActionRequest_ACTION_END_BREAK
	}
	response, err := b.client.ExecuteSelfClockAction(b.rpcContext(ctx), request)
	if err != nil {
		return clockActionWire{}, err
	}
	projection := response.GetStatus()
	wire := clockActionWire{
		ReceiptID: response.GetReceiptId(), WorkflowInstanceRef: response.GetWorkflowInstanceRef(), PublishedPlanRef: response.GetPublishedPlanRef(),
		WorkflowID: response.GetWorkflowId(), WorkflowTraceID: response.GetWorkflowTraceId(), WorkflowNodeID: response.GetWorkflowNodeId(),
		WorkflowAttempt: int(response.GetWorkflowAttempt()), WorkflowVersion: response.GetWorkflowInstanceVersion(),
		Status: clockSelfStatusWire{WorkerLabel: projection.GetWorkerLabel(), ScheduleLabel: projection.GetScheduleLabel(), StatusLabel: projection.GetStatusLabel(), LastEventLabel: projection.GetLastEventLabel(), StatusCode: projection.GetStatusCode(), Revision: projection.GetRevision()},
	}
	_, receiptErr := uuid.Parse(wire.ReceiptID)
	_, instanceErr := uuid.Parse(wire.WorkflowInstanceRef)
	if receiptErr != nil || instanceErr != nil || wire.WorkflowID != "hcmnext.workflows.time.clock_in_out" || strings.TrimSpace(wire.PublishedPlanRef) == "" || strings.TrimSpace(wire.WorkflowTraceID) == "" || wire.WorkflowAttempt < 1 || wire.WorkflowVersion < 1 || wire.Status.Revision == 0 {
		return clockActionWire{}, errors.New("clock self service returned an incomplete workflow receipt")
	}
	wantNode := "commit_punch"
	if action == "out" {
		wantNode = "commit_clock_out"
	}
	if wire.WorkflowNodeID != wantNode {
		return clockActionWire{}, errors.New("clock self service returned an unexpected workflow node")
	}
	return wire, nil
}

func validClockSelfAction(action string) bool {
	switch action {
	case "in", "out", "start_break", "end_break":
		return true
	default:
		return false
	}
}

func (b *clockLiveBinding) idempotencyKey(action string, revision uint64) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := fmt.Sprintf("%s:%d", action, revision)
	if existing := b.keys[key]; existing != "" {
		return existing, nil
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("clock self action idempotency key: %w", err)
	}
	value := hex.EncodeToString(raw[:])
	b.keys[key] = value
	return value, nil
}

// clockRefusalReason reads the closed reason out of a FailedPrecondition
// refusal's canonical error detail, and reports false for every other failure.
// The reference is only ever matched against the closed vocabulary; it is
// never rendered.
func clockRefusalReason(err error) (string, bool) {
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.FailedPrecondition {
		return "", false
	}
	for _, raw := range st.Details() {
		detail, ok := raw.(*commonv1.ErrorDetail)
		if !ok {
			continue
		}
		lower, found := strings.CutPrefix(detail.GetReasonRef(), clockSelfReasonRefPrefix)
		if !found {
			continue
		}
		reason := strings.ToUpper(lower)
		return reason, productui.NormalizeClockReason(reason) != productui.ClockReasonNone
	}
	return "", false
}
