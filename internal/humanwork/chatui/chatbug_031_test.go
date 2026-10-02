package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestTodo_CHATBUG_031(t *testing.T) {
	// The loader asks the geometry as well as the observer: a tile inside the
	// margin loads, one far below or above does not, one with no layout waits.
	for _, tc := range []struct {
		name                                     string
		top, bottom, rootTop, rootBottom, margin float64
		want                                     bool
	}{
		{"inside", 100, 260, 0, 700, 400, true},
		{"just below within the margin", 1000, 1160, 0, 700, 400, true},
		{"far below", 1500, 1660, 0, 700, 400, false},
		{"above within the margin", -300, -140, 0, 700, 400, true},
		{"far above", -900, -700, 0, 700, 400, false},
		{"no layout yet", 0, 0, 0, 700, 400, false},
		{"not a number", 0, 160, 0, 700 * nanForTest(), 400, false},
	} {
		if got := chatImageNearViewport(tc.top, tc.bottom, tc.rootTop, tc.rootBottom, tc.margin); got != tc.want {
			t.Errorf("%s: chatImageNearViewport = %v, want %v", tc.name, got, tc.want)
		}
	}
	// The failed tile is laid out so its badge and its action never share a row.
	if got := chatbugCascadeValue(ChatMsgListStyles, ".attachment-image.failed .attachment-badge", "inset-block-start"); got != "6px" {
		t.Fatalf("failed badge inset-block-start = %q, want 6px (the top of the tile)", got)
	}
	if got := chatbugCascadeValue(ChatMsgListStyles, ".attachment-image.failed .attachment-badge", "inset-block-end"); got != "auto" {
		t.Fatalf("failed badge inset-block-end = %q, want auto", got)
	}
	if got := chatbugCascadeValue(ChatMsgListStyles, ".attachment-image.failed .attachment-pending", "padding"); !strings.HasPrefix(got, "28px") {
		t.Fatalf("failed tile does not leave room above its content for the badge: padding %q", got)
	}
	if got := chatbugCascadeValue(ChatMsgListStyles, ".attachment-image.failed.unmeasured", "min-height"); got == "" {
		t.Fatal("a small failed tile can be shorter than its own message and action")
	}
	if got := chatbugCascadeValue(ChatMsgListStyles, ".attachment-loading", "display"); got != "none" {
		t.Fatalf("the loading placeholder is shown at rest: display %q", got)
	}
	if !strings.Contains(ChatMsgListStyles, ".attachment-image:not(.failed):has(>.attachment-image-open img:not([src])) .attachment-loading{display:flex") {
		t.Fatal("the placeholder is not tied to an <img> that has no src and has not failed")
	}
}

// nanForTest is a NaN without importing math for one comparison.
func nanForTest() float64 {
	zero := 0.0
	return zero / zero
}

func TestTodo_CHATBUG_031_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "sent", false)
		m.Callbacks.DownloadAttachment = func(string, string) {}
		msg := Message{ID: "post", AuthorID: "bob", Author: "Bob", Attachments: []Attachment{
			{ID: "png", Name: "person-hc-009-small.png", ContentType: "image/png", Width: 120, Height: 120},
			{ID: "gif", Name: "confetti.gif", ContentType: "image/gif", Width: 60, Height: 40},
		}}
		markup := chatPolishMarkup(t, attachments(m, msg), 390, "light")
		figures := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "figure" && chatPolishHasClass(n, "attachment-image") })
		if len(figures) != 2 {
			t.Fatalf("%s: %d tiles", locale, len(figures))
		}
		for i, figure := range figures {
			name := []string{"person-hc-009-small.png", "confetti.gif"}[i]
			// Loading: an <img> with no src, and a placeholder that names the file
			// and says it is loading, not announced twice (it is decorative).
			imgs := chatPolishNodesIn(figure, func(n *xhtml.Node) bool { return n.Data == "img" })
			if len(imgs) != 1 || chatPolishAttr(imgs[0], "src") != "" {
				t.Fatalf("%s %s: image elements %d or one already has a src", locale, name, len(imgs))
			}
			loading := chatPolishNodesIn(figure, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "attachment-loading") })
			if len(loading) != 1 || chatPolishAttr(loading[0], "aria-hidden") != "true" {
				t.Fatalf("%s %s: loading placeholders %d (aria-hidden %q)", locale, name, len(loading), chatPolishAttr(loading[0], "aria-hidden"))
			}
			text := chatbug030Text(loading[0])
			if !strings.Contains(text, name) || !strings.Contains(text, m.t(KeyAttachmentLoading)) {
				t.Fatalf("%s: placeholder text %q lacks the file name %q or the loading copy", locale, text, name)
			}
			// Failed: the pre-rendered fallback names the file and carries one download action.
			fallback := chatPolishNodesIn(figure, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "attachment-fallback") })
			if len(fallback) != 1 {
				t.Fatalf("%s %s: %d fallbacks", locale, name, len(fallback))
			}
			ft := chatbug030Text(fallback[0])
			if !strings.Contains(ft, m.t(KeyAttachmentUnavailable)) || !strings.Contains(ft, name) {
				t.Fatalf("%s: fallback text %q", locale, ft)
			}
			actions := chatPolishNodesIn(fallback[0], func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-action") == "download-attachment" })
			if len(actions) != 1 {
				t.Fatalf("%s %s: %d download actions in the fallback", locale, name, len(actions))
			}
			// The badge is a sibling of the fallback, never inside the action.
			badges := chatPolishNodesIn(figure, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "attachment-badge") })
			if (i == 1) != (len(badges) == 1) {
				t.Fatalf("%s %s: %d GIF badges", locale, name, len(badges))
			}
			for _, badge := range badges {
				for p := badge.Parent; p != nil; p = p.Parent {
					if p == fallback[0] || p == actions[0] {
						t.Fatalf("%s: the GIF badge sits inside the failed fallback", locale)
					}
				}
			}
		}
	}
}

// chatPolishNodesIn finds descendants of root that match.
func chatPolishNodesIn(root *xhtml.Node, match func(*xhtml.Node) bool) []*xhtml.Node {
	var out []*xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xhtml.ElementNode && match(c) {
				out = append(out, c)
			}
			walk(c)
		}
	}
	walk(root)
	return out
}
