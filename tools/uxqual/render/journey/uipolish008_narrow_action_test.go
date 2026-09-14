package journey

import (
	"regexp"
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_008_Regression_NarrowJourneyHeaderActionWraps(t *testing.T) {
	css := Stylesheet()
	container := regexp.MustCompile(`\.jn-context-actions\{[^}]*\}`).FindString(css)
	if !strings.Contains(container, "min-width:0") || !strings.Contains(container, "max-width:100%") {
		t.Fatalf("Journey header actions lost their bounded responsive measure: %s", container)
	}
	button := regexp.MustCompile(`\.jn-context-actions \.jn-btn\{[^}]*\}`).FindString(css)
	if !strings.Contains(button, "max-width:100%") || !strings.Contains(button, "white-space:normal") || !strings.Contains(button, "overflow-wrap:break-word") {
		t.Fatalf("Journey header action can overflow instead of wrapping its label: %s", button)
	}
	page := Page{Locale: "en-US", List: &ListView{People: &PeopleView{DirectoryLink: NavLink{Href: "/workspace/app/people"}}}}
	markup := renderNode(t, embeddedListView(page, *page.List))
	if !strings.Contains(markup, `class="jn-context-actions"`) || !strings.Contains(markup, `Choose an employee to promote`) {
		t.Fatal("production Journey list no longer exercises the bounded action")
	}
}
