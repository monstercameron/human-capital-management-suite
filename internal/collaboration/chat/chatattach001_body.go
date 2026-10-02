package chat

import "strings"

// CHATATTACH-001: a message can be its files. A person who drops a screenshot
// into the composer and presses Send has said what they meant to say, and the
// service used to refuse it for having no text. A post still has to say
// something: text, or at least one attached file. The files are validated as
// they always were (validateMediaReference), so an empty body with a
// reference that is not an admitted file of this conversation is refused
// there, and a mention with no text is still not a message.

// postSaysNothing reports whether a new post has neither text nor a file.
func postSaysNothing(body string, references []Reference) bool {
	if strings.TrimSpace(body) != "" {
		return false
	}
	for _, ref := range references {
		if ref.Kind == MediaAttachment {
			return false
		}
	}
	return true
}
