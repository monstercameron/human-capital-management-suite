package chat

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// FilterMetadataService preserves Service's optional ports while adding the
// metadata check, which the original post-only ContentPolicy port lacked.
type FilterMetadataService struct{ *Service }

func WithFilterMetadata(s *Service) *FilterMetadataService {
	if policy, ok := s.contentPolicy.(*FilterContentPolicy); ok {
		policy.AttachmentTypes = func(ctx context.Context, in ContentInput) ([]string, error) {
			var types []string
			for _, ref := range in.References {
				if ref.Kind != MediaAttachment {
					continue
				}
				if s.mediaDirectory == nil {
					return nil, ErrUnavailable
				}
				facts, err := s.mediaDirectory.MediaArtifact(ctx, in.Conversation.TenantID, in.Conversation.ID, ref.ID)
				if err != nil || !facts.Admitted {
					return nil, ErrNotFound
				}
				types = append(types, facts.ContentType)
			}
			return types, nil
		}
	}
	return &FilterMetadataService{Service: s}
}
func (s *FilterMetadataService) CreateConversation(ctx context.Context, r CreateConversationRequest) (Conversation, error) {
	if err := s.ValidateCreate(ctx, r); err != nil {
		return Conversation{}, err
	}
	c := Conversation{TenantID: r.TenantID, ID: r.ConversationID, Kind: r.Kind}
	if err := s.checkContent(ctx, ContentInput{Principal: r.Principal, Conversation: c, Body: strings.TrimSpace(r.Name)}); err != nil {
		return Conversation{}, err
	}
	return s.Service.CreateConversation(ctx, r)
}
func (s *FilterMetadataService) UpdateConversation(ctx context.Context, r UpdateConversationRequest) (Conversation, error) {
	if err := validatePrincipal(r.Principal, r.Conversation.TenantID); err != nil {
		return Conversation{}, err
	}
	c, err := s.store.GetConversation(ctx, r.Conversation.TenantID, r.Conversation.ID)
	if err != nil {
		return Conversation{}, err
	}
	if r.Principal.TenantID != c.TenantID {
		return Conversation{}, ErrPermissionDenied
	}
	if err = s.authorize(ctx, r.Principal, c, chatpolicy.ActionRead); err != nil {
		return Conversation{}, err
	}
	m, err := s.store.GetMembership(ctx, c.TenantID, c.ID, r.Principal.TenantID, r.Principal.SubjectID)
	if err != nil || m.JoinedAt == nil || m.LeftAt != nil || m.Role != Manager {
		return Conversation{}, ErrPermissionDenied
	}
	if err = s.checkContent(ctx, ContentInput{Principal: r.Principal, Conversation: c, Body: strings.TrimSpace(r.Conversation.Name), Edit: true}); err != nil {
		return Conversation{}, err
	}
	return s.Service.UpdateConversation(ctx, r)
}

func (s *FilterMetadataService) CommitPersonaReply(ctx context.Context, r PersonaReplyCommitRequest) (Post, error) {
	if r.Proof == nil || !r.Proof.ValidFor(r.TenantID, r.ConversationID, r.AuthorID, r.ExpectedAudienceRevision, r.OutputDigest, r.Body, r.ParentID) {
		return Post{}, ErrPermissionDenied
	}
	principal := Principal{TenantID: r.AuthorHomeTenantID, SubjectID: r.AuthorID}
	c, err := s.store.GetConversation(ctx, r.TenantID, r.ConversationID)
	if err != nil {
		return Post{}, err
	}
	if policy, ok := s.contentPolicy.(*FilterContentPolicy); ok {
		err = policy.checkPersonaReplyContent(ctx, r, c)
	} else {
		err = s.checkContent(ctx, ContentInput{Principal: principal, Conversation: c, Body: r.Body})
	}
	if err != nil {
		return Post{}, err
	}
	return s.Service.CommitPersonaReply(ctx, r)
}
