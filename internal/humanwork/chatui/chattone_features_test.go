package chatui

import (
	"encoding/json"
	"testing"
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
