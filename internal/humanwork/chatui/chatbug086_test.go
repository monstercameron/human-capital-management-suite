package chatui

import (
	"os"
	"strings"
	"testing"
	"unicode/utf16"

	xhtml "golang.org/x/net/html"
)

// chatbug086Children is the composer form's direct children, each as its tag
// and class: the order the reconciler matches one render against the next.
func chatbug086Children(t *testing.T, form string, class string) []string {
	t.Helper()
	forms := chatPolishNodes(t, form, func(n *xhtml.Node) bool { return n.Data == "form" && chatPolishHasClass(n, class) })
	if len(forms) != 1 {
		t.Fatalf("want one form.%s, got %d", class, len(forms))
	}
	var children []string
	for c := forms[0].FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode {
			children = append(children, c.Data+"."+strings.Fields(chatPolishAttr(c, "class") + " -")[0])
		}
	}
	return children
}

// TestTodo_CHATBUG_086 holds the three parts of the fix that do not need a
// page: the caret's place after an emoji is accepted and the text area staying
// the same element when the list closes; which presses focus the text area;
// and which input events may open the list.
func TestTodo_CHATBUG_086(t *testing.T) {
	t.Run("the emoji replaces the typed text and the caret follows it", func(t *testing.T) {
		const draft = "nice :thum and more"
		caret := len(utf16.Encode([]rune("nice :thum")))
		query, start, found := emojiCompletionToken(draft, caret)
		if !found || query != "thum" || start != 5 {
			t.Fatalf("token = %q at %d (%v), want thum at 5", query, start, found)
		}
		updated, next := insertEmojiAtUTF16(draft, "👍 ", start, caret)
		if updated != "nice 👍  and more" {
			t.Fatalf("the draft reads %q", updated)
		}
		// 👍 is two UTF-16 units; the caret is after it and its space.
		if next != 8 || string(utf16.Decode(utf16.Encode([]rune(updated))[:next])) != "nice 👍 " {
			t.Fatalf("the caret is at %d, want directly after the emoji and its space", next)
		}
		// Typing on goes in at the caret.
		typed, after := insertEmojiAtUTF16(updated, "work", next, next)
		if typed != "nice 👍 work and more" || after != 12 {
			t.Fatalf("typing after the emoji gave %q with the caret at %d", typed, after)
		}
	})

	t.Run("the list closing does not move the text area", func(t *testing.T) {
		m := emojiModel("en-US")
		m.State, m.SelectedID, m.CurrentUser, m.CurrentTenantID = StateReady, "room", "walt", "tenant"
		m.Conversations = []Conversation{{ID: "room", Name: "general", Kind: PublicChannel, Joined: true}}
		m.Callbacks.SendMessage = func(string, string) {}
		m.Callbacks.ReplyInThread = func(string, string) {}
		m.Messages = []Message{{ID: "p1", AuthorID: "walt", Author: "Walt Brennan", Body: "Parent"}}
		m.ShowThread, m.ThreadParentID = true, "p1"
		withEmojiHost(t, m, func(*localUI) {
			for target, form := range map[string]struct {
				class  string
				render func(h handlers) string
			}{
				"chat-composer":   {"chat-composer", func(h handlers) string { return renderNode(t, composer(m, h)) }},
				"thread-composer": {"thread-composer", func(h handlers) string { return renderNode(t, threadPane(m, h)) }},
			} {
				open := handlers{local: localUI{emojiCompletion: emojiCompletion{Target: target, Query: "fire", Start: 0, End: 5, Open: true, Active: 0}}}
				shut := handlers{local: localUI{emojiCompletion: emojiCompletion{Active: -1}}}
				opened, closed := form.render(open), form.render(shut)
				if !strings.Contains(opened, `id="`+target+`-emoji-completion"`) || strings.Contains(closed, `id="`+target+`-emoji-completion"`) {
					t.Fatalf("%s: the list is not drawn only while it is open", target)
				}
				before, after := chatbug086Children(t, opened, form.class), chatbug086Children(t, closed, form.class)
				if len(before) != len(after) {
					t.Fatalf("%s: the form has %d children with the list open and %d with it closed, so every child after the list moves:\n open %v\n shut %v", target, len(before), len(after), before, after)
				}
				for i := range before {
					if before[i] != after[i] && !strings.Contains(before[i], "emoji-completion") && !strings.HasPrefix(before[i], "div.mention-menu") {
						t.Errorf("%s: child %d is %s with the list open and %s with it closed", target, i, before[i], after[i])
					}
				}
			}
		})
	})

	t.Run("a press in the composer that is on no control focuses the text area", func(t *testing.T) {
		for _, tc := range []struct {
			name                                                        string
			inComposer, onControl, selecting, fieldUsable, fieldFocused bool
			want                                                        bool
		}{
			{"the composer's padding", true, false, false, true, false, true},
			{"a button of the tool row", true, true, false, true, false, false},
			{"the text area itself", true, true, false, true, true, false},
			{"padding while the box already has the caret", true, false, false, true, true, false},
			{"the end of selecting a preview's text", true, false, true, true, false, false},
			{"a composer that cannot take text", true, false, false, false, false, false},
			{"anywhere else on the page", false, false, false, true, false, false},
		} {
			if got := chatbug086FocusesField(tc.inComposer, tc.onControl, tc.selecting, tc.fieldUsable, tc.fieldFocused); got != tc.want {
				t.Errorf("%s: focuses=%v, want %v", tc.name, got, tc.want)
			}
		}
		// The page's main region is focusable and holds the composer: a press
		// under the text area finds it as the nearest "control", and it must
		// not count, or no press in a composer ever focuses the text area.
		for _, tc := range []struct {
			name          string
			found, inside bool
			want          bool
		}{
			{"a button of the tool row", true, true, true},
			{"the hint row under the text area, whose nearest control is the page's main region", true, false, false},
			{"nothing focusable above the press", false, false, false},
		} {
			if got := chatbug086OnControl(tc.found, tc.inside); got != tc.want {
				t.Errorf("%s: on a control=%v, want %v", tc.name, got, tc.want)
			}
		}
		source, err := os.ReadFile("chatbug086_focus_js.go")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(source), `chatbug086OnControl(control.Truthy(), control.Truthy() && !control.Equal(form) && form.Call("contains", control).Bool())`) {
			t.Error("the press handler counts a control outside the composer (the page's focusable main region) as the composer's own")
		}
		// The selectors name both composers and the controls a composer holds.
		m := emojiModel("en-US")
		m.State, m.SelectedID, m.CurrentUser, m.CurrentTenantID = StateReady, "room", "walt", "tenant"
		m.Conversations = []Conversation{{ID: "room", Name: "general", Kind: PublicChannel, Joined: true}}
		m.Callbacks.SendMessage = func(string, string) {}
		form := renderNode(t, composer(m, handlers{}))
		if !strings.Contains(form, `<form`) || !strings.Contains(form, `class="chat-composer"`) || !strings.Contains(form, `class="composer-input"`) || !strings.Contains(form, "<textarea") {
			t.Fatalf("the composer is no longer form.chat-composer with textarea.composer-input, which the press handler looks for: %s", form)
		}
		for _, selector := range []string{".chat-composer", ".thread-composer"} {
			if !strings.Contains(chatbug086Composers, selector) {
				t.Errorf("the press handler does not know %s", selector)
			}
		}
		for _, control := range []string{"button", "a[href]", "textarea", "input", "label", `[role="option"]`, `[role="dialog"]`} {
			if !strings.Contains(chatbug086Controls, control) {
				t.Errorf("a press on %s would take the caret away from it", control)
			}
		}
	})

	t.Run("only the person's typing opens the list", func(t *testing.T) {
		for _, tc := range []struct {
			name             string
			trusted, focused bool
			want             bool
		}{
			{"typing in the box", true, true, true},
			{"a draft restored on load, before the box has focus", false, false, false},
			{"a draft the page wrote into a focused box", false, true, false},
			{"an event for a box that does not hold the caret", true, false, false},
		} {
			if got := chatbug086OpensList(tc.trusted, tc.focused); got != tc.want {
				t.Errorf("%s: opens=%v, want %v", tc.name, got, tc.want)
			}
		}
	})
}
