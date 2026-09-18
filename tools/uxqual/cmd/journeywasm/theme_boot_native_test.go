//go:build !(js && wasm)

package main

import (
	"os"
	"strings"
	"testing"
)

// The theme and accessibility controllers run only in a browser, so this test
// guards the sequence they are driven in by reading the sources that drive
// them. What it pins is the one ordering fact that produced the flash:
//
// The server renders the stored theme onto <html>. Both controllers start from
// defaults -- DefaultCustomerTheme's colour mode is "system" -- and the boot
// sequence used to apply those defaults straight away, replacing the server's
// correct answer. On a dark device a light workspace went dark for as long as
// the page's data read took (634ms measured warm) and came back when Load ran.
// Every in-app link is a document load, so it happened on every navigation.

func readJourneyWasmSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// TestBootDoesNotApplyDefaultsOverTheServersTheme is the regression test for
// the flash.
func TestBootDoesNotApplyDefaultsOverTheServersTheme(t *testing.T) {
	boot := readJourneyWasmSource(t, "product_wasm.go")
	for _, forbidden := range []string{
		"appearance.Apply(appearance.Saved())",
		"accessibility.Apply(accessibility.Saved())",
	} {
		if strings.Contains(boot, forbidden) {
			t.Errorf("the client applies %q, which before the first Load is the defaults, over the theme the server rendered", forbidden)
		}
	}
	// The route loader still has to discard an unsaved preview on navigation;
	// it does that through Reapply, which knows whether it has anything to say.
	for _, required := range []string{"appearance.Reapply()", "accessibility.Reapply()"} {
		if !strings.Contains(boot, required) {
			t.Errorf("the route loader no longer restores the saved selection with %q", required)
		}
	}
	// And the controllers still take over when real data arrives.
	for _, required := range []string{"appearance.Load(view.Appearance)", "accessibility.Load(view.Accessibility)"} {
		if !strings.Contains(boot, required) {
			t.Errorf("the page load no longer hands the stored selection to the client: %q", required)
		}
	}
}

// TestReapplyWaitsForALoad pins the guard itself: Reapply is a no-op until
// Load (or Save) has given the controller a real selection.
func TestReapplyWaitsForALoad(t *testing.T) {
	for _, name := range []string{"theme_wasm.go", "accessibility_wasm.go"} {
		src := readJourneyWasmSource(t, name)
		at := strings.Index(src, ") Reapply() {")
		if at < 0 {
			t.Fatalf("%s: no Reapply method", name)
		}
		body := src[at:]
		if end := strings.Index(body, "\n}\n"); end >= 0 {
			body = body[:end]
		}
		if !strings.Contains(body, "c.loaded") {
			t.Errorf("%s: Reapply applies without checking that anything has been loaded:\n%s", name, body)
		}
		for _, setter := range []string{"c.loaded = true"} {
			if strings.Count(src, setter) < 2 {
				t.Errorf("%s: loaded is not set by both Load and Save", name)
			}
		}
	}
}
