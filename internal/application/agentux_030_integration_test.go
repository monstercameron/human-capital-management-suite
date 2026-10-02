package application

import (
	"strings"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_AGENTUX_030_Integration prepares the administrator's conversation
// with Policy Helper the way the local demo preparation does (the same step,
// over the real chat store) and reads it back the way the conversation list
// does for the administrator: it is named after the agent, never after the
// 36 character identifier the preparation derives its identity from, and the
// name survives the second preparation run. The agent's own side of the list
// (the agent rail) is covered by TestTodo_CHATBUG_033; the person's
// conversation with another person keeps that person's name.
func TestTodo_AGENTUX_030_Integration(t *testing.T) {
	raw, service, ctx, admin := agentUXPrepareChatFixture(t)

	// The agent's chat identity is an identifier like the one the review cell
	// showed as the conversation's title; its published name is what the
	// preparation names the conversation after.
	const agentID = "673214ec-4402-5f09-bf93-0d42e691712f"
	id, _, err := ensureLocalAgentDemoDirectConversation(ctx, raw, service, "tenant-a", admin.SubjectID, agentID, "Policy Helper")
	if err != nil {
		t.Fatal(err)
	}
	// A conversation between two people, listed beside it.
	if _, err := service.CreateConversation(ctx, chatcore.CreateConversationRequest{Principal: admin, TenantID: "tenant-a", ConversationID: "person-dm", Kind: chatcore.Direct, Name: "Priya Shah", Members: []chatcore.MemberRef{{TenantID: "tenant-a", SubjectID: "priya"}}}); err != nil {
		t.Fatal(err)
	}

	read := func() map[string]string {
		listed, err := service.ListConversations(ctx, chatcore.ListConversationsRequest{Principal: admin, TenantID: "tenant-a", Page: chatcore.Page{PageSize: 50}})
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]string{}
		for _, room := range listed.Conversations {
			names[room.ID] = room.Name
		}
		return names
	}
	names := read()
	if names[id] != "Policy Helper" {
		t.Fatalf("the list row of the prepared conversation is %q, want Policy Helper (all: %v)", names[id], names)
	}
	if strings.Contains(names[id], "-") && len(names[id]) >= 36 || names[id] == id || strings.Contains(names[id], "673214ec") {
		t.Fatalf("the conversation is named after its identifier: %q", names[id])
	}
	if names["person-dm"] != "Priya Shah" {
		t.Fatalf("a conversation with a person lost its name: %v", names)
	}
	opened, err := service.GetConversation(ctx, chatcore.GetConversationRequest{Principal: admin, TenantID: "tenant-a", ConversationID: id})
	if err != nil || opened.Name != "Policy Helper" || opened.Kind != chatcore.Direct {
		t.Fatalf("the opened conversation is %+v %v", opened, err)
	}

	// Preparing again changes nothing: same conversation, same name.
	again, _, err := ensureLocalAgentDemoDirectConversation(ctx, raw, service, "tenant-a", admin.SubjectID, agentID, "Policy Helper")
	if err != nil || again != id || read()[id] != "Policy Helper" {
		t.Fatalf("second preparation: id=%q err=%v name=%q", again, err, read()[id])
	}

	// A preparation that is given no published name makes a conversation with
	// no name of its own. The identifier is not made into one.
	const unnamed = "9f1c2d3e-0000-4a5b-8c7d-112233445566"
	bare, _, err := ensureLocalAgentDemoDirectConversation(ctx, raw, service, "tenant-a", admin.SubjectID, unnamed)
	if err != nil {
		t.Fatal(err)
	}
	if got := read()[bare]; got != "" {
		t.Fatalf("a conversation with an agent that has no published name is called %q", got)
	}
}
