package chatui

import "testing"

func TestInsertEmojiAtUTF16(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		emoji      string
		start, end int
		want       string
		caret      int
	}{
		{name: "inserts after astral character", text: "A😀B", emoji: "👍", start: 3, end: 3, want: "A😀👍B", caret: 5},
		{name: "replaces selection", text: "hello world", emoji: "🎉", start: 6, end: 11, want: "hello 🎉", caret: 8},
		{name: "clamps offsets", text: "abc", emoji: "❤️", start: 99, end: 120, want: "abc❤️", caret: 5},
		{name: "normalizes reversed selection", text: "abc", emoji: "🙂", start: 2, end: 1, want: "ab🙂c", caret: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, caret := insertEmojiAtUTF16(tt.text, tt.emoji, tt.start, tt.end)
			if got != tt.want || caret != tt.caret {
				t.Fatalf("insertEmojiAtUTF16() = (%q, %d), want (%q, %d)", got, caret, tt.want, tt.caret)
			}
		})
	}
}
