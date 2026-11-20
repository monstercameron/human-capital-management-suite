package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// #3944: the poll preview says who may add answer options and lets the author
// choose; the posted poll shows the policy and says when it is full.

func TestIssue3944_PreviewOffersWhoCanAddOptions(t *testing.T) {
	m := chatcmd003UIFixture()
	m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
	local := localStore{box: &localUI{}}
	chatcmd003Follow(m, local, "chat-composer", "/poll Which time works? Morning or Afternoon")
	markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
	for _, want := range []string{`id="chatcmd003-add-label"`, "Who can add options", `data-id="add"`, `data-extra="author"`, `data-extra="members"`, "Only me", "Conversation members"} {
		if !strings.Contains(markup, want) {
			t.Errorf("preview lacks %q: %s", want, markup)
		}
	}
	if !strings.Contains(markup, `aria-checked="true" class="chatcmd003-choice selected" data-action="chatcmd003-set" data-extra="author" data-id="add"`) {
		t.Errorf("the author-only default is not the chosen option: %s", markup)
	}
	if !chatcmd003Action(m, local, "chatcmd003-set", "add", "members") || local.get().chatcmd003.Draft.Card.Poll.AddOptions != "members" {
		t.Fatalf("choosing members did not reach the card: %+v", local.get().chatcmd003.Draft.Card.Poll)
	}
	// The choice holds while the text is edited, and reaches the posted card.
	chatcmd003Follow(m, local, "chat-composer", "/poll Which time works? Morning or Afternoon or Evening")
	var sent chat.Chatcmd002Card
	m.Chatcmd002.Post = func(_, _ string, c chat.Chatcmd002Card, done func(error)) { sent = c; done(nil) }
	chatcmd003Post(m, local)
	if sent.Poll == nil || sent.Poll.AddOptions != "members" {
		t.Fatalf("posted card %+v", sent.Poll)
	}
}

func TestIssue3944_PostedPollShowsPolicyAndFullNote(t *testing.T) {
	m := chatcmd003UIFixture()
	poll := &chat.Chatcmd002Poll{Results: "always", AddOptions: "members", Options: []chat.ChannelPollOption{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}}}
	view := chat.Chatcmd002View{Card: chat.Chatcmd002Card{Kind: "poll", Title: "Q?", Poll: poll}, ResultsVisible: true}
	if markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false)); !strings.Contains(markup, "Anyone in the conversation can add options") {
		t.Fatalf("a poll members may add to does not say so: %s", markup)
	}
	poll.AddOptions = "author"
	if markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false)); strings.Contains(markup, "can add options") {
		t.Fatalf("an author-only poll shows the members policy: %s", markup)
	}
	poll.AddOptions = "members"
	poll.Options = nil
	for i := 0; i < chat.Chatcmd002MaxOptions; i++ {
		poll.Options = append(poll.Options, chat.ChannelPollOption{ID: string(rune('a' + i)), Text: strings.Repeat("x", i+1)})
	}
	if markup := renderNode(t, Chatcmd002RenderCard(m, "post-1", view, false)); !strings.Contains(markup, "No more options can be added") {
		t.Fatalf("a full poll does not say no more options can be added: %s", markup)
	}
}

func TestIssue3944_AddOptionsCopyInThreeLanguages(t *testing.T) {
	for _, key := range []string{"add-options", "add-author", "add-members", "chip-add-members", "options-full"} {
		copy, ok := chatcmd003Copy[key]
		if !ok {
			t.Fatalf("no copy for %s", key)
		}
		for i, line := range copy {
			if strings.TrimSpace(line) == "" {
				t.Errorf("%s has no text for language %d", key, i)
			}
		}
		if copy[0] == copy[1] || copy[0] == copy[2] {
			t.Errorf("%s is not translated: %v", key, copy)
		}
	}
}
