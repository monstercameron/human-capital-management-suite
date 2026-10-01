package chatstore

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

// SealedPublicPersonaAuthorizer resolves the authenticated workload only after
// rechecking this exact output's current admission, delegation and sources.
type SealedPublicPersonaAuthorizer interface {
	AuthorizeSealedPublicPersonaReply(context.Context, agentsecurity.FinalOutputPersistence) (workload.Identity, error)
}

// SealedPublicPersonaDelivery writes a persona author from sealed output while
// retaining the human invoker's context. It never fabricates an agent principal.
type SealedPublicPersonaDelivery struct {
	store *Store
	owner SealedPublicPersonaAuthorizer
	now   func() time.Time
}

func NewSealedPublicPersonaDelivery(store *Store, owner SealedPublicPersonaAuthorizer, now func() time.Time) (*SealedPublicPersonaDelivery, error) {
	if store == nil || owner == nil || now == nil {
		return nil, chat.ErrUnavailable
	}
	return &SealedPublicPersonaDelivery{store: store, owner: owner, now: now}, nil
}

// CommitPersonaReply refuses requests without their gateway-sealed output.
func (*SealedPublicPersonaDelivery) CommitPersonaReply(context.Context, chat.PersonaReplyCommitRequest) (chat.Post, error) {
	return chat.Post{}, chat.ErrPermissionDenied
}

// CommitSealedPersonaReply combines a current workload owner with the audience
// proof and exact output identity. The existing native writer fences the
// audience revision atomically before creating a shared post.
func (s *SealedPublicPersonaDelivery) CommitSealedPersonaReply(ctx context.Context, output agentsecurity.FinalOutputPersistence, r chat.PersonaReplyCommitRequest) (chat.Post, error) {
	if s == nil || ctx == nil || s.store == nil || s.owner == nil || s.now == nil {
		return chat.Post{}, chat.ErrUnavailable
	}
	i := output.Identity()
	if _, _, err := output.Payload(); err != nil || output.Digest() == "" || i.TenantID == "" || i.PersonaID == "" || i.InvokerID == "" || i.AdmissionID == "" ||
		r.TenantID != i.TenantID || r.AuthorHomeTenantID != i.TenantID || r.ConversationID != i.ConversationID || r.AuthorID != i.PersonaID || r.ParentID != i.ThreadID ||
		r.IdempotencyKey != i.AdmissionID || r.OutputDigest != output.Digest() || strings.TrimSpace(r.Body) == "" || r.ExpectedAudienceRevision == 0 || r.Proof == nil ||
		!r.Proof.ValidFor(r.TenantID, r.ConversationID, r.AuthorID, r.ExpectedAudienceRevision, r.OutputDigest, r.Body, r.ParentID) {
		return chat.Post{}, chat.ErrPermissionDenied
	}
	worker, err := s.owner.AuthorizeSealedPublicPersonaReply(ctx, output)
	if err != nil || worker.Role() != workload.RoleWorker || worker.Fingerprint() == "" || worker.Issuer() == "" || worker.Subject() == "" || worker.Cell() == "" || worker.KeyID() == "" || !worker.ValidAt(s.now().UTC()) {
		return chat.Post{}, chat.ErrPermissionDenied
	}
	raw := SendRequest{TenantID: i.TenantID, ConversationID: i.ConversationID, HomeTenantID: i.TenantID, AuthorID: i.PersonaID,
		ClientKey: r.IdempotencyKey, Body: r.Body, ParentID: i.ThreadID, TrustedAuthor: true, ExpectedAudienceRevision: r.ExpectedAudienceRevision}
	raw.RouteEpoch, raw.ShardID = leaseFence(ctx)
	post, err := s.store.sendPostRaw(ctx, raw)
	if err != nil {
		return chat.Post{}, err
	}
	return chatPost(post)
}
