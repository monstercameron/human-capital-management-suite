package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// TestTodo_UIPOLISH_005_Browser is the BROWSER matrix entry: a static
// SSR/DOM assertion over the rendered documents, following the precedent
// TestTodo_UIPOLISH_004_Browser's own comment records -- this lane cannot
// run Playwright, so this proves what a browser would structurally render
// rather than what it paints.
//
// It proves the browser color contract on every surveyed production page:
// the document carries exactly one theme stylesheet, the root exposes the
// color-mode and contrast attributes the dark/high-contrast cascade keys
// on, and every status-bearing element carries non-empty text, so status
// meaning never rides on color alone.
func TestTodo_UIPOLISH_005_Browser(t *testing.T) {
	pages := []PageID{PageHome, PageMyself, PageWork, PageJourneys, PagePeople, PagePerson, PageHistory, PageOrganization, PageAdmin, PageAppearance, PageSettings}
	for _, page := range pages {
		doc, err := Render(testView(page))
		if err != nil {
			t.Fatalf("render %s: %v", page, err)
		}
		if got := strings.Count(doc, "<style>"); got != 1 {
			t.Errorf("page %s renders %d theme style blocks, want exactly 1", page, got)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatalf("page %s: %v", page, err)
		}
		htmlEl := firstElement(root, "html")
		if htmlEl == nil {
			t.Fatalf("page %s has no <html> element", page)
		}
		for _, name := range []string{"data-hcm-color-mode", "data-hcm-contrast"} {
			if attr(htmlEl, name) == "" {
				t.Errorf("page %s root omits %s, the dark/high-contrast cascade key", page, name)
			}
		}
		for _, class := range []string{"status", "count"} {
			for _, node := range findElementsByClassContains(root, class) {
				if strings.TrimSpace(nodeText(node)) == "" {
					t.Errorf("page %s renders an empty .%s element: status carried by color alone", page, class)
				}
			}
		}
		walkElements(root, func(node *xhtml.Node) {
			if node.Type == xhtml.ElementNode && attr(node, "role") == "status" {
				// A live region may legitimately start empty and announce
				// later updates (for example the brand-asset status, which
				// fills only after a logo action). An empty region with no
				// live behavior can never communicate status at all.
				if strings.TrimSpace(nodeText(node)) == "" && attr(node, "aria-live") == "" {
					t.Errorf("page %s renders an empty, non-live role=status region", page)
				}
			}
		})
	}
}

// TestTodo_UIPOLISH_005_Browser_StatusTextReachesTheDocument renders one
// representative status badge directly: the browser contract above is only
// as strong as the components that emit the text.
func TestTodo_UIPOLISH_005_Browser_StatusTextReachesTheDocument(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(EmptyState, EmptyStateProps{
		Title: "No journeys", Description: "Nothing here yet.", Badge: "Awaiting approval", Tone: "warning",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="status warning"`) || !strings.Contains(markup, "Awaiting approval") {
		t.Fatalf("status badge lost its text label: %s", markup)
	}
}
