package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// VoiceSwitchStore keeps the workspace, channel and personal voice switches.
type VoiceSwitchStore interface {
	PutVoiceSwitch(ctx context.Context, tenant, by, scope, scopeID string, enabled bool) error
}

// VoiceStoreAccess is the VoiceAccess of the product: it reads the current
// switches from the database and the conversation's kind from Chat, as the
// person who is asking, so a conversation they cannot open answers "not found".
type VoiceStoreAccess struct {
	Store         *chatstore.Store
	Conversations chat.ConversationService
}

func (a VoiceStoreAccess) AuthorizeVoice(ctx context.Context, p chat.Principal, tenant, conversation, _ string) (chat.VoicePolicy, chat.ConversationKind, error) {
	if a.Store == nil || a.Conversations == nil {
		return chat.VoicePolicy{}, "", chat.ErrVoiceUnavailable
	}
	c, err := a.Conversations.GetConversation(ctx, chat.GetConversationRequest{Principal: p, TenantID: tenant, ConversationID: conversation})
	if err != nil {
		return chat.VoicePolicy{}, "", err
	}
	policy, err := a.Store.VoicePolicy(ctx, tenant, p.SubjectID, conversation)
	return policy, c.Kind, err
}

// VoicePolicyView is what the composer needs: whether to offer the microphone
// and, when it is not offered, the stable reason code the page words.
//
// The view also carries the three switches as they stand and whether the person
// may change the shared ones, so the settings rows show a true state. Engine is
// sent only to a workspace administrator.
type VoicePolicyView struct {
	Allowed bool
	Reason  string
	// Workspace, Channel and Person are the switches in effect. Channel is the
	// stored channel switch (off until an administrator enables it) and only
	// means something when IsChannel is true.
	Workspace, Channel, Person bool
	IsChannel                  bool
	CanManageWorkspace         bool
	CanManageChannel           bool
	Engine                     *VoiceEngineView `json:",omitempty"`
}

// VoiceEngineInfo is what composition knows about the engine behind voice
// messages; it is logged at start-up and, through VoiceEngineView, shown on the
// workspace settings page.
type VoiceEngineInfo struct {
	// Kind is "fixture" (local development, no model is called), "outside" (a
	// model reached through the gateway) or "none".
	Kind string
	// Transcriber and Speech are the model names; Note says why there is no engine.
	Transcriber, Speech, Note string
}

// VoiceEngineView is the engine, the model and whether the workspace lets text
// and audio go to an outside service.
type VoiceEngineView struct {
	Kind, Transcriber, Speech, Note string
	OutsideAllowed, OutsideKnown    bool
}

// VoiceSwitchReader reads the workspace and personal switches without a
// conversation, for the settings pages.
type VoiceSwitchReader interface {
	VoiceSwitchValues(ctx context.Context, tenant, person string) (workspace, personal bool, err error)
}

// Policy answers for one conversation, from the same switches Send enforces.
func (s VoiceService) Policy(ctx context.Context, p chat.Principal, tenant, conversation string) (VoicePolicyView, error) {
	if p.TenantID == "" || p.SubjectID == "" || tenant == "" || conversation == "" {
		return VoicePolicyView{}, chat.ErrInvalidArgument
	}
	if s.Access == nil {
		return VoicePolicyView{Reason: "unavailable"}, nil
	}
	policy, kind, err := s.Access.AuthorizeVoice(ctx, p, tenant, conversation, "policy")
	if err != nil {
		return VoicePolicyView{}, err
	}
	view := VoicePolicyView{Workspace: policy.WorkspaceEnabled, Channel: policy.ChannelEnabled, Person: policy.PersonalEnabled, IsChannel: kind == chat.PublicChannel || kind == chat.PrivateChannel}
	view.CanManageWorkspace = s.mayChange(ctx, p, "")
	view.CanManageChannel = view.IsChannel && s.mayChange(ctx, p, conversation)
	switch {
	case !policy.WorkspaceEnabled:
		view.Reason = "workspace_off"
	case !policy.PersonalEnabled:
		view.Reason = "personal_off"
	case !policy.Allows(kind):
		view.Reason = "channel_off"
	case s.Media == nil || s.Messages == nil || s.Decoder == nil:
		view.Reason = "unavailable"
	default:
		view.Allowed = true
	}
	return view, nil
}

