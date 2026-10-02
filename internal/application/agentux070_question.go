package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

// personaQuestionAskedPrivately reads the question the sealed output answers and
// reports whether the asker asked, in it, for the answer to stay with them. The
// question is read again through the same authorized thread reader the model
// run used; nothing the browser sent is trusted. A question that cannot be read
// is an error, and the caller keeps the answer private.
func personaQuestionAskedPrivately(ctx context.Context, threads agentinvoke.ThreadReader, identity agentsecurity.FinalOutputIdentity) (bool, error) {
	if isNilPersonaOutputPort(threads) || strings.TrimSpace(identity.PostID) == "" {
		return false, ErrPersonaReplyDeliveryUnavailable
	}
	posts, err := threads.ReadThread(ctx, agentinvoke.ThreadReadRequest{TenantID: identity.TenantID, ConversationID: identity.ConversationID, ThreadID: identity.ThreadID, InvokerID: identity.InvokerID, InvokingPostID: identity.PostID, Limit: agentinvoke.MaxThreadPosts})
	if err != nil {
		return false, err
	}
	for _, post := range posts {
		if post.ID == identity.PostID && post.TenantID == identity.TenantID && post.ConversationID == identity.ConversationID && post.AuthorID == identity.InvokerID && !post.Bot {
			return AskedForPrivacy(post.Body), nil
		}
	}
	return false, ErrPersonaReplyDeliveryUnavailable
}

// withPrivacyRequest sets PrivacyRequested from the question. Where a reader is
// composed and the question cannot be read, the answer is kept private: whether
// the asker wanted privacy could not be established.
func (d *personaRuntimeCurrentReply) withPrivacyRequest(ctx context.Context, req PersonaReplyDeliveryRequest) PersonaReplyDeliveryRequest {
	if isNilPersonaOutputPort(d.questions) {
		return req
	}
	asked, err := personaQuestionAskedPrivately(ctx, d.questions, req.Output.Identity())
	req.PrivacyRequested = req.PrivacyRequested || (asked && err == nil)
	req.PrivacyUnknown = err != nil
	return req
}
