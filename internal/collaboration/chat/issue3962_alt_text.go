package chat

import "unicode/utf8"

// MaxImageDescriptionRunes bounds the description an author writes for an
// image attachment (issue 3962). It is long enough for a chart's summary plus
// the figures that matter, and short enough that a description stays a
// description: a longer explanation belongs in the message or a linked source.
const MaxImageDescriptionRunes = 1000

// validImageDescription checks the author's description of an attachment
// against what the media service says the file is. A description or the
// decorative mark is only meaningful on an image; a description and the mark
// together contradict each other; and the text is bounded in characters.
func validImageDescription(ref Reference, facts MediaFacts) bool {
	if ref.AltText == "" && !ref.Decorative {
		return true
	}
	if ref.AltText != "" && ref.Decorative {
		return false
	}
	if utf8.RuneCountInString(ref.AltText) > MaxImageDescriptionRunes {
		return false
	}
	return len(facts.ContentType) > 6 && facts.ContentType[:6] == "image/"
}
