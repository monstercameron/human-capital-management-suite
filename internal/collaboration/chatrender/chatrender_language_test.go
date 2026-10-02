package chatrender

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func languageCorpus() map[string]string {
	return map[string]string{
		"en": "Please read this message with your team today.",
		"de": "Bitte lesen wir die Nachricht heute mit dem Team.",
		"fr": "Bonjour, nous avons un message pour les personnes avec nous.",
		"es": "Hola, gracias por el mensaje para nosotros hoy.",
		"pt": "Olá, obrigado pela mensagem para nós hoje. Não temos uma reunião.",
		"ar": "مرحبًا بكم في هذه المحادثة اليوم مع جميع الزملاء",
		"ja": "こんにちは皆さん、今日の会議についてお知らせします。",
		"hi": "नमस्ते सभी साथियों आज की बैठक के लिए यह संदेश है",
	}
}
func TestTodo_CHATLANG_002(t *testing.T) {
	for lang, text := range languageCorpus() {
		t.Run(lang, func(t *testing.T) {
			got := Detect(text)
			if got.Language != lang || got.Confidence <= 0 || got.Confidence > 1 {
				t.Fatalf("%s: %+v", text, got)
			}
		})
	}
	for _, text := range []string{"ok", "👍", "Alexander", "```go\nfunc main() {}\n```", "x := 12", "a b c d"} {
		if d := Detect(text); d.Language != "und" {
			t.Fatalf("ambiguous %q = %+v", text, d)
		}
	}
	mixed := Detect("Please read this message with your team today. Bitte lesen wir die Nachricht heute mit dem Team.")
	if mixed.Language != "mul" || len(mixed.Spans) != 2 || mixed.Spans[0].Language != "en" || mixed.Spans[1].Language != "de" {
		t.Fatal(mixed)
	}
	mixed = Detect("Please read this message مرحبًا بكم في هذه المحادثة اليوم مع جميع الزملاء")
	if mixed.Language != "mul" || len(mixed.Spans) != 2 || mixed.Spans[1].Language != "ar" {
		t.Fatal(mixed)
	}
	pref := DefaultPreference("de-DE")
	// CHATLANG-002: translation is on for a person who has not chosen (it still
	// takes a workspace that offers it); this line used to assert it was off.
	if pref.ReadingLanguage != "de" || !pref.Translate {
		t.Fatal(pref)
	}
	pref.Translate = true
	pref.FurtherLanguages = []string{"fr"}
	pref.SourceOverrides = map[string]bool{"es": false}
	if WantsTranslation(pref, "fr") || WantsTranslation(pref, "es") || WantsTranslation(pref, "und") || WantsTranslation(pref, "de") || !WantsTranslation(pref, "en") {
		t.Fatal("translation overrides ignored")
	}
}
func TestTodo_CHATLANG_002_Property(t *testing.T) {
	for _, text := range languageCorpus() {
		a, b := Detect(text), Detect(text)
		if !reflect.DeepEqual(a, b) {
			t.Fatal(a, b)
		}
		for _, s := range a.Spans {
			if s.Start < 0 || s.End > len(text) || s.Start >= s.End {
				t.Fatal(s)
			}
		}
	}
	if Detect(languageCorpus()["en"]).Language == Detect(languageCorpus()["de"]).Language {
		t.Fatal("edit did not redetect")
	}
	ctx := WithRevisionLanguage(context.Background(), languageCorpus()["en"])
	if PreparedDetection(ctx, languageCorpus()["en"]).Language != "en" || PreparedDetection(ctx, languageCorpus()["de"]).Language != "de" {
		t.Fatal("cached detection used for different committed text")
	}
}
func TestTodo_CHATLANG_002_Security(t *testing.T) {
	p := DefaultPreference("unsupported")
	if p.ReadingLanguage != "en" {
		t.Fatal(p)
	}
	for _, bad := range []Preference{{Tone: "unknown", ReadingLanguage: "en"}, {Tone: AsWritten, ReadingLanguage: "xx"}, {Tone: AsWritten, ReadingLanguage: "en", FurtherLanguages: []string{"und"}}, {Tone: AsWritten, ReadingLanguage: "en", SourceOverrides: map[string]bool{"xx": true}}} {
		if bad.Validate() == nil {
			t.Fatal(bad)
		}
	}
	if DefaultPreference("ar").Validate() != nil {
		t.Fatal("Arabic invalid")
	}
}
func TestTodo_CHATLANG_002_Performance(t *testing.T) {
	start := time.Now()
	const count = 1000
	for i := 0; i < count; i++ {
		if Detect(languageCorpus()["en"]).Language != "en" {
			t.Fatal("detection")
		}
	}
	if elapsed := time.Since(start) / count; elapsed >= 2*time.Millisecond {
		t.Fatalf("detection %v >= 2ms", elapsed)
	}
	long := strings.Repeat(languageCorpus()["en"], 1000)
	start = time.Now()
	for i := 0; i < 100; i++ {
		if Detect(long).Language != "en" {
			t.Fatal("long detection")
		}
	}
	if elapsed := time.Since(start) / 100; elapsed >= 2*time.Millisecond {
		t.Fatalf("long detection %v >= 2ms", elapsed)
	}
}
