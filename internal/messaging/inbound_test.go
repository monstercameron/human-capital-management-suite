package messaging

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inboundmsg"
)

type inboundFakeRow struct{}

func (inboundFakeRow) Scan(...any) error { return errors.New("unexpected database access") }

type inboundFakeExecutor struct{}

func (inboundFakeExecutor) Exec(context.Context, string, ...any) (int64, error) {
	return 0, errors.New("unexpected database access")
}
func (inboundFakeExecutor) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected database access")
}
func (inboundFakeExecutor) QueryRow(context.Context, string, ...any) dbport.Row {
	return inboundFakeRow{}
}

type inboundFakeStore struct {
	message   inboundmsg.InboundMessage
	binding   inboundmsg.ReplyBinding
	ingested  int
	bound     int
	ingestErr error
}

func (s *inboundFakeStore) Ingest(_ context.Context, _ inboundmsg.Executor, in inboundmsg.InboundMessage, _ string) (inboundmsg.InboundMessage, bool, error) {
	s.ingested++
	if s.ingestErr != nil {
		return inboundmsg.InboundMessage{}, false, s.ingestErr
	}
	in.InboundMessageID = uuid.New()
	s.message = in
	return in, true, nil
}
func (s *inboundFakeStore) Bind(_ context.Context, _ inboundmsg.Executor, _ uuid.UUID, inboundID uuid.UUID, _ string, _ uint64, at time.Time) (inboundmsg.ReplyBinding, error) {
	s.bound++
	s.binding.InboundMessageID = inboundID
	s.binding.ResolvedAt = &at
	return s.binding, nil
}
func (s *inboundFakeStore) LoadBinding(context.Context, inboundmsg.Executor, uuid.UUID, uuid.UUID) (inboundmsg.ReplyBinding, error) {
	return s.binding, nil
}

type inboundFakeVerifier struct {
	identity VerifiedSender
	err      error
	calls    int
}

func (v *inboundFakeVerifier) VerifySender(context.Context, uuid.UUID, SenderClaim, time.Time) (VerifiedSender, error) {
	v.calls++
	return v.identity, v.err
}

type inboundFakeContent struct {
	content InboundContent
	err     error
	calls   int
}

func (c *inboundFakeContent) QuarantineInbound(context.Context, uuid.UUID, string, string, []byte) (InboundContent, error) {
	c.calls++
	return c.content, c.err
}

type inboundFakeTasks struct {
	id    string
	err   error
	calls int
	task  ReplyReviewTask
}

func (t *inboundFakeTasks) CreateReplyReviewTask(_ context.Context, task ReplyReviewTask) (string, error) {
	t.calls++
	t.task = task
	return t.id, t.err
}

type inboundFakeSignals struct {
	signals []InboundReplySignal
	err     error
}

func (s *inboundFakeSignals) EmitInboundReply(_ context.Context, signal InboundReplySignal) error {
	s.signals = append(s.signals, signal)
	return s.err
}

func inboundProcessor(state string) (InboundReplyProcessor, *inboundFakeStore, *inboundFakeVerifier, *inboundFakeContent, *inboundFakeTasks, *inboundFakeSignals) {
	recipientID := uuid.New()
	store := &inboundFakeStore{binding: inboundmsg.ReplyBinding{
		State: state, RecipientMessageID: &recipientID, RejectionReason: "correlation/thread not authorized",
	}}
	verifier := &inboundFakeVerifier{identity: VerifiedSender{
		PrincipalRef: "principal:verified", EndpointDigest: "sha256:sender", VerificationReceipt: "receipt:verify",
	}}
	content := &inboundFakeContent{content: InboundContent{
		Digest: "sha256:body", Reference: "artifact:quarantine", Classification: "CONFIDENTIAL",
	}}
	tasks := &inboundFakeTasks{id: "task:review"}
	signals := &inboundFakeSignals{}
	return InboundReplyProcessor{Verify: verifier, Content: content, Tasks: tasks, Signals: signals}, store, verifier, content, tasks, signals
}

func inboundRequest() InboundReplyRequest {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	return InboundReplyRequest{
		TenantID: uuid.New(), ClaimedThreadID: uuid.New(),
		Claim:             SenderClaim{Channel: "EMAIL", Endpoint: "opaque-endpoint", ProviderProof: "provider-proof"},
		ProviderMessageID: "provider-message-1", CorrelationToken: "reply-token-1",
		RawContent: []byte("provider delivered bytes"), ReceivedAt: now, Now: now,
	}
}

