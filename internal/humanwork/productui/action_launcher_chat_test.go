package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestChatGoToRanksVisitedJoinedConversationsAndRejectsStaleRooms(t *testing.T) {
	view := NewView(PageChat, "tenant-chat-go", "viewer-chat-go", "scope-chat-go")
	view.Chat.Text = func(key string) string { return key }
	view.Chat.Conversations = []chatui.Conversation{
		{ID: "room-frequent", Name: "Planning", Kind: chatui.PublicChannel, Joined: true},
		{ID: "room-once", Name: "Announcements", Kind: chatui.PrivateChannel, Joined: true},
		{ID: "room-unjoined", Name: "Browse only", Kind: chatui.PublicChannel, Joined: false},
		{ID: "room-stale", Name: "Removed", Kind: chatui.PublicChannel, Joined: false},
		{ID: "room-frequent", Name: "Duplicate", Kind: chatui.PublicChannel, Joined: true},
	}
	counts := map[string]int{"room-frequent": 9, "room-once": 2, "room-unjoined": 99, "room-stale": 15}
	visits := chatLauncherVisits(view.Chat.Conversations, counts)
	items := chatLauncherItems(view, visits)
	if len(items) != 2 {
		t.Fatalf("chat launcher item count = %d, want only two visited joined conversations: %#v", len(items), items)
	}
	ranked := RankActionLauncherItems(items, "", actionLauncherInitialLimit)
	if len(ranked) != 2 || ranked[0].ID != "chat:room-frequent" || ranked[1].ID != "chat:room-once" {
		t.Fatalf("chat launcher order = %#v, want frequent then once", ranked)
	}
	if ranked[0].Href != chatui.ChannelReferenceURL("room-frequent") || ranked[0].Label != "Planning" || ranked[0].Description != chatui.KeyKindPublic {
		t.Fatalf("chat destination lost canonical link or accessible context: %#v", ranked[0])
	}

	// Membership is checked again by shell policy. Even a well-formed
	// canonical hash cannot keep a room after it disappears from the current
	// authorized view.
	view.Chat.Conversations = []chatui.Conversation{{ID: "room-once", Name: "Announcements", Kind: chatui.PrivateChannel, Joined: true}}
	filtered := authorizedActionLauncherItems(view, items)
	if len(filtered) != 1 || filtered[0].ID != "chat:room-once" {
		t.Fatalf("stale conversation survived the current view policy: %#v", filtered)
	}
}

func TestChatGoToOmitsUnvisitedAndUnjoinedConversations(t *testing.T) {
	view := NewView(PageChat, "tenant-chat-go", "viewer-chat-go", "scope-chat-go")
	view.Chat.Conversations = []chatui.Conversation{
		{ID: "joined-unvisited", Name: "Quiet", Joined: true},
		{ID: "not-joined", Name: "Browse", Joined: false},
	}
	if got := chatLauncherItems(view, chatLauncherVisits(view.Chat.Conversations, nil)); len(got) != 0 {
		t.Fatalf("unvisited conversations appeared in Go to: %#v", got)
	}
}

func TestChatGoToUsesContextualLabelAndShowsRecordedDestination(t *testing.T) {
	oldProvider := ActionLauncherChatVisitsProvider
	ActionLauncherChatVisitsProvider = func() []ActionLauncherChatVisit {
		return []ActionLauncherChatVisit{{Conversation: chatui.Conversation{
			ID: "incident-review", Name: "Incident Review", Kind: chatui.PublicChannel, Joined: true,
		}, Count: 7}}
	}
	t.Cleanup(func() { ActionLauncherChatVisitsProvider = oldProvider })

	view := testView(PageChat)
	view.Chat.Text = func(key string) string { return key }
	view.Chat.Conversations = []chatui.Conversation{{ID: "incident-review", Name: "Incident Review", Kind: chatui.PublicChannel, Joined: true}}
	props := actionLauncherProps(view)
	props.Items = authorizedActionLauncherItems(view, props.Items)
	props.InitialQuery = "incident"
	for _, item := range props.Items {
		if item.Kind == ActionLauncherAction {
			t.Fatalf("chat Go to retained unrelated action %q", item.ID)
		}
	}

	markup, err := ui.RenderToString(ui.CreateElement(ActionLauncher, props))
	if err != nil {
		t.Fatalf("render chat Go to: %v", err)
	}
	for _, want := range []string{`aria-label="Jump to"`, `Incident Review`, `/workspace/app/chat#channel=incident-review`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("chat Go to markup missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Promote an employee") || strings.Contains(markup, "Browse employees") {
		t.Fatalf("chat Go to included generic actions: %s", markup)
	}
}
