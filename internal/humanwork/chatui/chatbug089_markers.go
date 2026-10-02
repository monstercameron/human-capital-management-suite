package chatui

import (
	"regexp"
	"strings"
)

// CHATBUG-089: the writers of an agent answer leave comments in the stored body
// for the system to read: the readable flag after a source line
// (chat.agent.source.readable:true) and the reason an answer was delivered
// privately (chat.agent.private:asked). A reader never sees either. This is the
// one function that removes them, and every surface that draws a stored body or
// a stretch of one goes through it: a message, a thread parent and its replies,
// a card, an excerpt, a search row, a saved row, a pinned preview and a quote.
var (
	// chatInternalMarker is a whole comment of the kinds above, with the blanks
	// that set it apart from the line it follows.
	chatInternalMarker = regexp.MustCompile(`[ \t]*<!--\s*chat\.agent\.[A-Za-z0-9_.]+:[^>]*-->`)
	// chatInternalMarkerCut is one an excerpt cut short before its closing: the
	// rest of its line has no ">" in it.
	chatInternalMarkerCut = regexp.MustCompile(`(?m)[ \t]*<!--\s*chat\.agent\.[^\n>]*$`)
)

// chatStripInternalMarkers removes the internal comments from text. Text with
// none of them is returned as it is, so a person's own words are never altered.
func chatStripInternalMarkers(text string) string {
	if !strings.Contains(text, "<!--") {
		return text
	}
	text = chatInternalMarker.ReplaceAllString(text, "")
	return chatInternalMarkerCut.ReplaceAllString(text, "")
}
