package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

var ErrPersonaReplyDeliveryUnavailable = errors.New("application: persona reply delivery unavailable")

// PersonaReplyDeliveryRequest binds a sealed output to its authenticated invoker.
type PersonaReplyDeliveryRequest struct {
	Principal      chat.Principal
	Output         agentsecurity.FinalOutputPersistence
	IdempotencyKey string
}

// PersonaReplyDeliveryReceipt reports the durable or recipient-only chat effect.
type PersonaReplyDeliveryReceipt struct {
	PublicPostID, EphemeralPostID, DurableCopyPostID, DurableCopyConversationID string
	Private, Public                                                             bool
}

// PersonaReplyDelivery sends policy-approved persona output into its source thread,
// routing anything that cannot be proven safe into the invoker's persona DM.
type PersonaReplyDelivery struct {
	chat         chat.ConversationService
	ephemeral    chat.EphemeralService
	commit       chat.PersonaReplyCommitter
	floor        chat.PersonaAudienceFloor
	outputPolicy PersonaReplyOutputPolicy
}

// NewPersonaReplyDelivery requires both the authenticated chat boundary and the
// existing audience-floor authority. It never manufactures a delivery proof.
func NewPersonaReplyDelivery(service chat.ConversationService, committer chat.PersonaReplyCommitter, floor chat.PersonaAudienceFloor) (*PersonaReplyDelivery, error) {
	return NewPersonaReplyDeliveryWithOutputPolicy(service, committer, floor, PersonaReplyOutputPolicy{})
}

// NewPersonaReplyDeliveryWithOutputPolicy composes trusted tenant/admin link
// origins used by both the shared and invoker-only persona output renderers.
func NewPersonaReplyDeliveryWithOutputPolicy(service chat.ConversationService, committer chat.PersonaReplyCommitter, floor chat.PersonaAudienceFloor, outputPolicy PersonaReplyOutputPolicy) (*PersonaReplyDelivery, error) {
	if isNilPersonaOutputPort(service) || isNilPersonaOutputPort(committer) || isNilPersonaOutputPort(floor) {
		return nil, ErrPersonaReplyDeliveryUnavailable
	}
	ephemeral, ok := service.(chat.EphemeralService)
	if !ok || isNilPersonaOutputPort(ephemeral) {
		return nil, ErrPersonaReplyDeliveryUnavailable
	}
	outputPolicy.AdminAllowedOrigins = append([]string(nil), outputPolicy.AdminAllowedOrigins...)
	return &PersonaReplyDelivery{chat: service, ephemeral: ephemeral, commit: committer, floor: floor, outputPolicy: outputPolicy}, nil
}

