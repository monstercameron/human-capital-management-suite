package chatui

import (
	"encoding/json"
	"regexp"
	"strings"
)

// CHATBUG-021 and CHATBUG-007: every surface that shows a stretch of a message
// (a search result, a saved row, a to-do source, a moderation list) gets it
// through chatDisplayBody or chatExcerptText, never from the stored body.
//
//   - chatDisplayBody is the text of a post as a person reads it: an announcement
//     is its sentence, whole or cut off by the service that gave the excerpt, and
//     the context token, share address and readable flags of an agent answer are
//     gone. It changes nothing else, so a person's own words are never altered.
//   - chatExcerptText is that text for a stretch cut out of the source: a link
//     the cut left half of is dropped back to its words, and a complete link is
//     its label, because an excerpt is read as plain text.

var (
	announcementTagPrefix = strings.TrimSpace(AgentAnnouncementBodyPrefix)
	// announcementCutText finds the Text field of an envelope whose string was
	// cut off before its closing quote.
	announcementCutText = regexp.MustCompile(`"Text"\s*:\s*"((?:[^"\\]|\\.)*)$`)
	// excerptLinkLabel is a complete Markdown link or image.
	excerptLinkLabel = regexp.MustCompile(`!?\[([^\[\]]*)\]\([^)]*\)`)
)

// chatDisplayBody is the text of a post for a surface that is not the post itself.
func chatDisplayBody(text string) string {
	if strings.HasPrefix(strings.TrimSpace(text), announcementTagPrefix) {
		return chatDisplayText(text)
	}
	cleaned := agentAnswerStripInternal(text)
	cleaned = agentInternalPartial.ReplaceAllString(cleaned, "")
	cleaned = agentShareAddress.ReplaceAllString(cleaned, "")
	cleaned = agentQuestionFragment.ReplaceAllString(cleaned, "")
	if cleaned == strings.TrimSpace(text) {
		return text
	}
	return strings.TrimSpace(cleaned)
}

// chatAnnouncementSentence is the sentence of an announcement envelope: whole,
// or cut off by the service that gave the excerpt (the closing brace, the later
// fields, even the end of the sentence). ok is false when text is no envelope.
func chatAnnouncementSentence(text string) (sentence string, ok bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, announcementTagPrefix) {
		return "", false
	}
	body := strings.TrimSpace(strings.TrimPrefix(trimmed, announcementTagPrefix))
	if announcement, decoded := DecodeAnnouncementMessageBody(AgentAnnouncementBodyPrefix + body); decoded {
		return announcement.Text, true
	}
	if match := agentAnnouncementText.FindStringSubmatch(body); len(match) == 2 {
		if json.Unmarshal([]byte(match[1]), &sentence) == nil {
			return sentence, true
		}
	}
	if match := announcementCutText.FindStringSubmatch(body); len(match) == 2 {
		// The cut may fall inside an escape: take the whole escapes before it.
		cut := strings.TrimSuffix(match[1], `\`)
		if json.Unmarshal([]byte(`"`+cut+`"`), &sentence) == nil {
			return sentence + "…", true
		}
	}
	return "", true
}

// chatExcerptText is a stretch of a post as plain text.
func chatExcerptText(text string) string {
	text = chatDropLinkFragments(chatDisplayBody(text))
	return excerptLinkLabel.ReplaceAllString(text, "$1")
}

// chatDropLinkFragments removes what a cut leaves of a Markdown link: the end of
// a link whose start was cut away ("…policy](/workspace/app/docs?document=x)"),
// the start of one whose end was cut away ("[2026 holiday guide](/workspace/ap")
// and a bracket that was never closed. The words stay.
func chatDropLinkFragments(text string) string {
	runes := []rune(text)
	drop := make([]bool, len(runes))
	dropFrom := func(from, to int) {
		for index := from; index <= to && index < len(runes); index++ {
			drop[index] = true
		}
	}
	// closeAt is where the parenthesis that opens at runes[from] closes, or -1.
	closeAt := func(from int) int {
		for index := from; index < len(runes); index++ {
			if runes[index] == ')' {
				return index
			}
		}
		return -1
	}
	var open []int
scan:
	for index := 0; index < len(runes); index++ {
		switch runes[index] {
		case '[':
			open = append(open, index)
		case ']':
			linked := index+1 < len(runes) && runes[index+1] == '('
			if len(open) == 0 {
				// The start of the link was cut away: drop the bracket and its address.
				drop[index] = true
				if linked {
					if end := closeAt(index + 2); end >= 0 {
						dropFrom(index+1, end)
						index = end
					} else {
						dropFrom(index+1, len(runes))
						break scan
					}
				}
				continue
			}
			start := open[len(open)-1]
			open = open[:len(open)-1]
			if linked && closeAt(index+2) < 0 {
				// The end of the link was cut away: keep its words.
				drop[start] = true
				dropFrom(index, len(runes))
				break scan
			}
		}
	}
	for _, start := range open {
		drop[start] = true
	}
	var out strings.Builder
	for index, r := range runes {
		if !drop[index] {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// chatSearchRowBody is the text a search result draws as the message it is. A
// whole announcement is left for the message renderer to draw as the agent's
// message; one the service cut short, or whose line break it flattened, is its
// sentence; and a stretch the service marked as cut ("…" at either end) loses
// the half links the cut left.
func chatSearchRowBody(text string) string {
	if _, whole := DecodeAnnouncementMessageBody(text); whole {
		return text
	}
	if strings.HasPrefix(strings.TrimSpace(text), announcementTagPrefix) {
		return chatDisplayBody(text)
	}
	trimmed := strings.TrimSpace(text)
	for _, mark := range []string{"…", "..."} {
		if strings.HasPrefix(trimmed, mark) || strings.HasSuffix(trimmed, mark) {
			return chatDropLinkFragments(text)
		}
	}
	return text
}
