package journey

import (
	"strings"
	"testing"
)

// TestTodo_UXBLIND_057_Browser verifies the standalone journey renderer opts
// into the same controller as the product shell. A real browser mount is
// exercised by the WASM harness; this native test pins the page contract that
// makes the Share disclosure participate in that controller when present.
func TestTodo_UXBLIND_057_Browser(t *testing.T) {
	markup, err := RenderToString(Page{Locale: "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `data-hcm-popover-controller="uxblind-q"`) {
		t.Fatal("standalone journey page did not mount the shared popover controller")
	}
}