// Deliver reauthorizes before every public write and relies on CommitPersonaReply
// to fence the same audience revision atomically. A denial or audience race is
// delivered privately through chat's recipient-only ephemeral path.
func (d *PersonaReplyDelivery) Deliver(ctx context.Context, request PersonaReplyDeliveryRequest) (PersonaReplyDeliveryReceipt, error) {
	if d == nil || isNilPersonaOutputPort(d.chat) || isNilPersonaOutputPort(d.commit) || isNilPersonaOutputPort(d.floor) || ctx == nil {
		return PersonaReplyDeliveryReceipt{}, ErrPersonaReplyDeliveryUnavailable
	}
	identity := request.Output.Identity()
	if strings.TrimSpace(request.Principal.TenantID) == "" || strings.TrimSpace(request.Principal.SubjectID) == "" ||
		identity.TenantID != request.Principal.TenantID || identity.InvokerID != request.Principal.SubjectID ||
		identity.ConversationID == "" || identity.ThreadID == "" || identity.PersonaID == "" ||
		identity.OutputID == "" || request.Output.Digest() == "" || strings.TrimSpace(request.IdempotencyKey) == "" {
		return PersonaReplyDeliveryReceipt{}, chat.ErrInvalidArgument
	}
	if _, _, err := request.Output.Payload(); err != nil {
		return PersonaReplyDeliveryReceipt{}, chat.ErrInvalidArgument
	}
	conversation, err := d.chat.GetConversation(ctx, chat.GetConversationRequest{Principal: request.Principal, TenantID: identity.TenantID, ConversationID: identity.ConversationID})
	if err != nil {
		return PersonaReplyDeliveryReceipt{}, fmt.Errorf("resolve persona reply conversation: %w", err)
	}
	if conversation.TenantID != identity.TenantID || conversation.ID != identity.ConversationID || conversation.Archived {
		return PersonaReplyDeliveryReceipt{}, chat.ErrPermissionDenied
	}
	if conversation.Kind != chat.PublicChannel {
		return d.deliverPrivate(ctx, request, false)
	}
	decision, err := d.floor.AuthorizePersonaOutput(ctx, request.Output)
	if err != nil || decision.Revision == 0 || strings.TrimSpace(decision.Body) == "" || decision.ParentID != identity.ThreadID {
		return d.deliverPrivate(ctx, request, true)
	}
	decision.Body = renderPersonaReplyText(decision.Body, identity.TenantID, identity.ConversationID, d.outputPolicy)
	if decision.Body == "" {
		return d.deliverPrivate(ctx, request, true)
	}
	proof, err := chat.IssuePersonaDeliveryProof(ctx, request.Output, personaFixedAudienceDecision{decision: decision})
	if err != nil {
		return d.deliverPrivate(ctx, request, true)
	}
	commitRequest := chat.PersonaReplyCommitRequest{
		TenantID: identity.TenantID, ConversationID: identity.ConversationID,
		AuthorID: identity.PersonaID, AuthorHomeTenantID: identity.TenantID,
		ParentID: decision.ParentID, Body: decision.Body, IdempotencyKey: request.IdempotencyKey,
		ExpectedAudienceRevision: decision.Revision, OutputDigest: request.Output.Digest(), Proof: proof,
	}
	var post chat.Post
	if sealed, ok := d.commit.(interface {
		CommitSealedPersonaReply(context.Context, agentsecurity.FinalOutputPersistence, chat.PersonaReplyCommitRequest) (chat.Post, error)
	}); ok {
		post, err = sealed.CommitSealedPersonaReply(ctx, request.Output, commitRequest)
	} else {
		post, err = d.commit.CommitPersonaReply(ctx, commitRequest)
	}
	if errors.Is(err, chat.ErrAudienceChanged) {
		return d.deliverPrivate(ctx, request, true)
	}
	if err != nil {
		return PersonaReplyDeliveryReceipt{}, fmt.Errorf("commit persona reply: %w", err)
	}
	return PersonaReplyDeliveryReceipt{Public: true, PublicPostID: post.ID}, nil
}

type personaFixedAudienceDecision struct{ decision chat.PersonaAudienceDecision }

func (f personaFixedAudienceDecision) AuthorizePersonaOutput(context.Context, agentsecurity.FinalOutputPersistence) (chat.PersonaAudienceDecision, error) {
	return f.decision, nil
}

func (d *PersonaReplyDelivery) deliverPrivate(ctx context.Context, request PersonaReplyDeliveryRequest, neutralReceipt bool) (PersonaReplyDeliveryReceipt, error) {
	draft, answer, err := request.Output.Payload()
	if err != nil {
		return PersonaReplyDeliveryReceipt{}, chat.ErrInvalidArgument
	}
	parts := make([]string, 0, len(answer.Parts))
	for _, part := range answer.Parts {
		if strings.TrimSpace(part.Text) != "" {
			parts = append(parts, strings.TrimSpace(part.Text))
		}
	}
	body := strings.Join(parts, "\n\n")
	if body == "" {
		body = strings.TrimSpace(draft.Narrative)
	}
	identity := request.Output.Identity()
	body = renderPersonaReplyText(body, identity.TenantID, identity.ConversationID, d.outputPolicy)
	if body == "" {
		return PersonaReplyDeliveryReceipt{}, chat.ErrInvalidArgument
	}
	post, err := d.ephemeral.SendEphemeralPost(ctx, chat.SendEphemeralPostRequest{
		Principal: request.Principal, TenantID: request.Principal.TenantID,
		ConversationID: request.Output.Identity().ConversationID, ThreadID: request.Output.Identity().ThreadID,
		Body: body, IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		return PersonaReplyDeliveryReceipt{}, fmt.Errorf("deliver private persona reply: %w", err)
	}
	receipt := PersonaReplyDeliveryReceipt{Private: true, EphemeralPostID: post.ID, DurableCopyPostID: post.DurableCopyPostID, DurableCopyConversationID: post.DurableCopyConversationID}
	if neutralReceipt {
		_, err = d.chat.SendPost(ctx, chat.SendPostRequest{
			Principal: request.Principal, TenantID: request.Principal.TenantID,
			ConversationID: request.Output.Identity().ConversationID,
			ParentID:       request.Output.Identity().ThreadID, Body: "The persona reply was sent privately to you.",
			IdempotencyKey: request.IdempotencyKey + ":private-receipt",
		})
		if err != nil {
			return PersonaReplyDeliveryReceipt{}, fmt.Errorf("commit private delivery receipt: %w", err)
		}
	}
	return receipt, nil
}

var _ chat.PersonaAudienceFloor = personaFixedAudienceDecision{}
