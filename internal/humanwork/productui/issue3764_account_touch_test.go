package productui

import (
	"strings"
	"testing"
)

// Issue 3764: the account menu's links are 44px touch targets on a phone.
func TestIssue3764_AccountMenuLinksAreTouchTargets(t *testing.T) {
	sheet := issue3764AccountLinkStylesheet()
	for _, want := range []string{"@media(max-width:760px)", ".account-menu-link{", "min-block-size:44px", "min-inline-size:44px"} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("account link sheet lacks %q: %s", want, sheet)
		}
	}
	if !strings.Contains(platformStyles, sheet) {
		t.Fatal("the platform sheet does not carry the account link touch rule")
	}
}
