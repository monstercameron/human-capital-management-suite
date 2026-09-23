//go:build !(js && wasm)

package chatui

import (
	"strings"
	"testing"
)

func TestChatChannelReferenceAppearsInTimelineThreadAndDraft(t *testing.T) {
	link := "https://hcm.example/workspace/app/chat#channel=room"
	body := "#Q4_hiring_huddle " + link
	root := Message{ID: "root", Body: body}
	m := Model{State: StateReady, SelectedID: "room", EmbedOrigin: "https://hcm.example", Conversations: []Conversation{{ID: "room", Name: "Q4 hiring huddle", Kind: PublicChannel, Joined: true}}, Messages: []Message{root}, ShowThread: true, ThreadParentID: root.ID, ThreadParent: &root, ThreadMessages: []Message{{ID: "reply", Body: body}}, Draft: body}
	markup := render(t, m)
	if got := strings.Count(markup, `data-action="open-channel-reference" data-id="room"`); got != 4 {
		t.Fatalf("linked reference count = %d, want timeline, thread root, reply, and draft", got)
	}
	if strings.Contains(markup, `#Q4_hiring_huddle `+link+`</p>`) {
		t.Fatal("posted reference stayed plain text")
	}
	m.Conversations[0].Joined = false
	markup = render(t, m)
	if strings.Contains(markup, `data-action="open-channel-reference"`) {
		t.Fatal("unadmitted room bypassed the authorized deep-link receiver")
	}
	if got := strings.Count(markup, `href="/workspace/app/chat#channel=room"`); got != 4 || !strings.Contains(markup, "#channel") {
		t.Fatalf("unadmitted room lost its generic canonical links: count=%d", got)
	}
}
