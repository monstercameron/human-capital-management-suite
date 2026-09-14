package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type appearanceColorField struct {
	Token string
	Label [3]string // en-US, de-DE, ar
}

var appearanceColorFields = []appearanceColorField{
	{Token: "color.brand.primary", Label: [3]string{"Brand", "Marke", "العلامة التجارية"}},
	{Token: "color.brand.hover", Label: [3]string{"Brand hover", "Marke bei Berührung", "العلامة عند المرور"}},
	{Token: "color.brand.soft", Label: [3]string{"Brand tint", "Markentönung", "خلفية العلامة"}},
	{Token: "color.text.primary", Label: [3]string{"Main text", "Haupttext", "النص الأساسي"}},
	{Token: "color.text.muted", Label: [3]string{"Supporting text", "Begleittext", "النص الثانوي"}},
	{Token: "color.canvas", Label: [3]string{"Page canvas", "Seitenhintergrund", "خلفية الصفحة"}},
	{Token: "color.surface", Label: [3]string{"Cards and panels", "Karten und Bereiche", "البطاقات واللوحات"}},
	{Token: "color.border", Label: [3]string{"Borders", "Rahmen", "الحدود"}},
}

func appearanceCustomColors(i18n I18nProps, theme CustomerTheme, mode ThemeMode, onChange func(string, string)) ui.Node {
	index := 0
	title, help := "Light color system", "Choose Custom colors above, then set your brand, canvas, text, and surface colors. Save to apply across the workspace. Each mode is checked for readable contrast."
	if mode == ThemeModeDark {
		title, help = "Dark color system", "Set colors for the dark workspace independently. Untouched colors follow an accessible dark variant of your light brand. Save to apply across the workspace."
	}
	switch i18n.Locale.normalized().Resolved {
	case "de-DE":
		index = 1
		title, help = "Helles Farbsystem", "Legen Sie Marken-, Hintergrund-, Text- und Flächenfarben fest. Speichern Sie, um die Farben im Arbeitsbereich anzuwenden. Der Kontrast wird für beide Modi geprüft."
		if mode == ThemeModeDark {
			title, help = "Dunkles Farbsystem", "Legen Sie die Farben für den dunklen Arbeitsbereich unabhängig fest. Unveränderte Farben folgen einer lesbaren dunklen Variante Ihrer Marke."
		}
	case "ar":
		index = 2
		title, help = "ألوان الوضع الفاتح", "حدد ألوان العلامة والخلفية والنص والبطاقات. احفظ لتطبيقها في مساحة العمل. يجري التحقق من التباين في كلا الوضعين."
		if mode == ThemeModeDark {
			title, help = "ألوان الوضع الداكن", "حدد ألوان مساحة العمل الداكنة بشكل مستقل. تتبع الألوان التي لم تُعدّل نسخة داكنة قابلة للقراءة من علامتك."
		}
	}
	modes, err := ResolveCustomerThemeModes(theme)
	if err != nil {
		modes, _ = ResolveThemeModes(nil)
	}
	defaults := modes[mode]
	overrides := theme.TokenOverrides
	if mode == ThemeModeDark {
		overrides = theme.DarkTokenOverrides
	}
	controls := make([]ui.Node, 0, len(appearanceColorFields))
	for _, field := range appearanceColorFields {
		field := field
		value, _ := defaults.Value(field.Token)
		if override := overrides[field.Token]; hexColorPattern.MatchString(override) {
			value = override
		}
		input := html.Props{ID: "appearance-" + string(mode) + "-" + strings.ReplaceAll(field.Token, ".", "-"), Name: string(mode) + "-" + field.Token, Type: "color", Value: value}
		if onChange != nil {
			input.OnInput = ui.UseEvent(func(event ui.InputEvent) { onChange(field.Token, event.GetValue()) })
		}
		controls = append(controls, html.Label(html.Props{Class: "appearance-color-field", For: input.ID},
			html.Span(html.Props{}, ui.Text(field.Label[index])), html.Input(input),
		))
	}
	return html.Fieldset(html.Props{Class: "surface appearance-group appearance-custom-colors", Data: map[string]string{"hcm-custom-colors": "admitted-tokens", "hcm-custom-color-mode": string(mode)}},
		html.Legend(html.Props{}, ui.Text(title)),
		html.P(html.Props{Class: "muted appearance-group-help"}, ui.Text(help)),
		html.Div(html.Props{Class: "appearance-color-grid"}, controls...),
	)
}
