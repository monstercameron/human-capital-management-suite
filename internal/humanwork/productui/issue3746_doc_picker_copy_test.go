package productui

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestIssue3746_DocPickerCopyTranslated(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		copy := chatTranslations(locale)
		for _, key := range []string{chatui.KeyDocSuggestNone, chatui.KeyDocSuggestFailed, chatui.KeyDocSuggestUnavailable} {
			if copy[key] == "" {
				t.Errorf("%s has no copy for %s", locale, key)
			}
		}
		if copy[chatui.KeyDocSuggestFailed] == copy[chatui.KeyDocSuggestNone] || copy[chatui.KeyDocSuggestUnavailable] == copy[chatui.KeyDocSuggestNone] {
			t.Errorf("%s reuses the no-match sentence for a failure", locale)
		}
	}
}
