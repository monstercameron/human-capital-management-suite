package productui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// #3770: every person-avatar palette pair is legible in its theme. The values
// are read back from the served stylesheet, so a token that never reached the
// sheet, or reached only one theme, fails here.
func TestChatDesign_3770_AvatarPaletteMeetsContrast(t *testing.T) {
	light, dark := chatDesignThemeVars(t, false), chatDesignThemeVars(t, true)
	for _, theme := range []struct {
		name string
		vars map[string]string
	}{{"light", light}, {"dark", dark}} {
		backgrounds := map[string]bool{}
		for slot := 1; slot <= len(avatarPalette()); slot++ {
			name := "hcm-avatar-" + strconv.Itoa(slot)
			background, ink := theme.vars[name+"-bg"], theme.vars[name+"-ink"]
			if background == "" || ink == "" {
				t.Fatalf("%s theme does not declare --%s-bg and --%s-ink", theme.name, name, name)
			}
			if ratio := chatDesignContrast(t, theme.vars, ink, background); ratio < 4.5 {
				t.Errorf("%s slot %d: initials %s on %s are %.2f:1, below 4.5:1", theme.name, slot, ink, background, ratio)
			}
			backgrounds[background] = true
			// Chat draws the slot with exactly these two tokens.
			if rule := `[data-avatar-hue="` + strconv.Itoa(slot) + `"]{background:var(--` + name + `-bg);color:var(--` + name + `-ink)}`; !strings.Contains(chatui.Stylesheet, rule) {
				t.Errorf("Chat has no rule %q", rule)
			}
		}
		if len(backgrounds) != len(avatarPalette()) {
			t.Errorf("%s palette repeats a background", theme.name)
		}
	}
	if light["hcm-avatar-1-bg"] == dark["hcm-avatar-1-bg"] {
		t.Fatal("the dark theme reuses the light avatar palette")
	}
	// A device that prefers dark under the system setting gets the dark palette.
	if css := Stylesheet(); !strings.Contains(css, `--hcm-avatar-8-ink:`+dark["hcm-avatar-8-ink"]) || strings.Count(css, `--hcm-avatar-8-ink:`+dark["hcm-avatar-8-ink"]) < 2 {
		t.Fatal("the dark avatar palette is not declared for both the explicit and the system dark mode")
	}
}
