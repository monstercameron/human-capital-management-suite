package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestChannelReferencesAcceptCanonicalSameOriginOnly(t *testing.T) {
	origin := "https://hcm.example"
	body := "#Q4_hiring_huddle https://hcm.example/workspace/app/chat#channel=room-42."
	refs := ChannelReferences(body, origin)
	if len(refs) != 1 || refs[0].ID != "room-42" || body[refs[0].Start:refs[0].End] != "https://hcm.example/workspace/app/chat#channel=room-42" {
		t.Fatalf("canonical reference = %#v", refs)
	}
	for _, bad := range []string{
		"#Q4_hiring_huddle", "https://evil.example/workspace/app/chat#channel=room-42",
		"https://hcm.example.evil.test/workspace/app/chat#channel=room-42",
		"//evil.example/workspace/app/chat#channel=room-42",
		"javascript:alert(1)", "/workspace/app/chat?room=room-42#channel=room-42",
		"/workspace/app/chat#channel=%3Cscript%3E", "/workspace/app/chat#channel=room-42&other=1",
	} {
		if got := ChannelReferences(bad, origin); len(got) != 0 {
			t.Errorf("accepted %q: %#v", bad, got)
		}
	}
	if got := ChannelReferenceLabel("  Q4  hiring\thuddle "); got != "#Q4_hiring_huddle" {
		t.Fatalf("copy label = %q", got)
	}
	complexID := "team/blue room"
	if got := ChannelReferences(ChannelReferenceURL(complexID), origin); len(got) != 1 || got[0].ID != complexID {
		t.Fatalf("copied escaped ID did not round-trip: %#v", got)
	}
}

func TestChannelReferenceBodyLinksOnlyAdmittedRoomWithoutDuplicateLabel(t *testing.T) {
	m := Model{EmbedOrigin: "https://hcm.example", Conversations: []Conversation{{ID: "room-42", Name: "Q4 hiring huddle", Joined: true}}}
	body := "Ask #Q4_hiring_huddle https://hcm.example/workspace/app/chat#channel=room-42 today."
	markup, err := ui.RenderToString(html.P(html.Props{}, channelReferenceBody(m, body)...))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(markup, "#Q4_hiring_huddle") != 1 || !strings.Contains(markup, `href="/workspace/app/chat#channel=room-42"`) || strings.Contains(markup, "https://hcm.example/workspace/app/chat#channel=room-42 today") {
		t.Fatalf("linked body = %s", markup)
	}
	m.Conversations = nil
	markup, err = ui.RenderToString(html.P(html.Props{}, channelReferenceBody(m, body)...))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `href="/workspace/app/chat#channel=room-42"`) || !strings.Contains(markup, "#channel") || strings.Contains(markup, "#Q4_hiring_huddle") || strings.Contains(markup, `data-action="open-channel-reference"`) {
		t.Fatalf("unknown room was not a generic authorized-lookup link: %s", markup)
	}
}
