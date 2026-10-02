package chatui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_CHATTONE_004_FeatureOff: the composer shows the writing-style
// controls only when the server's features answer says this person's workspace
// has them on. Before that answer arrives (all features false) and when the
// workspace has turned them off, the composer has no controls at all.
func TestTodo_CHATTONE_004_FeatureOff(t *testing.T) {
	model := Model{Locale: "en-US", SelectedID: "room", Draft: "one two three four", ChatFeatures: &ChatFeatures{WritingStyles: true}}
	for _, target := range []string{"chat-composer", "thread-composer"} {
		if chattoneToolbar(model, target, false) == nil {
			t.Fatalf("%s: the controls are missing for a workspace that has them on", target)
		}
	}
	for name, features := range map[string]*ChatFeatures{
		"before the features answer": {},
		"another feature only":       {Gates: true, Renderings: true},
	} {
		off := model
		off.ChatFeatures = features
		if chattoneToolbar(off, "chat-composer", false) != nil || chattoneToolbar(off, "thread-composer", false) != nil {
			t.Fatalf("%s: the controls are shown", name)
		}
	}
	// The wire form of an administrator's "off".
	var decoded ChatFeatures
	if err := json.Unmarshal([]byte(`{"gates":true,"renderings":true,"status":true,"search":true,"filters":true,"locations":true,"writing_styles":false}`), &decoded); err != nil {
		t.Fatal(err)
	}
	off := model
	off.ChatFeatures = &decoded
	if chattoneToolbar(off, "chat-composer", false) != nil {
		t.Fatal("a features answer with writing_styles false still shows the controls")
	}
}

// TestTodo_CHATTONE_004_UnavailableSaysWhy: where the server says why the
// writing styles are not offered, the composer states it in words (English,
// German and Arabic) instead of showing nothing; a long enough draft shows it, a
// short one keeps it hidden, and the style buttons are never drawn.
func TestTodo_CHATTONE_004_UnavailableSaysWhy(t *testing.T) {
	var features ChatFeatures
	if err := json.Unmarshal([]byte(`{"gates":true,"writing_styles":false,"writing_styles_note":"not_qualified"}`), &features); err != nil {
		t.Fatal(err)
	}
	for locale, want := range map[string]string{
		"en-US": "Writing styles are not available yet: no model has passed the quality check for them in this workspace.",
		"de-DE": "Schreibstile sind noch nicht verfügbar",
		"ar":    "أساليب الكتابة غير متاحة بعد",
	} {
		model := Model{Locale: locale, SelectedID: "room", Draft: "one two three four", ChatFeatures: &features}
		for _, target := range []string{"chat-composer", "thread-composer"} {
			node := chattoneToolbar(model, target, false)
			if node == nil {
				t.Fatalf("%s %s: no reason was given for the missing controls", locale, target)
			}
			markup, err := ui.RenderToString(node)
			if err != nil {
				t.Fatal(err)
			}
			if target == "chat-composer" && (!strings.Contains(markup, want) || strings.Contains(markup, "note_") || strings.Contains(markup, "chattone-style")) {
				t.Fatalf("%s: %s", locale, markup)
			}
			if !strings.Contains(markup, `role="status"`) {
				t.Fatalf("%s %s: the reason is not announced: %s", locale, target, markup)
			}
		}
	}
	short := Model{Locale: "en-US", SelectedID: "room", Draft: "two words", ChatFeatures: &features}
	markup, _ := ui.RenderToString(chattoneToolbar(short, "chat-composer", false))
	if !strings.Contains(markup, " hidden") {
		t.Fatalf("a short draft shows the reason: %s", markup)
	}
	off := features
	off.WritingStylesNote = "workspace_off"
	model := Model{Locale: "en-US", SelectedID: "room", Draft: "one two three four", ChatFeatures: &off}
	markup, _ = ui.RenderToString(chattoneToolbar(model, "chat-composer", false))
	if !strings.Contains(markup, "turned off for this workspace by an administrator") {
		t.Fatalf("an administrator's off: %s", markup)
	}
	// A code this build does not know is not turned into a raw key on the page.
	off.WritingStylesNote = "something_new"
	model.ChatFeatures = &off
	if chattoneToolbar(model, "chat-composer", false) != nil {
		t.Fatal("an unknown reason code was drawn")
	}
}
