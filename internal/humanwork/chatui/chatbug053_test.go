package chatui

import (
	stdhtml "html"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

const chatbug053Long1 = "https://example.com/a/very/long/path/that/keeps/going/and/going/for/a/while?with=query&and=more"

var chatbug053Tags = regexp.MustCompile(`<[^>]*>`)

func chatbug053Body(t *testing.T, m Model, body string) string {
	t.Helper()
	return stdhtml.UnescapeString(renderNode(t, spanOf(markdownMessageBody(m, body))))
}

// TestTodo_CHATBUG_053: every address in a message is a link, a long one is
// shortened in the middle and keeps its whole text, and no address is broken
// inside.
func TestTodo_CHATBUG_053(t *testing.T) {
	m := chatbug037Model("none")

	t.Run("a long address", func(t *testing.T) {
		markup := chatbug053Body(t, m, "First test message with a link "+chatbug053Long1+" and a second sentence.")
		if !strings.Contains(markup, `href="`+chatbug053Long1+`"`) || !strings.Contains(markup, `title="`+chatbug053Long1+`"`) {
			t.Fatalf("the address is not a link with the whole address as its target and title: %s", markup)
		}
		site, middle, end := chatbug053Parts(chatbug053Long1)
		if site != "https://example.com" || site+middle+end != chatbug053Long1 {
			t.Fatalf("the parts do not make up the address: %q %q %q", site, middle, end)
		}
		if shown := utf8.RuneCountInString(site) + 1 + utf8.RuneCountInString(end); shown > chatbug053Long {
			t.Fatalf("the shortened address shows %d characters, more than %d: %s…%s", shown, chatbug053Long, site, end)
		}
		if !strings.HasSuffix(end, "/for/a/while?with=query&and=more") || !strings.ContainsRune("/?&#=-_.", rune(end[0])) {
			t.Fatalf("the end shown is not the end of the path, begun at a separator: %q", end)
		}
		for _, want := range []string{`<span class="chat-link-site">` + site + `</span>`, `<span class="chat-link-cut">` + middle + `</span>`, `<span class="chat-link-end">` + end + `</span>`} {
			if !strings.Contains(markup, want) {
				t.Errorf("the link lacks %s: %s", want, markup)
			}
		}
		// A copy of the selected text is the text of the tree, and that is the
		// whole sentence with the whole address.
		if text := chatbug053Tags.ReplaceAllString(markup, ""); text != "First test message with a link "+chatbug053Long1+" and a second sentence." {
			t.Fatalf("the text of the message changed: %q", text)
		}
	})

	t.Run("short addresses and their neighbours", func(t *testing.T) {
		for body, want := range map[string]string{
			"see https://example.com/a/b":             `see <a class="chat-link" href="https://example.com/a/b">https://example.com/a/b</a>`,
			"see https://example.com/a/b.":            `<a class="chat-link" href="https://example.com/a/b">https://example.com/a/b</a>.`,
			"(https://example.com/a/b)":               `(<a class="chat-link" href="https://example.com/a/b">https://example.com/a/b</a>)`,
			"https://en.wikipedia.org/wiki/Go_(game)": `<a class="chat-link" href="https://en.wikipedia.org/wiki/Go_(game)">https://en.wikipedia.org/wiki/Go_(game)</a>`,
			"http://a.example and https://b.example":  `<a class="chat-link" href="http://a.example">http://a.example</a> and <a class="chat-link" href="https://b.example">https://b.example</a>`,
		} {
			if markup := chatbug053Body(t, m, body); !strings.Contains(markup, want) {
				t.Errorf("%q: want %s, got %s", body, want, markup)
			}
		}
		for _, body := range []string{"xhttps://example.com/a", "javascript://example.com/%0aalert(1)", "https://user:secret@example.com/a", "ftp://example.com/a", "no address here"} {
			if markup := chatbug053Body(t, m, body); strings.Contains(markup, "<a ") {
				t.Errorf("%q became a link: %s", body, markup)
			}
		}
	})

	t.Run("a link's own label is not linked again", func(t *testing.T) {
		markup := chatbug053Body(t, m, "[https://example.com/shown](https://example.com/target)")
		if strings.Count(markup, "<a ") != 1 || !strings.Contains(markup, `href="https://example.com/target"`) || !strings.Contains(markup, ">https://example.com/shown</a>") {
			t.Fatalf("a Markdown link with an address as its label: %s", markup)
		}
		if markup := chatbug053Body(t, m, "<"+chatbug053Long1+">"); strings.Count(markup, "<a ") != 1 || !strings.Contains(markup, `class="chat-link-cut"`) {
			t.Fatalf("an address in angle brackets is not shortened like a bare one: %s", markup)
		}
	})

	t.Run("a share link of another server", func(t *testing.T) {
		foreign := "http://chat.other.example/workspace/app/chat#share=" + chatbug037Token
		for locale, label := range map[string]string{"en-US": "a shared message", "de-DE": "eine geteilte Nachricht", "ar": "رسالة تمت مشاركتها"} {
			other := chatbug037Model("ready")
			other.Locale = locale
			markup := chatbug053Body(t, other, "did y'all see this "+foreign)
			if !strings.Contains(markup, ">"+label+" (chat.other.example)</a>") || !strings.Contains(markup, `href="`+foreign+`"`) || !strings.Contains(markup, `title="`+foreign+`"`) || !strings.Contains(markup, `data-share-link="foreign"`) {
				t.Errorf("%s: want a link reading %q with its site: %s", locale, label, markup)
			}
			if text := chatbug053Tags.ReplaceAllString(markup, ""); strings.Contains(text, chatbug037Token[:20]) {
				t.Errorf("%s: the token is printed: %s", locale, text)
			}
		}
		// The same address on this server is still the short link of CHATBUG-037.
		if markup := chatbug053Body(t, chatbug037Model("ready"), "see "+chatbug037Address); !strings.Contains(markup, ">a message in #design</a>") {
			t.Fatalf("a share link of this server lost its name: %s", markup)
		}
	})

	t.Run("an address is one piece", func(t *testing.T) {
		for selector, want := range map[string]map[string]string{
			".chat-workspace a.chat-link":                                     {"white-space": "nowrap", "text-overflow": "ellipsis", "max-inline-size": "100%", "overflow": "hidden"},
			".chat-workspace a.chat-link .chat-link-cut":                      {"white-space": "nowrap", "text-overflow": "ellipsis", "overflow": "hidden", "display": "inline-block"},
			`.chat-workspace a.chat-share-link[data-share-link="unresolved"]`: {"white-space": "nowrap", "text-overflow": "ellipsis", "max-inline-size": "100%"},
		} {
			for property, value := range want {
				if got := chatbugCascadeValue(Stylesheet, selector, property); got != value {
					t.Errorf("%s: %s=%q, want %q", selector, property, got, value)
				}
			}
		}
		if strings.Contains(ChatBug037Styles, "break-all") {
			t.Error("a share address may still be broken between any two characters")
		}
	})

	t.Run("the parts of an address", func(t *testing.T) {
		for _, address := range []string{
			"https://example.com/" + strings.Repeat("a", 100),
			"https://" + strings.Repeat("sub.", 14) + "example.com/path/to/a/page/with/a/long/name.html",
			"https://example.com/ü/" + strings.Repeat("é", 70) + "/end",
		} {
			site, middle, end := chatbug053Parts(address)
			if site+middle+end != address || middle == "" || !utf8.ValidString(middle) || !utf8.ValidString(end) {
				t.Errorf("%q: parts %q %q %q", address, site, middle, end)
			}
		}
		if site, middle, end := chatbug053Parts("https://example.com/short"); site != "https://example.com/short" || middle != "" || end != "" {
			t.Errorf("a short address was cut: %q %q %q", site, middle, end)
		}
		if got := chatbug053Trim("https://example.com/a).,"); got != "https://example.com/a" {
			t.Errorf("trailing punctuation was kept: %q", got)
		}
	})
}
