package chat

// Chatcmd004PhraseIsTimed reports whether a typed date phrase names a moment
// (a full timestamp, "in an hour") and not a day, so the review choices show a
// timed reading with its time and zone (#3951).
func Chatcmd004PhraseIsTimed(phrase string) bool { return chatcmd004Timed(phrase) }
