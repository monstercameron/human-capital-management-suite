package productui

import (
	"strings"
	"testing"
)

// The "More" menu and the "Add to a conversation" form are popovers styled
// with display:grid. An author display value overrides the browser's rule
// that hides a closed popover, so both were drawn permanently over the page.
func TestAgentSetupClosedPopoversAreHidden(t *testing.T) {
	css := personaAdminStylesheet() + agentUXR7Stylesheet()
	if !strings.Contains(css, ".persona-admin-page [popover]:not(:popover-open){display:none}") {
		t.Fatal("a closed popover on Agent setup is not hidden")
	}
	for _, selector := range []string{".persona-admin-secondary-actions", ".persona-admin-add-placement-form"} {
		if !strings.Contains(css, ".persona-admin-page "+selector+":popover-open") {
			t.Fatalf("%s has no open-state position rule", selector)
		}
	}
}
