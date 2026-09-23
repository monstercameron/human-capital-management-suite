package chatextensions

import (
	"context"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func teamWidgetOut(v chat.ChannelTeamWidget) *chatv1.ChannelTeamWidget {
	out := &chatv1.ChannelTeamWidget{ConversationId: v.ConversationID, Revision: v.Revision, Pinned: v.Pinned, Purpose: v.Purpose, CanPin: v.CanPin}
	for _, m := range v.Members {
		out.Members = append(out.Members, &chatv1.ChannelTeamMember{HomeTenantId: m.HomeTenantID, SubjectId: m.SubjectID, Role: m.Role, RoleLabel: m.RoleLabel})
	}
	return out
}
func projectWidgetOut(v chat.ChannelProjectWidget) *chatv1.ChannelProjectWidget {
	out := &chatv1.ChannelProjectWidget{ConversationId: v.ConversationID, Revision: v.Revision, Pinned: v.Pinned, Title: v.Title, Summary: v.Summary, CanPin: v.CanPin}
	for _, m := range v.Milestones {
		out.Milestones = append(out.Milestones, &chatv1.ChannelProjectMilestone{Id: m.ID, Text: m.Text, Status: m.Status, OwnerHomeTenantId: m.OwnerHomeTenantID, OwnerSubjectId: m.OwnerSubjectID, DueDate: m.DueDate})
	}
	return out
}

func (s *server) GetChannelWidgets(ctx context.Context, r *chatv1.GetChannelWidgetsRequest) (*chatv1.GetChannelWidgetsResponse, error) {
	p, err := s.principal(ctx)
	if err != nil {
		return nil, err
	}
	svc, ok := s.deps.Service.(ChannelWidgetService)
	if !ok {
		return nil, mapped(chat.ErrUnavailable)
	}
	v, err := svc.ChannelWidgets(ctx, p, r.GetHostTenantId(), r.GetConversationId())
	if err != nil {
		return nil, mapped(err)
	}
	return &chatv1.GetChannelWidgetsResponse{Team: teamWidgetOut(v.Team), Project: projectWidgetOut(v.Project)}, nil
}
func (s *server) MutateChannelWidget(ctx context.Context, r *chatv1.MutateChannelWidgetRequest) (*chatv1.MutateChannelWidgetResponse, error) {
	p, err := s.principal(ctx)
	if err != nil {
		return nil, err
	}
	svc, ok := s.deps.Service.(ChannelWidgetService)
	if !ok {
		return nil, mapped(chat.ErrUnavailable)
	}
	m := chat.ChannelWidgetMutation{Kind: r.GetKind(), Operation: r.GetOperation(), Pinned: r.GetPinned(), Purpose: r.GetPurpose(), MemberHomeTenantID: r.GetMemberHomeTenantId(), MemberSubjectID: r.GetMemberSubjectId(), RoleLabel: r.GetRoleLabel(), Title: r.GetTitle(), Summary: r.GetSummary(), MilestoneID: r.GetMilestoneId()}
	if x := r.GetMilestone(); x != nil {
		m.Milestone = chat.ChannelProjectMilestone{ID: x.GetId(), Text: x.GetText(), Status: x.GetStatus(), OwnerHomeTenantID: x.GetOwnerHomeTenantId(), OwnerSubjectID: x.GetOwnerSubjectId(), DueDate: x.GetDueDate()}
	}
	v, err := svc.MutateChannelWidget(ctx, p, r.GetHostTenantId(), r.GetConversationId(), r.GetExpectedRevision(), m)
	if err != nil {
		return nil, mapped(err)
	}
	return &chatv1.MutateChannelWidgetResponse{Team: teamWidgetOut(v.Team), Project: projectWidgetOut(v.Project)}, nil
}
