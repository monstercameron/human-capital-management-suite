package chatui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// RenderingMark is the common mark and switch region for every derived kind.
// Policy controls whether switches exist; a switch never grants access.
func RenderingMark(locale string, mark chatrender.Mark, original, written ui.Handler) ui.Node {
	t := func(key string) string { return RenderingText(locale, key) }
	children := []ui.Node{}
	if mark.State == "pending" || mark.State == "fallback" || mark.State == "unavailable" {
		children = append(children, html.Span(html.Props{Text: t(mark.State)}))
	}
	for _, kind := range mark.Kinds {
		key := string(kind)
		if kind == chatrender.Translate {
			children = append(children, html.Span(html.Props{Text: strings.ReplaceAll(t("translated"), "{language}", t(chatrender.Language(mark.SourceLanguage)))}))
		} else {
			children = append(children, html.Span(html.Props{Text: t(key)}))
		}
	}
	if mark.CanShowOriginal && mark.State == "ready" {
		children = append(children, html.Button(html.Props{Type: "button", Class: "button secondary", Text: t("original"), OnClick: original, Disabled: original.Value() == nil}))
	}
	if mark.CanShowAsWritten && mark.State == "ready" {
		for _, kind := range mark.Kinds {
			if kind == chatrender.Reword {
				children = append(children, html.Button(html.Props{Type: "button", Class: "button secondary", Text: t("written"), OnClick: written, Disabled: written.Value() == nil}))
				break
			}
		}
	}
	return html.Div(html.Props{Class: "chatrender-mark", Dir: direction(locale), Role: "status", Aria: map[string]string{"live": "polite"}}, children...)
}

type RenderingSettingsModel struct {
	Locale                 string
	Preference             chatrender.Preference
	Conversation           bool
	Loading, Failed, Saved bool
	TranslationUnavailable bool
	Submit                 ui.Handler
	// AutoSave (CHATBUG-045) saves every change at once and draws no Save button.
	AutoSave bool
}

func RenderingSettings(m RenderingSettingsModel) ui.Node {
	if m.AutoSave {
		return chatbug045ReadingForm(m)
	}
	t := func(k string) string { return RenderingText(m.Locale, k) }
	options := []ui.Node{}
	for _, l := range []string{"en", "de", "fr", "es", "pt", "ar", "ja", "hi"} {
		options = append(options, html.Option(html.Props{Value: l, Text: t(l), Selected: l == chatrender.Language(m.Preference.ReadingLanguage)}))
	}
	status := "help"
	if m.Loading {
		status = "loading"
	} else if m.Failed {
		status = "error"
	} else if m.Saved {
		status = "saved"
	}
	fields := []ui.Node{
		html.H3(html.Props{ID: "chatrender-settings-title", Text: t("title")}),
		html.P(html.Props{ID: "chatrender-settings-status", Role: "status", Text: t(status)}),
		html.Label(html.Props{For: "chatrender-reading", Text: t("reading")}),
		html.Select(html.Props{ID: "chatrender-reading", Name: "reading", Disabled: m.Loading, Aria: map[string]string{"describedby": "chatrender-settings-status"}}, options...),
		html.Label(html.Props{Class: "chatrender-check", For: "chatrender-translate"}, html.Input(html.Props{ID: "chatrender-translate", Name: "translate", Type: "checkbox", Checked: m.Preference.Translate, Disabled: m.Loading || m.TranslationUnavailable, Title: ChatFeatureUnavailable(m.Locale)}), html.Span(html.Props{Text: t("translate")})),
	}
	for _, field := range []struct{ key, name string }{{"further", "further"}, {"source", "never"}} {
		checkboxes := []ui.Node{html.Legend(html.Props{Text: t(field.key)})}
		for _, l := range []string{"en", "de", "fr", "es", "pt", "ar", "ja", "hi"} {
			checked := false
			if field.name == "further" {
				for _, known := range m.Preference.FurtherLanguages {
					if chatrender.Language(known) == l {
						checked = true
					}
				}
			} else {
				on, exists := m.Preference.SourceOverrides[l]
				checked = exists && !on
			}
			id := "chatrender-" + field.name + "-" + l
			checkboxes = append(checkboxes, html.Label(html.Props{Class: "chatrender-check", For: id}, html.Input(html.Props{ID: id, Name: field.name, Type: "checkbox", Value: l, Checked: checked, Disabled: m.Loading}), html.Span(html.Props{Text: t(l)})))
		}
		fields = append(fields, html.Fieldset(html.Props{Class: "chatrender-languages"}, checkboxes...))
	}
	if m.Conversation {
		fields = append(fields, html.Label(html.Props{Class: "chatrender-check", For: "chatrender-room"}, html.Input(html.Props{ID: "chatrender-room", Name: "conversation", Type: "checkbox", Disabled: m.Loading}), html.Span(html.Props{Text: t("scope")})))
	}
	fields = append(fields, html.Button(html.Props{Type: "submit", Class: "button", Text: t("save"), Disabled: m.Loading || m.Submit.Value() == nil}))
	return html.Form(html.Props{Class: "chatrender-settings", Dir: direction(m.Locale), OnSubmit: m.Submit, Aria: map[string]string{"labelledby": "chatrender-settings-title"}}, fields...)
}
func RenderingLanguageIndicator(locale string, counts map[string]int) ui.Node {
	return RenderingLanguageStatusIndicator(locale, counts, false, false)
}
func RenderingLanguageStatusIndicator(locale string, counts map[string]int, loading, failed bool) ui.Node {
	children := []ui.Node{html.H3(html.Props{Text: RenderingText(locale, "languages")})}
	if loading || failed {
		key := "languages_loading"
		if failed {
			key = "languages_error"
		}
		return html.Section(html.Props{Class: "chatrender-indicator", Dir: direction(locale)}, children[0], html.P(html.Props{Role: "status", Text: RenderingText(locale, key)}))
	}
	languages := []string{}
	for lang, n := range counts {
		// CHATBUG-045: a message whose language is not known is not a language to
		// list; "Not specified" is never shown to a person.
		if n > 0 && lang != "und" {
			languages = append(languages, lang)
		}
	}
	sort.Strings(languages)
	if len(languages) == 0 {
		children = append(children, html.P(html.Props{Text: RenderingText(locale, "empty")}))
	}
	for _, lang := range languages {
		children = append(children, html.Span(html.Props{Class: "chatrender-language-count", Text: RenderingText(locale, lang) + ": " + strconv.Itoa(counts[lang])}))
	}
	return html.Section(html.Props{Class: "chatrender-indicator", Dir: direction(locale), Aria: map[string]string{"label": RenderingText(locale, "languages")}}, children...)
}

// RenderingView uses escaped text nodes. Generated text is never trusted HTML.
func RenderingView(locale string, r chatrender.Rendering, mark chatrender.Mark, original, written ui.Handler) ui.Node {
	return html.Div(html.Props{Class: "chatrender-view"}, html.P(html.Props{Class: "chatrender-text", Text: r.Text, Dir: "auto"}), RenderingMark(locale, mark, original, written))
}
