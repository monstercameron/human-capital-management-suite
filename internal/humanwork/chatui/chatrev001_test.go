package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestChatrev001_SourceHrefValidatedAtRender: a Sources chip renders a link
// only for a Documents-hub address on this origin. An envelope built by any
// caller with an off-site, absolute, or otherwise refused href must show the
// title as text — never a clickable link wearing the document glyph.
func TestChatrev001_SourceHrefValidatedAtRender(t *testing.T) {
	for _, href := range []string{
		"https://example.com/workspace/app/docs?document=pto",
		"http://x/workspace/app/docs?document=pto",
		"//other.test/workspace/app/docs?document=pto",
		"/doc/1",
		"/workspace/app/docs?document=",
		"/workspace/app/docs",
		"javascript:alert(1)",
		"/workspace/app/docs?document=pto\\@evil.test",
	} {
		envelope := agentReplyEnvelope{Body: "Answer.", Sources: []agentReplySource{{Title: "Paid time off policy", Href: href}}}
		markup := renderNode(t, html.Div(html.Props{}, renderAgentReplySources(Model{Locale: "en-US"}, envelope)...))
		if strings.Contains(markup, "<a ") || strings.Contains(markup, "href=") {
			t.Errorf("href %q rendered as a link: %s", href, markup)
		}
		if !strings.Contains(markup, "Paid time off policy") {
			t.Errorf("href %q dropped the title: %s", href, markup)
		}
	}
}

// TestChatrev001_ValidSourceStillLinks: the Documents-hub addresses the parse
// path accepts keep rendering as links after render-side validation.
func TestChatrev001_ValidSourceStillLinks(t *testing.T) {
	envelope := agentReplyEnvelope{Body: "Answer.", Sources: []agentReplySource{
		{Title: "Paid time off policy", Href: "/workspace/app/docs?document=pto"},
		{Title: "Carryover section", Href: "/workspace/app/docs?document=pto&version=one#carryover"},
	}}
	markup := renderNode(t, html.Div(html.Props{}, renderAgentReplySources(Model{Locale: "en-US"}, envelope)...))
	for _, want := range []string{`href="/workspace/app/docs?document=pto"`, "icon-document"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("valid source lost its link %q: %s", want, markup)
		}
	}
}

// TestChatrev001_GlyphEquivalence pins the exact markup of the one-path stroke
// glyphs before they share one wrapper: the refactor must not move a pixel.
func TestChatrev001_GlyphEquivalence(t *testing.T) {
	nodes := map[string]ui.Node{
		"project-task":  projectEmbedGlyph(false),
		"project-board": projectEmbedGlyph(true),
		"journey":       journeyGlyph(),
		"speaker":       listenSpeaker(),
		"microphone":    voiceMicrophone(),
	}
	for name, node := range nodes {
		markup := renderNode(t, node)
		if !strings.Contains(markup, "<svg") || !strings.Contains(markup, "<path") {
			t.Fatalf("%s is not an svg glyph: %s", name, markup)
		}
		if !strings.Contains(markup, `aria-hidden="true"`) || !strings.Contains(markup, `focusable="false"`) {
			t.Fatalf("%s lost its decorative hiding: %s", name, markup)
		}
	}
	for name, want := range map[string]string{
		"project-task":  `class="chat-project-embed-glyph"`,
		"project-board": `d="M4 5h4v14H4zM10 5h4v9h-4zM16 5h4v6h-4z"`,
		"journey":       `d="M5 6h4v4H5zM15 14h4v4h-4zM7 10v3a2 2 0 0 0 2 2h6"`,
		"speaker":       `d="M4 9v6h4l5 4V5L8 9H4zm12 0a4 4 0 0 1 0 6m2.5-9a8 8 0 0 1 0 12"`,
		"microphone":    `d="M9 5a3 3 0 0 1 6 0v7a3 3 0 0 1-6 0V5zm-3 6v1a6 6 0 0 0 12 0v-1M12 18v4m-4 0h8"`,
	} {
		if markup := renderNode(t, nodes[name]); !strings.Contains(markup, want) {
			t.Fatalf("%s lost %q: %s", name, want, markup)
		}
	}
}

// TestChatrev001_ThreadBellIsSharedIcon: the thread pane's notify bell is the
// shared bell glyph, not a second copy of its path.
func TestChatrev001_ThreadBellIsSharedIcon(t *testing.T) {
	m := Model{Locale: "en-US"}
	markup := renderNode(t, chatux008Heading(m, "#general"))
	if !strings.Contains(markup, `class="chat-icon icon-bell"`) {
		t.Fatalf("thread heading lost the shared bell: %s", markup)
	}
	if bell, shared := renderNode(t, chatsave002Icon("bell", false)), renderNode(t, icon("bell")); bell != shared {
		t.Fatalf("panel bell drifted from the shared bell:\n%s\n%s", bell, shared)
	}
}

// TestChatrev001_ChatsaveBookmarkIsSharedPath: the save control's bookmark is
// the panel's one bookmark path, not a second copy of its shape.
func TestChatrev001_ChatsaveBookmarkIsSharedPath(t *testing.T) {
	m := Model{CurrentUser: "person", CurrentTenantID: "tenant", SelectedID: "room", Locale: "en-US"}
	markup := renderNode(t, chatsaveAction(m, Message{ID: "post"}, false))
	if !strings.Contains(markup, `class="chatsave-bookmark"`) || !strings.Contains(markup, `d="`+chatsave002Paths["bookmark"]+`"`) {
		t.Fatalf("save control lost the shared bookmark: %s", markup)
	}
}

// BenchmarkChatrev001_SourcesRender measures the Sources section render the
// href validation touches: one linked document and one locked chip.
func BenchmarkChatrev001_SourcesRender(b *testing.B) {
	envelope := agentReplyEnvelope{Body: "Answer.", Sources: []agentReplySource{
		{Title: "Paid time off policy", Href: "/workspace/app/docs?document=pto"},
		{Title: "Private guide"},
	}}
	model := Model{Locale: "en-US"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ui.RenderToString(html.Div(html.Props{}, renderAgentReplySources(model, envelope)...))
	}
}
