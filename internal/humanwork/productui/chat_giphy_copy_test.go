package productui

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestChatGiphyPickerStatusLabelsAreLocalized(t *testing.T) {
	keys := []string{chatui.KeyGiphyLoading, chatui.KeyGiphyLoadError, chatui.KeyGiphyNoResults, chatui.KeyGiphyClose}
	for _, locale := range []string{"de-DE", "ar"} {
		translations := chatTranslations(locale)
		for _, key := range keys {
			if got := translations[key]; got == "" || got == chatui.EnglishCopy()[key] {
				t.Errorf("%s GIPHY string %s = %q; want localized copy", locale, key, got)
			}
		}
	}
}
