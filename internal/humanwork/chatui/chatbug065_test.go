package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// chatbug065Controls reads a composer's controls the way the page lays them
// out: in flow (their own place in the row) or as anchors, which the styles
// take out of flow as a point.
func chatbug065Controls(t *testing.T, markup string) (flow, anchors []string) {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	for _, tools := range chatPolishNodesIn(root, func(n *xhtml.Node) bool { return strings.Contains(chatPolishAttr(n, "class"), "composer-tools") }) {
		for c := tools.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != xhtml.ElementNode {
				continue
			}
			class := chatPolishAttr(c, "class")
			if strings.Contains(class, "chatvoice-tool") || strings.Contains(class, "chatmap-control") {
				anchors = append(anchors, class)
				continue
			}
			flow = append(flow, class)
		}
	}
	return flow, anchors
}

// TestTodo_CHATBUG_065 is the composer's overlap test. At 1440, 800 and 390 px
// the only composer controls that are not in the row's flow are the voice and
// location openers, and the styles make each of them a point (no width, no
// height, whatever the pointer or the window), so no two controls of a
// composer can share a rectangle in a direct message, a group or a channel.
func TestTodo_CHATBUG_065(t *testing.T) {
	sheet := Stylesheet
	anchor := `.chat-workspace .chat-composer .composer-tools>.chatvoice-tool>.tool-button,.chat-workspace .chat-composer .composer-tools>.chatmap-control>.tool-button{position:absolute;inset-block-start:50%;inset-inline-start:15px;inline-size:0;block-size:0;min-inline-size:0!important;min-block-size:0!important`
	if !strings.Contains(sheet, anchor) {
		t.Fatal("the voice and location openers are not taken out of the row as a point")
	}
	// Nothing sizes an opener back up: the only rules about them are the ones above.
	for _, rule := range strings.Split(sheet, "}") {
		selector, body, ok := strings.Cut(rule, "{")
		if !ok || !(strings.Contains(selector, "chatvoice-tool>.tool-button") || strings.Contains(selector, "chatmap-control>.tool-button")) {
			continue
		}
		if strings.Contains(selector, ":focus-visible") || strings.Contains(selector, ".chat-icon") || strings.Contains(body, "inline-size:0;block-size:0") {
			continue
		}
		if strings.Contains(body, "width") || strings.Contains(body, "inline-size") || strings.Contains(body, "height") || strings.Contains(body, "block-size") {
			t.Errorf("a rule sizes an opener: %s{%s}", selector, body)
		}
	}
	for _, kind := range []struct {
		kind ConversationKind
		name string
	}{{DirectMessage, "Loretta Haynes"}, {GroupChat, "Q4 hiring huddle"}, {PublicChannel, "general"}, {PrivateChannel, "payroll-close"}} {
		m := chatux014Model(kind.kind, kind.name)
		m.ShowThread = false
		m.Chatattach001 = nil
		for _, width := range []int{1440, 800, 390} {
			flow, anchors := chatbug065Controls(t, renderNode(t, composer(m, handlers{})))
			if len(flow) < 3 {
				t.Fatalf("%s at %d px: the row holds %v", kind.name, width, flow)
			}
			wantAnchors := 1 // the location opener
			if kind.kind == DirectMessage || kind.kind == GroupChat {
				wantAnchors = 2 // and the voice opener
			}
			if len(anchors) != wantAnchors {
				t.Fatalf("%s at %d px: %d anchors %v, want %d", kind.name, width, len(anchors), anchors, wantAnchors)
			}
			seen := map[string]bool{}
			for _, class := range flow {
				// Two controls with the same class and place would be one
				// rectangle drawn twice; each is its own button or group.
				if strings.Contains(class, "tool-button") && seen[class] {
					t.Errorf("%s at %d px: two controls share %q", kind.name, width, class)
				}
				seen[class] = true
			}
		}
	}
}

// TestTodo_CHATBUG_065_Browser keeps the keyboard's ring: an opener the Add menu
// pressed gets focus back, and the ring is drawn around the + button's centre
// since the opener itself is a point.
func TestTodo_CHATBUG_065_Browser(t *testing.T) {
	want := `.chat-workspace .chat-composer .composer-tools>.chatvoice-tool>.tool-button:focus-visible,.chat-workspace .chat-composer .composer-tools>.chatmap-control>.tool-button:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:14px;border-radius:50%}`
	if !strings.Contains(Stylesheet, want) {
		t.Fatal("an opener that holds focus is not drawn")
	}
}
