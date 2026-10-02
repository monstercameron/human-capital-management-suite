package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATUX_023 checks the one measure of a wide conversation without a
// browser: the rules that set it exist, and the right edges they produce for the
// text, the day dividers and the composer are the same pixel.
func TestTodo_CHATUX_023(t *testing.T) {
	sheet := Stylesheet
	for _, want := range []string{
		// What the arithmetic below starts from.
		`.message-list{flex:1;min-height:0;overflow-y:auto;overscroll-behavior:contain;display:flex;flex-direction:column;padding:8px 20px`,
		`.message{position:relative;display:grid;grid-template-columns:36px minmax(0,1fr);column-gap:10px;padding:4px 8px;margin:10px -8px 0`,
		`.chat-composer{position:relative;flex:none;min-width:0;margin:4px 20px 20px`,
		// The measure and who uses it.
		`--chat-column:960px`,
		`.chat-workspace .message-list .message{max-width:calc(var(--chat-column) + 16px)}`,
		`.chat-workspace .message-list .day-divider,.chat-workspace .message-list .unread-divider{max-width:var(--chat-column)}`,
		`.chat-workspace .channel-tray{box-sizing:border-box;width:100%;max-width:calc(var(--chat-column) + 40px)}`,
		`.chat-workspace .chat-composer{box-sizing:border-box;width:min(calc(100% - 40px),var(--chat-column));margin-inline:20px auto}`,
		`.chat-workspace .message-body,.chat-workspace .agent-reply-answer{max-width:var(--chat-measure)}`,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("the stylesheet lacks %q", want)
		}
	}
	// A 1920 px window: the list's text starts at the list padding, the
	// message box is 16px wider than the column (8px padding and -8px margin a
	// side), and the composer is the column wide from the same left edge.
	const column, listPadding, messagePadding, messageMargin, composerMargin = 960, 20, 8, -8, 20
	messageBoxLeft := listPadding + messageMargin
	messageBoxRight := messageBoxLeft + column + 2*messagePadding
	textRight := messageBoxRight - messagePadding
	dividerRight := listPadding + column
	composerRight := composerMargin + column
	if textRight != composerRight || dividerRight != composerRight {
		t.Fatalf("right edges differ: text %d, day divider %d, composer %d", textRight, dividerRight, composerRight)
	}
	if listPadding != composerMargin {
		t.Fatalf("the left edges differ: %d and %d", listPadding, composerMargin)
	}
	// The text measure plus the avatar column and its gap fills the column.
	if !strings.Contains(sheet, `--chat-measure:calc(var(--chat-column) - 66px)`) || !strings.Contains(sheet, `grid-template-columns:56px minmax(0,1fr)`) {
		t.Fatal("the text column no longer ends where the composer ends")
	}
}

// TestTodo_CHATUX_023_Browser holds the narrow side of the rule: on a phone the
// composer is not the column wide but the window wide.
func TestTodo_CHATUX_023_Browser(t *testing.T) {
	if !strings.Contains(Stylesheet, `@media(max-width:767px){.chat-workspace .chat-composer{width:auto;margin-inline:12px}}`) {
		t.Fatal("a phone's composer must fill the window")
	}
}
