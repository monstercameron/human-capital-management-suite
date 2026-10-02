package chatrender

import (
	"reflect"
	"testing"
)

// TestTodo_CHATLANG_002_Selection pins what the selection tells a client about
// a translation: the mark says what was asked for, a channel that does not
// offer translation shows the original without a mark, and the person's own
// languages are never translated.
func TestTodo_CHATLANG_002_Selection(t *testing.T) {
	policy, pref, ready := renderingFixture()

	// Ready: the translation, marked as one, with the original one step away.
	got, mark := Select(policy, pref, Available{Renderings: []Rendering{ready}})
	if got.Text != "translation" || mark.State != "ready" || !reflect.DeepEqual(mark.Kinds, []Kind{Translate}) || mark.SourceLanguage != "de" || len(mark.Wanted) != 0 {
		t.Fatalf("ready: %+v %+v", got, mark)
	}

	// Pending and failed say what was asked for, so a client can say
	// "Translating…" and "Not translated" without guessing.
	_, mark = Select(policy, pref, Available{Pending: true})
	if mark.State != "pending" || !reflect.DeepEqual(mark.Wanted, []Kind{Translate}) || !mark.CanShowOriginal {
		t.Fatalf("pending: %+v", mark)
	}
	got, mark = Select(policy, pref, Available{Failed: true})
	if mark.State != "fallback" || got.Text != "original" || !reflect.DeepEqual(mark.Wanted, []Kind{Translate}) {
		t.Fatalf("failed: %+v %+v", got, mark)
	}
	reword := pref
	reword.Tone = Reworded
	_, mark = Select(policy, reword, Available{Failed: true})
	if !reflect.DeepEqual(mark.Wanted, []Kind{Reword, Translate}) {
		t.Fatalf("reworded and translated: %+v", mark)
	}

	// A channel whose policy does not allow translation shows the original with
	// no mark and requests nothing.
	noEngine := policy
	noEngine.AllowedKinds = []Kind{Mask}
	got, mark = Select(noEngine, pref, Available{})
	if got.Text != "original" || mark.State != "original" || len(mark.Wanted) != 0 {
		t.Fatalf("no translation offered: %+v %+v", got, mark)
	}
	if targets := RequestsForPolicy(noEngine, []Preference{pref}); len(targets) != 0 {
		t.Fatalf("a translation was requested where none is offered: %+v", targets)
	}
	if targets := RequestsForPolicy(policy, []Preference{pref, pref}); len(targets) != 1 || targets[0].Language != "en" || !hasKind(targets[0].Kinds, Translate) {
		t.Fatalf("one request for two readers of the same language: %+v", targets)
	}

	// Languages the person reads, and languages they never want translated.
	pref.FurtherLanguages = []string{"de"}
	got, mark = Select(policy, pref, Available{})
	if got.Text != "original" || mark.State != "original" {
		t.Fatalf("a further language was translated: %+v %+v", got, mark)
	}
	pref.FurtherLanguages = nil
	pref.SourceOverrides = map[string]bool{"de": false}
	if got, mark = Select(policy, pref, Available{}); got.Text != "original" || mark.State != "original" {
		t.Fatalf("a never-translate language was translated: %+v %+v", got, mark)
	}
	pref.SourceOverrides = nil
	pref.Translate = false
	if got, mark = Select(policy, pref, Available{}); got.Text != "original" || mark.State != "original" {
		t.Fatalf("translation off was ignored: %+v %+v", got, mark)
	}

	// A message with no language is never translated, whatever was asked.
	und := policy
	und.Original.SourceLanguage, und.Original.Language = "und", "und"
	pref = DefaultPreference("de-DE")
	got, mark = Select(und, pref, Available{})
	if got.Text != "original" || mark.State != "original" || len(mark.Wanted) != 0 {
		t.Fatalf("a message with no language was translated: %+v %+v", got, mark)
	}
	if !DefaultPreference("de-DE").Translate || DefaultPreference("de-DE").ReadingLanguage != "de" || DefaultPreference("xx").ReadingLanguage != "en" {
		t.Fatal("the default reading language is the interface language, with translation on")
	}
}
