package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// TestTodo_CHATUX_049: close buttons, disabled buttons, focus rings, selects,
// message edges, the pencil and mentions each came in several forms. There is
// one of each now.
func TestTodo_CHATUX_049(t *testing.T) {
	t.Run("one close button", func(t *testing.T) {
		want := ".chat-panel-head .chat-panel-close.chat-panel-close.icon-button{box-sizing:border-box;inline-size:32px;min-inline-size:32px;block-size:32px;min-block-size:32px;padding:0;border:0;border-radius:var(--hcm-radius-control);background:transparent;box-shadow:none;color:var(--muted);display:inline-flex;align-items:center;justify-content:center}"
		if !strings.Contains(Stylesheet, want) {
			t.Error("the plain-X close button rule is gone")
		}
		for _, ring := range []string{
			`.chat-workspace .chat-panel-close:focus:not(:focus-visible){outline:none;box-shadow:none}`,
			`.chat-workspace .chat-panel-close:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus)`,
		} {
			if !strings.Contains(Stylesheet, ring) {
				t.Errorf("the close button lost its keyboard-only ring: %.60q", ring)
			}
		}
		// No close rule paints a fill: every chat-panel-close rule keeps a
		// transparent background or none at all.
		for _, at := range regexp.MustCompile(`chat-panel-close[^{]*\{[^}]*\}`).FindAllString(Stylesheet+ScopedStylesheet(), -1) {
			if strings.Contains(at[:strings.Index(at, "{")], ":hover") {
				continue
			}
			body := at[strings.Index(at, "{"):]
			if strings.Contains(body, "background:") && !strings.Contains(body, "background:transparent") {
				t.Errorf("a close button is filled again: %.100q", at)
			}
		}
	})

	t.Run("one disabled button style", func(t *testing.T) {
		want := ".chat-workspace .send-button:disabled,.chat-workspace .button:not(.secondary):disabled,.chat-workspace .button:not(.secondary)[aria-disabled=\"true\"]{background:var(--soft);border-color:var(--line);color:var(--muted);opacity:1;cursor:not-allowed;filter:none}"
		if !strings.Contains(Stylesheet, want) {
			t.Error("Send and the primary buttons no longer share one disabled style")
		}
		// Full opacity on the muted-on-soft token pair: the label keeps the
		// tokens' own contrast instead of being faded beneath it. Chat owns no
		// palette, so the numeric ratio is the shell theme's contract; what
		// chat guarantees is that it never dims the pair.
	})

	t.Run("one focus ring", func(t *testing.T) {
		if !strings.Contains(Stylesheet, ".chat-workspace :focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}") {
			t.Error("the base focus ring is gone")
		}
		// Both sheets: the viewer hangs outside the workspace in the scoped
		// sheet, and its rings answer to the same token.
		for _, rule := range regexp.MustCompile(`:focus-visible[^{]*\{[^}]*outline:[^}]*\}`).FindAllString(Stylesheet+ScopedStylesheet(), -1) {
			body := rule[strings.Index(rule, "{"):]
			outline := body[strings.Index(body, "outline:"):]
			if end := strings.IndexAny(outline, ";}"); end >= 0 {
				outline = outline[:end]
			}
			if strings.Contains(outline, "var(--hcm-color-focus") || strings.Contains(outline, "var(--hcm-focus-ring") ||
				strings.Contains(outline, "transparent") || strings.Contains(outline, "none") || strings.Contains(outline, ":0") {
				continue
			}
			t.Errorf("a focus ring leaves the focus token: %.100q", rule)
		}
	})

	t.Run("one select arrow", func(t *testing.T) {
		if n := strings.Count(Stylesheet, "background-image:linear-gradient(45deg,transparent 50%,currentColor 50%)"); n != 1 {
			t.Errorf("the panel select arrow is defined %d times, want once", n)
		}
	})

	t.Run("a hovered row and the composer share inline edges", func(t *testing.T) {
		// Desktop: the list pads 20, the composer margins 20, the row takes no
		// negative margin and caps at the column, so row and composer are the
		// same box.
		for _, want := range []string{
			`.message-list{flex:1;min-height:0;overflow-y:auto;overscroll-behavior:contain;display:flex;flex-direction:column;padding:8px 20px;scroll-padding-top:8px}`,
			`margin:10px 0 0;border-radius:var(--hcm-radius-control)}`,
			`.chat-workspace .message-list .message{max-width:var(--chat-column)}`,
			`.chat-composer{position:relative;flex:none;min-width:0;margin:4px 20px 20px;`,
			// Compact: 12 all round.
			`.message{padding:6px 12px;margin-inline:0}`,
			`.message-list{padding-inline:12px}`,
			`.chat-composer{margin:4px 12px 12px}`,
		} {
			if !strings.Contains(Stylesheet, want) {
				t.Errorf("shared edges broken, missing %.70q", want)
			}
		}
	})

	t.Run("the pencil is for editing only", func(t *testing.T) {
		if iconPaths["compose"] == iconPaths["edit"] {
			t.Error("compose and edit are the same glyph; new conversations read as edits")
		}
		m := chat4Fixture("en-US", "sent", false)
		m.Preferences.Drafts = map[string]string{"people": "half a thought"}
		markup := renderNode(t, Build(m))
		if !strings.Contains(markup, ">Draft<") {
			t.Error("the draft row has no Draft tag")
		}
		// Every remaining pencil edits something: the channel purpose or a
		// saved note. Nothing else on the page may carry it.
		for _, at := range regexp.MustCompile(`icon-edit`).FindAllStringIndex(markup, -1) {
			window := markup[max(0, at[0]-600):at[0]]
			if !strings.Contains(window, "details-purpose-cue") && !strings.Contains(window, "chatsave-note") && !strings.Contains(window, "chatsave002") {
				t.Errorf("the pencil edits nothing here: %.120q", window[len(window)-120:])
			}
		}
		at := strings.Index(markup, `data-action="open-create"`)
		if at < 0 {
			t.Fatal("no new-conversation button to check")
		}
		if !strings.Contains(markup[at:min(at+600, len(markup))], "icon-compose") {
			t.Error("new conversation does not use the compose icon")
		}
	})

	t.Run("mentions use the accent tint", func(t *testing.T) {
		if !strings.Contains(Stylesheet, ".mention-chip{display:inline;") || !strings.Contains(Stylesheet, "background:color-mix(in srgb,var(--accent) 12%,var(--surface))") {
			t.Error("mention chips left the accent tint")
		}
		for _, rule := range regexp.MustCompile(`\.mention[a-z-]*\{[^}]*\}`).FindAllString(Stylesheet+ScopedStylesheet(), -1) {
			if strings.Contains(strings.ToLower(rule), "khaki") || regexp.MustCompile(`#[0-9a-fA-F]{6}`).MatchString(rule) {
				t.Errorf("a mention rule paints outside the tokens: %.100q", rule)
			}
		}
	})
}
