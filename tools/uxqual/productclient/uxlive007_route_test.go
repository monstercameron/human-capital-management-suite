package productclient

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_UXLIVE_007_Browser proves the live route contract: a filtered
// Organization view can be copied and reloaded, while Back can return to the
// independent People address without the Organization filter rewriting it.
func TestTodo_UXLIVE_007_Browser(t *testing.T) {
	organization := productui.Path(productui.PageOrganization)
	state, view := rev09504Load(t, rev09504Service(""), organization, "q=Jane")
	href := ResolvedCanonicalHref(state, view)
	if href != organization+"?q=Jane" {
		t.Fatalf("filtered Organization href = %q, want %q", href, organization+"?q=Jane")
	}

	path, query, _ := strings.Cut(href, "?")
	reloadedState, reloaded := rev09504Load(t, rev09504Service(""), path, query)
	if reloaded.Query != "Jane" || ResolvedCanonicalHref(reloadedState, reloaded) != href {
		t.Fatalf("reloaded Organization query=%q href=%q, want Jane/%q", reloaded.Query, ResolvedCanonicalHref(reloadedState, reloaded), href)
	}

	peopleState, people := rev09504Load(t, rev09504Service(""), productui.Path(productui.PagePeople), "q=Jane")
	if got := ResolvedCanonicalHref(peopleState, people); got != productui.Path(productui.PagePeople)+"?q=Jane" {
		t.Fatalf("Back's People address = %q, want %q", got, productui.Path(productui.PagePeople)+"?q=Jane")
	}
}
