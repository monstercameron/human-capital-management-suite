package productui

import (
	"strings"
	"testing"
)

// TestIssue92_OrgRoutesDoNotShowUnavailableCopyOverContent proves the outline
// and explorer pages, which render authorized organization content, do not
// also carry the page-level "not available yet" notice in their subtitle.
func TestIssue92_OrgRoutesDoNotShowUnavailableCopyOverContent(t *testing.T) {
	const notice = "This area is not available yet."
	for _, page := range []PageID{PageOrgOutline, PageOrgExplorer} {
		view := uxAudit004View()
		view.Page = page
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(doc, "ownership-tree") && !strings.Contains(doc, "organization-") {
			t.Fatalf("%s: fixture rendered no organization content", page)
		}
		if strings.Contains(doc, notice) {
			t.Errorf("%s: unavailable notice shown above populated organization content", page)
		}
	}
}
