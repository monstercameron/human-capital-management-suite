package productui

import (
	"strings"
	"testing"
)

// TestTodo_UXBLIND_087_Browser navigates three pages and checks the
// browser-tab title each time: the page's own name plus the company display
// name, refreshed on every navigation.
func TestTodo_UXBLIND_087_Browser(t *testing.T) {
	pages := []struct {
		page  PageID
		title string
	}{
		{PageHome, "Home · Ironridge Builders"},
		{PagePeople, "People · Ironridge Builders"},
		{PageOrganization, "Organization · Ironridge Builders"},
	}
	for _, tc := range pages {
		view := testView(tc.page)
		view.Tenant = "Ironridge Builders"
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(doc, "<title>"+tc.title+"</title>") {
			start := strings.Index(doc, "<title>")
			end := strings.Index(doc, "</title>")
			got := ""
			if start >= 0 && end > start {
				got = doc[start : end+8]
			}
			t.Fatalf("%s title = %q, want %q", tc.page, got, tc.title)
		}
	}
}

// TestTodo_UXBLIND_087_Regression pins the RED observations: the tab never
// shows the product suite name, never the Home greeting, and the People page
// is titled People rather than Journeys.
func TestTodo_UXBLIND_087_Regression(t *testing.T) {
	for _, page := range []PageID{PageHome, PagePeople, PageOrganization} {
		view := testView(page)
		view.Tenant = "Ironridge Builders"
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		start := strings.Index(doc, "<title>")
		end := strings.Index(doc, "</title>")
		if start < 0 || end <= start {
			t.Fatalf("%s render has no title element", page)
		}
		title := doc[start+len("<title>") : end]
		if strings.Contains(title, "Human Capital Management Suite") {
			t.Fatalf("%s title shows the suite name: %q", page, title)
		}
		if strings.Contains(title, "Good ") {
			t.Fatalf("%s title leaked the greeting: %q", page, title)
		}
	}
	people := testView(PagePeople)
	people.Tenant = "Ironridge Builders"
	doc, err := Render(people)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "<title>People · Ironridge Builders</title>") {
		t.Fatalf("People page is not titled People: %s", doc[strings.Index(doc, "<title>"):strings.Index(doc, "</title>")+8])
	}
}
