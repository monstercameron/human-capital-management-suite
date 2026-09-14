package productui

import (
	"fmt"
	"math"
	"strconv"
)

type ThemeMode string

const (
	ThemeModeLight ThemeMode = "light"
	ThemeModeDark  ThemeMode = "dark"
)

// ResolveThemeModes qualifies a tenant theme against both rendered color modes.
// The dark values describe the effective CSS colors; they are not another set
// of customer overrides to feed back into ResolveTheme.
func ResolveThemeModes(overrides map[string]string) (map[ThemeMode]Theme, error) {
	return ResolveThemeModesWithDark(overrides, nil)
}

// ResolveThemeModesWithDark admits the authored light and dark palettes as a
// pair. Dark values are checked against the dark canvas, not light defaults.
func ResolveThemeModesWithDark(overrides, darkOverrides map[string]string) (map[ThemeMode]Theme, error) {
	light, err := ResolveTheme(overrides)
	if err != nil {
		return nil, err
	}
	darkValues := darkThemeValues(light.values)
	for name, value := range darkOverrides {
		if err := validateCustomerColorToken(name); err != nil {
			return nil, err
		}
		definition := themeTokenByName(name)
		if err := validateThemeValue(definition, value); err != nil {
			return nil, fmt.Errorf("dark theme token %q: %w", name, err)
		}
		darkValues[name] = value
	}
	dark := Theme{values: darkValues}
	if failures := validateThemeContrast(dark.values); len(failures) != 0 {
		return nil, fmt.Errorf("dark theme qualification: %w", failures[0])
	}
	return map[ThemeMode]Theme{ThemeModeLight: light, ThemeModeDark: dark}, nil
}

func themeTokenByName(name string) ThemeToken {
	for _, token := range registeredThemeTokens {
		if token.Name == name {
			return token
		}
	}
	return ThemeToken{}
}

func darkThemeValues(light map[string]string) map[string]string {
	dark := make(map[string]string, len(light))
	for name, value := range light {
		dark[name] = value
	}
	for name, value := range map[string]string{
		"color.brand.soft": "#16202a", "color.on.brand": "#071118", "color.text.primary": "#f3f7fb", "color.text.muted": "#aebdcb",
		"color.canvas": "#0b1118", "color.surface": "#131c26", "color.border": "#354454", "color.status.success": "#69dda2", "color.status.success.surface": "#123424",
		"color.status.warning": "#f3c56f", "color.status.warning.surface": "#382a15", "color.status.danger": "#ff9d95", "color.status.danger.surface": "#3b1d20",
		"color.status.info": "#8abfff", "color.status.info.surface": "#152c48", "color.focus": "#d8e9ff",
	} {
		dark[name] = value
	}
	brand := light["color.brand.primary"]
	hover := light["color.brand.hover"]
	// Keep these blends in lockstep with darkModeDeclarationRules: qualifying
	// an adjusted color that CSS never paints would admit inaccessible brands.
	dark["color.brand.primary"] = blendThemeColor(brand, "#ffffff", .60)
	dark["color.brand.hover"] = blendThemeColor(hover, "#ffffff", .68)
	dark["color.brand.soft"] = blendThemeColor(brand, "#16202a", .82)
	return dark
}

func blendThemeColor(a, b string, amount float64) string {
	channel := func(value string, offset int) float64 {
		parsed, _ := strconv.ParseUint(value[offset:offset+2], 16, 8)
		return float64(parsed)
	}
	return fmt.Sprintf("#%02x%02x%02x", int(math.Round(channel(a, 1)*(1-amount)+channel(b, 1)*amount)), int(math.Round(channel(a, 3)*(1-amount)+channel(b, 3)*amount)), int(math.Round(channel(a, 5)*(1-amount)+channel(b, 5)*amount)))
}