func TestTodo_MSG_011_InboundReplyRequiresVerifiedSenderAndQuarantinedContent(t *testing.T) {
	processor, store, verifier, content, tasks, signals := inboundProcessor(inboundmsg.Bound)
	verifier.err = errors.New("sender is not verified")
	_, err := processor.Handle(context.Background(), store, inboundFakeExecutor{}, inboundRequest())
	if !errors.Is(err, ErrInboundReply) {
		t.Fatalf("Handle error = %v, want ErrInboundReply", err)
	}
	if verifier.calls != 1 || content.calls != 0 || store.ingested != 0 || store.bound != 0 || tasks.calls != 0 || len(signals.signals) != 0 {
		t.Fatalf("unverified ingress side effects: verifier=%d quarantine=%d ingest=%d bind=%d task=%d signals=%d", verifier.calls, content.calls, store.ingested, store.bound, tasks.calls, len(signals.signals))
	}

	processor, store, verifier, content, tasks, signals = inboundProcessor(inboundmsg.Bound)
	content.err = errors.New("quarantine store unavailable")
	_, err = processor.Handle(context.Background(), store, inboundFakeExecutor{}, inboundRequest())
	if !errors.Is(err, ErrInboundReply) {
		t.Fatalf("Handle quarantine error = %v, want ErrInboundReply", err)
	}
	if verifier.calls != 1 || content.calls != 1 || store.ingested != 0 || store.bound != 0 || tasks.calls != 0 || len(signals.signals) != 0 {
		t.Fatalf("unquarantined ingress side effects: verifier=%d quarantine=%d ingest=%d bind=%d task=%d signals=%d", verifier.calls, content.calls, store.ingested, store.bound, tasks.calls, len(signals.signals))
	}
}

func TestTodo_MSG_011_BoundReplyCreatesReviewedTaskThenSignal(t *testing.T) {
	processor, store, _, _, tasks, signals := inboundProcessor(inboundmsg.Bound)
	result, err := processor.Handle(context.Background(), store, inboundFakeExecutor{}, inboundRequest())
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if store.ingested != 1 || store.bound != 1 || tasks.calls != 1 || result.TaskID != "task:review" {
		t.Fatalf("Handle effects = ingest %d, bind %d, tasks %d, task id %q", store.ingested, store.bound, tasks.calls, result.TaskID)
	}
	if !tasks.task.RequiresHumanReview || tasks.task.ContentReference != "artifact:quarantine" || tasks.task.SenderPrincipal != "principal:verified" || tasks.task.VerificationReceipt != "receipt:verify" {
		t.Fatalf("review task did not preserve governed content and verified identity: %+v", tasks.task)
	}
	if len(signals.signals) != 1 || signals.signals[0].Kind != ReplyReviewNeeded || signals.signals[0].TaskID != "task:review" || signals.signals[0].RecipientMessageID == nil {
		t.Fatalf("reply signals = %+v, want one review-required signal correlated to the task and recipient", signals.signals)
	}
}

func TestTodo_MSG_011_RejectedReplySignalsWithoutTask(t *testing.T) {
	processor, store, _, _, tasks, signals := inboundProcessor(inboundmsg.Rejected)
	result, err := processor.Handle(context.Background(), store, inboundFakeExecutor{}, inboundRequest())
	if err != nil {
		t.Fatalf("Handle rejected reply: %v", err)
	}
	if result.Signal.Kind != ReplyRejected || tasks.calls != 0 || len(signals.signals) != 1 {
		t.Fatalf("rejected reply result=%+v tasks=%d signals=%+v", result, tasks.calls, signals.signals)
	}
	if signals.signals[0].RecipientMessageID != nil || signals.signals[0].Evidence == "" {
		t.Fatalf("rejected reply signal exposed recipient or lacks evidence: %+v", signals.signals[0])
	}
}

func TestTodo_MSG_011_TaskFailureDoesNotEmitReviewSignal(t *testing.T) {
	processor, store, _, _, tasks, signals := inboundProcessor(inboundmsg.Bound)
	tasks.err = errors.New("task service unavailable")
	_, err := processor.Handle(context.Background(), store, inboundFakeExecutor{}, inboundRequest())
	if !errors.Is(err, ErrInboundReply) {
		t.Fatalf("Handle task failure = %v, want ErrInboundReply", err)
	}
	if tasks.calls != 1 || len(signals.signals) != 0 {
		t.Fatalf("task failure effects: tasks=%d signals=%+v, want no workflow signal", tasks.calls, signals.signals)
	}
}
