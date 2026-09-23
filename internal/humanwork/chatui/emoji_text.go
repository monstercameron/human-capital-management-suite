package chatui

import "unicode/utf16"

// insertEmojiAtUTF16 replaces the selected UTF-16 code-unit range in text,
// matching the offsets exposed by an HTML textarea's selectionStart/End.
// Emoji can occupy multiple UTF-8 bytes and UTF-16 code units, so byte slicing
// directly with browser offsets would corrupt drafts containing non-ASCII text.
func insertEmojiAtUTF16(text, emoji string, start, end int) (string, int) {
	units := utf16.Encode([]rune(text))
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if start > len(units) {
		start = len(units)
	}
	if end > len(units) {
		end = len(units)
	}
	inserted := utf16.Encode([]rune(emoji))
	result := make([]uint16, 0, len(units)-(end-start)+len(inserted))
	result = append(result, units[:start]...)
	result = append(result, inserted...)
	result = append(result, units[end:]...)
	return string(utf16.Decode(result)), start + len(inserted)
}
