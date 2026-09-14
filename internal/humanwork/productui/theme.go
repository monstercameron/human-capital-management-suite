package productui

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ThemeTokenKind is one closed BrandPack token family. The names mirror the
// experience contract so a later registry-backed BrandPack can resolve into
// this renderer without translating page-specific CSS values.
type ThemeTokenKind string

const (
	ThemeColor      ThemeTokenKind = "color"
	ThemeTypography ThemeTokenKind = "typography"
	ThemeSpacing    ThemeTokenKind = "spacing"
	ThemeRadius     ThemeTokenKind = "radius"
	ThemeElevation  ThemeTokenKind = "elevation"
	ThemeMotion     ThemeTokenKind = "motion"
)

// ThemeToken is one stable semantic contract. CustomerOverridable means a
// governed BrandPack may supply a value; it never bypasses validation.
type ThemeToken struct {
	Name                string
	CSSVariable         string
	Kind                ThemeTokenKind
	Default             string
	CustomerOverridable bool
	Purpose             string
}

var registeredThemeTokens = []ThemeToken{
	{Name: "color.brand.primary", CSSVariable: "--hcm-color-brand-primary", Kind: ThemeColor, Default: "#006b57", CustomerOverridable: true, Purpose: "primary actions and active navigation"},
	{Name: "color.brand.hover", CSSVariable: "--hcm-color-brand-hover", Kind: ThemeColor, Default: "#005344", CustomerOverridable: true, Purpose: "primary action hover state"},
	{Name: "color.brand.soft", CSSVariable: "--hcm-color-brand-soft", Kind: ThemeColor, Default: "#eaf3ef", CustomerOverridable: true, Purpose: "selected and supporting brand surfaces"},
	{Name: "color.on.brand", CSSVariable: "--hcm-color-on-brand", Kind: ThemeColor, Default: "#ffffff", Purpose: "text and glyphs on brand-colored surfaces"},
	{Name: "color.text.primary", CSSVariable: "--hcm-color-text", Kind: ThemeColor, Default: "#102238", CustomerOverridable: true, Purpose: "primary text"},
	{Name: "color.text.muted", CSSVariable: "--hcm-color-text-muted", Kind: ThemeColor, Default: "#526171", CustomerOverridable: true, Purpose: "supporting text"},
	{Name: "color.canvas", CSSVariable: "--hcm-color-canvas", Kind: ThemeColor, Default: "#fafaf7", CustomerOverridable: true, Purpose: "application canvas"},
	{Name: "color.surface", CSSVariable: "--hcm-color-surface", Kind: ThemeColor, Default: "#ffffff", CustomerOverridable: true, Purpose: "raised content surface"},
	{Name: "color.border", CSSVariable: "--hcm-color-border", Kind: ThemeColor, Default: "#d5ddd8", CustomerOverridable: true, Purpose: "non-text boundaries"},
	{Name: "color.control.border", CSSVariable: "--hcm-color-control-border", Kind: ThemeColor, Default: "#7b8997", Purpose: "minimum-contrast interactive control boundary"},
	{Name: "color.status.success", CSSVariable: "--hcm-color-success", Kind: ThemeColor, Default: "#0f6136", Purpose: "successful and completed state"},
	{Name: "color.status.success.surface", CSSVariable: "--hcm-color-success-surface", Kind: ThemeColor, Default: "#dff3e6", Purpose: "successful and completed state surface"},
	{Name: "color.status.warning", CSSVariable: "--hcm-color-warning", Kind: ThemeColor, Default: "#925400", Purpose: "warning text and icon"},
	{Name: "color.status.warning.surface", CSSVariable: "--hcm-color-warning-surface", Kind: ThemeColor, Default: "#fff6df", Purpose: "warning surface"},
	{Name: "color.status.danger", CSSVariable: "--hcm-color-danger", Kind: ThemeColor, Default: "#b42318", Purpose: "destructive and failed status"},
	{Name: "color.status.danger.surface", CSSVariable: "--hcm-color-danger-surface", Kind: ThemeColor, Default: "#fdecea", Purpose: "destructive and failed status surface"},
	{Name: "color.status.info", CSSVariable: "--hcm-color-info", Kind: ThemeColor, Default: "#1555a3", Purpose: "informational state"},
	{Name: "color.status.info.surface", CSSVariable: "--hcm-color-info-surface", Kind: ThemeColor, Default: "#eaf1fb", Purpose: "informational state surface"},
	{Name: "color.focus", CSSVariable: "--hcm-color-focus", Kind: ThemeColor, Default: "#102238", Purpose: "platform-owned focus indicator"},
	{Name: "typography.font.sans", CSSVariable: "--hcm-font-sans", Kind: ThemeTypography, Default: `"Segoe UI Variable","Segoe UI",system-ui,sans-serif`, CustomerOverridable: true, Purpose: "product interface typeface"},
	{Name: "typography.font.mono", CSSVariable: "--hcm-font-mono", Kind: ThemeTypography, Default: `ui-monospace,"Cascadia Mono",Consolas,monospace`, Purpose: "identifiers and evidence"},
	{Name: "typography.size.body", CSSVariable: "--hcm-font-size-body", Kind: ThemeTypography, Default: "1rem", Purpose: "default readable text"},
	{Name: "typography.size.small", CSSVariable: "--hcm-font-size-small", Kind: ThemeTypography, Default: ".8125rem", Purpose: "supporting interface text"},
	{Name: "typography.size.heading", CSSVariable: "--hcm-font-size-heading", Kind: ThemeTypography, Default: "clamp(1.75rem,2.5vw,2.15rem)", Purpose: "page heading"},
	{Name: "typography.line-height", CSSVariable: "--hcm-line-height", Kind: ThemeTypography, Default: "1.5", Purpose: "body line height"},
	{Name: "spacing.1", CSSVariable: "--hcm-space-1", Kind: ThemeSpacing, Default: ".5rem", Purpose: "tight internal gap"},
	{Name: "spacing.2", CSSVariable: "--hcm-space-2", Kind: ThemeSpacing, Default: "1rem", Purpose: "control and row gap"},
	{Name: "spacing.3", CSSVariable: "--hcm-space-3", Kind: ThemeSpacing, Default: "1.5rem", Purpose: "section gap"},
	{Name: "spacing.4", CSSVariable: "--hcm-space-4", Kind: ThemeSpacing, Default: "2rem", Purpose: "page rhythm"},
	{Name: "spacing.density", CSSVariable: "--hcm-density", Kind: ThemeSpacing, Default: "1", CustomerOverridable: true, Purpose: "bounded comfortable layout density"},
	{Name: "radius.control", CSSVariable: "--hcm-radius-control", Kind: ThemeRadius, Default: "8px", CustomerOverridable: true, Purpose: "controls and compact affordances"},
	{Name: "radius.surface", CSSVariable: "--hcm-radius-surface", Kind: ThemeRadius, Default: "12px", CustomerOverridable: true, Purpose: "cards and panels"},
	{Name: "elevation.resting", CSSVariable: "--hcm-shadow-resting", Kind: ThemeElevation, Default: "0 1px 2px rgba(16,34,56,.06)", Purpose: "resting raised surface"},
	{Name: "elevation.raised", CSSVariable: "--hcm-shadow-raised", Kind: ThemeElevation, Default: "0 10px 30px rgba(16,34,56,.12)", Purpose: "popover and active surface"},
	{Name: "motion.duration.fast", CSSVariable: "--hcm-motion-fast", Kind: ThemeMotion, Default: "120ms", CustomerOverridable: true, Purpose: "direct manipulation feedback"},
	{Name: "motion.duration.normal", CSSVariable: "--hcm-motion-normal", Kind: ThemeMotion, Default: "180ms", CustomerOverridable: true, Purpose: "component state transition"},
	{Name: "motion.duration.slow", CSSVariable: "--hcm-motion-slow", Kind: ThemeMotion, Default: "280ms", CustomerOverridable: true, Purpose: "page and panel entrance"},
	{Name: "motion.easing.standard", CSSVariable: "--hcm-motion-easing", Kind: ThemeMotion, Default: "cubic-bezier(.2,.8,.2,1)", Purpose: "platform-owned non-linear easing"},
	{Name: "motion.distance", CSSVariable: "--hcm-motion-distance", Kind: ThemeMotion, Default: "8px", Purpose: "bounded decorative travel"},
}

