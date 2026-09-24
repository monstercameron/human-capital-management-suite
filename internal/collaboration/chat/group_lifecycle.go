package chat

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// GroupLifecycleService is the private-group lifecycle surface implemented by
// Service. It keeps group operations explicitly separate from direct-message
// identity and history.
type GroupLifecycleService interface {
	CreatePrivateGroup(context.Context, CreateConversationRequest) (Conversation, error)
	RenamePrivateGroup(context.Context, UpdateConversationRequest) (Conversation, error)
	InviteGroupMember(context.Context, AddMembershipRequest) (Membership, error)
	RevokeGroupMember(context.Context, RemoveMembershipRequest) (Membership, error)
}

// CreatePrivateGroup creates a new group conversation with a fresh history.
// A group has at least three distinct participants: its creator and two invitees.
func (s *Service) CreatePrivateGroup(ctx context.Context, r CreateConversationRequest) (Conversation, error) {
	if r.Kind != Group {
		return Conversation{}, ErrInvalidArgument
	}
	refs := r.Members
	if len(refs) == 0 {
		for _, id := range r.MemberIDs {
			refs = append(refs, MemberRef{TenantID: r.TenantID, SubjectID: id})
		}
	}
	if len(refs) < 2 {
		return Conversation{}, ErrInvalidArgument
	}
	for _, ref := range refs {
		if ref.TenantID != r.TenantID || ref.SubjectID != strings.TrimSpace(ref.SubjectID) || ref.TenantID != strings.TrimSpace(ref.TenantID) {
			return Conversation{}, ErrPermissionDenied
		}
		if ref.TenantID == r.Principal.TenantID && ref.SubjectID == r.Principal.SubjectID {
			return Conversation{}, ErrInvalidArgument
		}
	}
	return s.CreateConversation(ctx, r)
}

// RenamePrivateGroup allows an active group manager to change only the shared
// name. The conversation identity, owner, kind, and history remain store-owned.
func (s *Service) RenamePrivateGroup(ctx context.Context, r UpdateConversationRequest) (Conversation, error) {
	if err := validatePrincipal(r.Principal, r.Conversation.TenantID); err != nil || r.ExpectedRevision == 0 || r.Conversation.ID == "" {
		return Conversation{}, errOr(err, ErrInvalidArgument)
	}
	c, err := s.store.GetConversation(ctx, r.Conversation.TenantID, r.Conversation.ID)
	if err != nil {
		return Conversation{}, err
	}
	if c.Kind != Group || r.Principal.TenantID != c.TenantID || r.Conversation.TenantID != c.TenantID {
		return Conversation{}, ErrPermissionDenied
	}
	if err := s.authorize(ctx, r.Principal, c, chatpolicy.ActionRead); err != nil {
		return Conversation{}, err
	}
	actor, err := s.store.GetMembership(ctx, c.TenantID, c.ID, r.Principal.TenantID, r.Principal.SubjectID)
	if err != nil || actor.JoinedAt == nil || actor.LeftAt != nil || actor.Role != Manager {
		return Conversation{}, ErrPermissionDenied
	}
	name := strings.TrimSpace(r.Conversation.Name)
	if name == "" || len(name) > 200 {
		return Conversation{}, ErrInvalidArgument
	}
	desired := c
	desired.Name = name
	return s.store.UpdateConversation(ctx, r.Principal, desired, r.ExpectedRevision)
}

// InviteGroupMember adds an explicit invite to a private group. New invitees
// begin at the current history boundary; callers cannot grant them older posts
// or manager authority through this operation.
func (s *Service) InviteGroupMember(ctx context.Context, r AddMembershipRequest) (Membership, error) {
	m := r.Membership
	if err := validatePrincipal(r.Principal, m.TenantID); err != nil || m.TenantID == "" || m.HomeTenantID == "" || m.ConversationID == "" || strings.TrimSpace(m.SubjectID) == "" {
		return Membership{}, errOr(err, ErrInvalidArgument)
	}
	c, err := s.store.GetConversation(ctx, m.TenantID, m.ConversationID)
	if err != nil {
		return Membership{}, err
	}
	if c.Kind != Group || c.TenantID != r.Principal.TenantID {
		return Membership{}, ErrPermissionDenied
	}
	if m.HomeTenantID != c.TenantID {
		return Membership{}, ErrPermissionDenied
	}
	if err := s.authorize(ctx, r.Principal, c, chatpolicy.ActionRead); err != nil {
		return Membership{}, err
	}
	if !s.activeGroupManager(ctx, r.Principal, c) {
		return Membership{}, ErrPermissionDenied
	}
	if m.HomeTenantID == r.Principal.TenantID && m.SubjectID == r.Principal.SubjectID {
		return Membership{}, ErrInvalidArgument
	}
	m.Role = Member
	m.JoinedAt = timePtr(s.now())
	m.LeftAt = nil
	m.HistoryVisibility = FromJoin
	return s.store.PutMembership(ctx, r.Principal, m)
}

// RevokeGroupMember lets an active group manager revoke any member except the
// stable owner. A target cannot be removed from a different home-tenant
// identity by supplying only a subject ID.
func (s *Service) RevokeGroupMember(ctx context.Context, r RemoveMembershipRequest) (Membership, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil || r.ConversationID == "" || r.SubjectID == "" || r.HomeTenantID == "" || r.ExpectedRevision == 0 {
		return Membership{}, errOr(err, ErrInvalidArgument)
	}
	c, err := s.store.GetConversation(ctx, r.TenantID, r.ConversationID)
	if err != nil {
		return Membership{}, err
	}
	if c.Kind != Group || c.TenantID != r.Principal.TenantID || r.HomeTenantID == c.TenantID && r.SubjectID == c.OwnerID {
		return Membership{}, ErrPermissionDenied
	}
	if err := s.authorize(ctx, r.Principal, c, chatpolicy.ActionRead); err != nil {
		return Membership{}, err
	}
	if !s.activeGroupManager(ctx, r.Principal, c) {
		return Membership{}, ErrPermissionDenied
	}
	return s.store.RemoveMembership(ctx, r.Principal, c.TenantID, c.ID, r.HomeTenantID, r.SubjectID, r.ExpectedRevision)
}

func (s *Service) activeGroupManager(ctx context.Context, p Principal, c Conversation) bool {
	m, err := s.store.GetMembership(ctx, c.TenantID, c.ID, p.TenantID, p.SubjectID)
	return err == nil && m.JoinedAt != nil && m.LeftAt == nil && m.Role == Manager
}

var _ GroupLifecycleService = (*Service)(nil)
