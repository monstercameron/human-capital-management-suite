package chatui

// Issue3718MessageClipboardText is what Copy text puts on the clipboard for one
// message: the words as the reader sees them, without Markdown markup or the
// hidden card payload (#3718).
func Issue3718MessageClipboardText(m Model, msg Message) string {
	return Issue3718CopyText(Chatcmd002PlainBody(ReaderMessageBody(m, msg)))
}
