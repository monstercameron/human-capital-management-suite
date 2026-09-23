package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderMarkdownBody(t *testing.T, m Model, body string) string {
	t.Helper()
	markup, err := ui.RenderToString(html.Div(html.Props{Class: "message-body"}, markdownMessageBody(m, body)...))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestChatMarkdownRendersAccessibleStructure(t *testing.T) {
	body := "**Strong** and *emphasis* with `code` and [guide](https://example.org/guide).\nNext line\n\n- First\n- Second\n\n3. One\n4. Two\n\n> Quote\n\n```go\nfmt.Println(\"hi\")\n```"
	markup := renderMarkdownBody(t, Model{}, body)
	for _, want := range []string{
		"<strong>Strong</strong>", "<em>emphasis</em>", "<code>code</code>",
		`href="https://example.org/guide"`, "<br", "<ul", "<ol", `start="3"`, "<li", "<blockquote", "<pre", "fmt.Println",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("Markdown output missing %q: %s", want, markup)
		}
	}
}

func TestChatMarkdownRejectsActiveHTMLAndUnsafeLinks(t *testing.T) {
	body := "<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n\n" +
		"[bad](javascript:alert%281%29) [data](data:text/html,evil) [local](/workspace/app/chat) " +
		"[external](https://example.org/path) ![remote image](https://tracker.example/pixel.png)"
	markup := renderMarkdownBody(t, Model{}, body)
	for _, forbidden := range []string{"<script", "<img", `href="javascript:`, `href="data:`, "tracker.example/pixel.png", "<iframe"} {
		if strings.Contains(markup, forbidden) {
			t.Errorf("unsafe output contains %q: %s", forbidden, markup)
		}
	}
	for _, want := range []string{"&lt;script&gt;", "bad", "data", "remote image", `href="/workspace/app/chat"`, `href="https://example.org/path"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("safe output missing %q: %s", want, markup)
		}
	}
	for _, raw := range []string{"javascript:alert(1)", "data:text/html,evil", "//evil.example/path", "/%2Fevil.example/path", "http:evil", "https://user@example.org/"} {
		if _, ok := safeMarkdownHref(raw); ok {
			t.Errorf("accepted unsafe href %q", raw)
		}
	}
}

func TestChatMarkdownPreservesAdmittedChannelReferences(t *testing.T) {
	m := Model{EmbedOrigin: "https://hcm.example", Conversations: []Conversation{{ID: "room-42", Name: "Q4 hiring huddle", Joined: true}}}
	body := "Ask #Q4_hiring_huddle https://hcm.example/workspace/app/chat#channel=room-42 today."
	markup := renderMarkdownBody(t, m, body)
	if strings.Count(markup, "#Q4_hiring_huddle") != 1 || !strings.Contains(markup, `data-action="open-channel-reference"`) {
		t.Fatalf("admitted reference = %s", markup)
	}
	m.Conversations = nil
	markup = renderMarkdownBody(t, m, body)
	if strings.Contains(markup, "#Q4_hiring_huddle") || !strings.Contains(markup, "#channel") || strings.Contains(markup, `data-action="open-channel-reference"`) {
		t.Fatalf("unadmitted reference = %s", markup)
	}
}

func TestChatMarkdownRendersTimelineAndThreadBodies(t *testing.T) {
	root := Message{ID: "root", Author: "Alice", Body: "**Important** [guide](https://example.org/guide)"}
	m := Model{
		State: StateReady, SelectedID: "room", ShowThread: true, ThreadParentID: root.ID,
		Conversations: []Conversation{{ID: "room", Name: "Room"}},
		Messages:      []Message{root}, ThreadMessages: []Message{{ID: "reply", Author: "Bob", Body: "*Reply*"}},
	}
	markup := render(t, m)
	if strings.Count(markup, `<div class="message-body"`) != 3 || strings.Contains(markup, `<p class="message-body"`) {
		t.Fatalf("message body wrappers are invalid: %s", markup)
	}
	if strings.Count(markup, "<strong>Important</strong>") != 2 || !strings.Contains(markup, "<em>Reply</em>") || strings.Count(markup, `href="https://example.org/guide"`) != 2 {
		t.Fatalf("timeline and thread formatting = %s", markup)
	}
}
