package productui

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_WEB_013(t *testing.T) {
	theme, err := ResolveTheme(map[string]string{
		"color.brand.primary": "#7a1f5c",
		"color.brand.hover":   "#5a1241",
		"color.brand.soft":    "#f7eaf1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := theme.Value("color.brand.primary"); value != "#7a1f5c" {
		t.Fatalf("resolved primary = %q", value)
	}
	for _, want := range []string{"--hcm-color-brand-primary:#7a1f5c", "--hcm-color-text:#102238", "--hcm-color-focus:#102238"} {
		if !strings.Contains(theme.CSS(), want) {
			t.Fatalf("compiled theme missing %q", want)
		}
	}
}

func TestTodo_WEB_013_Golden(t *testing.T) {
	first, _ := ResolveTheme(nil)
	second, _ := ResolveTheme(map[string]string{})
	if first.CSS() != second.CSS() {
		t.Fatal("default theme compilation is nondeterministic")
	}
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(first.CSS())))
	// Updated when the shape scale was tightened: radius.control 8px -> 6px,
	// radius.surface 12px -> 10px, and a radius.xs added for the marks that
	// were carrying raw pixel literals, and again when size.control joined it
	// so every inline control could share one height. The digest exists to catch a theme
	// that changed without anybody deciding to; this one was decided.
	const want = "d4061d7be5771679d355a6e9df2dd72cae76440ca698dec70d8a4b8a0c49fbc3"
	if got != want {
		t.Fatalf("default theme digest = %s, want %s", got, want)
	}
}

func TestTodo_WEB_013_Browser(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{"--hcm-color-brand-primary", "--accent:var(--hcm-color-brand-primary)", "background-color:var(--canvas)", "color:var(--ink)"} {
		if !strings.Contains(css, want) {
			t.Fatalf("browser stylesheet does not consume semantic token %q", want)
		}
	}
	custom, err := StylesheetForTheme(map[string]string{
		"color.brand.primary": "#7a1f5c",
		"color.brand.hover":   "#5a1241",
		"color.brand.soft":    "#f7eaf1",
	})
	if err != nil || !strings.Contains(custom, "--hcm-color-brand-primary:#7a1f5c") {
		t.Fatalf("customer theme did not reach the complete browser sheet: err=%v", err)
	}
}

func TestTodo_WEB_013_Conformance(t *testing.T) {
	for name, overrides := range map[string]map[string]string{
		"unknown token":         {"color.brand.secret": "#000000"},
		"protected focus":       {"color.focus": "#ffffff"},
		"css injection":         {"color.brand.primary": "#006b57;background:red"},
		"insufficient contrast": {"color.brand.primary": "#fefefe"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ResolveTheme(overrides); err == nil {
				t.Fatal("unsafe theme override was accepted")
			}
		})
	}
}

func TestTodo_WEB_014(t *testing.T) {
	theme, err := ResolveTheme(map[string]string{"typography.font.sans": `Inter,"Segoe UI",sans-serif`})
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"typography.font.sans", "typography.size.body", "typography.size.small", "typography.size.heading", "typography.line-height"} {
		if _, ok := theme.Value(token); !ok {
			t.Errorf("typography scale missing %q", token)
		}
	}
}

func TestTodo_WEB_014_Golden(t *testing.T) { assertThemeKindOrder(t, ThemeTypography) }
func TestTodo_WEB_014_Browser(t *testing.T) {
	assertCSSContains(t, "font-family:var(--hcm-font-sans)", "font-size:var(--hcm-font-size-heading)")
}
func TestTodo_WEB_014_Conformance(t *testing.T) {
	if _, err := ResolveTheme(map[string]string{"typography.font.sans": "Inter;display:none"}); err == nil {
		t.Fatal("unsafe font stack was accepted")
	}
}

func TestTodo_WEB_015(t *testing.T) {
	theme, err := ResolveTheme(map[string]string{"spacing.density": ".875"})
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := theme.Value("spacing.density"); value != ".875" {
		t.Fatalf("density = %q", value)
	}
}

func TestTodo_WEB_015_Golden(t *testing.T)  { assertThemeKindOrder(t, ThemeSpacing) }
func TestTodo_WEB_015_Browser(t *testing.T) { assertCSSContains(t, "--hcm-space-1", "--hcm-density") }
func TestTodo_WEB_015_Conformance(t *testing.T) {
	if _, err := ResolveTheme(map[string]string{"spacing.density": ".25"}); err == nil {
		t.Fatal("unbounded density was accepted")
	}
}

