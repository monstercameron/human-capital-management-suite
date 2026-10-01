package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PersonaLivePrivateDeliveryReader resolves the actual durable recipient-only
// chat record. The matching receipt hash binds its private and durable-copy
// IDs to the worker checkpoint; its recipient scope is the disclosure witness.
type PersonaLivePrivateDeliveryReader struct {
	Store          chatcore.EphemeralStore
	InvokerContext context.Context
	Outputs        *agentpersonastore.TenantStore
	Verifier       *agentsecurity.FinalOutputRecoveryVerifier
	Rehydrator     agentsecurity.FinalOutputRecoveryRehydrator
}

func (r *PersonaLivePrivateDeliveryReader) record(ctx context.Context, output agentpersonastore.FinalOutputRecord, expected string) (chatcore.EphemeralPost, error) {
	if r == nil || r.Store == nil || r.InvokerContext == nil || ctx == nil || !personaRequestDigest(expected) {
		return chatcore.EphemeralPost{}, agenteval.ErrPersonaEvaluation
	}
	principal, ok := trust.FromContext(r.InvokerContext)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant() != output.TenantID || principal.Subject() != output.InvokerID {
		return chatcore.EphemeralPost{}, agenteval.ErrPersonaEvaluation
	}
	queryCtx := trust.WithPrincipal(ctx, principal)
	posts, _, err := r.Store.ListEphemeral(queryCtx, chatcore.Principal{TenantID: principal.Tenant().String(), SubjectID: principal.Subject(), Roles: principal.Roles()}, output.TenantID.String(), output.ConversationID, 0, 100)
	if err != nil {
		return chatcore.EphemeralPost{}, err
	}
	for _, post := range posts {
		if post.TenantID != output.TenantID.String() || post.ConversationID != output.ConversationID || post.ThreadID != output.ThreadID || post.RecipientHomeTenantID != output.TenantID.String() || post.RecipientSubjectID != output.InvokerID || !post.OnlyVisibleToYou {
			continue
		}
		receipt := PersonaReplyDeliveryReceipt{Private: true, EphemeralPostID: post.ID, DurableCopyPostID: post.DurableCopyPostID, DurableCopyConversationID: post.DurableCopyConversationID}
		digest, err := personaRunDeliveryDigest(receipt)
		if err == nil && digest == expected {
			return post, nil
		}
	}
	return chatcore.EphemeralPost{}, agenteval.ErrPersonaEvaluation
}

func (r *PersonaLivePrivateDeliveryReader) ReadPersonaEvaluationDelivery(ctx context.Context, output agentpersonastore.FinalOutputRecord, digest string) ([]string, error) {
	post, err := r.record(ctx, output, digest)
	if err != nil {
		return nil, err
	}
	return []string{post.RecipientSubjectID}, nil
}

func (r *PersonaLivePrivateDeliveryReader) ReadPersonaEvaluationDisclosure(ctx context.Context, output agentpersonastore.FinalOutputRecord, digest string) (PersonaLiveDisclosureEvidence, error) {
	post, err := r.record(ctx, output, digest)
	if err != nil {
		return PersonaLiveDisclosureEvidence{}, err
	}
	if r.Outputs == nil || r.Verifier == nil || r.Rehydrator == nil {
		return PersonaLiveDisclosureEvidence{}, agenteval.ErrPersonaEvaluation
	}
	principal, _ := trust.FromContext(r.InvokerContext)
	current, err := r.Outputs.RecoverFinalOutput(trust.WithPrincipal(ctx, principal), output.OutputID, r.Verifier, r.Rehydrator)
	if err != nil || current.Digest() != output.PersistenceDigest || current.SemanticDigest() != output.Agent026Digest || current.AdmissionDigest() != output.AdmissionDigest || current.Identity().InvokerID != post.RecipientSubjectID || current.Identity().TenantID != post.RecipientHomeTenantID {
		return PersonaLiveDisclosureEvidence{}, agenteval.ErrPersonaEvaluation
	}
	proof := struct {
		Tenant, Conversation, Thread, Post, HomeTenant, Recipient string
		Private                                                   bool
	}{post.TenantID, post.ConversationID, post.ThreadID, post.ID, post.RecipientHomeTenantID, post.RecipientSubjectID, post.OnlyVisibleToYou}
	return PersonaLiveDisclosureEvidence{AuthorizedRecipients: []string{post.RecipientSubjectID}, AudienceFloorDigest: personaLiveEvidenceDigest("committed-private-disclosure", proof, struct {
		Output, Semantic, Admission, Delivery string
		Materials                             []agentsecurity.OutputMaterial
		Citations                             []agentsecurity.Citation
	}{current.Digest(), current.SemanticDigest(), current.AdmissionDigest(), digest, current.Materials(), current.Citations()})}, nil
}
