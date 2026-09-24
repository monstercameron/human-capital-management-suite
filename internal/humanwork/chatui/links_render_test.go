package chatui

import (
	"strings"
	"testing"
)

func TestChatLinkEmbedsRenderDraftTimelineAndThread(t *testing.T) {
	link := "https://hcm.example/workspace/app/chat#share=token"
	m := Model{State: StateReady, CurrentTenantID: "tenant", CurrentUser: "reader", SelectedID: "room", EmbedOrigin: "https://hcm.example", Conversations: []Conversation{{ID: "room", Name: "General", Kind: PublicChannel}}, Draft: link, Messages: []Message{{ID: "root", Author: "Alice", Body: link}}, ThreadMessages: []Message{{ID: "reply", Author: "Bob", Body: link}}, ShowThread: true, ThreadParentID: "root", Embeds: map[string]LinkEmbed{"token": {State: "ready", SourceRoom: "source", SourcePost: "post", Channel: "Source", Author: "Dana", Body: "Authorized preview", AttachmentCount: 1}}, Callbacks: Callbacks{OpenEmbeddedMessage: func(string) {}}}
	markup := render(t, m)
	if strings.Count(markup, "Authorized preview") != 4 {
		t.Fatalf("expected draft, timeline, thread root and reply previews; got %d", strings.Count(markup, "Authorized preview"))
	}
	if !strings.Contains(markup, "Attachments: 1") || !strings.Contains(markup, "Open source channel") {
		t.Fatal("attachment count or accessible open label missing")
	}
	m.Embeds["token"] = LinkEmbed{State: "unavailable", Body: "secret"}
	markup = render(t, m)
	if strings.Contains(markup, "secret") || !strings.Contains(markup, "Message preview unavailable") {
		t.Fatal("unavailable card leaked content")
	}
	delete(m.Embeds, "token")
	if strings.Contains(render(t, m), "Loading message preview") {
		t.Fatal("uncached token rendered permanent loader")
	}
}
