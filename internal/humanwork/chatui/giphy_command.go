package chatui

import (
	"strings"
	"unicode"
)

// giphyCommand reads a Slack-style "/giphy <search>" draft. The command is
// consumed by the composer, never posted: it opens the GIF picker searching
// for the rest of the line. "/giphy" alone opens the trending GIFs.
func giphyCommand(body string) (string, bool) {
	body = strings.TrimSpace(body)
	const prefix = "/giphy"
	if len(body) < len(prefix) || !strings.EqualFold(body[:len(prefix)], prefix) {
		return "", false
	}
	rest := body[len(prefix):]
	if rest != "" && !unicode.IsSpace(rune(rest[0])) {
		return "", false
	}
	return LimitGiphyQuery(strings.TrimSpace(rest)), true
}
