package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// The browser-level tests of CHATBUG-089..091 and CHATUX-031..036 render the
// whole Chat page with the product's own catalog in en-US, de-DE and ar, so a
// word the page forgot to translate or a key it printed shows up, and read the
// result the way a browser would: as a tree, and as the text a person sees.

var w7Locales = []string{"en-US", "de-DE", "ar"}

// w7Page is the raw markup of the Chat page for locale. change sets up the
// model; the room "general" is selected and the person is Walt Brennan.
func w7Page(t *testing.T, locale string, change func(*chatui.Model)) string {
	t.Helper()
	ctx := productui.ResolveProductLocale(locale)
	room := chatui.Conversation{ID: "general", Name: "general", Kind: chatui.PublicChannel, OwnerID: "walt", MemberCount: 18, Joined: true}
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: room.ID, CurrentUser: "walt", CurrentTenantID: "t",
		Text:          func(key string) string { return ctx.Text(key) },
		Conversations: []chatui.Conversation{room, {ID: "sales", Name: "sales", Kind: chatui.PublicChannel, Joined: true}},
		Members:       []chatui.Member{{ID: "walt", HomeTenantID: "t", Name: "Walt Brennan"}, {ID: "jake", HomeTenantID: "t", Name: "Jake Sullivan"}},
		Callbacks: chatui.Callbacks{CreateConversation: func(chatui.ConversationKind, string, []string) {}, CloseCreate: func() {}, OpenCreate: func() {}, Search: func(string) {}, SelectConversation: func(string) {},
			OpenRailMenu: func(string) {}, ToggleDetails: func(bool) {}, SendMessage: func(string, string) {}, ReplyInThread: func(string, string) {}},
	}
	if change != nil {
		change(&m)
	}
	page, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, "⟦") {
		t.Fatalf("%s: the page prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(page))
	}
	return page
}

// w7Visible is the text of markup after its tags are dropped and its entities
// read: what a person would see, comments and attributes excluded.
func w7Visible(markup string) string {
	return html.UnescapeString(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(markup, " "))
}

func w7Parse(t *testing.T, markup string) *xhtml.Node {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func w7Attr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func w7Has(n *xhtml.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func w7Class(n *xhtml.Node, class string) bool {
	for _, token := range strings.Fields(w7Attr(n, "class")) {
		if token == class {
			return true
		}
	}
	return false
}

// w7All is every element under n (n included) the predicate accepts.
func w7All(n *xhtml.Node, pred func(*xhtml.Node) bool) []*xhtml.Node {
	var found []*xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(c *xhtml.Node) {
		if c.Type == xhtml.ElementNode && pred(c) {
			found = append(found, c)
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return found
}

// w7One is the one element the predicate accepts; any other count fails.
func w7One(t *testing.T, where string, n *xhtml.Node, pred func(*xhtml.Node) bool) *xhtml.Node {
	t.Helper()
	found := w7All(n, pred)
	if len(found) != 1 {
		t.Fatalf("%s: %d elements match, want 1", where, len(found))
	}
	return found[0]
}

func w7Text(n *xhtml.Node) string {
	var out strings.Builder
	var walk func(*xhtml.Node)
	walk = func(c *xhtml.Node) {
		if c.Type == xhtml.TextNode {
			out.WriteString(c.Data)
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(out.String()), " ")
}

func w7Inside(n *xhtml.Node, pred func(*xhtml.Node) bool) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == xhtml.ElementNode && pred(p) {
			return true
		}
	}
	return false
}
