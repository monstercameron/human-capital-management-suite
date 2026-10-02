package chatui

import (
	"fmt"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func chatperfMarkup(t *testing.T, nodes []ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(html.Div(html.Props{}, nodes...))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// TestTodo_CHATBUG_014_MarkdownParsedOnce: a message body is parsed the first
// time it is drawn and not again, and what is drawn from the kept tree is what
// a fresh parse draws, however many times it is drawn.
func TestTodo_CHATBUG_014_MarkdownParsedOnce(t *testing.T) {
	m := Model{}
	bodies := []string{
		"plain text",
		"**bold** and _emphasis_ with `code` and a [link](https://example.test/path)",
		"- one\n- two\n  - nested\n\n1. first\n2. second",
		"> quoted\n\n```go\nfunc main() {}\n```\n",
		"line one\\\nline two &amp; a <b>tag</b> that stays text",
		"",
	}
	cache := newChatperfMarkdownCache(8)
	for _, body := range bodies {
		fresh := chatperfParseMarkdown(body)
		want := chatperfMarkup(t, markdownChildren(m, fresh.root, fresh.source))
		for draw := 0; draw < 3; draw++ {
			root, source := cache.tree(body)
			if got := chatperfMarkup(t, markdownChildren(m, root, source)); got != want {
				t.Fatalf("draw %d of %q from the kept tree:\n got %s\nwant %s", draw, body, got, want)
			}
		}
		if got := chatperfMarkup(t, markdownMessageBody(m, body)); got != want {
			t.Fatalf("markdownMessageBody(%q) = %s, want %s", body, got, want)
		}
	}
	if cache.parsed != len(bodies) {
		t.Fatalf("%d bodies drawn three times each were parsed %d times, want once each", len(bodies), cache.parsed)
	}

	// The table is bounded: it never holds more than its limit, and a body it
	// has let go is simply parsed again.
	for i := 0; i < 40; i++ {
		cache.tree(fmt.Sprintf("message %d", i))
		if len(cache.trees) > cache.limit {
			t.Fatalf("the table holds %d trees, over its limit of %d", len(cache.trees), cache.limit)
		}
	}
	before := cache.parsed
	root, source := cache.tree(bodies[1])
	if cache.parsed != before+1 || root == nil || string(source) != bodies[1] {
		t.Fatalf("a body the table let go was not parsed again: parsed %d -> %d", before, cache.parsed)
	}
}
