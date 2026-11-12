package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestChatRetentionInvalidStorageLimitShowsInlineRangeMessage3703 proves an
// invalid storage limit is reported inline at the moment it is entered, because
// the save button stays disabled and a submit-only check never runs.
func TestChatRetentionInvalidStorageLimitShowsInlineRangeMessage3703(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		resolved := ResolveProductLocale(locale)
		copy := retentionCopy(resolved)
		for _, mib := range []string{"-5", "0", "abc", "99999999999999999999"} {
			draft := retentionDraft(ChatRetentionPolicy{Mode: "SIZE_BUDGET", BudgetBytes: 1 << 30}, true)
			draft.BudgetMiB = mib
			next := retentionEdited(draft)
			if !next.Invalid {
				t.Fatalf("%s: storage limit %q was not flagged invalid after edit", locale, mib)
			}
			if retentionDraftValid(next) {
				t.Fatalf("%s: storage limit %q unexpectedly valid", locale, mib)
			}
			doc, err := ui.RenderToString(validationMessage(copy, next))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(doc, `role="alert"`) || !strings.Contains(doc, copy.invalidBudget) {
				t.Fatalf("%s: storage limit %q rendered no inline range message: %s", locale, mib, doc)
			}
		}
	}
}

func TestChatRetentionValidStorageLimitClearsInlineMessage3703(t *testing.T) {
	draft := retentionDraft(ChatRetentionPolicy{Mode: "SIZE_BUDGET", BudgetBytes: 1 << 30}, true)
	draft.BudgetMiB = "-5"
	draft = retentionEdited(draft)
	draft.BudgetMiB = "5"
	next := retentionEdited(draft)
	if next.Invalid {
		t.Fatalf("valid storage limit 5 still flagged invalid")
	}
	doc, err := ui.RenderToString(validationMessage(retentionCopy(ResolveProductLocale("en-US")), next))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `role="alert"`) {
		t.Fatalf("valid storage limit still renders an alert: %s", doc)
	}
}
