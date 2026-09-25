package productui

import (
	"net/url"
	"strings"
	"testing"
)

// The navigation toggle on a project board rebuilt its link from the page
// profile, which dropped project/board_view; the board then failed to load
// as a malformed route. Shell links must keep the board's selectors.
func TestNavigationToggleKeepsProjectSelectors(t *testing.T) {
	for _, collapsed := range []bool{false, true} {
		view := View{Page: PageProject, ProjectID: "p-1", ProjectBoardViewID: "default", ProjectTaskID: "t-9", NavCollapsed: collapsed}
		href := navigationToggleProps(view).Href
		parsed, err := url.Parse(href)
		if err != nil || !strings.HasSuffix(parsed.Path, "/project") {
			t.Fatalf("toggle href %q is not a project board address", href)
		}
		query := parsed.Query()
		if query.Get("project") != "p-1" || query.Get("board_view") != "default" || query.Get("task") != "t-9" {
			t.Fatalf("toggle href %q dropped project selectors", href)
		}
	}
}
