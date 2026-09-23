package chatui

import (
	"strings"
	"testing"
)

func TestTodo_CHAT_030_SharePicker(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "source", Conversations: []Conversation{{ID: "source", Name: "Source"}}, Messages: []Message{{ID: "post", Author: "Ari", Body: "Review this", Attachments: []Attachment{{ID: "image"}}}, {ID: "media", Author: "Ari", Attachments: []Attachment{{ID: "image"}}}}, MenuID: "post", SharePostID: "post", ShareSource: &Message{ID: "post", Author: "Ari", Body: "Review this", Attachments: []Attachment{{ID: "image"}}}, ShareDestinations: []Conversation{{ID: "public", Name: "General", Kind: PublicChannel}, {ID: "private", Name: "Managers", Kind: PrivateChannel}}, ShareDestinationID: "private", Callbacks: Callbacks{OpenMenu: func(string) {}, OpenShare: func(string) {}, CloseShare: func() {}, SelectShareDestination: func(string) {}, ShareMessage: func() {}}}
	markup := render(t, m)
	if !strings.Contains(markup, `class="share-notices"`) || strings.Index(markup, `class="share-notices"`) < strings.Index(markup, `class="share-preview"`) {
		t.Fatal("attachment disclosure must follow scrollable preview")
	}
	for _, want := range []string{`data-action="open-share" data-id="post"`, `role="dialog"`, `Share message to a channel`, `Review this`, `Attachments are not included`, `data-action="share-destination" data-id="public"`, `data-action="share-destination" data-id="private"`, `aria-pressed="true"`, `data-action="share-submit"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
	}
	m.MenuID, m.SharePostID = "media", ""
	markup = render(t, m)
	if !strings.Contains(markup, `data-action="open-share" data-id="media" disabled`) {
		t.Fatal("media-only share was enabled")
	}
}

func TestTodo_CHAT_030_ShareDispatch(t *testing.T) {
	calls := []string{}
	m := Model{Callbacks: Callbacks{OpenShare: func(id string) { calls = append(calls, "open:"+id) }, CloseShare: func() { calls = append(calls, "close") }, SelectShareDestination: func(id string) { calls = append(calls, "dest:"+id) }, ShareMessage: func() { calls = append(calls, "share") }}}
	m.act("open-share", "post")
	m.act("share-destination", "dest")
	m.act("share-submit", "")
	m.act("close-share", "")
	if strings.Join(calls, ",") != "open:post,dest:dest,share,close" {
		t.Fatalf("calls = %v", calls)
	}
}
