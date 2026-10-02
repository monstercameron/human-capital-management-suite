package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATUX_011 is the phone's composer and header in one test: the
// header is one row of a fixed height, the composer one line until it has focus
// or text and six at most, and its tool row is out of the page until focus.
// The same rules are checked piece by piece in TestTodo_CHATUX_011_Browser.
func TestTodo_CHATUX_011(t *testing.T) {
	if !strings.Contains(Stylesheet, `.chat-workspace .conversation-header{min-height:56px;height:56px;padding-block:0;box-sizing:border-box}`) {
		t.Error("the phone's conversation header is not one row of 56 px")
	}
	if !strings.Contains(ChatComposerToolsStyles, `.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown){flex-direction:row;align-items:center;gap:4px;padding:3px 6px}`) {
		t.Error("an empty, unfocused composer is not one row")
	}
	if !strings.Contains(ChatComposerToolsStyles, `.chat-workspace .chat-composer .composer-draft .composer-input{min-block-size:40px;max-block-size:calc(6*1.45*.9375rem + 18px)`) {
		t.Error("the field does not grow from one line to six")
	}
	if !strings.Contains(ChatComposerToolsStyles, `:not(:focus-within):has(.composer-input:placeholder-shown) :is(.composer-tools,.composer-help,.composer-format-row,.composer-embeds){display:none}`) {
		t.Error("the tool row is drawn before the composer has focus")
	}
}
