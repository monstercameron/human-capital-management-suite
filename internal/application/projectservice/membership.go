package projectservice

import (
	"context"
	"math"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func (s Service) ProjectMemberships(ctx context.Context, p *trust.Principal, projectID string) (MembershipSnapshot, error) {
	if err := validPrincipal(p); err != nil {
		return MembershipSnapshot{}, err
	}
	if s.Auth == nil || s.Members == nil {
		return MembershipSnapshot{}, ErrUnavailable
	}
	if strings.TrimSpace(projectID) == "" {
		return MembershipSnapshot{}, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, p, projectID, projectaccess.ManageMembers); err != nil {
		return MembershipSnapshot{}, err
	}
	return s.Members.GetMemberships(ctx, tenant(p), projectID)
}

func (s Service) ProjectMembership(ctx context.Context, p *trust.Principal, projectID, userID string) (MembershipRecord, uint64, error) {
	if strings.TrimSpace(userID) == "" {
		return MembershipRecord{}, 0, ErrInvalidRequest
	}
	snapshot, err := s.ProjectMemberships(ctx, p, projectID)
	if err != nil {
		return MembershipRecord{}, 0, err
	}
	for _, member := range snapshot.Members {
		if member.UserID == userID {
			return member, snapshot.Revision, nil
		}
	}
	return MembershipRecord{}, 0, projectaccess.ErrMemberNotFound
}

func (s Service) InviteMember(ctx context.Context, p *trust.Principal, projectID, userID string, role projectaccess.Role, expected uint64, key string) error {
	if err := s.membershipMutation(ctx, p, projectID, expected, key); err != nil {
		return err
	}
	if strings.TrimSpace(userID) == "" {
		return ErrInvalidRequest
	}
	if s.Invitees == nil {
		return ErrUnavailable
	}
	inviteeClass, err := s.Invitees.CheckInvitee(ctx, p, userID)
	if err != nil {
		return err
	}
	if err := s.Members.ValidateInviteeClassification(ctx, tenant(p), projectID, inviteeClass); err != nil {
		return err
	}
	return s.Members.InviteMember(ctx, tenant(p), projectID, p.Subject(), userID, role, inviteeClass, expected, key)
}
func (s Service) AcceptMemberInvitation(ctx context.Context, p *trust.Principal, projectID string, expected uint64, key string) error {
	if err := s.membershipRequest(ctx, p, projectID, expected, key); err != nil {
		return err
	}
	if s.Invitees == nil {
		return ErrUnavailable
	}
	inviteeClass, err := s.Invitees.CheckInvitee(ctx, p, p.Subject())
	if err != nil {
		return err
	}
	if err := s.Members.ValidateInviteeClassification(ctx, tenant(p), projectID, inviteeClass); err != nil {
		return err
	}
	return s.Members.AcceptMemberInvitation(ctx, tenant(p), projectID, p.Subject(), expected, key)
}
func (s Service) ChangeMemberRole(ctx context.Context, p *trust.Principal, projectID, userID string, role projectaccess.Role, expected uint64, key string) error {
	if err := s.membershipMutation(ctx, p, projectID, expected, key); err != nil {
		return err
	}
	return s.Members.ChangeMemberRole(ctx, tenant(p), projectID, p.Subject(), userID, role, expected, key)
}
func (s Service) RevokeMember(ctx context.Context, p *trust.Principal, projectID, userID string, expected uint64, key string) error {
	if err := s.membershipMutation(ctx, p, projectID, expected, key); err != nil {
		return err
	}
	return s.Members.RevokeMember(ctx, tenant(p), projectID, p.Subject(), userID, expected, key)
}
func (s Service) TransferMemberOwnership(ctx context.Context, p *trust.Principal, projectID, userID string, expected uint64, key string) error {
	if err := s.membershipMutation(ctx, p, projectID, expected, key); err != nil {
		return err
	}
	return s.Members.TransferMemberOwnership(ctx, tenant(p), projectID, p.Subject(), userID, expected, key)
}
func (s Service) membershipMutation(ctx context.Context, p *trust.Principal, projectID string, expected uint64, key string) error {
	if err := s.membershipRequest(ctx, p, projectID, expected, key); err != nil {
		return err
	}
	return s.Auth.Authorize(ctx, p, projectID, projectaccess.ManageMembers)
}
func (s Service) membershipRequest(ctx context.Context, p *trust.Principal, projectID string, expected uint64, key string) error {
	if err := validPrincipal(p); err != nil {
		return err
	}
	if s.Auth == nil || s.Members == nil {
		return ErrUnavailable
	}
	if strings.TrimSpace(projectID) == "" || expected == 0 || expected >= math.MaxInt64 || strings.TrimSpace(key) == "" {
		return ErrInvalidRequest
	}
	return nil
}