func TestTodo_WEB_016(t *testing.T) {
	theme, err := ResolveTheme(map[string]string{"radius.control": "6px", "radius.surface": "1rem"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--hcm-radius-control:6px", "--hcm-radius-surface:1rem", "--hcm-shadow-resting", "--hcm-shadow-raised"} {
		if !strings.Contains(theme.CSS(), want) {
			t.Errorf("surface theme missing %q", want)
		}
	}
}

func TestTodo_WEB_016_Golden(t *testing.T) { assertThemeKindOrder(t, ThemeElevation) }
func TestTodo_WEB_016_Browser(t *testing.T) {
	assertCSSContains(t, "--radius:var(--hcm-radius-control)", "box-shadow:var(--hcm-shadow-raised)")
}
func TestTodo_WEB_016_Conformance(t *testing.T) {
	if _, err := ResolveTheme(map[string]string{"radius.surface": "40px"}); err == nil {
		t.Fatal("unbounded surface radius was accepted")
	}
}

func TestTodo_WEB_017(t *testing.T) {
	definitions := IconDefinitions()
	if len(definitions) < 10 {
		t.Fatalf("governed icon vocabulary has only %d entries", len(definitions))
	}
	seen := map[string]bool{}
	for _, definition := range definitions {
		if definition.Name == "" || definition.Path == "" || seen[definition.Name] {
			t.Fatalf("invalid icon definition %+v", definition)
		}
		seen[definition.Name] = true
	}
}

func TestTodo_WEB_017_Golden(t *testing.T) {
	if got := iconPath("journeys"); got != "M5 4h10l4 4v12H5zM15 4v4h4M8 12h8M8 16h6" {
		t.Fatalf("journey glyph changed to %q", got)
	}
}
func TestTodo_WEB_017_Browser(t *testing.T) {
	out, err := ui.RenderToString(navIcon("journeys"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `aria-hidden="true"`) || !strings.Contains(out, `focusable="false"`) {
		t.Fatalf("decorative icon entered the accessibility tree: %s", out)
	}
}
func TestTodo_WEB_017_Conformance(t *testing.T) {
	if got := iconPath("<script>"); got != fallbackIconPath {
		t.Fatalf("unknown icon selected an ungoverned path %q", got)
	}
}

func TestTodo_WEB_018(t *testing.T) {
	theme, err := ResolveTheme(map[string]string{"motion.duration.fast": "100ms", "motion.duration.normal": "200ms", "motion.duration.slow": "360ms"})
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := theme.Value("motion.duration.slow"); value != "360ms" {
		t.Fatalf("motion duration = %q", value)
	}
}

func TestTodo_WEB_018_Golden(t *testing.T) { assertThemeKindOrder(t, ThemeMotion) }
func TestTodo_WEB_018_Browser(t *testing.T) {
	assertCSSContains(t,
		"@keyframes hcm-page-enter", "@keyframes hcm-shimmer", "prefers-reduced-motion:no-preference",
		`.brand-cluster{align-items:center;`, `.header-nav-toggle{display:grid`, `.nav-group::details-content`,
		`:root[data-hcm-motion-preference="limited"]`,
	)
}
func TestTodo_WEB_018_Conformance(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{"@media (prefers-reduced-motion:reduce)", "animation:none!important", "transition-duration:.01ms!important", "scroll-behavior:auto!important"} {
		if !strings.Contains(css, want) {
			t.Fatalf("reduced-motion safety missing %q", want)
		}
	}
	if _, err := ResolveTheme(map[string]string{"motion.duration.slow": "5000ms"}); err == nil {
		t.Fatal("unbounded customer motion was accepted")
	}
}

func assertThemeKindOrder(t *testing.T, kind ThemeTokenKind) {
	t.Helper()
	last := -1
	for index, token := range ThemeTokens() {
		if token.Kind == kind {
			if index <= last {
				t.Fatalf("%s token registry is not stable", kind)
			}
			last = index
		}
	}
	if last < 0 {
		t.Fatalf("theme registry has no %s tokens", kind)
	}
}

// TestTypedKeyframesNamesResolve guards the runtime contract behind GWC's
// content-hashed animation names: every animation-name the sheet assigns must
// have a matching @keyframes block, and every plain hcm-* animation reference
// must resolve. Otherwise an animation silently never runs.
func TestTypedKeyframesNamesResolve(t *testing.T) {
	css := Stylesheet()
	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`@keyframes\s+([A-Za-z0-9_-]+)`).FindAllStringSubmatch(css, -1) {
		defined[m[1]] = true
	}
	if len(defined) == 0 {
		t.Fatal("production stylesheet defines no keyframes")
	}
	for _, m := range regexp.MustCompile(`animation-name:([A-Za-z0-9_-]+)`).FindAllStringSubmatch(css, -1) {
		if !defined[m[1]] {
			t.Errorf("animation-name %q has no @keyframes block", m[1])
		}
	}
	for _, m := range regexp.MustCompile(`animation:(hcm-[A-Za-z0-9_-]+)`).FindAllStringSubmatch(css, -1) {
		if !defined[m[1]] {
			t.Errorf("plain animation reference %q has no @keyframes block", m[1])
		}
	}
}

func assertCSSContains(t *testing.T, wants ...string) {
	t.Helper()
	css := Stylesheet()
	for _, want := range wants {
		if !strings.Contains(css, want) {
			t.Fatalf("stylesheet missing %q", want)
		}
	}
}
