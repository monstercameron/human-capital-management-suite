package productui

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestChatEmojiPickerLabelsAreTranslated(t *testing.T) {
	for _, tt := range []struct {
		locale, trigger, title string
	}{
		{locale: "de-DE", trigger: "Emoji einfügen", title: "Emoji auswählen"},
		{locale: "ar", trigger: "إدراج رمز تعبيري", title: "اختر رمزًا تعبيريًا"},
	} {
		translations := chatTranslations(tt.locale)
		if translations[chatui.KeyEmojiPicker] != tt.trigger {
			t.Errorf("%s picker label = %q, want %q", tt.locale, translations[chatui.KeyEmojiPicker], tt.trigger)
		}
		if translations[chatui.KeyEmojiPickerTitle] != tt.title {
			t.Errorf("%s picker title = %q, want %q", tt.locale, translations[chatui.KeyEmojiPickerTitle], tt.title)
		}
		if got := translations[chatui.KeyEmojiItem]; got == "" {
			t.Errorf("%s emoji item label is missing", tt.locale)
		}
	}
}
