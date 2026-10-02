package chatui

import (
	stdhtml "html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func chatbug085Dialog(t *testing.T, m ModerationDialogModel) string {
	t.Helper()
	if m.Model.Locale == "" {
		m.Model.Locale = "en-US"
	}
	m.Selection = chat.RemovalSelection{ConversationID: "room", PostIDs: []string{"post"}}
	if m.Target.Body == "" {
		m.Target = ModerationTargetView{AuthorID: "jake", HomeTenantID: "t", AuthorName: "Jake Sullivan", Body: "Things to do:\n\n- first **item**\n- second item"}
	}
	return stdhtml.UnescapeString(renderNode(t, ModerationDialog(m)))
}

// chatbug085Submit is the opening tag of a dialog's own button (not the bulk
// form's), found by its label.
func chatbug085Submit(t *testing.T, markup, label string) string {
	t.Helper()
	tag := regexp.MustCompile(`<button[^>]*type="submit"[^>]*>` + regexp.QuoteMeta(label) + `</button>`).FindString(markup)
	if tag == "" {
		t.Fatalf("the dialog has no %q button: %s", label, markup)
	}
	return tag
}

// TestTodo_CHATBUG_085 holds the report and removal forms to the standard Chat
// dialog, to a reason before their button works, and to the quoted message
// drawn as the conversation draws it; and the message list to one action bar
// and an add-reaction button the size of a reaction.
func TestTodo_CHATBUG_085(t *testing.T) {
	t.Run("the standard dialog with a heading and Close", func(t *testing.T) {
		for name, m := range map[string]ModerationDialogModel{
			"report":  {Report: true, Action: "report"},
			"remove":  {Action: "remove"},
			"restore": {Action: "restore"},
			"sent":    {Report: true, Sent: true},
		} {
			markup := chatbug085Dialog(t, m)
			if !strings.Contains(markup, `<section aria-labelledby="chatremove-title" aria-modal="true" class="chatremove chat-dialog"`) {
				t.Errorf("%s: the form is not the standard dialog: %s", name, markup[:min(len(markup), 200)])
			}
			heading := regexp.MustCompile(`<div class="side-heading"><h2[^>]*id="chatremove-title"[^>]*>[^<]+</h2><button[^>]*>`).FindString(markup)
			if heading == "" {
				t.Fatalf("%s: no heading row with the title and Close: %s", name, markup)
			}
			for _, want := range []string{`aria-label="Close"`, `title="Close"`, `data-chatremove-close="true"`, `type="button"`, `class="icon-button"`} {
				if !strings.Contains(heading, want) {
					t.Errorf("%s: Close lacks %s: %s", name, want, heading)
				}
			}
		}
	})

	t.Run("no reason, no button", func(t *testing.T) {
		for name, tc := range map[string]struct {
			model ModerationDialogModel
			label string
		}{
			"report":                     {ModerationDialogModel{Report: true, Action: "report"}, "Send report"},
			"remove":                     {ModerationDialogModel{Action: "remove"}, "Remove message"},
			"a queue item being decided": {ModerationDialogModel{Action: "remove", CaseID: "case-1"}, "Remove message"},
		} {
			off := chatbug085Submit(t, chatbug085Dialog(t, tc.model), tc.label)
			if !strings.Contains(off, " disabled") || !strings.Contains(off, ChatBug085NeedsReasonAttr+`="true"`) {
				t.Errorf("%s: %s can be pressed before a reason is chosen, or is not marked for the client: %s", name, tc.label, off)
			}
			chosen := tc.model
			chosen.ReasonCode = "spam"
			if on := chatbug085Submit(t, chatbug085Dialog(t, chosen), tc.label); strings.Contains(on, " disabled") {
				t.Errorf("%s: %s stays off with a reason chosen: %s", name, tc.label, on)
			}
		}
		// Restore and Message the author ask for no reason, so their button is on.
		for label, m := range map[string]ModerationDialogModel{"Restore": {Action: "restore"}, chatremoveText("en-US", "send_message"): {Action: "message"}} {
			if tag := chatbug085Submit(t, chatbug085Dialog(t, m), label); strings.Contains(tag, " disabled") || strings.Contains(tag, ChatBug085NeedsReasonAttr) {
				t.Errorf("%s waits for a reason it does not ask for: %s", label, tag)
			}
		}
	})

	t.Run("the quoted message is drawn as the message is", func(t *testing.T) {
		markup := chatbug085Dialog(t, ModerationDialogModel{Report: true, Action: "report"})
		quote := markup[strings.Index(markup, `<figure class="chatremove-quote"`):strings.Index(markup, "</figure>")]
		for _, want := range []string{">Message from Jake Sullivan<", "<ul", "<li", "<strong>item</strong>", `class="message-body"`, " inert"} {
			if !strings.Contains(quote, want) {
				t.Errorf("the quoted message lacks %q: %s", want, quote)
			}
		}
		if strings.Contains(quote, "- first") || strings.Contains(quote, "**") {
			t.Errorf("the quoted message prints its Markdown as typed: %s", quote)
		}
		// Markup in a reported message is text, as it is in the conversation.
		hostile := chatbug085Dialog(t, ModerationDialogModel{Report: true, Action: "report", Target: ModerationTargetView{AuthorName: "Jake Sullivan", Body: "<script>alert(1)</script> [x](javascript:alert(1))"}})
		if raw := renderNode(t, ModerationDialog(ModerationDialogModel{Model: Model{Locale: "en-US"}, Report: true, Target: ModerationTargetView{Body: "<script>alert(1)</script>"}})); strings.Contains(raw, "<script") {
			t.Fatalf("a reported message's markup is live in the dialog: %s", raw)
		}
		if strings.Contains(hostile, `href="javascript:`) {
			t.Fatalf("an unsafe link in a reported message is a link in the dialog: %s", hostile)
		}
	})

	t.Run("styles", func(t *testing.T) {
		if strings.Contains(ChatremoveStyles, "margin:auto") || strings.Contains(ChatremoveStyles, ".chatremove-overlay-dialog") {
			t.Error("the dialog still has a box of its own instead of the standard dialog's")
		}
		for _, rule := range []string{
			".chat-workspace .chatremove-overlay.chatremove-overlay-dialog{z-index:1600;background:rgba(7,17,24,.5);overflow:hidden}",
			".chatremove-overlay-dialog .chatremove.chat-dialog{padding:0 24px 22px;font-size:.875rem;line-height:1.45}",
			".chatremove-overlay-dialog .chatremove .side-heading h2{margin:0;font-size:1rem;line-height:1.3}",
			"@media(hover:hover) and (pointer:fine){.chat-workspace .message-list:has(.message-menu) .message:not(:has(.message-menu)) .message-actions{opacity:0;pointer-events:none}}",
			".chat-workspace .reaction-row>.reaction.add{position:relative;flex:none;min-width:0;min-height:0;height:32px;padding:0 11px}",
		} {
			if !strings.Contains(Stylesheet, rule) {
				t.Errorf("the served stylesheet lacks %s", rule)
			}
		}
		// The standard dialog's own rules are what centre it and dim the page.
		for _, rule := range []string{".chat-dialog-backdrop{position:fixed;inset:0;", ".chat-dialog{position:static;margin:0;"} {
			if !strings.Contains(Stylesheet, rule) {
				t.Errorf("the standard dialog rule %s is gone; the moderation dialog relies on it", rule)
			}
		}
		// On a touch screen the add button is a reaction's size: the rule that
		// made it 44 px is still there for the others and is overridden after it.
		touch, mine := strings.Index(Stylesheet, ".message-action,.reaction.add,"), strings.Index(Stylesheet, ".chat-workspace .reaction-row>.reaction.add{")
		if touch < 0 || mine < touch {
			t.Errorf("the add-reaction size (at %d) does not come after the 44 px touch rule (at %d)", mine, touch)
		}
	})

	t.Run("on a phone More does not cover the message", func(t *testing.T) {
		// More is as tall as the author line and beside it; the author line
		// keeps its room free; a grouped message, which has no author line,
		// shows More in its empty gutter on a phone and keeps room at the end
		// of its text on a wider touch screen. A 44 px touch still reaches it.
		for _, rule := range []string{
			"@media(max-width:767px),(pointer:coarse){\n.chat-workspace .message-list .message>.message-content>.message-meta{padding-inline-end:44px}",
			// The rule that puts the bar inside the row on a phone, which these build on.
			"@media(max-width:767px),(pointer:coarse){.chat-workspace .message-list .message .message-actions{top:4px;transform:none}}",
			`.chat-workspace .message-list .message .message-actions .message-action[data-action="menu"]{position:relative;inline-size:36px;min-inline-size:36px;block-size:26px;min-block-size:26px}`,
			`.chat-workspace .message-list .message .message-actions .message-action[data-action="menu"]::after{content:"";position:absolute;inset:-9px -4px}`,
			".chat-workspace .message-list .message.continued .message-content{padding-inline-end:44px}",
			"@media(max-width:760px){\n.chat-workspace .message-list .message.continued .message-actions{inset-inline:10px auto}",
		} {
			if !strings.Contains(Stylesheet, rule) {
				t.Errorf("the served stylesheet lacks %s", rule)
			}
		}
		// The room is kept whether More is shown or not: no rule here depends on
		// hover, focus or the bar being drawn, so showing More moves no line.
		for _, state := range []string{":hover", ":focus", ":has("} {
			if strings.Contains(chatbug085PhoneMoreStyles, state) {
				t.Errorf("the phone rules change the row on %s", state)
			}
		}
		// These rules come after the ones that made More a 40 px box in the row.
		box, mine := strings.Index(Stylesheet, ".message-action{width:40px;height:40px}"), strings.Index(Stylesheet, chatbug085PhoneMoreStyles)
		if box < 0 || mine < box {
			t.Errorf("the phone More rules (at %d) do not come after the 40 px box (at %d)", mine, box)
		}
	})
}
