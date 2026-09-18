package productclient

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// UXLIVE-022 was filed as "a cold load of a copied journey link renders the
// list". Re-measured against a rebuilt server it does not: the boot
// recognises the standalone fragment and canonicalises the address before
// the history router forms its first loader key, and the journey renders.
// The original observation was taken three seconds after navigation, before
// this bundle had mounted.
//
// No code changed for that todo. These tests pin the behaviour so the
// resolution cannot be lost silently, which is the only durable outcome a
// corrected finding can leave behind.

// TestTodo_UXLIVE_022 is the primary red/green test: a copied journey
// fragment resolves to the canonical product address for that journey.
func TestTodo_UXLIVE_022(t *testing.T) {
	const id = "01a0b18f-94ce-7560-b01f-71a6af3b581b"
	path := productui.Path(productui.PageJourneys)

	href, ok := ProductJourneyHashHref(path, "#/journeys/"+id, "locale=en-US&nav=collapsed")
	if !ok {
		t.Fatalf("a copied journey fragment was not recognised at all")
	}
	if !strings.Contains(href, "journey="+id) {
		t.Fatalf("the canonical address does not name the journey: %q", href)
	}
	if strings.Contains(href, "#") {
		t.Fatalf("the canonical address still carries a fragment: %q", href)
	}
	if !strings.HasPrefix(href, path) {
		t.Fatalf("the canonical address left the journeys page: %q", href)
	}

	// Reader preferences carried on the original address survive the
	// transition; journey-specific state does not.
	if !strings.Contains(href, "nav=collapsed") {
		t.Fatalf("a shell preference was dropped: %q", href)
	}
}

// TestTodo_UXLIVE_022_Browser keeps the resolution narrow: only this page's
// own journey fragments are adopted, so an unrelated fragment or another
// page's address is left exactly as the reader typed it.
func TestTodo_UXLIVE_022_Browser(t *testing.T) {
	path := productui.Path(productui.PageJourneys)
	for _, c := range []struct {
		name, path, fragment string
	}{
		{"another page", productui.Path(productui.PagePeople), "#/journeys/01a0b18f"},
		{"not a journey fragment", path, "#section-two"},
		{"empty fragment", path, ""},
	} {
		if _, ok := ProductJourneyHashHref(c.path, c.fragment, ""); ok {
			t.Fatalf("%s: %q was adopted as a journey address", c.name, c.fragment)
		}
	}
}
