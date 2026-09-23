package chatextensions

import (
	"context"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type widgetService struct {
	Service
	actor              chat.Principal
	host, conversation string
	expected           uint64
	mutation           chat.ChannelWidgetMutation
}

func (s *widgetService) ChannelWidgets(_ context.Context, p chat.Principal, host, conversation string) (chat.ChannelWidgets, error) {
	s.actor, s.host, s.conversation = p, host, conversation
	return chat.ChannelWidgets{Team: chat.ChannelTeamWidget{ConversationID: conversation, Revision: 1, Members: []chat.ChannelTeamMember{{HomeTenantID: "home", SubjectID: "admin", Role: "manager", RoleLabel: "Lead"}}}, Project: chat.ChannelProjectWidget{ConversationID: conversation, Revision: 1}}, nil
}
func (s *widgetService) MutateChannelWidget(_ context.Context, p chat.Principal, host, conversation string, expected uint64, m chat.ChannelWidgetMutation) (chat.ChannelWidgets, error) {
	s.actor, s.host, s.conversation, s.expected, s.mutation = p, host, conversation, expected, m
	return chat.ChannelWidgets{Team: chat.ChannelTeamWidget{ConversationID: conversation, Revision: 1}, Project: chat.ChannelProjectWidget{ConversationID: conversation, Revision: expected + 1, Milestones: []chat.ChannelProjectMilestone{m.Milestone}}}, nil
}
func TestChannelWidgetRPCBindsPrincipalAndFields(t *testing.T) {
	svc := &widgetService{}
	server := &server{deps: Dependencies{Service: svc}}
	ctx := grantContext(t, "home")
	got, err := server.GetChannelWidgets(ctx, &chatv1.GetChannelWidgetsRequest{HostTenantId: "host", ConversationId: "channel"})
	if err != nil || got.GetTeam().GetMembers()[0].GetRoleLabel() != "Lead" || svc.actor.TenantID != "home" || svc.host != "host" {
		t.Fatalf("get: %+v %+v %v", got, svc, err)
	}
	changed, err := server.MutateChannelWidget(ctx, &chatv1.MutateChannelWidgetRequest{HostTenantId: "host", ConversationId: "channel", Kind: "PROJECT", ExpectedRevision: 3, Operation: "ADD_MILESTONE", Milestone: &chatv1.ChannelProjectMilestone{Text: "Release", Status: "PLANNED", OwnerHomeTenantId: "guest", OwnerSubjectId: "worker", DueDate: "2026-10-01"}})
	if err != nil || changed.GetProject().GetRevision() != 4 || changed.GetProject().GetMilestones()[0].GetOwnerHomeTenantId() != "guest" || svc.mutation.Milestone.OwnerSubjectID != "worker" || svc.expected != 3 {
		t.Fatalf("mutate: %+v %+v %v", changed, svc, err)
	}
	if _, err := server.GetChannelWidgets(context.Background(), &chatv1.GetChannelWidgetsRequest{ConversationId: "channel"}); err == nil {
		t.Fatal("unauthenticated read succeeded")
	}
}
