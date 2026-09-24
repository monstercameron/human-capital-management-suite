package messaging

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/inboundmsg"
)

var ErrInboundReply = errors.New("messaging: inbound reply could not be governed")

// SenderClaim contains transport-supplied identity evidence. Callers must
// pass it through SenderVerifier before any inbound content is admitted.
type SenderClaim struct {
	Channel       string
	Endpoint      string
	ProviderProof string
}

type VerifiedSender struct {
	PrincipalRef        string
	EndpointDigest      string
	VerificationReceipt string
}

type SenderVerifier interface {
	VerifySender(context.Context, uuid.UUID, SenderClaim, time.Time) (VerifiedSender, error)
}

type InboundContent struct {
	Digest         string
	Reference      string
	Classification string
}

// ContentQuarantiner stores the exact received bytes in governed quarantine.
// Implementations must not expose bytes through logs or ordinary row fields.
type ContentQuarantiner interface {
	QuarantineInbound(context.Context, uuid.UUID, string, string, []byte) (InboundContent, error)
}

type InboundReplyStore interface {
	Ingest(context.Context, inboundmsg.Executor, inboundmsg.InboundMessage, string) (inboundmsg.InboundMessage, bool, error)
	LoadBinding(context.Context, inboundmsg.Executor, uuid.UUID, uuid.UUID) (inboundmsg.ReplyBinding, error)
	Bind(context.Context, inboundmsg.Executor, uuid.UUID, uuid.UUID, string, uint64, time.Time) (inboundmsg.ReplyBinding, error)
}

type ReplyReviewTask struct {
	ID                  string
	TenantID            uuid.UUID
	InboundMessageID    uuid.UUID
	RecipientMessageID  uuid.UUID
	SenderPrincipal     string
	VerificationReceipt string
	ContentDigest       string
	ContentReference    string
	Classification      string
	RequiresHumanReview bool
}

type ReplyReviewTaskCreator interface {
	CreateReplyReviewTask(context.Context, ReplyReviewTask) (string, error)
}

type InboundReplySignalKind string

const (
	ReplyRejected     InboundReplySignalKind = "REPLY_REJECTED"
	ReplyReviewNeeded InboundReplySignalKind = "REPLY_REVIEW_REQUIRED"
)

type InboundReplySignal struct {
	ID                 string
	TenantID           uuid.UUID
	InboundMessageID   uuid.UUID
	RecipientMessageID *uuid.UUID
	TaskID             string
	Kind               InboundReplySignalKind
	Evidence           string
}

type InboundReplySignaller interface {
	EmitInboundReply(context.Context, InboundReplySignal) error
}

type InboundReplyRequest struct {
	TenantID          uuid.UUID
	ClaimedThreadID   uuid.UUID
	Claim             SenderClaim
	ProviderMessageID string
	CorrelationToken  string
	RawContent        []byte
	ReceivedAt        time.Time
	Now               time.Time
}

type InboundReplyResult struct {
	Message inboundmsg.InboundMessage
	Binding inboundmsg.ReplyBinding
	TaskID  string
	Signal  InboundReplySignal
	Created bool
}

// InboundReplyProcessor admits a reply only after sender verification,
// content quarantine and an exact recipient/thread/correlation binding.
// Task and signal ports must be idempotent on the stable IDs in their inputs.
type InboundReplyProcessor struct {
	Verify  SenderVerifier
	Content ContentQuarantiner
	Tasks   ReplyReviewTaskCreator
	Signals InboundReplySignaller
}

