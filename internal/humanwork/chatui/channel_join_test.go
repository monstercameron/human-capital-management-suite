package chatui

import (
	"strings"
	"testing"
)

func TestUnjoinedChannelPreviewAsksBeforeAddingItToTheRail(t *testing.T) {
	joined := ""
	dismissed := 0
	m := Model{
		State: StateReady, SelectedID: "onboarding",
		PreviewConversation: &Conversation{ID: "onboarding", Name: "Onboarding", Kind: PublicChannel},
		JoinPromptID:        "onboarding",
		Callbacks: Callbacks{
			JoinConversation:  func(id string) { joined = id },
			DismissJoinPrompt: func() { dismissed++ },
		},
	}
	if got := m.selected(); got.ID != "onboarding" || got.Name != "Onboarding" {
		t.Fatalf("preview selection = %+v", got)
	}
	markup := render(t, m)
	for _, want := range []string{"Join #Onboarding?", "keep it in your left sidebar", "join-confirm", "join-dismiss"} {
		if !strings.Contains(markup, want) {
			t.Errorf("join prompt missing %q", want)
		}
	}
	if len(m.Conversations) != 0 {
		t.Fatalf("unjoined preview leaked into the sidebar list: %+v", m.Conversations)
	}
	m.act("join-confirm", "onboarding")
	if joined != "onboarding" {
		t.Fatalf("confirm joined %q", joined)
	}
	m.act("join-dismiss", "onboarding")
	if dismissed != 1 {
		t.Fatalf("dismiss calls = %d", dismissed)
	}
}

func TestJoinPromptPendingDisablesBothChoices(t *testing.T) {
	m := Model{
		State: StateReady, SelectedID: "design", JoinPromptID: "design", JoinPromptPending: true,
		PreviewConversation: &Conversation{ID: "design", Name: "Design", Kind: PublicChannel},
		Callbacks:           Callbacks{JoinConversation: func(string) {}, DismissJoinPrompt: func() {}},
	}
	markup := render(t, m)
	if !strings.Contains(markup, `disabled`) || !strings.Contains(markup, "Joining…") {
		t.Fatalf("pending join controls were not rendered disabled: %s", markup)
	}
}
