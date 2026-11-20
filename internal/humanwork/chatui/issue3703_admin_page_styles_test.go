package chatui

import (
	"strings"
	"testing"
)

// ISSUE-3703: the Chat settings page is outside the chat workspace scope, so the
// required mark on Basis (and the red refusal message) must be in the page's own
// sheet, not only in the scoped chat sheet where the live page never saw it.
func TestIssue3703_AdminPageSheetCarriesBasisMarker(t *testing.T) {
	if !strings.Contains(TranslationAdminPageStyles, `label[for="chatmapadmin-basis"]::after{content:" *"`) {
		t.Fatal("the admin page's own sheet lacks the required mark on Basis")
	}
	if !strings.Contains(TranslationAdminPageStyles, ".chatmapadmin .field-hint.field-error{color:var(--hcm-color-danger)}") {
		t.Fatal("the admin page's own sheet lacks the red refusal message")
	}
}
