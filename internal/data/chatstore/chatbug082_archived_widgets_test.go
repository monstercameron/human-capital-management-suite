package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestTodo_CHATBUG_082 reproduces the refused read. The membership check that
// every channel widget read and write shares required the channel to be
// active, so opening an archived channel was told its purpose, poll and to-do
// list did not exist, and the page said they could not be loaded. A member of
// an archived (or locked) channel reads all three; nothing in it can be changed.
func TestTodo_CHATBUG_082(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "old", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}, {MemberID: "worker", Role: "member", State: "active"}})
	ctx := context.Background()
	if _, err := s.MutateChannelWidget(ctx, "tenant-a", "tenant-a", "old", "owner", 1, ChannelWidgetMutation{Kind: "TEAM", Operation: "SET_PURPOSE", Purpose: "Scratch space for QA"}, todoAllow); err != nil {
		t.Fatal(err)
	}
	list, err := s.MutateChannelTodo(ctx, "tenant-a", "tenant-a", "old", "worker", 1, ChannelTodoMutation{Operation: "ADD", Text: "Order pizza"}, todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	poll, err := s.MutateChannelPoll(ctx, "tenant-a", "tenant-a", "old", "worker", 1, ChannelPollMutation{Operation: "CREATE", Question: "Lunch?", Options: []string{"Tacos", "Pho"}}, todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	for _, lifecycle := range []string{"ARCHIVED", "LOCKED"} {
		t.Run(lifecycle, func(t *testing.T) {
			if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
				_, err := tx.Exec(ctx, `UPDATE chat_conversation SET lifecycle=$1 WHERE tenant_id='tenant-a' AND id='old'`, lifecycle)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			for _, reader := range []string{"owner", "worker"} {
				widgets, err := s.ChannelWidgets(ctx, "tenant-a", "tenant-a", "old", reader, todoAllow)
				if err != nil || widgets.Team.Purpose != "Scratch space for QA" || widgets.Team.CanPin || widgets.Project.CanPin {
					t.Fatalf("%s reads widgets = %+v, %v", reader, widgets.Team, err)
				}
				todo, err := s.ChannelTodo(ctx, "tenant-a", "tenant-a", "old", reader, todoAllow)
				if err != nil || len(todo.Items) != 1 || todo.Items[0].Text != "Order pizza" || todo.Items[0].CanToggle || todo.Items[0].CanManageCompletionPolicy {
					t.Fatalf("%s reads the list = %+v, %v", reader, todo, err)
				}
				read, err := s.ChannelPoll(ctx, "tenant-a", "tenant-a", "old", reader, todoAllow)
				if err != nil || read.Question != "Lunch?" || len(read.Options) != 2 {
					t.Fatalf("%s reads the poll = %+v, %v", reader, read, err)
				}
			}
			// Reading is all an archived channel allows.
			if _, err := s.MutateChannelTodo(ctx, "tenant-a", "tenant-a", "old", "worker", list.Revision, ChannelTodoMutation{Operation: "ADD", Text: "Later"}, todoAllow); !errors.Is(err, chat.ErrNotFound) {
				t.Fatalf("list changed: %v", err)
			}
			if _, err := s.MutateChannelPoll(ctx, "tenant-a", "tenant-a", "old", "worker", poll.Revision, ChannelPollMutation{Operation: "VOTE", OptionID: poll.Options[0].ID}, todoAllow); !errors.Is(err, chat.ErrNotFound) {
				t.Fatalf("poll voted: %v", err)
			}
			if _, err := s.MutateChannelWidget(ctx, "tenant-a", "tenant-a", "old", "owner", 2, ChannelWidgetMutation{Kind: "TEAM", Operation: "SET_PURPOSE", Purpose: "Changed"}, todoAllow); !errors.Is(err, chat.ErrNotFound) {
				t.Fatalf("purpose changed: %v", err)
			}
			// Someone who is not a member still reads nothing.
			if _, err := s.ChannelWidgets(ctx, "tenant-a", "tenant-a", "old", "outsider", todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
				t.Fatalf("outsider read: %v", err)
			}
		})
	}
}
