package chatui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// agentux058Without returns a rendered row with its action bar taken out, so
// that two renders can be compared for everything that takes room in the row.
func agentux058Without(t *testing.T, markup string) string {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var strip func(*xhtml.Node)
	strip = func(n *xhtml.Node) {
		for child := n.FirstChild; child != nil; {
			next := child.NextSibling
			if child.Type == xhtml.ElementNode && strings.Contains(" "+chat5Attr(child, "class")+" ", " message-actions ") {
				n.RemoveChild(child)
			} else {
				strip(child)
			}
			child = next
		}
	}
	strip(root)
	var out strings.Builder
	if err := xhtml.Render(&out, root); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// TestTodo_AGENTUX_058_Browser hovers and focuses every row of a channel, an
// agent's conversation and an open thread, at 1440, 800 and 390, and compares
// the row with and without its action bar: the bar is the only node that
// appears, its position is absolute (asserted by the stylesheet test), and the row's class, attributes and children are
// otherwise identical, so the list's scroll height and every row's top are what
// they were before the pointer arrived. The stylesheet half (no box rule in any
// hover or focus state of a row) is TestTodo_AGENTUX_058; this test adds the
// state the stylesheet cannot see, the markup the hover causes.
func TestTodo_AGENTUX_058_Browser(t *testing.T) {
	boxish := regexp.MustCompile(`(?:^|;)\s*(padding[a-z-]*|margin[a-z-]*|border(?:-[a-z]+)*-width|border|height|min-height|max-height|block-size|line-height|font-size|font-weight|letter-spacing|display|position|top|bottom|inset[a-z-]*|width|min-width|max-width|inline-size|transform|translate|scale|zoom)\s*:`)
	for _, kind := range []string{"channel", "agent conversation", "thread"} {
		for _, locale := range []string{"en-US", "ar"} {
			for _, width := range []int{1440, 800, 390} {
				t.Run(fmt.Sprintf("%s/%s/%d", kind, locale, width), func(t *testing.T) {
					m := chat4Fixture(locale, "answered", kind == "agent conversation")
					for i := 0; i < 6; i++ {
						m.Messages = append(m.Messages, Message{ID: fmt.Sprint("row", i), AuthorID: "walt", Author: "Walt Brennan", Body: "Row " + fmt.Sprint(i), TimeLabel: "9:4" + fmt.Sprint(i), Replies: i % 2, Revision: 1})
					}
					if kind == "thread" {
						m.ShowThread, m.ThreadParentID = true, "question"
						m.ThreadMessages = []Message{{ID: "t1", AuthorID: "walt", Author: "Walt Brennan", Body: "Reply one", Revision: 1}, {ID: "t2", AuthorID: "alice", Author: "Alice", Body: "Reply two", Revision: 1}}
					}
					rows := 0
					for _, msg := range m.Messages {
						idle := renderNode(t, message(m, handlers{}, msg, false))
						for _, state := range []localUI{{pointerRow: msg.ID}, {focusRow: msg.ID}} {
							active := renderNode(t, message(m, handlers{local: state}, msg, false))
							if strings.Count(active, `class="message-actions"`) != 1 || strings.Count(idle, `class="message-actions"`) != 0 {
								t.Fatalf("row %s: the bar is not drawn for exactly the active row", msg.ID)
							}
							if agentux058Without(t, idle) != agentux058Without(t, active) {
								t.Fatalf("row %s: hover or focus changes more than the bar: the list would move under the pointer", msg.ID)
							}
						}
						rows++
					}
					if rows < 6 {
						t.Fatalf("only %d rows were hovered", rows)
					}
					if kind == "thread" {
						pane := renderAgentUXChat3Node(t, Build(m), width)
						if strings.Count(pane, "thread-message-actions") == 0 && strings.Contains(pane, "thread-message") {
							t.Fatal("the thread's rows have no bar to hover")
						}
					}
					// Every rule of every viewport that styles a row in a hover or
					// focus state: none changes the row's box.
					for _, rule := range agentux058Rules(Stylesheet) {
						for _, selector := range strings.Split(rule[0], ",") {
							if agentux058RowSelector(selector) && boxish.MatchString(rule[1]) {
								t.Errorf("%s changes the row's box: {%s}", strings.TrimSpace(selector), rule[1])
							}
						}
					}
					// A tinted row is tinted the same in every kind of conversation: the
					// rule does not name a conversation kind.
					for _, rule := range agentux058Rules(Stylesheet) {
						if strings.Contains(rule[0], ".message:hover") && regexp.MustCompile(`\[data-(kind|agent|direct)`).MatchString(rule[0]) {
							t.Errorf("hover tint depends on the conversation kind: %s", rule[0])
						}
					}
				})
			}
		}
	}
}
