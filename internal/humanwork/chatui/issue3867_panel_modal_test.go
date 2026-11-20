package chatui

import (
	"reflect"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// Issue 3867: a panel that fills a phone screen takes keyboard focus with it.
// The conversation or list behind it is made inert so Tab cannot reach controls
// nobody can see.

func TestIssue3867_FullScreenThreadMakesConversationInert(t *testing.T) {
	plan := issue3867PlanPanels([]issue3867Panel{{Kind: "thread", Visible: true, Width: 354}}, 354)
	if plan.Kind != "thread" {
		t.Fatalf("kind = %q, want thread", plan.Kind)
	}
	want := []string{".chat-main", ".chat-rail", ".chat-skip"}
	if !reflect.DeepEqual(plan.Inert, want) {
		t.Fatalf("inert = %q, want %q", plan.Inert, want)
	}
}

func TestIssue3867_FullScreenDetailsAndPersonMakeConversationInert(t *testing.T) {
	for _, kind := range []string{"details", "person"} {
		plan := issue3867PlanPanels([]issue3867Panel{{Kind: kind, Visible: true, Width: 354}}, 354)
		if plan.Kind != kind || !reflect.DeepEqual(plan.Inert, []string{".chat-main", ".chat-rail", ".chat-skip"}) {
			t.Fatalf("%s plan = %+v", kind, plan)
		}
	}
}

func TestIssue3867_SavedPanelMakesWholeLayoutInertAndWinsOverSidePanels(t *testing.T) {
	plan := issue3867PlanPanels([]issue3867Panel{
		{Kind: "thread", Visible: true, Width: 354},
		{Kind: "saved", Visible: true, Width: 354},
	}, 354)
	if plan.Kind != "saved" {
		t.Fatalf("kind = %q, want saved", plan.Kind)
	}
	want := []string{".chat-layout", ".chat-skip"}
	if !reflect.DeepEqual(plan.Inert, want) {
		t.Fatalf("inert = %q, want %q", plan.Inert, want)
	}
}

func TestIssue3867_PanelBesideConversationLeavesItReachable(t *testing.T) {
	for name, panels := range map[string][]issue3867Panel{
		"narrower than the screen": {{Kind: "details", Visible: true, Width: 380}},
		"not shown":                {{Kind: "thread", Visible: false, Width: 354}},
		"none":                     nil,
	} {
		viewport := 354.0
		if name == "narrower than the screen" {
			viewport = 900
		}
		if plan := issue3867PlanPanels(panels, viewport); plan.Kind != "" || len(plan.Inert) != 0 {
			t.Fatalf("%s: plan = %+v, want none", name, plan)
		}
	}
}

func TestIssue3867_FocusSelectorReachesFirstControlOfPanel(t *testing.T) {
	for _, want := range []string{"button:not([disabled])", "a[href]", "textarea:not([disabled])", "input:not([disabled])"} {
		if !strings.Contains(issue3867FocusSelector, want) {
			t.Fatalf("focus selector %q lacks %q", issue3867FocusSelector, want)
		}
	}
}

func TestIssue3867_PanelKindOfSelectors(t *testing.T) {
	cases := map[string]string{
		".chat-side.thread-pane":        "thread",
		".chat-side.person-pane":        "person",
		".chat-side.chat-details":       "details",
		".chatsave-panel:not([hidden])": "saved",
	}
	got := map[string]string{}
	for _, p := range issue3867PanelSelectors {
		got[p.Selector] = p.Kind
	}
	for selector, kind := range cases {
		if got[selector] != kind {
			t.Fatalf("selector %q -> %q, want %q (have %v)", selector, got[selector], kind, got)
		}
	}
}

// The regions made inert must not contain the panel that owns the screen, or
// the panel itself would be dead to the keyboard. This reads the real markup.
func TestIssue3867_InertRegionsDoNotContainThePanels(t *testing.T) {
	root := Message{ID: "root", Body: "Question", AuthorID: "alice", Author: "Alice"}
	m := Model{State: StateReady, Locale: "en-US", CurrentTenantID: "tenant", CurrentUser: "reader", SelectedID: "room",
		Conversations: []Conversation{{ID: "room", Name: "General", Kind: PublicChannel, Joined: true}},
		Messages:      []Message{root}, ShowThread: true, ThreadParentID: root.ID, ThreadParent: &root}
	doc, err := xhtml.Parse(strings.NewReader(render(t, m)))
	if err != nil {
		t.Fatal(err)
	}
	hasClass := func(n *xhtml.Node, class string) bool {
		if n.Type != xhtml.ElementNode {
			return false
		}
		for _, a := range n.Attr {
			if a.Key == "class" {
				for _, c := range strings.Fields(a.Val) {
					if c == class {
						return true
					}
				}
			}
		}
		return false
	}
	var find func(n *xhtml.Node, class string) *xhtml.Node
	find = func(n *xhtml.Node, class string) *xhtml.Node {
		if hasClass(n, class) {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if got := find(c, class); got != nil {
				return got
			}
		}
		return nil
	}
	inside := func(region *xhtml.Node, class string) bool { return region != nil && find(region, class) != nil }
	if find(doc, "thread-pane") == nil {
		t.Fatal("the thread pane is not rendered")
	}
	for _, region := range []string{"chat-main", "chat-rail"} {
		node := find(doc, region)
		if node == nil {
			t.Fatalf("no %s region in the page", region)
		}
		for _, panel := range []string{"thread-pane", "person-pane", "chat-details"} {
			if inside(node, panel) {
				t.Errorf("%s contains .%s: making it inert would lock the panel too", region, panel)
			}
		}
	}
	layout := find(doc, "chat-layout")
	if layout == nil || inside(layout, "chatsave-panel") {
		t.Error("the Saved panel is inside .chat-layout, which is made inert while it is open")
	}
	if find(doc, "chatsave-panel") == nil {
		t.Error("the Saved panel is not in the page")
	}
}
