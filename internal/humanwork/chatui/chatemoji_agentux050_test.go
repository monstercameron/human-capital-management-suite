package chatui

import (
	"strings"
	"testing"
	"unicode/utf16"
)

func emojiEnd(value string) int { return len(utf16.Encode([]rune(value))) }

// AGENTUX-050, the emoji half: typing "explain our PTO policy in detail: accrual"
// opened emoji completion at ": acc" and Enter no longer sent the message. The
// list opens only for a colon at the start of a word followed by letters, never
// for a colon inside a word, after a URL scheme or before a space, never with the
// caret inside a word, and choosing an emoji changes nothing but the typed ":query".
func TestTodo_AGENTUX_050_EmojiCompletion(t *testing.T) {
	t.Run("a sentence being typed never opens the list", func(t *testing.T) {
		sentence := "explain our PTO policy in detail: accrual"
		for caret := 0; caret <= emojiEnd(sentence); caret++ {
			if q, _, ok := emojiCompletionToken(sentence, caret); ok {
				t.Fatalf("caret %d of %q opened the list with %q", caret, sentence, q)
			}
		}
		for _, value := range []string{
			"Note: ok", "Note:smile", "word:fire", "a:fire", "x:smile:", "10:30", "10:30pm", "at 10:30:45 sharp",
			"http:fire", "https:smile", "mailto:fire", "see http://example.com/a:smile", "https://example.com:8080/fire",
			"ftp:", "Hinweis:Feuer", ":fire ", ":f", ":", "fire:", "😀:fire", "a ::fire",
		} {
			if q, _, ok := emojiCompletionToken(value, emojiEnd(value)); ok {
				t.Errorf("%q opened the list with %q", value, q)
			}
		}
	})

	t.Run("a colon at the start of a word does open it, and filters as the word grows", func(t *testing.T) {
		for _, tc := range []struct{ value, query string }{
			{":smile", "smile"}, {":sm", "sm"}, {"I am :smile", "smile"}, {"line one\n:smile", "smile"},
			{"( :smile", "smile"}, {"ja :feuer", "feuer"}, {":نار", "نار"}, {":thumbs_up", "thumbs_up"},
		} {
			q, start, ok := emojiCompletionToken(tc.value, emojiEnd(tc.value))
			if !ok || q != tc.query {
				t.Errorf("%q gave %q (open %v), want %q", tc.value, q, ok, tc.query)
			}
			if ok && !strings.HasPrefix(string(utf16.Decode(utf16.Encode([]rune(tc.value))[start:])), ":") {
				t.Errorf("%q: the replaced range does not start at the colon (start %d)", tc.value, start)
			}
		}
		m := emojiModel("en-US")
		withEmojiHost(t, m, func(*localUI) {
			for _, tc := range []struct{ query, first string }{{"smile", "smiling"}, {"smi", ""}} {
				items := emojiCompletionItems(tc.query)
				if len(items) == 0 {
					t.Fatalf(":%s offers nothing", tc.query)
				}
				if tc.first != "" && !strings.Contains(items[0].label, tc.first) {
					t.Errorf(":%s leads with %q, want a %s emoji", tc.query, items[0].label, tc.first)
				}
			}
		})
	})

	t.Run("a caret inside a word does not open it", func(t *testing.T) {
		value := "hello :fire"
		if _, _, ok := emojiCompletionToken(value, emojiEnd("hello :fi")); ok {
			t.Error(":fi|re opened the list, and choosing would cut the word in two")
		}
		if _, _, ok := emojiCompletionToken(value, emojiEnd(value)); !ok {
			t.Error("the caret at the end of :fire did not open the list")
		}
		if _, _, ok := emojiCompletionToken("hello :fire and more", emojiEnd(value)); !ok {
			t.Error("the caret at the end of :fire before a space did not open the list")
		}
	})

	t.Run("Enter sends unless a match is highlighted", func(t *testing.T) {
		for _, draft := range []string{"explain our PTO policy in detail: accrual", "see http://x.test", "10:30"} {
			state := composerKeyState{Draft: draft}
			if got := composerKeyAction(state, "Enter"); got != composerSend {
				t.Errorf("Enter on %q = %s, want send", draft, got)
			}
			state.EmojiOpen = true // a list that is open with nothing highlighted must not take the key
			if got := composerKeyAction(state, "Enter"); got != composerSend {
				t.Errorf("Enter on %q with an unhighlighted emoji list = %s, want send", draft, got)
			}
		}
		if got := composerKeyAction(composerKeyState{Draft: ":fire", EmojiOpen: true, EmojiHighlighted: true}, "Enter"); got != composerPickEmoji {
			t.Errorf("Enter with a highlighted match = %s, want the emoji picked", got)
		}
	})

	t.Run("choosing an emoji replaces only the typed query", func(t *testing.T) {
		for _, tc := range []struct{ value, typed, want string }{
			{"I said hello :smile and left", "I said hello :smile", "I said hello 😀 and left"},
			{"I said hello :smile", "I said hello :smile", "I said hello 😀 "},
			{":smile\nnext line", ":smile", "😀\nnext line"},
			{"Q: what is :smile?", "Q: what is :smile", "Q: what is 😀 ?"},
		} {
			caret := emojiEnd(tc.typed)
			q, start, ok := emojiCompletionToken(tc.value, caret)
			if !ok || q != "smile" {
				t.Fatalf("%q did not open the list: %q %v", tc.value, q, ok)
			}
			got, next := insertEmojiAtUTF16(tc.value, emojiCompletionInsert(tc.value, caret, "😀"), start, caret)
			if tc.want != got {
				t.Errorf("%q became %q, want %q", tc.value, got, tc.want)
			}
			if next != emojiEnd(got[:strings.Index(got, "😀")+len("😀")]) && next != emojiEnd(got[:strings.Index(got, "😀")+len("😀")])+1 {
				t.Errorf("%q: the caret is at %d, not just after the emoji", tc.value, next)
			}
		}
	})
}

