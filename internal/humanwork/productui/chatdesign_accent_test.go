package productui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

var (
	chatDesignMixPattern   = regexp.MustCompile(`^color-mix\(in srgb,(.+) (\d+)%,(.+)\)$`)
	chatDesignVarPattern   = regexp.MustCompile(`^var\(--([a-z0-9-]+)\)$`)
	chatDesignBrandAsText  = regexp.MustCompile(`[;{]color:var\(--hcm-color-brand-primary\)`)
	chatDesignAccentToken  = regexp.MustCompile(`--chat-accent-text:var\(--([a-z0-9-]+)\)`)
	chatDesignDeclarations = regexp.MustCompile(`--([a-z0-9-]+):([^;}]+)`)
)

// chatDesignThemeVars returns the custom properties one theme declares on
// :root, read from the served stylesheet: every top-level :root rule in order,
// then, for dark, the explicit dark rules laid over them. Rules inside @media
// (print, forced colours, contrast) belong to neither theme and are skipped.
func chatDesignThemeVars(t *testing.T, dark bool) map[string]string {
	t.Helper()
	css := Stylesheet()
	vars := map[string]string{}
	read := func(selector string) {
		found := false
		depth, start := 0, 0
		for index := 0; index < len(css); index++ {
			switch css[index] {
			case '{':
				if depth == 0 && strings.TrimSpace(css[start:index]) == selector {
					found = true
					end := index + strings.Index(css[index:], "}")
					for _, match := range chatDesignDeclarations.FindAllStringSubmatch(css[index:end], -1) {
						vars[match[1]] = strings.TrimSpace(match[2])
					}
				}
				depth++
			case '}':
				depth--
				if depth == 0 {
					start = index + 1
				}
			}
		}
		if !found {
			t.Fatalf("stylesheet has no top-level %q rule", selector)
		}
	}
	read(":root")
	if dark {
		read(`:root[data-hcm-color-mode="dark"]`)
	}
	return vars
}

// chatDesignResolve reduces a declared colour to #rrggbb, following var() and
// color-mix(in srgb, A P%, B) the way the browser does.
func chatDesignResolve(vars map[string]string, value string, depth int) (string, error) {
	value = strings.TrimSpace(value)
	if depth > 8 {
		return "", fmt.Errorf("reference cycle at %q", value)
	}
	if match := chatDesignVarPattern.FindStringSubmatch(value); match != nil {
		next, ok := vars[match[1]]
		if !ok {
			return "", fmt.Errorf("--%s is not declared", match[1])
		}
		return chatDesignResolve(vars, next, depth+1)
	}
	if match := chatDesignMixPattern.FindStringSubmatch(value); match != nil {
		first, err := chatDesignResolve(vars, match[1], depth+1)
		if err != nil {
			return "", err
		}
		second, err := chatDesignResolve(vars, match[3], depth+1)
		if err != nil {
			return "", err
		}
		share, _ := strconv.Atoi(match[2])
		mixed := "#"
		for index := 0; index < 3; index++ {
			a, _ := strconv.ParseUint(first[1+index*2:3+index*2], 16, 8)
			b, _ := strconv.ParseUint(second[1+index*2:3+index*2], 16, 8)
			mixed += fmt.Sprintf("%02x", (int(a)*share+int(b)*(100-share)+50)/100)
		}
		return mixed, nil
	}
	if len(value) == 4 && value[0] == '#' {
		value = "#" + strings.Repeat(value[1:2], 2) + strings.Repeat(value[2:3], 2) + strings.Repeat(value[3:4], 2)
	}
	if !hexColorPattern.MatchString(value) {
		return "", fmt.Errorf("unsupported colour %q", value)
	}
	return strings.ToLower(value), nil
}

func chatDesignContrast(t *testing.T, vars map[string]string, foreground, background string) float64 {
	t.Helper()
	front, err := chatDesignResolve(vars, foreground, 0)
	if err != nil {
		t.Fatalf("resolve %s: %v", foreground, err)
	}
	back, err := chatDesignResolve(vars, background, 0)
	if err != nil {
		t.Fatalf("resolve %s: %v", background, err)
	}
	ratio, err := contrastRatio(front, back)
	if err != nil {
		t.Fatalf("contrast %s on %s: %v", front, back, err)
	}
	return ratio
}

// #3769: "7 replies", "Show the conversation around it" and the channel links in
// moderation reports were painted with the brand fill colour, which is about
// 2.6:1 on the dark message surface. Accent-coloured text in Chat now reads one
// token, and that token meets 4.5:1 on the message, canvas and selected-card
// backgrounds of both themes.
func TestChatDesign_3769_AccentTextMeetsContrastInBothThemes(t *testing.T) {
	token := chatDesignAccentToken.FindStringSubmatch(chatui.Stylesheet)
	if token == nil {
		t.Fatal("the Chat stylesheet does not declare --chat-accent-text")
	}
	if found := chatDesignBrandAsText.FindAllString(chatui.Stylesheet, -1); len(found) != 0 {
		t.Errorf("%d Chat rules still colour text with the brand fill instead of var(--chat-accent-text)", len(found))
	}
	for _, uses := range []string{`.message-stats{color:var(--chat-accent-text)`, `.chatmod005-context>summary{`, `.chatremove .chatmod005-channel{`} {
		if !strings.Contains(chatui.Stylesheet, uses) {
			t.Errorf("Chat stylesheet no longer carries %q", uses)
		}
	}
	for _, theme := range []struct {
		name string
		dark bool
	}{{"light", false}, {"dark", true}} {
		vars := chatDesignThemeVars(t, theme.dark)
		for _, background := range []string{"surface", "canvas", "soft"} {
			ratio := chatDesignContrast(t, vars, "var(--"+token[1]+")", "var(--"+background+")")
			if ratio < 4.5 {
				t.Errorf("%s: accent text on --%s is %.2f:1, below 4.5:1", theme.name, background, ratio)
			}
			t.Logf("%s: accent text on --%s is %.2f:1", theme.name, background, ratio)
		}
	}
	// The defect itself: the brand fill as text on the dark surface.
	dark := chatDesignThemeVars(t, true)
	if ratio := chatDesignContrast(t, dark, "var(--hcm-color-brand-primary)", "var(--surface)"); ratio >= 4.5 {
		t.Fatalf("the brand fill reads %.2f:1 on the dark surface; this test no longer proves the token is needed", ratio)
	}
}
