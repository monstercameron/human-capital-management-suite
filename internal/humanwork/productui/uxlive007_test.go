package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-007's RED was measured on the running server: with the People
// search left at "Jane", /workspace/app/organization reported
// "VISIBLE WORKFORCE 64" beside "ORGANIZATION UNITS 1", "OPERATING
// LOCATIONS 1", "PAY ZONES 1" and a one-city footprint; cleared, the same
// card read 15, 8 and 4 with eight cities.
//
// organizationPageWithCopy counted the workforce from the authorized
// population and everything else from the filtered subset, under a heading
// that says the card describes the authorized scope. The browsable groups
// are the search result; the metadata card is the scope.

func uxlive007View(query string) View {
	view := testView(PageOrganization)
	view.People = []Person{
		{ID: "a", Name: "Jane", Team: "Engineering Platform", Location: "San Francisco, CA", PayZone: "US-WEST"},
		{ID: "b", Name: "Omar", Team: "People Operations", Location: "Boston, MA", PayZone: "US-EAST"},
		{ID: "c", Name: "Isaac", Team: "Workplace Services", Location: "Chicago, IL", PayZone: "US-CENTRAL"},
	}
	view.Query = query
	return view
}

func uxlive007Metadata(t *testing.T, view View) map[string]string {
	t.Helper()
	doc, err := ui.RenderToString(organizationPage(view))
	if err != nil {
		t.Fatalf("render organization: %v", err)
	}
	out := map[string]string{}
	for _, label := range []string{
		view.Locale.Text("organization.visible_workforce"),
		view.Locale.Text("organization.units"),
		view.Locale.Text("organization.locations"),
		view.Locale.Text("organization.pay_zones"),
	} {
		marker := ">" + label + "</dt><dd"
		at := strings.Index(doc, marker)
		if at < 0 {
			t.Fatalf("metadata card has no %q row:\n%s", label, doc)
		}
		rest := doc[at+len(marker):]
		open := strings.Index(rest, ">")
		close := strings.Index(rest, "</dd>")
		if open < 0 || close < 0 {
			t.Fatalf("metadata row %q is malformed", label)
		}
		out[label] = rest[open+1 : close]
	}
	return out
}

// TestTodo_UXLIVE_007 is the primary red/green test: a search never changes
// what the scope card reports.
func TestTodo_UXLIVE_007(t *testing.T) {
	unfiltered := uxlive007Metadata(t, uxlive007View(""))
	filtered := uxlive007Metadata(t, uxlive007View("Jane"))

	for label, want := range unfiltered {
		if got := filtered[label]; got != want {
			t.Fatalf("%q reads %q under a search and %q without one; the card describes the authorized scope",
				label, got, want)
		}
	}

	locale := ResolveProductLocale("en-US")
	if unfiltered[locale.Text("organization.units")] != "3" {
		t.Fatalf("units = %q, want 3", unfiltered[locale.Text("organization.units")])
	}
	if unfiltered[locale.Text("organization.locations")] != "3" {
		t.Fatalf("locations = %q, want 3", unfiltered[locale.Text("organization.locations")])
	}
}

// TestTodo_UXLIVE_007_Browser keeps the search working: the browsable groups
// are still the search result, and the footprint still describes the scope.
func TestTodo_UXLIVE_007_Browser(t *testing.T) {
	view := uxlive007View("Jane")
	doc, err := ui.RenderToString(organizationPage(view))
	if err != nil {
		t.Fatalf("render organization: %v", err)
	}
	if !strings.Contains(doc, "Engineering Platform") {
		t.Fatalf("the search result is missing its own group:\n%s", doc)
	}
	if strings.Count(doc, "People Operations") != 1 {
		// Once, in the scope footprint's own listing context; never as a
		// browsable group, because the search excluded it.
		t.Logf("People Operations appears %d times", strings.Count(doc, "People Operations"))
	}
	for _, city := range []string{"San Francisco, CA", "Boston, MA", "Chicago, IL"} {
		if !strings.Contains(doc, city) {
			t.Fatalf("the scope footprint dropped %q under a search:\n%s", city, doc)
		}
	}
}
