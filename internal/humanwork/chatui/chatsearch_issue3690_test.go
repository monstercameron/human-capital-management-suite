package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// ISSUE-3690: the Refine search controls mirror the active query. A control
// used to render with no value and no checked state, so the form showed blank
// fields and unticked boxes while chips said the filters were on, and a box
// ticked by an earlier search stayed ticked and was re-applied.

// issue3690Element returns the element with the given id from rendered markup.
func issue3690Element(t *testing.T, markup, id string) *xhtml.Node {
	t.Helper()
	doc, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var found *xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if found != nil {
			return
		}
		if n.Type == xhtml.ElementNode {
			for _, a := range n.Attr {
				if a.Key == "id" && a.Val == id {
					found = n
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if found == nil {
		t.Fatalf("no element #%s in markup", id)
	}
	return found
}

func issue3690Attr(n *xhtml.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func TestTodo_ISSUE3690_RefineMirrorsActiveQuery(t *testing.T) {
	markup := renderNode(t, RenderChatSearch("en-US", ChatSearchView{Query: `payroll in:announcements from:"Julian Miller" lang:en is:mine has:link`}))

	if v, _ := issue3690Attr(issue3690Element(t, markup, "chatsearch-channel"), "value"); v != "announcements" {
		t.Fatalf("channel field value %q, want announcements", v)
	}
	if v, _ := issue3690Attr(issue3690Element(t, markup, "chatsearch-person"), "value"); v != "Julian Miller" {
		t.Fatalf("person field value %q, want Julian Miller", v)
	}
	for _, id := range []string{"chatsearch-mine", "chatsearch-link"} {
		if _, ok := issue3690Attr(issue3690Element(t, markup, id), "checked"); !ok {
			t.Fatalf("%s is active in the query but not checked", id)
		}
	}
	if _, ok := issue3690Attr(issue3690Element(t, markup, "chatsearch-file"), "checked"); ok {
		t.Fatal("file box checked although the query has no has:file")
	}
	written := issue3690Element(t, markup, "chatsearch-written")
	selected := ""
	for c := written.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode {
			if _, ok := issue3690Attr(c, "selected"); ok {
				selected, _ = issue3690Attr(c, "value")
			}
		}
	}
	if selected != "en" {
		t.Fatalf("written select selects %q, want en", selected)
	}
}

func TestTodo_ISSUE3690_PlainSearchClearsStaleControls(t *testing.T) {
	markup := renderNode(t, RenderChatSearch("en-US", ChatSearchView{Query: "payroll"}))

	if _, ok := issue3690Attr(issue3690Element(t, markup, "chatsearch-link"), "checked"); ok {
		t.Fatal("a plain search renders the link box checked, so a stale tick would be re-applied")
	}
	if v, ok := issue3690Attr(issue3690Element(t, markup, "chatsearch-channel"), "value"); ok && v != "" {
		t.Fatalf("plain search renders channel value %q", v)
	}
}
