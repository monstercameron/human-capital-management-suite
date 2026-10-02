package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"log/slog"
	"os"
)

var ErrPersonaReplyDeliveryUnavailable = errors.New("application: persona reply delivery unavailable")

// PersonaReplyDeliveryRequest binds a sealed output to its authenticated invoker.
type PersonaReplyDeliveryRequest struct {
	Principal       chat.Principal
	Output          agentsecurity.FinalOutputPersistence
	IdempotencyKey  string
	Documents       []agentdocref.ResolvedDocument
	Omissions       []agentdocref.Omission
	CitationDetails []PersonaReplyCitationDetail
	// PrivacyRequested is set by the owner of the run when the asker asked, in
	// the question, for the answer to stay with them (AGENTUX-070).
	PrivacyRequested bool
	// PrivacyUnknown is set when the question could not be read to find out; the
	// answer then stays private, as anything that cannot be established does.
	PrivacyUnknown bool
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
	if conversation.Kind == chat.Direct {
		// A direct conversation with an agent is private by nature and is unchanged.
		return d.deliverPrivate(ctx, request, "")
	}
	if request.PrivacyRequested {
		return d.deliverPrivate(ctx, request, chat.PrivateReasonAsked)
	}
	if request.PrivacyUnknown {
		return d.deliverPrivate(ctx, request, chat.PrivateReasonAudience)
	}
	if conversation.Kind != chat.PublicChannel {
		// The audience check can establish a public channel's readers only.
		return d.privateFallback(ctx, request, chat.PrivateReasonAudience)
	}
	if fence, ok := d.floor.(interface {
		WithPersonaOutputFence(context.Context, agentsecurity.FinalOutputPersistence, func() error) error
	}); ok {
		var receipt PersonaReplyDeliveryReceipt
		err := fence.WithPersonaOutputFence(ctx, request.Output, func() error {
			var deliveryErr error
			receipt, deliveryErr = d.deliverPublic(ctx, request)
			return deliveryErr
		})
		if err != nil && !receipt.Public {
			return d.privateFallback(ctx, request, personaPrivateReasonOf(err))
		}
		return receipt, err
	}
	return d.deliverPublic(ctx, request)
}

func (d *PersonaReplyDelivery) deliverPublic(ctx context.Context, request PersonaReplyDeliveryRequest) (PersonaReplyDeliveryReceipt, error) {
	identity := request.Output.Identity()
	decision, err := d.floor.AuthorizePersonaOutput(ctx, request.Output)
	if err != nil || decision.Revision == 0 || strings.TrimSpace(decision.Body) == "" || decision.ParentID != identity.ThreadID {
		personaReplyFallbackNote(ctx, "audience_decision", fmt.Sprintf("err=%v revision=%d body_empty=%t parent_match=%t", err, decision.Revision, strings.TrimSpace(decision.Body) == "", decision.ParentID == identity.ThreadID))
		return d.privateFallback(ctx, request, personaPrivateReasonOf(err))
	}
	decision.Body = renderPersonaReplyWithAgentDocuments(decision.Body, identity.TenantID, identity.ConversationID, d.outputPolicy, announcementPublicSourceTitles(request.Output, request.Documents), nil, request.CitationDetails...)
	if decision.Body == "" {
		personaReplyFallbackNote(ctx, "rendered_body_empty", "")
		return d.privateFallback(ctx, request, chat.PrivateReasonAudience)
	}
	proof, err := chat.IssuePersonaDeliveryProof(ctx, request.Output, personaFixedAudienceDecision{decision: decision})
	if err != nil {
		personaReplyFallbackNote(ctx, "delivery_proof", err.Error())
		return d.privateFallback(ctx, request, chat.PrivateReasonAudience)
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
		personaReplyFallbackNote(ctx, "audience_changed", err.Error())
		return d.privateFallback(ctx, request, chat.PrivateReasonAudience)
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

// privateFallback delivers an answer that could not be posted to the
// conversation to the asker alone, with one line saying why.
func (d *PersonaReplyDelivery) privateFallback(ctx context.Context, request PersonaReplyDeliveryRequest, reason string) (PersonaReplyDeliveryReceipt, error) {
	return d.deliverPrivate(ctx, request, reason)
}

// deliverPrivate sends the answer to the asker alone. A non-empty reason is
// appended as the marker that lets the card say why it is private.
func (d *PersonaReplyDelivery) deliverPrivate(ctx context.Context, request PersonaReplyDeliveryRequest, reason string) (PersonaReplyDeliveryReceipt, error) {
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
	body = renderPersonaReplyWithAgentDocuments(body, identity.TenantID, identity.ConversationID, d.outputPolicy, request.Documents, request.Omissions, request.CitationDetails...)
	if body == "" {
		return PersonaReplyDeliveryReceipt{}, chat.ErrInvalidArgument
	}
	if marker := chat.PrivateReasonMarker(reason); marker != "" {
		body += "\n" + marker
	}
	post, err := d.ephemeral.SendEphemeralPost(ctx, chat.SendEphemeralPostRequest{
		Principal: request.Principal, TenantID: request.Principal.TenantID,
		ConversationID: request.Output.Identity().ConversationID, ThreadID: request.Output.Identity().ThreadID,
		QuestionPostID: request.Output.Identity().PostID,
		Body:           body, AuthorAsAgent: true, IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		return PersonaReplyDeliveryReceipt{}, fmt.Errorf("deliver private persona reply: %w", err)
	}
	return PersonaReplyDeliveryReceipt{Private: true, EphemeralPostID: post.ID, DurableCopyPostID: post.DurableCopyPostID, DurableCopyConversationID: post.DurableCopyConversationID}, nil
}

var _ chat.PersonaAudienceFloor = personaFixedAudienceDecision{}

// personaReplyFallbackNote records why a public reply was redirected to the
// invoker privately. It carries no reply text.
func personaReplyFallbackNote(ctx context.Context, reason, detail string) {
	if os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
		slog.InfoContext(ctx, "hcmnext.persona_reply_private_fallback", "reason", reason, "detail", detail)
	}
}
