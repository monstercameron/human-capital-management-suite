package chatui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// AGENTUX-060: one answer named its document three times: as a label in front
// of the answer ("Paid time off policy: …"), in the sentence, and under
// Sources. The model is told not to write the label; an answer stored before
// that, or one that does it anyway, is shown without it. The stored answer is
// not changed.

// agentux060WithoutTitlePrefix removes a source's title standing at the very
// start of the answer as a label, followed by a colon or a dash. A title that
// is part of the first sentence ("Paid time off policy allows …") is left
// alone.
func agentux060WithoutTitlePrefix(body string, sources []agentReplySource) string {
	for _, source := range sources {
		title := agentSourceBaseTitle(source.Title)
		if utf8.RuneCountInString(title) < 3 {
			continue
		}
		leads := []string{title}
		if source.Href != "" {
			// The title may already have been made a link to its document.
			leads = append([]string{"[" + title + "](" + source.Href + ")"}, leads...)
		}
		for _, lead := range leads {
			rest, found := strings.CutPrefix(body, lead)
			if !found {
				continue
			}
			rest = strings.TrimLeft(rest, "  ")
			for _, mark := range []string{":", "—", "–", "-"} {
				after, marked := strings.CutPrefix(rest, mark)
				if !marked || after == "" || !unicode.IsSpace(rune(after[0])) {
					continue
				}
				if text := strings.TrimSpace(after); text != "" {
					return agentux060UpperFirst(text)
				}
			}
		}
	}
	return body
}

// agentux060UpperFirst starts the remaining sentence with a capital letter.
func agentux060UpperFirst(text string) string {
	first, size := utf8.DecodeRuneInString(text)
	if first == utf8.RuneError || !unicode.IsLower(first) {
		return text
	}
	return string(unicode.ToUpper(first)) + text[size:]
}
