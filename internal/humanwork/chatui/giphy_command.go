package chatui

import "strings"

// giphyCommand reads a Slack-style "/giphy <search>" draft. The command is
// consumed by the composer, never posted: it opens the GIF picker searching
// for the rest of the line. "/giphy" alone opens the trending GIFs. The line is
// read by the command registry's parser (composer_commands.go), so this is not
// a second comparison of the command's name.
func giphyCommand(body string) (string, bool) {
	name, args, ok := parseComposerCommand(body)
	if !ok || !strings.EqualFold(name, "giphy") {
		return "", false
	}
	return LimitGiphyQuery(args), true
}
