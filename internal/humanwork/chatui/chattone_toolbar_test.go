package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"strings"
	"testing"
)

func chattoneMarkup(t *testing.T, p ChattoneToolbarProps) string {
	t.Helper()
	markup, err := ui.RenderToString(RenderChattoneToolbar(p))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}
func TestTodo_CHATTONE_004(t *testing.T) {
	p := ChattoneToolbarProps{Locale: "en-US", Target: "chat-composer", Conversation: "room", Draft: "Please retain this draft", Styles: chatrewrite.DefaultStyles(), Enabled: true, Suggestion: &chatrewrite.Suggestion{StyleID: "concise", ReasonKey: "short"}}
	markup := chattoneMarkup(t, p)
	for _, want := range []string{"Professional", "Friendly", "Concise", "Suggested", `data-ready="true"`, `data-style="concise"`, "Recent messages in this conversation are short and direct.", "only when you choose a style"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	if strings.Count(markup, `data-suggested="true"`) != 1 {
		t.Fatal("more than one suggested style")
	}
	p.Draft = "two words"
	if !strings.Contains(chattoneMarkup(t, p), `data-ready="false"`) {
		t.Fatal("controls shown too early")
	}
	p.Enabled = false
	if got := chattoneMarkup(t, p); got != "" {
		t.Fatal("disabled controls render", got)
	}
}
func TestTodo_CHATTONE_004_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		p := ChattoneToolbarProps{Locale: locale, Target: "chat-composer", Draft: "one two three", Styles: chatrewrite.DefaultStyles(), Enabled: true}
		markup := chattoneMarkup(t, p)
		for _, want := range []string{`type="button"`, `role="group"`, `role="status"`, `aria-live="polite"`, `title=`, `aria-label=`, `data-chat-layer="writing-style"`, ChattoneText(locale, "professional"), ChattoneText(locale, "notice")} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s missing %s", locale, want)
			}
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Fatal("missing RTL")
		}
		preview, err := ui.RenderToString(RenderChattonePreview(ChattonePreviewProps{Locale: locale, Original: "  exactly\n typed <script>  ", Rewritten: "Changed", ShowChanges: true}))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{ChattoneText(locale, "undo"), ChattoneText(locale, "changes"), `aria-expanded="true"`, `<del`, `<ins`, "&lt;script&gt;"} {
			if !strings.Contains(preview, want) {
				t.Fatalf("preview %s missing %s", locale, want)
			}
		}
	}
	for _, want := range []string{"min-height:44px", "focus-visible", "max-width:600px", "prefers-reduced-motion:reduce", "overflow-wrap:anywhere", "--hcm-color-surface", "--hcm-color-text"} {
		if !strings.Contains(ChattoneStyles, want) {
			t.Fatal("style contract missing", want)
		}
	}
	if strings.Contains(ChattoneStyles, "#") || strings.Contains(ChattoneStyles, "font-family") {
		t.Fatal("hardcoded branding")
	}
}
func TestTodo_CHATTONE_004_Browser(t *testing.T) {
	m := Model{Locale: "en-US", State: StateReady, SelectedID: "room", Draft: "Please retain these words", Conversations: []Conversation{{ID: "room", Name: "Team"}}, Callbacks: Callbacks{SendMessage: func(string, string) {}}}
	markup := render(t, m)
	if !strings.Contains(markup, `data-chattone="toolbar"`) || !strings.Contains(markup, `data-target="chat-composer"`) || !strings.Contains(Stylesheet, ChattoneStyles) {
		t.Fatal("composer/style hook unreachable")
	}
	if strings.Contains(markup, `value="Please retain these words"`) {
		t.Fatal("controlled draft introduced")
	}
}
func TestTodo_CHATTONE_004_Golden(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, key := range []string{"style", "professional", "friendly", "concise", "suggested", "notice", "working", "limit", "preservation", "unavailable", "policy", "undo", "changes", "before", "after", "edited", "incident", "formal", "short", "close", "neutral", "audience"} {
			if ChattoneText(locale, key) == "" {
				t.Fatalf("missing copy %s %s", locale, key)
			}
		}
	}
	markup, err := ui.RenderToString(RenderChattonePreview(ChattonePreviewProps{Locale: "en-US", Original: "Original", Rewritten: "Rewritten"}))
	if err != nil || strings.Contains(markup, "<del") || !strings.Contains(markup, `aria-expanded="false"`) {
		t.Fatal("preview default", markup, err)
	}
}

func TestTodo_CHATTONE_004_HouseStyle(t *testing.T) {
	styles := chatrewrite.DefaultStyles()
	styles[0].Label = "House voice"
	p := ChattoneToolbarProps{Locale: "de-DE", Target: "chat-composer", Draft: "one two three", Styles: styles, Enabled: true}
	markup := chattoneMarkup(t, p)
	if !strings.Contains(markup, "House voice") || !strings.Contains(markup, `aria-keyshortcuts="Alt+Shift+1"`) || !strings.Contains(markup, `role="tooltip"`) || !strings.Contains(markup, `aria-describedby="chat-composer-chattone-help-1"`) {
		t.Fatal("renamed style or focus tooltip missing", markup)
	}
}
