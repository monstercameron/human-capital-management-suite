//go:build js && wasm

package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/testkit/render"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestChatGoToUsesLiveVisitsAndCurrentMembership(t *testing.T) {
	oldModel := chatBrowser.snapshot()
	oldScope, oldCurrent := chatVisitScope, chatVisitCurrent
	t.Cleanup(func() {
		chatBrowser.mutate(func(model *chatui.Model) { *model = oldModel })
		chatVisitScope, chatVisitCurrent = oldScope, oldCurrent
	})

	room := chatui.Conversation{ID: "incident-review", Name: "Incident Review", Kind: chatui.PublicChannel, Joined: true}
	chatBrowser.mutate(func(model *chatui.Model) {
		*model = chatui.Model{
			State: chatui.StateReady, CurrentTenantID: "tenant-id", CurrentUser: "principal-id",
			SelectedID: room.ID, Conversations: []chatui.Conversation{room},
		}
	})
	chatVisitScope, chatVisitCurrent = "", nil
	recordSelectedChatVisit("tenant-id", "principal-id", room.ID)

	// Product Views carry human-readable labels. The live provider must use the
	// authenticated IDs in the chat model, then the real shell policy must keep
	// that visited, joined destination in the rendered Go to menu.
	view := productui.NewView(productui.PageChat, "HarborCare", "Rafael Torres", "scope")
	view.Chat = chatui.Model{State: chatui.StateReady, Text: func(key string) string { return key }}
	fixture := render.New(t)
	defer fixture.Cleanup()
	fixture.Render(productui.BuildShell(view, html.Section(html.Props{ID: "chat-visit-shell"}, ui.Text("chat")), true))
	fixture.Stabilize()
	trigger := fixture.ByRole("button", "Go to")
	if trigger == nil {
		t.Fatal("chat shell did not render its contextual Go to trigger")
	}
	trigger.Click()
	fixture.Stabilize()
	channel := fixture.ByRole("link", "Incident Review")
	if channel == nil || channel.Attr("href") != chatui.ChannelReferenceURL(room.ID) {
		t.Fatalf("live Go to menu omitted the visited joined channel or used a noncanonical link: %#v; text=%q", channel, fixture.Text())
	}
	if channelPosition, helpPosition := strings.Index(fixture.Text(), "Incident Review"), strings.Index(fixture.Text(), "Help"); helpPosition >= 0 && channelPosition > helpPosition {
		t.Fatalf("generic destination ranked ahead of frequent channel: %q", fixture.Text())
	}

	// A different viewer has a separate visit scope even if the same joined
	// channel is present in their model.
	chatBrowser.mutate(func(model *chatui.Model) { model.CurrentUser = "another-principal" })
	if visits := currentChatLauncherVisits(); len(visits) != 1 || visits[0].Count != 0 {
		t.Fatalf("visit count crossed viewer scope: %#v", visits)
	}
	fixture.Rerender(productui.BuildShell(view, html.Section(html.Props{ID: "chat-visit-shell"}, ui.Text("chat")), true))
	fixture.Stabilize()
	if channel := fixture.ByRole("link", "Incident Review"); channel != nil {
		t.Fatal("Go to menu exposed another viewer's recorded channel visit")
	}

	// Membership removal immediately removes the destination, even though the
	// previous viewer's session still has a recorded count for its ID.
	chatBrowser.mutate(func(model *chatui.Model) {
		model.CurrentUser = "principal-id"
		model.Conversations[0].Joined = false
	})
	if visits := currentChatLauncherVisits(); len(visits) != 0 {
		t.Fatalf("Go to provider retained a channel after membership removal: %#v", visits)
	}
	fixture.Rerender(productui.BuildShell(view, html.Section(html.Props{ID: "chat-visit-shell"}, ui.Text("chat")), true))
	fixture.Stabilize()
	if channel := fixture.ByRole("link", "Incident Review"); channel != nil {
		t.Fatal("shell kept a channel shortcut after the live membership projection removed it")
	}
}