// Settings answers for the settings pages, which are not about one conversation:
// the workspace and personal switches, who may change the workspace's, and for
// an administrator the engine.
func (s VoiceService) Settings(ctx context.Context, p chat.Principal, tenant string) (VoicePolicyView, error) {
	if p.TenantID == "" || p.SubjectID == "" {
		return VoicePolicyView{}, chat.ErrInvalidArgument
	}
	// The workspace page does not know its tenant's id: it is the caller's own.
	if tenant == "" {
		tenant = p.TenantID
	}
	if s.SwitchValues == nil {
		return VoicePolicyView{}, chat.ErrVoiceUnavailable
	}
	workspace, personal, err := s.SwitchValues.VoiceSwitchValues(ctx, tenant, p.SubjectID)
	if err != nil {
		return VoicePolicyView{}, err
	}
	view := VoicePolicyView{Workspace: workspace, Person: personal, CanManageWorkspace: s.mayChange(ctx, p, "")}
	if view.CanManageWorkspace {
		engine := VoiceEngineView{Kind: s.Engine.Kind, Transcriber: s.Engine.Transcriber, Speech: s.Engine.Speech, Note: s.Engine.Note}
		if engine.Kind == "" {
			engine.Kind = "none"
		}
		if s.Barred != nil {
			// A setting that cannot be read is not reported as allowed.
			if barred, _, err := s.Barred.VoiceListenBarred(ctx, tenant, p.TenantID, p.SubjectID); err == nil {
				engine.OutsideKnown, engine.OutsideAllowed = true, !barred
			}
		}
		view.Engine = &engine
	}
	return view, nil
}

// mayChange is whether the person may change the shared switch of a channel, or
// of the workspace when channel is empty: the rule SetSwitch applies.
func (s VoiceService) mayChange(ctx context.Context, p chat.Principal, channel string) bool {
	return s.Admin != nil && s.Admin.AuthorizeFilters(ctx, chatfilter.Actor{Tenant: p.TenantID, Subject: p.SubjectID, Channel: channel}, channel) == nil
}

// VoiceSwitchRequest sets one switch. Scope is "workspace", "channel" or
// "person"; a person's switch is always the caller's own.
type VoiceSwitchRequest struct {
	TenantID, Scope, ConversationID string
	Enabled                         bool
}

// SetSwitch stores a switch for a caller who may change it: a person their own,
// a channel's or the workspace's administrator the shared ones, by the rule the
// message filters use.
func (s VoiceService) SetSwitch(ctx context.Context, p chat.Principal, r VoiceSwitchRequest) error {
	if r.TenantID == "" {
		r.TenantID = p.TenantID
	}
	if p.TenantID == "" || p.SubjectID == "" || r.TenantID == "" || s.Switches == nil {
		return chat.ErrInvalidArgument
	}
	scopeID := ""
	switch r.Scope {
	case chatstore.VoiceScopePerson:
		scopeID = p.SubjectID
	case chatstore.VoiceScopeChannel, chatstore.VoiceScopeWorkspace:
		if r.Scope == chatstore.VoiceScopeChannel {
			scopeID = r.ConversationID
		}
		if s.Admin == nil || (r.Scope == chatstore.VoiceScopeChannel && scopeID == "") {
			return chat.ErrPermissionDenied
		}
		if err := s.Admin.AuthorizeFilters(ctx, chatfilter.Actor{Tenant: p.TenantID, Subject: p.SubjectID, Channel: scopeID}, scopeID); err != nil {
			return chat.ErrPermissionDenied
		}
	default:
		return chat.ErrInvalidArgument
	}
	return s.Switches.PutVoiceSwitch(ctx, r.TenantID, p.SubjectID, r.Scope, scopeID, r.Enabled)
}
