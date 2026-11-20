package chatui

import "testing"

// The text Copy text puts on the clipboard for a message is what the reader
// sees, not the Markdown source (#3718).
func TestIssue3718_ClipboardTextForMessageIsPlain(t *testing.T) {
	m := Model{Locale: "en-US"}
	msg := Message{ID: "p1", Body: "second **bold** message with `code` and https://example.com"}
	want := "second bold message with code and https://example.com"
	if got := Issue3718MessageClipboardText(m, msg); got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
}

// The English confirmation names the action the menu item is called. The
// German and Arabic entries live in productui's catalog and are pinned there.
func TestIssue3718_CopyConfirmationCopy(t *testing.T) {
	if got := EnglishCopy()[KeyCopyContentsSuccess]; got != "Text copied" {
		t.Fatalf("en-US confirmation = %q, want %q", got, "Text copied")
	}
}
