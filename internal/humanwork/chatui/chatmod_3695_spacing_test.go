package chatui

import (
	"strings"
	"testing"
)

// TestTodo_3695_PermissionsSpacing: the Permissions tab's explanatory text is
// not set against the tab bar, and the table's bordered region keeps its space
// above and below, so the page does not run the text into the borders.
func TestTodo_3695_PermissionsSpacing(t *testing.T) {
	for _, want := range []string{
		".chatremove .chatmod005-tabs+.chatmod005-muted{margin-block:12px 16px}",
		".chatremove .chatmod005-perm-scroll{margin-block:12px 16px}",
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Fatalf("the stylesheet lacks %q", want)
		}
	}
	// The muted rule resets the margin to zero for every muted paragraph; the
	// Permissions rule must come after it and be more specific, or it loses.
	base := strings.Index(Stylesheet, ".chatmod005-muted{")
	spacing := strings.Index(Stylesheet, ".chatremove .chatmod005-tabs+.chatmod005-muted{")
	if base < 0 || spacing < base {
		t.Fatalf("the tab spacing rule is not after the muted reset: base=%d spacing=%d", base, spacing)
	}
}
