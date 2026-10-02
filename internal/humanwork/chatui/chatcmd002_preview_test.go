package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_CHATCMD_002_PreviewScroll: Post and Cancel are not part of what
// scrolls. The settings, the changes and the card are in the body, which is the
// one scrolling box; the actions follow it, and the stylesheet gives the body a
// bounded height, a shadow at its top edge and a fade above the actions.
func TestTodo_CHATCMD_002_PreviewScroll(t *testing.T) {
	m := chatcmd003UIFixture()
	m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
	local := localStore{box: &localUI{}}
	if !chatcmd003Follow(m, local, "chat-composer", `/poll Where for lunch? tacos, pho or pizza`) {
		t.Fatal("no preview")
	}
	markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
	body := strings.Index(markup, `class="chatcmd003-body"`)
	actions := strings.Index(markup, `class="chatcmd003-actions"`)
	settings := strings.Index(markup, "chatcmd003-settings")
	if body < 0 || actions < body || settings < body || settings > actions {
		t.Fatalf("settings in the body, actions after it: body %d settings %d actions %d\n%s", body, settings, actions, markup)
	}
	for _, key := range []string{`id="chatcmd003-post"`, `data-action="chatcmd003-cancel"`} {
		if at := strings.Index(markup, key); at < actions {
			t.Errorf("%s is inside the scrolled body", key)
		}
	}
	for _, want := range []string{
		".chatcmd003-preview{display:flex;flex-direction:column",
		".chatcmd003-body{display:grid;align-content:start;gap:8px;flex:1 1 auto;min-height:0",
		"overflow-y:auto",
		"no-repeat local",
		"@keyframes chatcmd002-preview-more",
		"animation-timeline:--chatcmd002-preview",
		".chatcmd003-actions{position:relative",
	} {
		if !strings.Contains(chatcmd002PreviewStyles, want) {
			t.Errorf("preview styles lack %s", want)
		}
	}
	if !strings.Contains(chatcmd003Styles, "max-height:min(46vh,440px)") || !strings.Contains(Stylesheet, ChatLane2Styles) || !strings.Contains(ChatLane2Styles, chatcmd002PreviewStyles) {
		t.Fatal("the preview keeps its bounded height and the new rules are in the stylesheet")
	}
	if got := s31LaterOverrides(t, chatcmd002PreviewStyles); len(got) != 0 {
		t.Fatalf("a later block redraws the preview rules: %v", got)
	}
}
