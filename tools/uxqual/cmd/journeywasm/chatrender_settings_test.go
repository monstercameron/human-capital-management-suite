package main

import (
	"net/url"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

func TestTodo_CHATLANG_002(t *testing.T) {
	previous := chatrender.DefaultPreference("en-US")
	values := url.Values{"reading": {"de"}, "translate": {"on"}, "further": {"fr", "ar"}, "never": {"es"}}
	pref, err := renderingSettingsFromForm(values, previous)
	if err != nil || pref.ReadingLanguage != "de" || !pref.Translate || len(pref.FurtherLanguages) != 2 || pref.SourceOverrides["es"] {
		t.Fatal(pref, err)
	}
	if previous.ReadingLanguage != "en" || !previous.Translate || len(previous.SourceOverrides) > 0 {
		t.Fatal("input mutated")
	}
	values.Set("reading", "xx")
	if _, err = renderingSettingsFromForm(values, previous); err != chatrender.ErrInvalid {
		t.Fatal(err)
	}
	if got := renderingSettingsURL("room & more"); got != "/api/chat/renderings/v1/settings?conversation=room+%26+more" {
		t.Fatal(got)
	}
	if got := renderingSettingsURL(""); got != "/api/chat/renderings/v1/settings" {
		t.Fatal(got)
	}
}