func (p InboundReplyProcessor) Handle(ctx context.Context, store InboundReplyStore, ex inboundmsg.Executor, req InboundReplyRequest) (InboundReplyResult, error) {
	if p.Verify == nil || p.Content == nil || p.Tasks == nil || p.Signals == nil || store == nil ||
		req.TenantID == uuid.Nil || req.ClaimedThreadID == uuid.Nil || req.ProviderMessageID == "" ||
		ex == nil || req.CorrelationToken == "" || len(req.RawContent) == 0 || req.ReceivedAt.IsZero() || req.Now.IsZero() {
		return InboundReplyResult{}, fmt.Errorf("%w: required ingress fields or ports are missing", ErrInboundReply)
	}
	if req.Claim.Channel == "" || strings.TrimSpace(req.Claim.Endpoint) == "" || req.Claim.ProviderProof == "" {
		return InboundReplyResult{}, fmt.Errorf("%w: sender claim is incomplete", ErrInboundReply)
	}
	verified, err := p.Verify.VerifySender(ctx, req.TenantID, req.Claim, req.Now)
	if err != nil {
		return InboundReplyResult{}, fmt.Errorf("%w: sender verification: %v", ErrInboundReply, err)
	}
	if strings.TrimSpace(verified.PrincipalRef) == "" || strings.TrimSpace(verified.EndpointDigest) == "" || strings.TrimSpace(verified.VerificationReceipt) == "" {
		return InboundReplyResult{}, fmt.Errorf("%w: verifier returned incomplete identity evidence", ErrInboundReply)
	}
	content, err := p.Content.QuarantineInbound(ctx, req.TenantID, req.Claim.Channel, req.ProviderMessageID, req.RawContent)
	if err != nil {
		return InboundReplyResult{}, fmt.Errorf("%w: quarantine content: %v", ErrInboundReply, err)
	}
	if strings.TrimSpace(content.Digest) == "" || strings.TrimSpace(content.Reference) == "" || strings.TrimSpace(content.Classification) == "" {
		return InboundReplyResult{}, fmt.Errorf("%w: quarantine returned incomplete evidence", ErrInboundReply)
	}
	message, created, err := store.Ingest(ctx, ex, inboundmsg.InboundMessage{
		TenantID: req.TenantID, ThreadID: req.ClaimedThreadID, Channel: req.Claim.Channel,
		SenderEndpointDigest: verified.EndpointDigest, ReceivedAt: req.ReceivedAt,
		ProviderMessageID: req.ProviderMessageID, ContentDigest: content.Digest,
		ContentRef: content.Reference, Classification: content.Classification,
	}, req.CorrelationToken)
	if err != nil {
		return InboundReplyResult{}, fmt.Errorf("%w: persist inbound receipt: %v", ErrInboundReply, err)
	}
	var binding inboundmsg.ReplyBinding
	if created {
		binding, err = store.Bind(ctx, ex, req.TenantID, message.InboundMessageID, verified.PrincipalRef, 1, req.Now)
	} else {
		binding, err = store.LoadBinding(ctx, ex, req.TenantID, message.InboundMessageID)
		if err == nil && binding.State == inboundmsg.Unresolved {
			binding, err = store.Bind(ctx, ex, req.TenantID, message.InboundMessageID, verified.PrincipalRef, binding.Version, req.Now)
		}
	}
	if err != nil {
		return InboundReplyResult{}, fmt.Errorf("%w: resolve reply binding: %v", ErrInboundReply, err)
	}
	result := InboundReplyResult{Message: message, Binding: binding, Created: created}
	if binding.State != inboundmsg.Bound || binding.RecipientMessageID == nil {
		signal := InboundReplySignal{
			ID: "inbound-reply:" + message.InboundMessageID.String() + ":rejected", TenantID: req.TenantID,
			InboundMessageID: message.InboundMessageID, Kind: ReplyRejected,
			Evidence: binding.RejectionReason,
		}
		if strings.TrimSpace(signal.Evidence) == "" {
			return InboundReplyResult{}, fmt.Errorf("%w: rejected binding has no evidence", ErrInboundReply)
		}
		if err := p.Signals.EmitInboundReply(ctx, signal); err != nil {
			return InboundReplyResult{}, fmt.Errorf("%w: emit rejected signal: %v", ErrInboundReply, err)
		}
		result.Signal = signal
		return result, nil
	}
	task := ReplyReviewTask{
		ID: "inbound-reply-review:" + message.InboundMessageID.String(), TenantID: req.TenantID,
		InboundMessageID: message.InboundMessageID, RecipientMessageID: *binding.RecipientMessageID,
		SenderPrincipal: verified.PrincipalRef, VerificationReceipt: verified.VerificationReceipt, ContentDigest: content.Digest,
		ContentReference: content.Reference, Classification: content.Classification,
		RequiresHumanReview: true,
	}
	taskID, err := p.Tasks.CreateReplyReviewTask(ctx, task)
	if err != nil {
		return InboundReplyResult{}, fmt.Errorf("%w: create review task: %v", ErrInboundReply, err)
	}
	if strings.TrimSpace(taskID) == "" {
		return InboundReplyResult{}, fmt.Errorf("%w: task creator returned an empty task id", ErrInboundReply)
	}
	signal := InboundReplySignal{
		ID: "inbound-reply:" + message.InboundMessageID.String() + ":review", TenantID: req.TenantID,
		InboundMessageID: message.InboundMessageID, RecipientMessageID: binding.RecipientMessageID,
		TaskID: taskID, Kind: ReplyReviewNeeded, Evidence: content.Digest,
	}
	if err := p.Signals.EmitInboundReply(ctx, signal); err != nil {
		return InboundReplyResult{}, fmt.Errorf("%w: emit review signal: %v", ErrInboundReply, err)
	}
	result.TaskID, result.Signal = taskID, signal
	return result, nil
}
