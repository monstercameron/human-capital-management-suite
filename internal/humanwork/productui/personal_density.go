package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// REV-092-01: a person may choose their own layout density without changing
// what anyone else in the organization sees. The organization appearance's
// density stays the default; a personal choice overrides it for that person
// only, and is limited to the same qualified presets, so it can never select
// a layout below the 44px control-height floor UIPOLISH-006 set for every
// density.

// NormalizePersonalDensity returns value when it names an admitted density
// preset and "" (inherit the organization density) otherwise.
func NormalizePersonalDensity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if hasAppearancePreset(densityPresets, value) {
		return value
	}
	return ""
}

// EffectiveAppearance is the one resolution every renderer uses for the
// appearance a person sees: the organization theme, with the person's own
// density when they chose one.
func EffectiveAppearance(theme CustomerTheme, personalDensity string) CustomerTheme {
	theme = NormalizeCustomerTheme(theme)
	if personal := NormalizePersonalDensity(personalDensity); personal != "" {
		theme.Density = personal
	}
	return theme
}

// EffectiveAppearance resolves the view's appearance for its viewer.
func (view View) EffectiveAppearance() CustomerTheme {
	return EffectiveAppearance(view.Appearance, view.StoredPreferences.Density)
}

// PersonalDensityProps drives the Settings control for the person's own
// density. Value is their stored choice ("" inherits OrganizationDensity).
type PersonalDensityProps struct {
	Locale              LocaleContext
	Value               string
	OrganizationDensity string
	OnSave              func(string)
}

// personalDensityInheritValue is the radio value for "no personal choice".
// It is never stored: NormalizePersonalDensity maps it back to "" (inherit).
const personalDensityInheritValue = "organization"

// personalDensityFieldset renders the density choice as one group of the
// accessibility form, with the organization default as its first option so
// "no personal choice" is an explicit, reversible selection rather than a
// missing value. draft receives the selection; the form's Save stores it.
func personalDensityFieldset(props *PersonalDensityProps, draft *string) ui.Node {
	if props == nil {
		return nil
	}
	text := personalDensityCopy(props.Locale.normalized().Resolved)
	organization := NormalizeCustomerTheme(CustomerTheme{Density: props.OrganizationDensity}).Density
	options := []struct{ id, label string }{
		{personalDensityInheritValue, strings.ReplaceAll(text.inherit, "{density}", text.label(organization))},
		{"compact", text.compact}, {"comfortable", text.comfortable}, {"spacious", text.spacious},
	}
	selected := NormalizePersonalDensity(*draft)
	if selected == "" {
		selected = personalDensityInheritValue
	}
	items := make([]ui.Node, 0, len(options))
	for _, option := range options {
		option := option
		id := "personal-density-" + option.id
		input := html.Props{ID: id, Type: "radio", Name: "personal_density", Value: option.id, Checked: option.id == selected}
		input.OnChange = ui.UseEvent(func(ui.InputEvent) { *draft = NormalizePersonalDensity(option.id) })
		items = append(items, html.Label(html.Props{Class: "accessibility-choice", For: id},
			html.Input(input),
			html.Span(html.Props{}, html.Strong(html.Props{}, ui.Text(option.label))),
		))
	}
	return html.Fieldset(html.Props{Class: "accessibility-group accessibility-group-density", Data: map[string]string{"hcm-setting-group": "personal-density"}, Raw: map[string]any{"aria-describedby": "personal-density-help"}},
		html.Legend(html.Props{}, ui.Text(text.title)),
		html.P(html.Props{ID: "personal-density-help", Class: "muted"}, ui.Text(text.help)),
		html.Div(html.Props{Class: "accessibility-options"}, items...),
	)
}

type personalDensityText struct {
	title, help, inherit, compact, comfortable, spacious string
}

func (c personalDensityText) label(density string) string {
	switch density {
	case "compact":
		return c.compact
	case "spacious":
		return c.spacious
	default:
		return c.comfortable
	}
}

func personalDensityCopy(locale string) personalDensityText {
	switch locale {
	case "de-DE":
		return personalDensityText{
			title:   "Layoutdichte",
			help:    "Diese Einstellung ändert nur Ihre eigene Ansicht, nicht die anderer Personen.",
			inherit: "Standard der Organisation ({density})", compact: "Kompakt", comfortable: "Komfortabel", spacious: "Großzügig",
		}
	case "ar":
		return personalDensityText{
			title:   "كثافة التخطيط",
			help:    "يغيّر هذا الإعداد عرضك أنت فقط، ولا يغيّر ما يراه الآخرون.",
			inherit: "الإعداد الافتراضي للمؤسسة ({density})", compact: "مضغوط", comfortable: "مريح", spacious: "واسع",
		}
	default:
		return personalDensityText{
			title:   "Layout density",
			help:    "This setting changes only your own view, not what other people see.",
			inherit: "Organization default ({density})", compact: "Compact", comfortable: "Comfortable", spacious: "Spacious",
		}
	}
}
