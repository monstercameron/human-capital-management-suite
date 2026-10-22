package application

import (
	"context"
	"errors"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

type agentux052PolicyStore struct {
	rows map[string]chatcore.AgentReactionPolicy
	err  error
}

func (s agentux052PolicyStore) ReadAgentReactionPolicy(_ context.Context, _, conversation string) (chatcore.AgentReactionPolicy, *chatcore.AgentReactionPolicy, error) {
	if s.err != nil {
		return chatcore.AgentReactionPolicy{}, nil, s.err
	}
	workspace := chatcore.DefaultAgentReactionPolicy()
	if p, ok := s.rows[""]; ok {
		workspace = p
	}
	if conversation == "" {
		return workspace, nil, nil
	}
	if p, ok := s.rows[conversation]; ok {
		channel := p
		return workspace, &channel, nil
	}
	return workspace, nil, nil
}

func (s agentux052PolicyStore) WriteAgentReactionPolicy(_ context.Context, _, conversation string, p chatcore.AgentReactionPolicy, _ string) error {
	if s.err != nil {
		return s.err
	}
	if s.rows == nil {
		s.rows = map[string]chatcore.AgentReactionPolicy{}
	}
	s.rows[conversation] = p
	return nil
}

type agentux052FilterAdmin struct {
	allow map[string]bool
}

func (a agentux052FilterAdmin) AuthorizeFilters(_ context.Context, actor chatfilter.Actor, _ string) error {
	if a.allow[actor.Channel] {
		return nil
	}
	return chatcore.ErrPermissionDenied
}

func (a agentux052FilterAdmin) CanReadFilterConversation(context.Context, chatfilter.Actor, string) bool {
	return true
}

type agentux052Conversations struct {
	chatcore.ConversationService
	kinds map[string]chatcore.ConversationKind
}

func (c agentux052Conversations) GetConversation(_ context.Context, r chatcore.GetConversationRequest) (chatcore.Conversation, error) {
	kind, ok := c.kinds[r.ConversationID]
	if !ok {
		return chatcore.Conversation{}, chatcore.ErrNotFound
	}
	return chatcore.Conversation{ID: r.ConversationID, TenantID: r.TenantID, Kind: kind, Revision: 1}, nil
}

func agentux052Service(rows map[string]chatcore.AgentReactionPolicy, allow map[string]bool) (AgentReactionService, agentux052PolicyStore) {
	if rows == nil {
		rows = map[string]chatcore.AgentReactionPolicy{}
	}
	store := agentux052PolicyStore{rows: rows}
	return AgentReactionService{Store: store, Admin: agentux052FilterAdmin{allow: allow}, Conversations: agentux052Conversations{kinds: map[string]chatcore.ConversationKind{"room": chatcore.PublicChannel, "dm": chatcore.Direct}}}, store
}

// The settings pages' reads: a conversation read proves membership and reports
// both switches with who may change them; a direct conversation mirrors the
// workspace switch and offers no channel switch; the workspace read answers the
// workspace switch for the caller's own tenant.
func TestAgentUX052_PolicyReads(t *testing.T) {
	ctx := context.Background()
	alice := chatcore.Principal{TenantID: "t1", SubjectID: "alice"}
	admin := map[string]bool{"": true, "room": true}
	service, _ := agentux052Service(map[string]chatcore.AgentReactionPolicy{"": {ReactionsEnabled: true}, "room": {ReactionsEnabled: false}}, admin)
	view, err := service.Policy(ctx, alice, "t1", "room")
	if err != nil || !view.Workspace || view.Channel || !view.IsChannel || !view.CanManageWorkspace || !view.CanManageChannel {
		t.Fatalf("channel read: %+v %v", view, err)
	}
	view, err = service.Policy(ctx, alice, "t1", "dm")
	if err != nil || !view.Workspace || !view.Channel || view.IsChannel || !view.CanManageWorkspace || view.CanManageChannel {
		t.Fatalf("direct read: %+v %v", view, err)
	}
	view, err = service.Settings(ctx, alice, "")
	if err != nil || !view.Workspace || !view.CanManageWorkspace {
		t.Fatalf("workspace read: %+v %v", view, err)
	}
	// A caller who administers nothing learns the switches but no mandate.
	service, _ = agentux052Service(nil, nil)
	view, err = service.Policy(ctx, alice, "t1", "room")
	if err != nil || !view.Workspace || !view.Channel || view.CanManageWorkspace || view.CanManageChannel {
		t.Fatalf("unprivileged read: %+v %v", view, err)
	}
	// Outsiders, foreign tenants and missing stores get no answers.
	if _, err = service.Policy(ctx, alice, "t1", "no-such-room"); err == nil {
		t.Error("a conversation the caller is not in was read")
	}
	if _, err = service.Policy(ctx, alice, "t2", "room"); err == nil {
		t.Error("a foreign tenant was read")
	}
	if _, err = service.Settings(ctx, alice, "t2"); err == nil {
		t.Error("a foreign workspace was read")
	}
	bare := AgentReactionService{}
	if _, err = bare.Policy(ctx, alice, "t1", "room"); !errors.Is(err, chatcore.ErrUnavailable) {
		t.Errorf("an uncomposed conversation read: %v", err)
	}
	if _, err = bare.Settings(ctx, alice, ""); !errors.Is(err, chatcore.ErrUnavailable) {
		t.Errorf("an uncomposed workspace read: %v", err)
	}
}

// The switch write: a channel's or the workspace's administrator stores it by
// the rule the message filters use; anyone else is refused, and the write
// names its scope and stays inside its tenant.
func TestAgentUX052_SwitchWrites(t *testing.T) {
	ctx := context.Background()
	alice := chatcore.Principal{TenantID: "t1", SubjectID: "alice"}
	service, store := agentux052Service(nil, map[string]bool{"": true})
	if err := service.SetSwitch(ctx, alice, AgentReactionSwitchRequest{Scope: AgentReactionScopeWorkspace, Enabled: false}); err != nil {
		t.Fatalf("the workspace administrator's write: %v", err)
	}
	if p, ok := store.rows[""]; !ok || p.ReactionsEnabled {
		t.Fatalf("the workspace row: %+v %v", p, ok)
	}
	if err := service.SetSwitch(ctx, alice, AgentReactionSwitchRequest{Scope: AgentReactionScopeChannel, ConversationID: "room", Enabled: false}); err == nil {
		t.Error("a channel the caller does not administer was switched")
	}
	service, store = agentux052Service(nil, map[string]bool{"room": true})
	if err := service.SetSwitch(ctx, alice, AgentReactionSwitchRequest{Scope: AgentReactionScopeChannel, ConversationID: "room", Enabled: false}); err != nil {
		t.Fatalf("the channel administrator's write: %v", err)
	}
	if p, ok := store.rows["room"]; !ok || p.ReactionsEnabled {
		t.Fatalf("the channel row: %+v %v", p, ok)
	}
	for name, request := range map[string]AgentReactionSwitchRequest{
		"unknown scope":            {Scope: "person", Enabled: true},
		"channel without a room":   {Scope: AgentReactionScopeChannel, Enabled: true},
		"foreign tenant":           {TenantID: "t2", Scope: AgentReactionScopeWorkspace, Enabled: true},
		"empty scope and room off": {Scope: AgentReactionScopeChannel, ConversationID: "", Enabled: false},
	} {
		if err := service.SetSwitch(ctx, alice, request); err == nil {
			t.Errorf("%s was stored", name)
		}
	}
	service, _ = agentux052Service(nil, nil)
	if err := service.SetSwitch(ctx, alice, AgentReactionSwitchRequest{Scope: AgentReactionScopeWorkspace, Enabled: false}); !errors.Is(err, chatcore.ErrPermissionDenied) {
		t.Errorf("an unprivileged write: %v", err)
	}
	bare := AgentReactionService{Conversations: agentux052Conversations{}}
	if err := bare.SetSwitch(ctx, alice, AgentReactionSwitchRequest{Scope: AgentReactionScopeWorkspace, Enabled: false}); !errors.Is(err, chatcore.ErrInvalidArgument) {
		t.Errorf("a write without storage: %v", err)
	}
}
