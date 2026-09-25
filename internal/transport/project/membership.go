package project

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func memberWire(m projectservice.MembershipRecord) *projectv1.ProjectMember {
	return &projectv1.ProjectMember{UserId: m.UserID, Role: string(m.Role), State: string(m.State), Revision: m.Revision}
}
func (s *server) GetProjectMembership(ctx context.Context, req *projectv1.GetProjectMembershipRequest) (*projectv1.GetProjectMembershipResponse, error) {
	if err := requireService(s); err != nil {
		return nil, err
	}
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	m, rev, err := s.service.ProjectMembership(ctx, p, req.GetProjectId(), req.GetUserId())
	if err != nil {
		return nil, mapError(err)
	}
	return &projectv1.GetProjectMembershipResponse{Member: memberWire(m), MembershipRevision: rev}, nil
}
func (s *server) ListProjectMembers(ctx context.Context, req *projectv1.ListProjectMembersRequest) (*projectv1.ListProjectMembersResponse, error) {
	if err := requireService(s); err != nil {
		return nil, err
	}
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	snap, err := s.service.ProjectMemberships(ctx, p, req.GetProjectId())
	if err != nil {
		return nil, mapError(err)
	}
	limit := int(req.GetPageSize())
	if limit <= 0 {
		limit = defaultPageSize
	}
	if limit > 100 {
		limit = 100
	}
	after := ""
	start := 0
	if req.GetPageCursor() != "" {
		parts := strings.SplitN(req.GetPageCursor(), ".", 2)
		cursorRevision, parseErr := strconv.ParseUint(parts[0], 10, 64)
		if len(parts) != 2 || parseErr != nil || cursorRevision != snap.Revision {
			return nil, mapError(projectservice.ErrInvalidRequest)
		}
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
		if decodeErr != nil {
			return nil, mapError(projectservice.ErrInvalidRequest)
		}
		after = string(decoded)
		for start < len(snap.Members) && snap.Members[start].UserID <= after {
			start++
		}
	}
	end := start + limit
	if end > len(snap.Members) {
		end = len(snap.Members)
	}
	out := &projectv1.ListProjectMembersResponse{MembershipRevision: snap.Revision}
	for _, m := range snap.Members[start:end] {
		out.Members = append(out.Members, memberWire(m))
	}
	if end < len(snap.Members) && end > start {
		out.NextPageCursor = fmt.Sprintf("%d.%s", snap.Revision, base64.RawURLEncoding.EncodeToString([]byte(snap.Members[end-1].UserID)))
	}
	return out, nil
}
func memberRole(v string) projectaccess.Role { return projectaccess.Role(v) }
func membershipResponse(revision uint64) *projectv1.MutateProjectMembershipResponse {
	return &projectv1.MutateProjectMembershipResponse{MembershipRevision: revision + 1}
}
func (s *server) InviteProjectMember(ctx context.Context, r *projectv1.InviteProjectMemberRequest) (*projectv1.MutateProjectMembershipResponse, error) {
	return s.mutateMembership(ctx, r.GetProjectId(), r.GetExpectedMembershipRevision(), r.GetIdempotencyKey(), func(p *trust.Principal) error {
		return s.service.InviteMember(ctx, p, r.GetProjectId(), r.GetUserId(), memberRole(r.GetRole()), r.GetExpectedMembershipRevision(), r.GetIdempotencyKey())
	})
}
func (s *server) AcceptProjectInvitation(ctx context.Context, r *projectv1.AcceptProjectInvitationRequest) (*projectv1.MutateProjectMembershipResponse, error) {
	return s.mutateMembership(ctx, r.GetProjectId(), r.GetExpectedMembershipRevision(), r.GetIdempotencyKey(), func(p *trust.Principal) error {
		return s.service.AcceptMemberInvitation(ctx, p, r.GetProjectId(), r.GetExpectedMembershipRevision(), r.GetIdempotencyKey())
	})
}
func (s *server) ChangeProjectMemberRole(ctx context.Context, r *projectv1.ChangeProjectMemberRoleRequest) (*projectv1.MutateProjectMembershipResponse, error) {
	return s.mutateMembership(ctx, r.GetProjectId(), r.GetExpectedMembershipRevision(), r.GetIdempotencyKey(), func(p *trust.Principal) error {
		return s.service.ChangeMemberRole(ctx, p, r.GetProjectId(), r.GetUserId(), memberRole(r.GetRole()), r.GetExpectedMembershipRevision(), r.GetIdempotencyKey())
	})
}
func (s *server) RevokeProjectMember(ctx context.Context, r *projectv1.RevokeProjectMemberRequest) (*projectv1.MutateProjectMembershipResponse, error) {
	return s.mutateMembership(ctx, r.GetProjectId(), r.GetExpectedMembershipRevision(), r.GetIdempotencyKey(), func(p *trust.Principal) error {
		return s.service.RevokeMember(ctx, p, r.GetProjectId(), r.GetUserId(), r.GetExpectedMembershipRevision(), r.GetIdempotencyKey())
	})
}
func (s *server) TransferProjectOwnership(ctx context.Context, r *projectv1.TransferProjectOwnershipRequest) (*projectv1.MutateProjectMembershipResponse, error) {
	return s.mutateMembership(ctx, r.GetProjectId(), r.GetExpectedMembershipRevision(), r.GetIdempotencyKey(), func(p *trust.Principal) error {
		return s.service.TransferMemberOwnership(ctx, p, r.GetProjectId(), r.GetUserId(), r.GetExpectedMembershipRevision(), r.GetIdempotencyKey())
	})
}
func (s *server) mutateMembership(ctx context.Context, projectID string, rev uint64, key string, call func(*trust.Principal) error) (*projectv1.MutateProjectMembershipResponse, error) {
	if err := requireService(s); err != nil {
		return nil, err
	}
	p, err := admitted(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(projectID) == "" || rev == 0 || strings.TrimSpace(key) == "" {
		return nil, mapError(projectservice.ErrInvalidRequest)
	}
	if err = call(p); err != nil {
		return nil, mapError(err)
	}
	return membershipResponse(rev), nil
}
