package main

import (
	"net/url"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// renderingSettingsFromForm is shared with the wasm event adapter. It consumes
// submit-time DOM values, never a Value binding on an editable input.
func renderingSettingsFromForm(form url.Values, previous chatrender.Preference) (chatrender.Preference, error) {
	p := previous
	p.ReadingLanguage = form.Get("reading")
	p.Translate = form.Get("translate") == "on"
	p.FurtherLanguages = append([]string(nil), form["further"]...)
	p.SourceOverrides = map[string]bool{}
	for _, lang := range form["never"] {
		p.SourceOverrides[lang] = false
	}
	if p.Tone == "" {
		p.Tone = chatrender.AsWritten
	}
	return p, p.Validate()
}
func renderingSettingsURL(conversation string) string {
	path := "/api/chat/renderings/v1/settings"
	if conversation != "" {
		path += "?conversation=" + url.QueryEscape(conversation)
	}
	return path
}
