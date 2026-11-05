package application

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// chatrevStaleSettingsStore serves a stale document from Load even after Save:
// it models the read that was already in flight (or on a lagging replica) when
// an administrator saved, so the service must not let that read overwrite the
// save it already configured.
type chatrevStaleSettingsStore struct {
	load  chatstore.WritingStyleSetting
	saves int
}

func (s *chatrevStaleSettingsStore) LoadWritingStyleSetting(context.Context, string) (chatstore.WritingStyleSetting, error) {
	return s.load, nil
}

func (s *chatrevStaleSettingsStore) SaveWritingStyleSetting(_ context.Context, _ string, _ string, _ bool, _ json.RawMessage) (int64, error) {
	s.saves++
	return int64(s.saves), nil
}

type chatrevModelStub struct{}

func (chatrevModelStub) Rewrite(context.Context, chatrewrite.Prompt) (string, error) {
	return "", chatrewrite.ErrUnavailable
}

type chatrevPolicyStub struct{}

func (chatrevPolicyStub) Accept(context.Context, chatrewrite.Identity, string) (bool, error) {
	return true, nil
}

type chatrevConversationsStub struct{}

func (chatrevConversationsStub) WritingContext(context.Context, chatrewrite.Identity) (chatrewrite.ConversationFacts, []string, error) {
	return chatrewrite.ConversationFacts{}, nil, nil
}

func chatrevAdminStyles() []ChattoneAdminStyle {
	return []ChattoneAdminStyle{
		{ID: "professional", Label: "Professional", Instruction: "Use neutral, courteous language; remove heat.", Register: "professional"},
		{ID: "friendly", Label: "Friendly", Instruction: "Use warm, positive language.", Register: "friendly"},
	}
}

// An administrator's save configures the registry; a first load for the same
// workspace afterwards must not overwrite it with the older stored document.
func TestChatrev_SaveThenFirstLoadKeepsTheSave(t *testing.T) {
	old, err := json.Marshal([]ChattoneAdminStyle{
		{ID: "professional", Label: "Professional", Instruction: "Old instruction.", Register: "professional"},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &chatrevStaleSettingsStore{load: chatstore.WritingStyleSetting{Found: true, Enabled: false, Styles: old}}
	service, err := NewChattoneService(ChattoneServiceConfig{
		Model:         chatrevModelStub{},
		Policy:        chatrevPolicyStub{},
		Conversations: chatrevConversationsStub{},
		Settings:      store,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	fresh := chatrevAdminStyles()
	if err := service.saveSettings(ctx, "tenant-a", "admin", true, fresh); err != nil {
		t.Fatal(err)
	}
	if _, enabled := service.Rewrite.Registry.Styles("tenant-a"); !enabled {
		t.Fatal("save did not enable the workspace")
	}
	// The stale first load arrives after the save.
	if err := service.ensureSettings(ctx, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	styles, enabled := service.Rewrite.Registry.Styles("tenant-a")
	if !enabled {
		t.Fatal("stale first load disabled a workspace the administrator just enabled")
	}
	if len(styles) != len(fresh) || styles[0].ID != fresh[0].ID || styles[0].Instruction != fresh[0].Instruction {
		t.Fatalf("stale first load overwrote the save: %+v", styles)
	}
	if store.saves != 1 {
		t.Fatalf("saves = %d, want 1", store.saves)
	}
}