// ThemeTokens returns a copy of the ordered token registry.
func ThemeTokens() []ThemeToken {
	return append([]ThemeToken(nil), registeredThemeTokens...)
}

// Theme is a validated, fully resolved token set. Its map is private so no
// unvalidated string can be inserted after resolution.
type Theme struct{ values map[string]string }

// ResolveTheme is the production customer-theme admission boundary. It applies
// one BrandPack layer over platform defaults and qualifies both the light
// values and the effective dark-mode values before returning a renderable
// theme. Storage, publication, inheritance, and effective dating remain
// outside this package; callers must not render an unadmitted map directly.
func ResolveTheme(overrides map[string]string) (Theme, error) {
	values := make(map[string]string, len(registeredThemeTokens))
	definitions := make(map[string]ThemeToken, len(registeredThemeTokens))
	for _, token := range registeredThemeTokens {
		values[token.Name] = token.Default
		definitions[token.Name] = token
	}
	keys := make([]string, 0, len(overrides))
	for name := range overrides {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	var failures []error
	for _, name := range keys {
		token, ok := definitions[name]
		if !ok {
			failures = append(failures, fmt.Errorf("unknown theme token %q", name))
			continue
		}
		if !token.CustomerOverridable {
			failures = append(failures, fmt.Errorf("theme token %q is platform-owned", name))
			continue
		}
		value := strings.TrimSpace(overrides[name])
		if err := validateThemeValue(token, value); err != nil {
			failures = append(failures, fmt.Errorf("theme token %q: %w", name, err))
			continue
		}
		values[name] = value
	}
	if len(failures) == 0 {
		for _, failure := range validateThemeContrast(values) {
			failures = append(failures, fmt.Errorf("light theme qualification: %w", failure))
		}
	}
	if len(failures) == 0 {
		for _, failure := range validateThemeContrast(darkThemeValues(values)) {
			failures = append(failures, fmt.Errorf("dark theme qualification: %w", failure))
		}
	}
	if err := errors.Join(failures...); err != nil {
		// Keep the rejection actionable for a customer-theme editor while
		// retaining the per-token/pair reason for preview and diagnostics.
		return Theme{}, fmt.Errorf("customer theme admission rejected: %w", err)
	}
	return Theme{values: values}, nil
}

// Value returns one resolved semantic value.
func (t Theme) Value(name string) (string, bool) {
	value, ok := t.values[name]
	return value, ok
}

// CSS renders variables in registry order. The deterministic output can be
// CSP-hashed, signed, previewed, and compared across BrandPack versions.
//
// The registry order is load-bearing: TestTodo_WEB_013_Golden pins the
// sha256 of this exact byte stream, and the typed GWC path sorts
// declarations within a block, so this emitter must stay string-built.
func (t Theme) CSS() string {
	if len(t.values) == 0 {
		t, _ = ResolveTheme(nil)
	}
	var b strings.Builder
	b.WriteString(":root{")
	for _, token := range registeredThemeTokens {
		b.WriteString(token.CSSVariable)
		b.WriteByte(':')
		b.WriteString(t.values[token.Name])
		b.WriteByte(';')
	}
	b.WriteByte('}')
	return b.String()
}

var (
	hexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	fontPattern     = regexp.MustCompile(`^[A-Za-z0-9 ,.'"-]{1,160}$`)
	lengthPattern   = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(px|rem)$`)
	durationPattern = regexp.MustCompile(`^([0-9]+)ms$`)
)

func validateThemeValue(token ThemeToken, value string) error {
	if value == "" || strings.ContainsAny(value, ";{}\n\r") {
		return errors.New("value is empty or contains CSS control characters")
	}
	switch token.Kind {
	case ThemeColor:
		if !hexColorPattern.MatchString(value) {
			return errors.New("color must be an opaque six-digit hex value")
		}
	case ThemeTypography:
		if token.Name == "typography.font.sans" && !fontPattern.MatchString(value) {
			return errors.New("font stack contains unsupported characters")
		}
	case ThemeSpacing:
		if token.Name == "spacing.density" && value != ".875" && value != "1" && value != "1.125" {
			return errors.New("density must be .875, 1, or 1.125")
		}
	case ThemeRadius:
		match := lengthPattern.FindStringSubmatch(value)
		if match == nil {
			return errors.New("radius must use px or rem")
		}
		amount, _ := strconv.ParseFloat(match[1], 64)
		if match[2] == "rem" {
			amount *= 16
		}
		if amount > 24 {
			return errors.New("radius exceeds the 24px product bound")
		}
	case ThemeMotion:
		match := durationPattern.FindStringSubmatch(value)
		if match == nil {
			return errors.New("motion duration must use milliseconds")
		}
		amount, _ := strconv.Atoi(match[1])
		if amount < 80 || amount > 600 {
			return errors.New("motion duration must be between 80ms and 600ms")
		}
	}
	return nil
}

func validateThemeContrast(values map[string]string) []error {
	pairs := []struct {
		foreground, background, purpose string
	}{
		{"color.on.brand", "color.brand.primary", "text on primary brand surface"},
		{"color.on.brand", "color.brand.hover", "text on hovered brand surface"},
		{"color.brand.primary", "color.surface", "primary action"},
		{"color.brand.hover", "color.surface", "primary action hover"},
		{"color.text.primary", "color.canvas", "body text on canvas"},
		{"color.text.primary", "color.surface", "body text on surface"},
		{"color.text.primary", "color.brand.soft", "text on brand-soft surface"},
		{"color.text.muted", "color.canvas", "muted text on canvas"},
		{"color.text.muted", "color.surface", "muted text on surface"},
		{"color.status.success", "color.status.success.surface", "success status"},
		{"color.status.warning", "color.status.warning.surface", "warning status"},
		{"color.status.danger", "color.status.danger.surface", "danger status"},
		{"color.status.info", "color.status.info.surface", "information status"},
	}
	var failures []error
	for _, pair := range pairs {
		ratio, err := contrastRatio(values[pair.foreground], values[pair.background])
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if ratio < 4.5 {
			failures = append(failures, fmt.Errorf("%s contrast %.2f:1 is below 4.5:1", pair.purpose, ratio))
		}
	}
	return failures
}

func contrastRatio(first, second string) (float64, error) {
	left, err := relativeLuminance(first)
	if err != nil {
		return 0, err
	}
	right, err := relativeLuminance(second)
	if err != nil {
		return 0, err
	}
	if left < right {
		left, right = right, left
	}
	return (left + .05) / (right + .05), nil
}

func relativeLuminance(value string) (float64, error) {
	if !hexColorPattern.MatchString(value) {
		return 0, fmt.Errorf("invalid color %q", value)
	}
	channels := make([]float64, 3)
	for index := range channels {
		component, err := strconv.ParseUint(value[1+index*2:3+index*2], 16, 8)
		if err != nil {
			return 0, err
		}
		channel := float64(component) / 255
		if channel <= .04045 {
			channels[index] = channel / 12.92
		} else {
			channels[index] = math.Pow((channel+.055)/1.055, 2.4)
		}
	}
	return .2126*channels[0] + .7152*channels[1] + .0722*channels[2], nil
}
