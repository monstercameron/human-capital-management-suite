package productui

import (
	"strings"
	"testing"
)

// TestIssue3607_PhoneListBarKeepsOnlyBellAndAccount ties the phone rule that
// hides the workspace bar's other controls to the header the shell really
// draws: the notification slot and the account menu are direct children of the
// topbar and carry the classes the rule spares, while the brand cluster (whose
// link took the click meant for Chat preferences) does not and so is hidden.
// This pins the markup and the rule together; rendered geometry and native hit
// behaviour still need a served browser.
func TestIssue3607_PhoneListBarKeepsOnlyBellAndAccount(t *testing.T) {
	if !strings.Contains(chatShellStylesheet(), `.topbar>:not(.notifications,.account-menu){display:none!important}`) {
		t.Fatal("the list's phone bar no longer hides the other workspace-bar controls with an important declaration")
	}
	spared, hidden := 0, 0
	brandHidden := false
	for _, class := range chatbug080BarChildren(t) {
		fields := strings.Fields(class)
		keep := false
		for _, f := range fields {
			if f == "notifications" || f == "account-menu" {
				keep = true
			}
			if f == "brand-cluster" {
				brandHidden = true
			}
		}
		if keep {
			spared++
		} else {
			hidden++
		}
	}
	if spared != 2 || !brandHidden || hidden < 2 {
		t.Fatalf("topbar children no longer match the phone rule: spared=%d hidden=%d brandHidden=%v", spared, hidden, brandHidden)
	}
}
