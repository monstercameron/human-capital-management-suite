package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// LocaleOptionProps is one supported product-language destination. The
// component receives a complete href so locale switching preserves all other
// software-navigation state without owning route rules.
type LocaleOptionProps struct {
	Code          string
	ShortCode     string
	Label         string
	Description   string
	Href          string
	Language      string
	TextDirection string
	Current       bool
	CurrentLabel  string
	Navigate      func(string)
}

type LocalePreferencesProps struct {
	I18nProps
	Title       string
	Description string
	Status      string
	Options     []LocaleOptionProps
}

func LocalePreferencesPanel(props LocalePreferencesProps) ui.Node {
	options := make([]ui.Node, 0, len(props.Options))
	for _, option := range props.Options {
		class := "locale-choice"
		linkProps := html.Props{Class: class}
		if option.Current {
			linkProps.Class += " current"
			linkProps.Aria = map[string]string{"current": "page"}
		}
		content := []ui.Node{
			html.Span(html.Props{Class: "locale-choice-code", Aria: map[string]string{"hidden": "true"}}, ui.Text(option.ShortCode)),
			html.Span(html.Props{Class: "locale-choice-copy"},
				html.Strong(html.Props{Lang: option.Language, Dir: option.TextDirection}, ui.Text(option.Label)),
				html.Small(html.Props{}, ui.Text(option.Description)),
			),
		}
		if option.Current {
			content = append(content, html.Span(html.Props{Class: "locale-current"}, ui.Text(option.CurrentLabel)))
		}
		options = append(options, html.Li(html.Props{}, softwareLink(option.Navigate, linkProps, option.Href, content...)))
	}
	return html.Section(html.Props{Class: "surface locale-preferences", Data: map[string]string{"hcm-setting-group": "language"}, Raw: map[string]any{"aria-labelledby": "locale-preferences-title"}},
		ui.CreateElement(SectionHeading, SectionHeadingProps{
			ID: "locale-preferences-title", Title: props.Title, Description: props.Description, Level: 3,
		}),
		html.Ul(html.Props{Class: "locale-choice-list", Raw: map[string]any{"role": "list", "aria-label": props.Title}}, options...),
		html.P(html.Props{Class: "locale-preferences-status", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(props.Status)),
	)
}

func localePreferencesProps(view View) LocalePreferencesProps {
	locale := view.Locale.normalized()
	options := make([]LocaleOptionProps, 0, len(SupportedProductLocales()))
	for _, code := range SupportedProductLocales() {
		candidateLocale := ResolveProductLocale(code)
		candidateView := view
		candidateView.Locale = candidateLocale
		directionKey := "settings.locale_ltr"
		if string(candidateLocale.Direction) == "rtl" {
			directionKey = "settings.locale_rtl"
		}
		options = append(options, LocaleOptionProps{
			Code: code, ShortCode: strings.ToUpper(strings.Split(code, "-")[0]),
			Label: candidateLocale.Text(productLocaleLabelKey(code)),
			Description: view.Locale.Text("settings.locale_option_detail", map[string]string{
				"code": code, "direction": view.Locale.Text(directionKey),
			}),
			Href: currentPageHref(candidateView, view.NavCollapsed), Language: code,
			TextDirection: string(candidateLocale.Direction), Current: code == locale.Resolved,
			CurrentLabel: view.Locale.Text("settings.locale_current"), Navigate: view.Navigate,
		})
	}
	return LocalePreferencesProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Title:     view.Locale.Text("settings.locale_title"), Description: view.Locale.Text("settings.locale_description"),
		Status: view.Locale.Text("settings.locale_status"), Options: options,
	}
}

func productLocaleLabelKey(code string) string {
	return map[string]string{"en-US": "shell.locale_en", "de-DE": "shell.locale_de", "ar": "shell.locale_ar"}[code]
}
