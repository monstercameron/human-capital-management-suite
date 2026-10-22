package application

import (
	"context"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// The workspace and conversation switches behind an agent's reaction
// (AGENTUX-052), served to the settings pages the way the voice switches are:
// one read per conversation, one for the workspace page, and one write that
// only an administrator of that scope may make, by the rule the message
// filters use.

// AgentReactionScopeWorkspace and AgentReactionScopeChannel are the two scopes
// a switch write names.
const (
	AgentReactionScopeWorkspace = "workspace"
	AgentReactionScopeChannel   = "channel"
)

// AgentReactionPolicyView is what the settings pages learn: the switches in
// effect and whether the caller may change the shared ones.
type AgentReactionPolicyView struct {
	Workspace, Channel bool
	IsChannel          bool
	CanManageWorkspace bool
	CanManageChannel   bool
}

// AgentReactionService answers the settings pages and stores an
// administrator's switches. Conversations proves membership and kind for a
// conversation read; Admin decides who may change a workspace's or a channel's
// switch; Store holds the rows.
type AgentReactionService struct {
	Store         chatcore.AgentReactionPolicyStore
	Admin         chatfilter.Authority
	Conversations chatcore.ConversationService
}

// Policy answers for one conversation: its switch and the workspace's, and
// whether the caller administers either scope.
func (s AgentReactionService) Policy(ctx context.Context, p chatcore.Principal, tenant, conversation string) (AgentReactionPolicyView, error) {
	if p.TenantID == "" || p.SubjectID == "" || tenant == "" || conversation == "" || p.TenantID != tenant {
		return AgentReactionPolicyView{}, chatcore.ErrInvalidArgument
	}
	if s.Store == nil || s.Conversations == nil {
		return AgentReactionPolicyView{}, chatcore.ErrUnavailable
	}
	c, err := s.Conversations.GetConversation(ctx, chatcore.GetConversationRequest{Principal: p, TenantID: tenant, ConversationID: conversation})
	if err != nil {
		return AgentReactionPolicyView{}, err
	}
	workspace, channel, err := s.Store.ReadAgentReactionPolicy(ctx, tenant, conversation)
	if err != nil {
		return AgentReactionPolicyView{}, err
	}
	view := AgentReactionPolicyView{Workspace: workspace.ReactionsEnabled, IsChannel: c.Kind == chatcore.PublicChannel || c.Kind == chatcore.PrivateChannel}
	if view.IsChannel {
		view.Channel = channel == nil || channel.ReactionsEnabled
	} else {
		view.Channel = workspace.ReactionsEnabled
	}
	view.CanManageWorkspace = s.mayChange(ctx, p, "")
	view.CanManageChannel = view.IsChannel && s.mayChange(ctx, p, conversation)
	return view, nil
}

// Settings answers for the workspace page: the workspace switch and whether
// the caller may change it.
func (s AgentReactionService) Settings(ctx context.Context, p chatcore.Principal, tenant string) (AgentReactionPolicyView, error) {
	if p.TenantID == "" || p.SubjectID == "" {
		return AgentReactionPolicyView{}, chatcore.ErrInvalidArgument
	}
	// The workspace page does not know its tenant's id: it is the caller's own.
	if tenant == "" {
		tenant = p.TenantID
	}
	if p.TenantID != tenant {
		return AgentReactionPolicyView{}, chatcore.ErrInvalidArgument
	}
	if s.Store == nil {
		return AgentReactionPolicyView{}, chatcore.ErrUnavailable
	}
	workspace, _, err := s.Store.ReadAgentReactionPolicy(ctx, tenant, "")
	if err != nil {
		return AgentReactionPolicyView{}, err
	}
	return AgentReactionPolicyView{Workspace: workspace.ReactionsEnabled, CanManageWorkspace: s.mayChange(ctx, p, "")}, nil
}

// mayChange is whether the person may change the shared switch of a channel,
// or of the workspace when channel is empty: the rule SetSwitch applies.
func (s AgentReactionService) mayChange(ctx context.Context, p chatcore.Principal, channel string) bool {
	return s.Admin != nil && s.Admin.AuthorizeFilters(ctx, chatfilter.Actor{Tenant: p.TenantID, Subject: p.SubjectID, Channel: channel}, channel) == nil
}

// AgentReactionSwitchRequest sets one switch. Scope is "workspace" or
// "channel"; a channel switch names its conversation.
type AgentReactionSwitchRequest struct {
	TenantID, Scope, ConversationID string
	Enabled                         bool
}

// SetSwitch stores a switch for a caller who may change it: a channel's or the
// workspace's administrator, by the rule the message filters use.
func (s AgentReactionService) SetSwitch(ctx context.Context, p chatcore.Principal, r AgentReactionSwitchRequest) error {
	if r.TenantID == "" {
		r.TenantID = p.TenantID
	}
	if p.TenantID == "" || p.SubjectID == "" || r.TenantID == "" || p.TenantID != r.TenantID || s.Store == nil {
		return chatcore.ErrInvalidArgument
	}
	conversation := ""
	switch r.Scope {
	case AgentReactionScopeWorkspace:
	case AgentReactionScopeChannel:
		conversation = r.ConversationID
		if conversation == "" {
			return chatcore.ErrInvalidArgument
		}
	default:
		return chatcore.ErrInvalidArgument
	}
	if s.Admin == nil {
		return chatcore.ErrPermissionDenied
	}
	if err := s.Admin.AuthorizeFilters(ctx, chatfilter.Actor{Tenant: p.TenantID, Subject: p.SubjectID, Channel: conversation}, conversation); err != nil {
		return chatcore.ErrPermissionDenied
	}
	return s.Store.WriteAgentReactionPolicy(ctx, r.TenantID, conversation, chatcore.AgentReactionPolicy{ReactionsEnabled: r.Enabled}, p.SubjectID)
}
