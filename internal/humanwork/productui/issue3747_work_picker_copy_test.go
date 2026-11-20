package productui

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestIssue3747_WorkPickerCopyTranslated(t *testing.T) {
	keys := []string{chatui.KeyWorkSuggestTickets, chatui.KeyWorkSuggestTicketsNone, chatui.KeyWorkSuggestBoards, chatui.KeyWorkSuggestBoardsNone, chatui.KeyWorkSuggestFailed, chatui.KeyWorkSuggestUnavailable}
	for _, locale := range []string{"de-DE", "ar"} {
		copy := chatTranslations(locale)
		seen := map[string]string{}
		for _, key := range keys {
			value := copy[key]
			if value == "" {
				t.Errorf("%s has no copy for %s", locale, key)
				continue
			}
			if other, dup := seen[value]; dup {
				t.Errorf("%s gives %s and %s the same sentence", locale, key, other)
			}
			seen[value] = key
		}
	}
}
