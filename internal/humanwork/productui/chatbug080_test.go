package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

func chatbug080BarChildren(t *testing.T) []string {
	t.Helper()
	markup, err := ui.RenderToString(appHeader(testView(PagePeople), false, nil))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var classes []string
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "header" {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				for _, a := range c.Attr {
					if a.Key == "class" {
						classes = append(classes, a.Val)
					}
				}
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return classes
}

// TestTodo_CHATBUG_080 checks what the tablet-width page got wrong in the bar:
// five children, each in a column of its own, and only the navigation tools'
// column may stretch; every other column is as wide as what it holds.
func TestTodo_CHATBUG_080(t *testing.T) {
	children := chatbug080BarChildren(t)
	want := []string{"brand-cluster", "header-navigation-tools", "locale-menu", "notifications", "account-menu"}
	if len(children) != len(want) {
		t.Fatalf("the bar has %d children: %v", len(children), children)
	}
	for i, class := range want {
		if !strings.Contains(children[i], class) {
			t.Fatalf("child %d of the bar is %q, want %s", i+1, children[i], class)
		}
	}
	tracks := strings.Fields(strings.ReplaceAll(chatbug080TopbarColumns, "minmax(0,1fr)", "flex"))
	if len(tracks) != len(want) || tracks[1] != "flex" {
		t.Fatalf("the tablet grid is %q", chatbug080TopbarColumns)
	}
	for i, track := range tracks {
		if i != 1 && track != "auto" {
			t.Fatalf("column %d is %q: every column but the tools' is as wide as its content", i+1, track)
		}
	}
	sheet := chatbug080TopbarStylesheet()
	for _, rule := range []string{
		`@media(min-width:700px) and (max-width:1000px){`,
		`.app-shell .topbar>.brand-cluster{grid-column:1;grid-row:1}`,
		`.app-shell .topbar>.header-navigation-tools{grid-column:2;grid-row:1`,
		`.app-shell .topbar>.locale-menu{grid-column:3;grid-row:1;flex:none;width:auto`,
		`.app-shell .topbar>:nth-child(4){grid-column:4;grid-row:1}`,
		`.app-shell .topbar>:nth-child(5){grid-column:5;grid-row:1}`,
	} {
		if !strings.Contains(sheet, rule) {
			t.Errorf("the tablet bar lacks %q", rule)
		}
	}
	platformStylesOnce.Do(initPlatformStyles)
	if !strings.HasSuffix(platformStyles, sheet) {
		t.Error("the tablet bar is not the last word of the product stylesheet")
	}
}

// TestTodo_CHATBUG_080_Browser renders the whole page with a favourite and
// reads the rail: collapsed (icons only) the destination is listed once; open,
// the Favorites section keeps its own entry beside the full list.
func TestTodo_CHATBUG_080_Browser(t *testing.T) {
	rail := func(collapsed bool) string {
		view := ApplyRequest(testView(PagePeople), PageRequest{FavoritePages: []PageID{PagePeople}, NavCollapsed: collapsed})
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		return doc
	}
	collapsed, expanded := rail(true), rail(false)
	if strings.Contains(collapsed, `>Favorites</li>`) || !strings.Contains(expanded, `>Favorites</li>`) {
		t.Error("the Favorites section belongs to the open rail alone")
	}
	label := `aria-label="People"`
	if got := strings.Count(collapsed, label); got != 1 {
		t.Errorf("collapsed, People is listed %d times, want once", got)
	}
	if got := strings.Count(expanded, label); got != 2 {
		t.Errorf("open, People is listed %d times, want in Favorites and in the full list", got)
	}
}
