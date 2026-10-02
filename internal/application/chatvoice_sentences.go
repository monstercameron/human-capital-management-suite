package application

import (
	"strings"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// VoiceSentences splits spoken text into the sentences Listen can mark while
// they are read. A sentence ends at ., !, ? (or their full-width and Arabic
// forms) that is followed by a space or the end of the text, so "3.5" and a
// host name are not cut.
func VoiceSentences(text string) []string {
	var out []string
	runes := []rune(strings.TrimSpace(text))
	start := 0
	for i, r := range runes {
		switch r {
		case '.', '!', '?', '。', '！', '？', '؟':
		default:
			continue
		}
		end := i + 1
		for end < len(runes) && strings.ContainsRune(".!?\"')”", runes[end]) {
			end++
		}
		if end < len(runes) && !unicode.IsSpace(runes[end]) {
			continue
		}
		if s := strings.TrimSpace(string(runes[start:end])); s != "" {
			out = append(out, s)
		}
		start = end
	}
	if s := strings.TrimSpace(string(runes[start:])); s != "" {
		out = append(out, s)
	}
	return out
}

// chatVoiceFixtureSentenceMS is how long the local development engine "speaks"
// one sentence: long enough to see the mark move on the page.
const chatVoiceFixtureSentenceMS = 900

// chatVoiceFixtureTimings lays the sentences end to end, one after another.
func chatVoiceFixtureTimings(text string) []agentmodel.SpeechTiming {
	sentences := VoiceSentences(text)
	if len(sentences) > 30 {
		sentences = sentences[:30]
	}
	out := make([]agentmodel.SpeechTiming, 0, len(sentences))
	for i, s := range sentences {
		out = append(out, agentmodel.SpeechTiming{Text: s, StartMS: int64(i) * chatVoiceFixtureSentenceMS, EndMS: int64(i+1) * chatVoiceFixtureSentenceMS})
	}
	return out
}
