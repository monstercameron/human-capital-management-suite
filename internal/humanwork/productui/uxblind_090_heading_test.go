package productui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestTodo_UXBLIND_090_PersonHeadingRenderedOnce(t *testing.T) {
	view := testView(PagePerson)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if findBreadcrumbNav(root) != nil {
		t.Fatal("single person breadcrumb duplicates the page heading")
	}
	if got := countElementsWithID(root, "h1", "page-title"); got != 1 {
		t.Fatalf("person page has %d primary headings, want one", got)
	}
	if !strings.Contains(doc, ResolvePageIdentity(view).Title) {
		t.Fatal("person page heading lost the admitted display name")
	}
}

func TestTodo_UXBLIND_090_GenericPersonDoesNotDiscloseName(t *testing.T) {
	view := testView(PagePerson)
	view.SelectedPerson = ""
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if findBreadcrumbNav(root) != nil {
		t.Fatal("generic person page rendered a redundant breadcrumb")
	}
	if strings.Contains(doc, "Avery Patel") {
		t.Fatal("generic person page disclosed an unselected worker")
	}
	if !strings.Contains(doc, ">Person</h1>") {
		t.Fatal("generic person page lost its generic heading")
	}
}

func TestTodo_UXBLIND_090_MultiSegmentBreadcrumbRemains(t *testing.T) {
	view := testView(PageHistory)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if findBreadcrumbNav(root) == nil {
		t.Fatal("ordinary multi-segment breadcrumb was hidden")
	}
}

func countElementsWithID(root *xhtml.Node, name, id string) int {
	count := 0
	var visit func(*xhtml.Node)
	visit = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && node.Data == name {
			for _, attr := range node.Attr {
				if attr.Key == "id" && attr.Val == id {
					count++
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(root)
	return count
}
