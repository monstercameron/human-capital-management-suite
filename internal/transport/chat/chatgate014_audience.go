package chat

import (
	"context"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATGATE-014: a message addressed to segments of the channel's gate. The
// request names the segments and whether the message is restricted to them; the
// service decides whether the sender may. An answer that carries posts tells the
// reader, post by post, who each addressed message is for.

// audienceIn is the audience a send asks for, nil for a message for everyone.
func audienceIn(r *chatv1.SendPostRequest) *chatcore.PostAudienceRequest {
	if len(r.GetAudienceSegments()) == 0 {
		return nil
	}
	return &chatcore.PostAudienceRequest{Segments: append([]string(nil), r.GetAudienceSegments()...), Restricted: r.GetAudienceRestricted()}
}

// markAudiences sets the audience fields of the posts that have one. The service
// the transport fronts may implement PostAudienceLister; when it does not, or the
// read fails, the posts are answered without tags, as they were.
func (s *server) markAudiences(ctx context.Context, caller chatcore.Principal, tenantID string, posts []*chatv1.Post) {
	lister, ok := s.deps.Service.(chatcore.PostAudienceLister)
	if !ok || len(posts) == 0 {
		return
	}
	ids := make([]string, 0, len(posts))
	for _, post := range posts {
		if post != nil && post.GetId() != "" {
			ids = append(ids, post.GetId())
		}
	}
	if len(ids) == 0 {
		return
	}
	infos := lister.PostAudiences(ctx, caller, tenantID, ids)
	for _, post := range posts {
		if post == nil {
			continue
		}
		if info, tagged := infos[post.GetId()]; tagged && len(info.Segments) > 0 {
			post.AudienceSegments, post.AudienceRestricted, post.AudiencePeople = info.Segments, info.Restricted, uint32(info.People)
		}
	}
}
