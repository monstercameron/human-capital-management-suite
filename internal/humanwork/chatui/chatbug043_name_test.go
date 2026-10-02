package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATBUG_043_CountryName: a flag drawn as the letters "US" in a box,
// which is what a platform without flag glyphs (Windows) gets, is named for the
// country, in the reader's language, for assistive technology and as its
// tooltip. The letters are the sighted fallback; the name is the country's.
func TestTodo_CHATBUG_043_CountryName(t *testing.T) {
	// The Arabic emoji file names the flags too, so the chip is
	// named for the country in the reader's language under every locale.
	for locale, want := range map[string]string{"en-US": "United States", "de-DE": "Vereinigte Staaten", "ar": "الولايات المتحدة"} {
		m := emojiModel(locale)
		withEmojiHost(t, m, func(*localUI) {
			withFlagsDrawn(false, func() {
				chip := renderNode(t, chatEmojiGlyphNode(m, "🇺🇸"))
				if !strings.Contains(chip, `class="emoji-flag-chip"`) || !strings.Contains(chip, ">US<") || !strings.Contains(chip, `role="img"`) {
					t.Fatalf("%s: the fallback is not the letters in a chip: %s", locale, chip)
				}
				if name := emojiFlagName(m, "🇺🇸"); !strings.Contains(name, want) || !strings.Contains(chip, `aria-label="`+name+`"`) || !strings.Contains(chip, `title="`+name+`"`) {
					t.Errorf("%s: the chip is named %q, want the country %q: %s", locale, name, want, chip)
				}
			})
		})
	}
}
