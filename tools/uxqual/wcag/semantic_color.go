package wcag

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/tokens"
)

// ColorPair is one semantic foreground/background relationship. MinRatio is
// explicit because essential control boundaries require 3:1 while decorative
// dividers may use the lower non-text boundary floor.
type ColorPair struct {
	Name, Foreground, Background string
	MinRatio                     float64
}

// Theme is the closed set of semantic colors admitted by the UI. Callers
// should provide semantic names (text, surface, focus, status-*) rather than
// component-specific palette values.
type Theme struct {
	Name   string
	Colors map[string]string
}

type PairResult struct {
	Pair  ColorPair
	Ratio float64
	Pass  bool
	Error string
}

type ThemeReport struct {
	Theme   Theme
	Pairs   []PairResult
	Passed  bool
	Preview string
}

// DefaultTheme returns a built-in reference palette for exercising the
// generic qualifier. It is not the productui runtime theme and must not be
// used as evidence that a production tenant has been admitted.
func DefaultTheme() Theme {
	p := tokens.Palette
	return Theme{Name: "default-light", Colors: map[string]string{
		"text": p.Text.Hex, "text-muted": p.TextMuted.Hex, "background": p.Background.Hex,
		"surface": p.Surface.Hex, "border": "#737887", "accent": p.Accent.Hex,
		"accent-text": p.AccentText.Hex, "success": p.Success.Hex, "warning": p.Warning.Hex,
		"warning-text": p.WarningText.Hex, "danger": p.Danger.Hex, "danger-text": p.DangerText.Hex,
		"info": p.Info.Hex, "focus": p.Accent.Hex, "selection": "#dbeafe", "selection-text": "#1e3a8a", "hover": "#163f73", "hover-text": "#ffffff",
	}}
}

// DarkTheme is a built-in reference dark-mode combination for the generic
// qualifier; product adapters must supply their effective CSS values.
func DarkTheme() Theme {
	t := DefaultTheme()
	t.Name = "default-dark"
	for k, v := range map[string]string{"text": "#f9fafb", "text-muted": "#d1d5db", "background": "#111827", "surface": "#1f2937", "border": "#9ca3af", "accent": "#93c5fd", "accent-text": "#111827", "success": "#86efac", "warning": "#fde68a", "warning-text": "#422006", "danger": "#fda4af", "danger-text": "#450a0a", "info": "#93c5fd", "focus": "#fbbf24", "selection": "#1e3a8a", "selection-text": "#eff6ff"} {
		t.Colors[k] = v
	}
	t.Colors["hover"], t.Colors["hover-text"] = "#bfdbfe", "#111827"
	return t
}

func semanticPairs(t Theme) []ColorPair {
	c := t.Colors
	return []ColorPair{
		{"text/background", c["text"], c["background"], 4.5}, {"text/surface", c["text"], c["surface"], 4.5},
		{"muted/background", c["text-muted"], c["background"], 4.5}, {"muted/surface", c["text-muted"], c["surface"], 4.5},
		{"accent-text/accent", c["accent-text"], c["accent"], 4.5}, {"success/surface", c["success"], c["surface"], 4.5},
		{"warning-text/warning", c["warning-text"], c["warning"], 4.5}, {"danger-text/danger", c["danger-text"], c["danger"], 4.5},
		{"info/surface", c["info"], c["surface"], 4.5}, {"focus/background", c["focus"], c["background"], 3},
		{"selection-text/selection", c["selection-text"], c["selection"], 4.5},
		{"border/surface", c["border"], c["surface"], 3},
		{"divider/surface", c["border"], c["surface"], 1.4},
		{"hover-text/hover", c["hover-text"], c["hover"], 4.5},
	}
}

// QualifyTheme evaluates every semantic relationship supplied by the caller
// and returns deterministic results. It performs no product/runtime lookup.
func QualifyTheme(theme Theme) ThemeReport {
	pairs := semanticPairs(theme)
	results := make([]PairResult, 0, len(pairs))
	passed := true
	for _, pair := range pairs {
		ratio, err := tokens.ContrastRatio(pair.Foreground, pair.Background)
		result := PairResult{Pair: pair, Ratio: ratio, Pass: err == nil && ratio >= pair.MinRatio}
		if err != nil {
			result.Error = err.Error()
		} else if !result.Pass {
			result.Error = fmt.Sprintf("%.2f:1 is below %.1f:1", ratio, pair.MinRatio)
		}
		if !result.Pass {
			passed = false
		}
		results = append(results, result)
	}
	return ThemeReport{Theme: theme, Pairs: results, Passed: passed}
}

// GenerateTenantTheme generates a sample theme from a brand color, adjusting
// it toward black or white when needed. It is a generic preview helper, not a
// production tenant-admission path.
func GenerateTenantTheme(name, brand string) (Theme, ThemeReport, error) {
	themes, reports, err := GenerateTenantThemeModes(name, brand)
	return themes["light"], reports["light"], err
}

