package chatui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func listenModel(features *ChatFeatures, locale, menuID string, messages ...Message) Model {
	return Model{Locale: locale, State: StateReady, SelectedID: "general", MenuID: menuID, ChatFeatures: features, Conversations: []Conversation{{ID: "general", Name: "General", HostTenantID: "tenant-a"}}, Messages: messages}
}

var listenTypedMessage = Message{ID: "typed", AuthorID: "polly", Author: "Polly Adams", Body: "Lunch is at noon"}
var listenAgentMessage = Message{ID: "answer", AuthorID: "assistant", Author: "Assistant", Body: "The policy says ten days.", PersonaActor: &PersonaActor{PersonaID: "assistant", AgentID: "assistant", Trusted: true}}

// TestTodo_CHATVOICE_006_Browser renders Listen through the real message list:
// nothing is drawn for it at rest, and when a message's actions are open it is
// an icon in the action bar and an item of the "More actions" menu, on a typed
// message and on an agent answer, in the three languages.
func TestTodo_CHATVOICE_006_Browser(t *testing.T) {
	features := &ChatFeatures{Listen: true}
	atRest := render(t, listenModel(features, "en-US", "", listenTypedMessage, listenAgentMessage))
	if strings.Contains(atRest, "chatlisten") || strings.Contains(atRest, "Listen to this") {
		t.Fatal("Listen is drawn in or under a message at rest")
	}
	for _, c := range []struct{ locale, listen, typedLabel, agentLabel string }{
		{"en-US", "Listen", "Listen to this message", "Listen to this answer"},
		{"de-DE", "Anhören", "Diese Nachricht anhören", "Diese Antwort anhören"},
		{"ar", "استماع", "الاستماع إلى هذه الرسالة", "الاستماع إلى هذه الإجابة"},
	} {
		for _, msg := range []Message{listenTypedMessage, listenAgentMessage} {
			markup := render(t, listenModel(features, c.locale, msg.ID, msg))
			if strings.Count(markup, `data-chatlisten="start"`) != 2 {
				t.Fatalf("%s %s: want the action bar icon and the menu item, found %d", c.locale, msg.ID, strings.Count(markup, `data-chatlisten="start"`))
			}
			label := c.typedLabel
			if msg.PersonaActor != nil {
				label = c.agentLabel
			}
			for _, want := range []string{`aria-label="` + label + `"`, `title="` + label + `"`, `data-chatlisten-post="` + msg.ID + `"`, `data-chatlisten-tenant="tenant-a"`, `data-chatlisten-conversation="general"`, `role="menuitem"`, c.listen} {
				if !strings.Contains(markup, want) {
					t.Fatalf("%s %s: missing %q", c.locale, msg.ID, want)
				}
			}
			if !regexp.MustCompile(`(?s)class="message-actions".*chatlisten-action`).MatchString(markup) {
				t.Fatalf("%s: Listen is not in the action bar", c.locale)
			}
			// The control is an action: the message's content comes before its action
			// bar in the row, so the first Listen is after the bar begins.
			if first, bar := strings.Index(markup, "chatlisten"), strings.Index(markup, `class="message-actions"`); bar < 0 || first < bar {
				t.Fatalf("%s: Listen appears in the message content, before the action bar", c.locale)
			}
		}
	}
}

// TestTodo_CHATVOICE_006_Absent: where Listen cannot work it is absent, never
// disabled.
func TestTodo_CHATVOICE_006_Absent(t *testing.T) {
	voice := Message{ID: "voice", Body: "Voice message", Attachments: []Attachment{{ID: "audio", ContentType: "audio/webm"}}}
	for name, c := range map[string]struct {
		features *ChatFeatures
		msg      Message
	}{
		"no features":          {nil, listenTypedMessage},
		"engine not composed":  {&ChatFeatures{Listen: false}, listenTypedMessage},
		"channel bars outside": {&ChatFeatures{Listen: true, ListenBarred: "other,general"}, listenTypedMessage},
		"empty message":        {&ChatFeatures{Listen: true}, Message{ID: "empty", Body: "  "}},
		"voice message":        {&ChatFeatures{Listen: true}, voice},
	} {
		markup := render(t, listenModel(c.features, "en-US", c.msg.ID, c.msg))
		if strings.Contains(markup, "chatlisten") {
			t.Fatalf("%s: Listen offered", name)
		}
	}
	// Another channel that does not bar it still has it.
	if !strings.Contains(render(t, listenModel(&ChatFeatures{Listen: true, ListenBarred: "elsewhere"}, "en-US", "typed", listenTypedMessage)), `data-chatlisten="start"`) {
		t.Fatal("a bar on another conversation removed Listen here")
	}
}

// TestTodo_CHATVOICE_006_Accessibility checks the action's name and the player
// row's structure, status and controls.
func TestTodo_CHATVOICE_006_Accessibility(t *testing.T) {
	bar := render(t, listenModel(&ChatFeatures{Listen: true}, "en-US", "typed", listenTypedMessage))
	if !regexp.MustCompile(`<button[^>]*class="message-action chatlisten-action"[^>]*>`).MatchString(bar) || !strings.Contains(bar, `<span class="sr-only">Listen</span>`) || strings.Contains(bar, `tabindex="-1"`) && regexp.MustCompile(`chatlisten-action[^>]*tabindex="-1"`).MatchString(bar) {
		t.Fatalf("the action bar button has no name or is out of the keyboard order: %s", bar)
	}
	markup, err := ui.RenderToString(RenderListenPlayer(ListenPlayerProps{Locale: "en-US", PostID: "p"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`role="group"`, `aria-label="Reading aloud"`, `role="status"`, `aria-live="polite"`, `for="chatlisten-speed-p"`, `id="chatlisten-speed-p"`, `aria-label="Reading progress"`, `data-chatlisten="pause"`, `data-chatlisten="close"`, `aria-label="Close"`, `data-chatlisten-speed`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("player row is missing %s in %s", want, markup)
		}
	}
	// Pause, progress and speed stay hidden until the audio is ready.
	if !regexp.MustCompile(`data-chatlisten-controls[^>]*hidden|hidden[^>]*data-chatlisten-controls`).MatchString(markup) {
		t.Fatalf("controls are not hidden before reading: %s", markup)
	}
	arabic, _ := ui.RenderToString(RenderListenPlayer(ListenPlayerProps{Locale: "ar", PostID: "p"}))
	if !strings.Contains(arabic, `dir="rtl"`) {
		t.Fatal("the Arabic player row is not right to left")
	}
	for _, key := range []string{"listen", "listenlabel", "listenagent", "player", "loading", "playing", "pause", "resume", "close", "progress", "speed", "barred", "toolong", "unavailable", "failed"} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			if got := ListenCopy(locale, key); got == "" || got == key {
				t.Fatalf("%s: copy %q is %q", locale, key, got)
			}
		}
	}
	for _, want := range []string{"min-height:44px", "focus-visible", "prefers-reduced-motion", "min-width:0", "var(--hcm-"} {
		if !strings.Contains(chatListenStyles, want) {
			t.Fatal("style missing " + want)
		}
	}
	if strings.Contains(chatListenStyles, "#") || strings.Contains(chatListenStyles, "font-family") {
		t.Fatal("branding bypass")
	}
}