// The picker opened from a message never takes the conversation list's room.
func TestTodo_AGENTUX_050_EmojiPickerStaysOffTheSidebar(t *testing.T) {
	const vw, vh = 1440.0, 900.0
	left := chatEmojiColumn(chatLayerRect{0, 0, 280, vh}, true, vw, vh)
	if left != (chatLayerRect{280, 0, vw, vh}) {
		t.Fatalf("with the list on the left, the room is %+v", left)
	}
	right := chatEmojiColumn(chatLayerRect{1160, 0, vw, vh}, true, vw, vh)
	if right != (chatLayerRect{0, 0, 1160, vh}) {
		t.Fatalf("with the list on the right (Arabic), the room is %+v", right)
	}
	if none := chatEmojiColumn(chatLayerRect{}, false, vw, vh); none != (chatLayerRect{0, 0, vw, vh}) {
		t.Fatalf("with no list the room is %+v", none)
	}
	// An action bar at the left edge of the conversation: the picker aligned to
	// its end would reach into the list, and is held at the list's edge instead.
	bar := chatLayerRect{300, 200, 460, 230}
	if got := chatEmojiPlace(bar, left, vw, vh, false, true); got.Left < 280 {
		t.Errorf("the picker starts at %v, over the conversation list", got.Left)
	}
	rtlBar := chatLayerRect{980, 200, 1140, 230}
	if got := chatEmojiPlace(rtlBar, right, vw, vh, true, true); got.Left+got.Width > 1160 {
		t.Errorf("the picker ends at %v, over the conversation list", got.Left+got.Width)
	}
	// From the composer: the button at either end of the composer, picker inside it.
	column := chatLayerRect{360, 80, 1420, 880}
	for _, button := range []chatLayerRect{{366, 836, 394, 866}, {1380, 836, 1408, 866}} {
		got := chatEmojiPlace(button, column, vw, vh, false, false)
		if got.Left < column.left || got.Left+got.Width > column.right {
			t.Errorf("button %+v: the picker leaves the conversation column: %+v", button, got)
		}
		if got.Top+got.Height > button.top {
			t.Errorf("button %+v: the picker covers its button: %+v", button, got)
		}
	}
}
