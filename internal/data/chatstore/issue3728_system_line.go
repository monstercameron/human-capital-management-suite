package chatstore

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// SendSystemLine stores a workflow decision line (issue 3728) attributed to the
// person who decided. That person is a workspace person who may not be in the
// conversation: they decided on the journey page, so the membership check an
// ordinary send makes does not apply. Everything else does: the tenant row
// level security, the conversation fence, the idempotency key and the outbox
// event that tells the room. The door opens for one body shape only (a stage
// line) and only for an author in the conversation's own workspace, so it
// cannot be used to post a message as someone.
func (s *Adapter) SendSystemLine(ctx context.Context, r chat.SendPostRequest, p chat.Post) (chat.Post, error) {
	if _, ok := chat.ParseJourneyStage(p.Body); !ok || p.Body != r.Body {
		return chat.Post{}, chat.ErrInvalidArgument
	}
	if strings.TrimSpace(r.TenantID) == "" || strings.TrimSpace(r.ConversationID) == "" || strings.TrimSpace(r.IdempotencyKey) == "" ||
		p.AuthorID == "" || p.AuthorID != r.Principal.SubjectID ||
		r.Principal.TenantID != r.TenantID || p.AuthorHomeTenantID != r.TenantID {
		return chat.Post{}, chat.ErrPermissionDenied
	}
	refs, err := json.Marshal(p.References)
	if err != nil {
		return chat.Post{}, err
	}
	raw := SendRequest{TenantID: r.TenantID, ConversationID: r.ConversationID, HomeTenantID: r.TenantID, AuthorID: p.AuthorID, ClientKey: r.IdempotencyKey, Body: p.Body, References: refs, SystemLine: true, CreatedAt: p.CreatedAt}
	raw.RouteEpoch, raw.ShardID = leaseFence(ctx)
	q, err := s.sendPostRaw(ctx, raw)
	if err != nil {
		return chat.Post{}, err
	}
	return chatPost(q)
}
