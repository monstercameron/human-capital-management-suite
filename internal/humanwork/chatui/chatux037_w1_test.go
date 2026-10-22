package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATUX_037_SharedLoaderEverywhere holds the surfaces that moved onto
// the shared loading component after the first pass: the first paint of the
// message list offers Try again once it has taken long, a message preview and the
// message-link menu draw placeholder rows with the status read out once, and
// Saved shows no count it has not read.
func TestTodo_CHATUX_037_SharedLoaderEverywhere(t *testing.T) {
	// The message list: the five-second line carries the shared Try again, wired to
	// the retry action, and has no button where the page has no retry to run.
	m := chatux009LoadingFixture("en-US")
	m.Callbacks.Retry = func() {}
	list := renderNode(t, chatux009LoadingBody(m)[0])
	for _, want := range []string{"Still loading this conversation…", `data-action="retry"`, ">Try again<", "chatux037-retry"} {
		if !strings.Contains(list, want) {
			t.Errorf("message list loading lacks %q: %s", want, list)
		}
	}
	m.Callbacks.Retry = nil
	if list = renderNode(t, chatux009LoadingBody(m)[0]); strings.Contains(list, "Try again") {
		t.Errorf("a Try again with nothing to run: %s", list)
	}
	chat4Require(t, ChatUX009Styles, ".chatux009-slow{display:flex", "visibility:hidden")

	// The message-link menu while its suggestions are read.
	menu := renderNode(t, docSuggestMenu(Model{Locale: "en-US"}, docSuggestState{Open: true, Loading: true, Target: "chat-composer"}, "chat-composer"))
	if !strings.Contains(menu, `data-chat-loading="menu"`) || !strings.Contains(menu, "Loading message preview…") || strings.Contains(menu, `class="mention-empty"`) {
		t.Errorf("link menu loading: %s", menu)
	}

	// A message preview that has not been read: placeholder rows, the sentence only
	// as the status.
	card := render(t, Model{State: StateReady, CurrentTenantID: "tenant", CurrentUser: "reader", SelectedID: "room", EmbedOrigin: "https://hcm.example",
		Conversations: []Conversation{{ID: "room", Name: "General", Kind: PublicChannel}},
		Messages:      []Message{{ID: "root", Author: "Alice", Body: "https://hcm.example/workspace/app/chat#share=token"}},
		Embeds:        map[string]LinkEmbed{"token": {State: "loading"}}})
	if !strings.Contains(card, `data-chat-loading="section"`) || !strings.Contains(card, `role="status">Loading message preview…</span>`) {
		t.Errorf("message preview loading: %s", card)
	}

	// Saved: Open, Done and All show no count while the list is read.
	view := SavedMessagesView{Locale: "en-US", Loading: true}
	if loading := renderNode(t, chatsave002Render(view)); strings.Contains(loading, "chatsave-seg-count") {
		t.Errorf("saved counts while loading: %s", loading)
	}
	view.Loading = false
	if loaded := renderNode(t, chatsave002Render(view)); strings.Count(loaded, "chatsave-seg-count") != 3 {
		t.Errorf("saved counts after loading: %s", loaded)
	}

	// The page that names Moderation or Saved in its address keeps the last open
	// conversation out of sight until the page is open.
	chat4Require(t, ChatUX037Styles, `html[data-chat-page-pending] .chat-main{visibility:hidden}`)
}
