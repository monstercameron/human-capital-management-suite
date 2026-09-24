package docsdiagram

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

type rgb [3]float64

// paletteSchemes mirror the product's default light and dark token values
// (productui theme.go and darkModeDeclarationRules). A token rename or a
// palette slot this resolver cannot read fails the test rather than passing.
var paletteSchemes = map[string]map[string]string{
	"light": {
		"--accent": "#006b57", "--hcm-color-info": "#1555a3", "--hcm-color-warning": "#925400",
		"--hcm-color-danger": "#b42318", "--hcm-color-success": "#0f6136", "--muted": "#526171", "--surface": "#ffffff",
	},
	"dark": {
		"--accent": "color-mix(in srgb,#006b57 40%,#fff)", "--hcm-color-info": "#8abfff", "--hcm-color-warning": "#f3c56f",
		"--hcm-color-danger": "#ff9d95", "--hcm-color-success": "#69dda2", "--muted": "#aebdcb", "--surface": "#131c26",
	},
}

func parseHex(t *testing.T, s string) rgb {
	t.Helper()
	s = strings.TrimPrefix(s, "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	var c rgb
	for i := range 3 {
		v, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			t.Fatalf("bad hex %q", s)
		}
		c[i] = float64(v)
	}
	return c
}

// resolveColor evaluates the small CSS colour grammar the palette uses:
// hex, var(), color-mix(in srgb,A P%,B) and light-dark(L,D).
func resolveColor(t *testing.T, expr, scheme string) rgb {
	t.Helper()
	expr = strings.TrimSpace(expr)
	switch {
	case strings.HasPrefix(expr, "#"):
		return parseHex(t, expr)
	case strings.HasPrefix(expr, "var(") && strings.HasSuffix(expr, ")"):
		v, ok := paletteSchemes[scheme][expr[4:len(expr)-1]]
		if !ok {
			t.Fatalf("unknown token in %q", expr)
		}
		return resolveColor(t, v, scheme)
	case strings.HasPrefix(expr, "light-dark(") && strings.HasSuffix(expr, ")"):
		parts := splitArgs(expr[len("light-dark(") : len(expr)-1])
		if scheme == "light" {
			return resolveColor(t, parts[0], scheme)
		}
		return resolveColor(t, parts[1], scheme)
	case strings.HasPrefix(expr, "color-mix(in srgb,") && strings.HasSuffix(expr, ")"):
		parts := splitArgs(expr[len("color-mix(in srgb,") : len(expr)-1])
		a, pctText, _ := strings.Cut(strings.TrimSpace(parts[0]), " ")
		pct, err := strconv.ParseFloat(strings.TrimSuffix(pctText, "%"), 64)
		if err != nil || len(parts) != 2 {
			t.Fatalf("unreadable color-mix %q", expr)
		}
		ca, cb := resolveColor(t, a, scheme), resolveColor(t, parts[1], scheme)
		var out rgb
		for i := range 3 {
			out[i] = ca[i]*pct/100 + cb[i]*(1-pct/100)
		}
		return out
	}
	t.Fatalf("unreadable colour %q", expr)
	return rgb{}
}

// splitArgs splits CSS function arguments on commas outside parentheses.
func splitArgs(s string) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

func toLab(c rgb) [3]float64 {
	lin := func(v float64) float64 {
		v /= 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	r, g, b := lin(c[0]), lin(c[1]), lin(c[2])
	x := (0.4124*r + 0.3576*g + 0.1805*b) / 0.95047
	y := 0.2126*r + 0.7152*g + 0.0722*b
	z := (0.0193*r + 0.1192*g + 0.9505*b) / 1.08883
	f := func(t float64) float64 {
		if t > 0.008856 {
			return math.Cbrt(t)
		}
		return 7.787*t + 16.0/116
	}
	return [3]float64{116*f(y) - 16, 500 * (f(x) - f(y)), 200 * (f(y) - f(z))}
}

// TestSeriesPaletteIsDistinct holds every pair of drawn series fills (the
// 78% tint Stylesheet paints) at least ΔE76 15 apart in light and dark. The
// old slot 5, success green, sat 11.4 from the brand teal.
func TestSeriesPaletteIsDistinct(t *testing.T) {
	const minDeltaE = 15.0
	for scheme := range paletteSchemes {
		surface := resolveColor(t, "var(--surface)", scheme)
		labs := make([][3]float64, len(seriesTokens))
		for i, tok := range seriesTokens {
			c := resolveColor(t, tok, scheme)
			for k := range 3 {
				c[k] = c[k]*0.78 + surface[k]*0.22
			}
			labs[i] = toLab(c)
		}
		for i := range labs {
			for j := i + 1; j < len(labs); j++ {
				d := math.Sqrt(math.Pow(labs[i][0]-labs[j][0], 2) + math.Pow(labs[i][1]-labs[j][1], 2) + math.Pow(labs[i][2]-labs[j][2], 2))
				if d < minDeltaE {
					t.Errorf("%s: series %d and %d are ΔE %.1f apart, want >= %.0f", scheme, i, j, d, minDeltaE)
				}
			}
		}
	}
	if strings.Contains(strings.Join(seriesTokens[:], " "), "--hcm-color-success") {
		t.Error("success green is too close to the brand teal to be a series colour")
	}
}
