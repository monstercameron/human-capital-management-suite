package chat

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// authorizePin lets the conversation owner pin any message, and lets any
// current member who may post pin a message they wrote themselves. Pinning
// someone else's message in a room the caller does not own stays refused.
func (s *Service) authorizePin(ctx context.Context, p Principal, c Conversation, postID string) error {
	if p.TenantID != c.TenantID {
		return ErrPermissionDenied
	}
	if c.OwnerID == p.SubjectID {
		return s.authorize(ctx, p, c, chatpolicy.ActionRead)
	}
	// Membership first: a caller who may not post here learns nothing about
	// whether the message exists.
	if err := s.authorize(ctx, p, c, chatpolicy.ActionPost); err != nil {
		return err
	}
	post, err := s.store.GetPost(ctx, c.TenantID, c.ID, postID)
	if err != nil {
		return err
	}
	if post.Deleted || post.AuthorID != p.SubjectID || post.AuthorHomeTenantID != p.TenantID {
		return ErrPermissionDenied
	}
	return nil
}

// authorizeUnpin lets the owner or the person who pinned the message remove it.
func (s *Service) authorizeUnpin(ctx context.Context, p Principal, c Conversation, postID string) error {
	if p.TenantID != c.TenantID {
		return ErrPermissionDenied
	}
	if c.OwnerID == p.SubjectID {
		return s.authorize(ctx, p, c, chatpolicy.ActionRead)
	}
	// The room's rule first, as for pinning: nothing is read for a caller it
	// refuses.
	if err := s.authorize(ctx, p, c, chatpolicy.ActionPost); err != nil {
		return err
	}
	pins, err := s.store.ListPins(ctx, c.TenantID, c.ID)
	if err != nil {
		return err
	}
	for _, pin := range pins {
		if pin.PostID == postID && pin.PinnedBy == p.SubjectID && pin.PinnedByHomeTenantID == p.TenantID {
			return nil
		}
	}
	return ErrPermissionDenied
}
