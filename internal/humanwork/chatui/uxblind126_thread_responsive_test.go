package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestTodo_UXBLIND_126(t *testing.T) {
	css := ScopedStylesheet()
	for _, want := range []string{
		`@media(max-width:760px){.thread-root-body .message-meta,.thread-message-body .message-meta{display:grid;grid-template-columns:minmax(0,1fr) auto`,
		`.thread-root-body .message-meta .thread-view-in-channel{position:static;grid-column:1 / -1`,
		`.thread-pane .message-time{white-space:nowrap}`,
		`.thread-pane .message-author{min-width:0;overflow-wrap:anywhere}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("responsive thread stylesheet missing %q", want)
		}
	}
}

// uxblind126MetaChildren returns the (tag, class) of each element child of
// the first node matching class, in document order.
func uxblind126MetaChildren(t *testing.T, root *xhtml.Node, class string) [][2]string {
	t.Helper()
	holders := chatPolishNodesIn(root, func(n *xhtml.Node) bool {
		return n.Type == xhtml.ElementNode && n.Data == "div" && chatPolishAttr(n, "class") == class
	})
	if len(holders) == 0 {
		t.Fatalf("rendered thread has no %q", class)
	}
	var out [][2]string
	for c := holders[0].FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xhtml.ElementNode {
			continue
		}
		out = append(out, [2]string{c.Data, chatPolishAttr(c, "class")})
	}
	return out
}

func uxblind126Text(n *xhtml.Node) string {
	var sb strings.Builder
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.TextNode {
			sb.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(sb.String())
}

// TestTodo_UXBLIND_126_Browser is the contracted BROWSER matrix entry: it
// opens a seeded thread the way RED did (#engineering, populated root plus
// replies), inspects the author/time/actions rows at both audited widths,
// and proves the thread closes without posting. The native lane does not
// launch a browser, so widths select the same <=760px rules a phone matches
// and the test pins the rendered structure those rules lay out: the root
// meta holds author, time and the View in channel action in grid order
// (author beside the nowrap time, the action on its own full row), every
// reply meta holds author beside time, every control keeps its accessible
// name, and the reply composer stays labelled and enabled with a named
// type=button close control outside the form.
func TestTodo_UXBLIND_126_Browser(t *testing.T) {
	sheet := ScopedStylesheet()
	for _, want := range []string{
		`@media(max-width:760px){.thread-root-body .message-meta,.thread-message-body .message-meta{display:grid`,
		`align-items:baseline;gap:2px 8px`,
		`.thread-root-body .message-meta .thread-view-in-channel{position:static;grid-column:1 / -1;justify-self:start;margin-top:2px;white-space:nowrap}`,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("narrow thread layout missing %q", want)
		}
	}
	for _, width := range []int{320, 390} {
		m := Model{Locale: "en-US", SelectedID: "room", CurrentUser: "walt",
			Conversations: []Conversation{{ID: "room", Name: "engineering", Kind: PublicChannel, Joined: true, MemberCount: 18}},
			Messages:      []Message{{ID: "p1", AuthorID: "walt", Author: "Walt Brennan", Body: "Ironridge kickoff notes.", TimeLabel: "9:22 PM", Replies: 2}},
			ShowThread:    true, ThreadParentID: "p1",
			ThreadMessages: []Message{
				{ID: "r1", AuthorID: "ada", Author: "Ada Lovelace", Body: "On it.", TimeLabel: "9:31 PM"},
				{ID: "r2", AuthorID: "grace", Author: "Grace Hopper", Body: "Reviewed.", TimeLabel: "9:44 PM"},
			},
			Callbacks: walkCallbacks()}
		pane := renderNode(t, threadPane(m, handlers{}))
		root, err := xhtml.Parse(strings.NewReader(pane))
		if err != nil {
			t.Fatal(err)
		}
		// Root post: author, timestamp, actions in grid order, each named.
		bodies := chatPolishNodesIn(root, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && n.Data == "div" && chatPolishAttr(n, "class") == "thread-root-body"
		})
		if len(bodies) != 1 {
			t.Fatalf("%d px: thread root body count = %d, want one", width, len(bodies))
		}
		got := uxblind126MetaChildren(t, bodies[0], "message-meta")
		want := [][2]string{
			{"button", "message-author person-name"},
			{"time", "message-time"},
			{"button", "thread-view-in-channel"},
		}
		if len(got) != len(want) {
			t.Fatalf("%d px: root meta holds %v, want author/time/action rows", width, got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%d px: root meta holds %v, want %v", width, got, want)
			}
		}
		metas := chatPolishNodesIn(bodies[0], func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && n.Data == "div" && chatPolishAttr(n, "class") == "message-meta"
		})
		if len(metas) != 1 {
			t.Fatalf("%d px: root body holds %d message metas, want one", width, len(metas))
		}
		author, stamp, action := metas[0].FirstChild, metas[0].FirstChild.NextSibling, metas[0].LastChild
		for author != nil && author.Type != xhtml.ElementNode {
			author = author.NextSibling
		}
		for stamp != nil && (stamp.Type != xhtml.ElementNode || stamp.Data != "time") {
			stamp = stamp.NextSibling
		}
		for action != nil && action.Type != xhtml.ElementNode {
			action = action.PrevSibling
		}
		if chatPolishAttr(author, "aria-label") == "" {
			t.Errorf("%d px: root author lost its accessible name", width)
		}
		if uxblind126Text(stamp) == "" {
			t.Errorf("%d px: root timestamp lost its text", width)
		}
		if uxblind126Text(action) == "" {
			t.Errorf("%d px: View in channel lost its accessible name", width)
		}
		// Replies: author beside time in every reply meta.
		replyRegions := chatPolishNodesIn(root, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && n.Data == "div" && chatPolishAttr(n, "class") == "thread-replies"
		})
		if len(replyRegions) != 1 {
			t.Fatalf("%d px: thread replies region count = %d, want one", width, len(replyRegions))
		}
		replyMetas := chatPolishNodesIn(replyRegions[0], func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && n.Data == "div" && chatPolishAttr(n, "class") == "message-meta"
		})
		if len(replyMetas) != 2 {
			t.Fatalf("%d px: thread holds %d reply metas, want 2", width, len(replyMetas))
		}
		for _, meta := range replyMetas {
			var cells [][2]string
			for c := meta.FirstChild; c != nil; c = c.NextSibling {
				if c.Type != xhtml.ElementNode {
					continue
				}
				cells = append(cells, [2]string{c.Data, chatPolishAttr(c, "class")})
			}
			if len(cells) != 2 || cells[0] != [2]string{"button", "message-author person-name"} || cells[1] != [2]string{"time", "message-time"} {
				t.Errorf("%d px: reply meta holds %v, want author beside time", width, cells)
			}
		}
		// Reply composer: labelled, enabled, with a send path.
		areas := chatPolishNodesIn(root, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && n.Data == "textarea" && chatPolishAttr(n, "id") == "thread-composer"
		})
		if len(areas) != 1 {
			t.Fatalf("%d px: thread composer field count = %d, want one", width, len(areas))
		}
		if chatPolishAttr(areas[0], "disabled") != "" {
			t.Errorf("%d px: reply composer is disabled", width)
		}
		labels := chatPolishNodesIn(root, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && n.Data == "label" && chatPolishAttr(n, "for") == "thread-composer"
		})
		if len(labels) != 1 || uxblind126Text(labels[0]) == "" {
			t.Errorf("%d px: reply composer lost its label", width)
		}
		forms := chatPolishNodesIn(root, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && n.Data == "form" && strings.Contains(chatPolishAttr(n, "class"), "thread-composer")
		})
		if len(forms) != 1 || chatPolishAttr(forms[0], "aria-label") == "" {
			t.Errorf("%d px: reply composer form lost its accessible name", width)
		}
		if !strings.Contains(pane, "send-button") {
			t.Errorf("%d px: reply composer lost its send control", width)
		}
		// Close without posting: a named type=button close control outside
		// the composer form, so activating it submits nothing.
		closes := chatPolishNodesIn(root, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && n.Data == "button" && chatPolishAttr(n, "data-action") == "close-thread"
		})
		if len(closes) == 0 {
			t.Fatalf("%d px: thread lost its close control", width)
		}
		for _, close := range closes {
			if chatPolishAttr(close, "aria-label") == "" {
				t.Errorf("%d px: close control lost its accessible name", width)
			}
			if chatPolishAttr(close, "type") != "button" {
				t.Errorf("%d px: close control is not type=button, closing could post", width)
			}
			for p := close.Parent; p != nil; p = p.Parent {
				if p.Type == xhtml.ElementNode && p.Data == "form" {
					t.Errorf("%d px: close control sits inside a form", width)
				}
			}
		}
	}
}
