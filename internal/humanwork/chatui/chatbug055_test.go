package chatui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// TestTodo_CHATBUG_055 draws a search result the way the conversation draws the
// message: the author's avatar and name and the time, a document by its title as
// a link, a mention as the chip it is, the searched phrase as one tint, and no
// identifier anywhere.
func TestTodo_CHATBUG_055(t *testing.T) {
	const doc = "doc-47892b80-d600-4401-8244-e2fa2a31caa7"
	at := time.Date(2026, time.October, 1, 22, 10, 0, 0, time.UTC)
	ref := ChatReference{Kind: "PERSON_MENTION", TenantID: "t", ID: "ben", Display: "Ben Whitaker"}
	m := Model{State: StateReady, Locale: "en-US", CurrentUser: "walt", CurrentTenantID: "t", SelectedID: "design",
		Conversations: []Conversation{{ID: "design", Name: "design", Kind: PublicChannel, Joined: true}},
		Members:       []Member{{ID: "ben", HomeTenantID: "t", Name: "Ben Whitaker"}},
		Messages:      []Message{{ID: "post", AuthorID: "ben", Author: "Ben Whitaker", Body: "x", PersonaReferences: []ChatReference{ref}}},
		DocPreviews:   map[string]DocPreview{doc: {ID: doc, Title: "Open enrollment guide", Readable: true, State: "ready"}}}
	view := ChatSearchView{Query: "open enrollment", Context: &m, Response: chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.Message, Rows: []chatsearch.Row{
		{Kind: chatsearch.Message, ID: "post", AuthorID: "ben", At: at, Text: "@Ben Whitaker the open enrollment steps are in doc:" + doc, Target: chatsearch.Target{ConversationID: "design", MessageID: "post", Sequence: 3}}}}}}}
	markup := renderNode(t, RenderChatSearch("en-US", view))
	for _, want := range []string{
		`in #design</button>`, // the line that names the conversation
		`>Ben Whitaker</strong>`, `class="avatar small chatsearch-avatar"`, `<time class="chatsearch-time"`,
		`Open enrollment guide`, `<a `, // the document by its title, as a link
		`mention-chip`, // the mention as a chip
		`<mark class="search-hit">open enrollment</mark>`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("the result lacks %q: %s", want, markup)
		}
	}
	visible := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(markup, " ")
	for _, bad := range []string{"doc:doc-", doc, "undefined", "⟦"} {
		if strings.Contains(visible, bad) {
			t.Errorf("the result prints %q", bad)
		}
	}
	if strings.Count(markup, `class="search-hit"`) != 1 {
		t.Errorf("a phrase is more than one highlight: %s", markup)
	}
}

// TestTodo_CHATBUG_055_Browser holds how a result is drawn on the page: the
// tint is soft over the whole phrase and not a solid block, and a result is a
// row of the list and not a bordered box.
func TestTodo_CHATBUG_055_Browser(t *testing.T) {
	soft := chatbugCascadeValue(Stylesheet, ".chat-workspace .chatsearch-view mark.search-hit", "background")
	if !strings.Contains(soft, "color-mix(in srgb,var(--hcm-color-warning) 32%") {
		t.Errorf("the match tint is %q, not a soft tint", soft)
	}
	if got := chatbugCascadeValue(Stylesheet, ".chat-workspace .chatsearch-view .chatsearch-result", "border"); got != "0" {
		t.Errorf("a result is drawn as a bordered box: border %q", got)
	}
	if got := chatbugCascadeValue(Stylesheet, ".chat-workspace .chatsearch-text", "-webkit-line-clamp"); got != "4" {
		t.Errorf("a result's text is cut at %q lines", got)
	}
}