// GenerateTenantThemeModes generates and qualifies both sample display modes.
func GenerateTenantThemeModes(name, brand string) (map[string]Theme, map[string]ThemeReport, error) {
	brand = strings.ToLower(strings.TrimSpace(brand))
	if _, err := tokens.ContrastRatio(brand, "#000000"); err != nil {
		return nil, nil, fmt.Errorf("brand color: %w", err)
	}
	whiteAccent, whiteDistance := bestBrand(brand, "#ffffff")
	darkAccent, darkDistance := bestBrand(brand, "#111827")
	light, accentText := whiteAccent, "#ffffff"
	if darkDistance < whiteDistance {
		light, accentText = darkAccent, "#111827"
	}
	accentText = readableTextFor(light, accentText)
	t := DefaultTheme()
	t.Name = name
	t.Colors["accent"], t.Colors["accent-text"] = light, accentText
	// Focus is an independent non-text indicator and must remain visible on
	// the page background even when a near-white brand is admitted.
	focus := light
	if ratio, _ := tokens.ContrastRatio(focus, t.Colors["background"]); ratio < 3 {
		focus, _ = bestBrandAgainst(light, t.Colors["background"])
	}
	t.Colors["focus"] = focus
	// Preview is a stable, human-readable artifact rather than an assertion
	// that every component has been rendered.
	dark := DarkTheme()
	dark.Name = name + "-dark"
	darkBrand, _ := bestBrandAgainst(brand, dark.Colors["background"])
	if readableTextFor(darkBrand, dark.Colors["accent-text"]) == dark.Colors["accent-text"] {
		// The non-text focus floor is weaker than the normal-text accent
		// contract. Re-adjust vivid customer colors when neither protected
		// neutral can carry readable accent text.
		darkBrand, _ = bestBrandWithRatio(darkBrand, dark.Colors["background"], tokens.MinRatioNormalText)
	}
	dark.Colors["accent"], dark.Colors["focus"] = darkBrand, darkBrand
	dark.Colors["accent-text"] = readableTextFor(darkBrand, dark.Colors["accent-text"])
	lightReport := QualifyTheme(t)
	lightReport.Preview = fmt.Sprintf("brand %s -> accent %s; light semantic pairs qualified", brand, light)
	darkReport := QualifyTheme(dark)
	darkReport.Preview = fmt.Sprintf("brand %s -> accent %s; dark semantic pairs qualified", brand, darkBrand)
	if !lightReport.Passed || !darkReport.Passed {
		return nil, nil, fmt.Errorf("brand color %q failed semantic qualification", brand)
	}
	return map[string]Theme{"light": t, "dark": dark}, map[string]ThemeReport{"light": lightReport, "dark": darkReport}, nil
}

// readableTextFor chooses a protected neutral for text placed on a customer
// accent. The preferred neutral is retained when it meets normal-text AA;
// otherwise the endpoint with sufficient contrast is selected.
func readableTextFor(background, preferred string) string {
	if ratio, err := tokens.ContrastRatio(preferred, background); err == nil && ratio >= tokens.MinRatioNormalText {
		return preferred
	}
	for _, candidate := range []string{"#111827", "#ffffff"} {
		if ratio, err := tokens.ContrastRatio(candidate, background); err == nil && ratio >= tokens.MinRatioNormalText {
			return candidate
		}
	}
	return preferred
}

func bestBrand(input, text string) (string, float64) {
	// Blend toward a safe endpoint, choosing the closest color that qualifies.
	best, distance := input, math.MaxFloat64
	for i := 0; i <= 100; i++ {
		for _, endpoint := range []string{"#000000", "#ffffff"} {
			candidate := blend(input, endpoint, float64(i)/100)
			ratio, err := tokens.ContrastRatio(text, candidate)
			if err == nil && ratio >= 4.5 {
				d := float64(i)
				if d < distance {
					best, distance = candidate, d
				}
			}
		}
	}
	return best, distance
}

func bestBrandAgainst(input, background string) (string, float64) {
	return bestBrandWithRatio(input, background, 3)
}

func bestBrandWithRatio(input, background string, minimum float64) (string, float64) {
	best, distance := input, math.MaxFloat64
	for i := 0; i <= 100; i++ {
		for _, endpoint := range []string{"#000000", "#ffffff"} {
			candidate := blend(input, endpoint, float64(i)/100)
			ratio, err := tokens.ContrastRatio(candidate, background)
			if err == nil && ratio >= minimum && float64(i) < distance {
				best, distance = candidate, float64(i)
			}
		}
	}
	return best, distance
}

func blend(a, b string, amount float64) string {
	parse := func(s string) [3]int {
		var x [3]int
		_, _ = fmt.Sscanf(strings.TrimPrefix(s, "#"), "%02x%02x%02x", &x[0], &x[1], &x[2])
		return x
	}
	x, y := parse(a), parse(b)
	return fmt.Sprintf("#%02x%02x%02x",
		int(math.Round(float64(x[0])*(1-amount)+float64(y[0])*amount)),
		int(math.Round(float64(x[1])*(1-amount)+float64(y[1])*amount)),
		int(math.Round(float64(x[2])*(1-amount)+float64(y[2])*amount)),
	)
}

// PairNames is useful to consumers producing stable evidence output.
func PairNames(report ThemeReport) []string {
	out := make([]string, len(report.Pairs))
	for i, p := range report.Pairs {
		out[i] = p.Pair.Name
	}
	sort.Strings(out)
	return out
}
