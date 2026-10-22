package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATLANG-005: a cited document in another language than the reader's is named
// with its language ("Paid time off policy, in English"). The language travels
// on the Sources line as a trailing marker the producer of the line writes; a
// line without one is shown as it was.
const (
	agentSourceLanguageOpen  = " <!--chat.agent.source.language:"
	agentSourceLanguageClose = "-->"
)

// agentSourceSplitLanguage removes the trailing language marker from a Sources
// line and returns the language it named, "" when there was none.
func agentSourceSplitLanguage(line string) (string, string) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasSuffix(trimmed, agentSourceLanguageClose) {
		return line, ""
	}
	start := strings.LastIndex(trimmed, agentSourceLanguageOpen)
	if start < 0 {
		return line, ""
	}
	tag := strings.TrimSuffix(trimmed[start+len(agentSourceLanguageOpen):], agentSourceLanguageClose)
	if len(tag) < 2 || len(tag) > 3 || strings.ContainsAny(tag, " <>\"") {
		return line, ""
	}
	return strings.TrimSpace(trimmed[:start]), strings.ToLower(tag)
}

// agentSourceLanguageNote is the words after a source's title ("in English"),
// empty for a source in the reader's language or in no known language.
func agentSourceLanguageNote(locale, language string) string {
	return strings.TrimPrefix(ChatlangSourceLabel(locale, "\x00", language), "\x00")
}

func agentSourceLanguageNodes(locale, language string) []ui.Node {
	note := agentSourceLanguageNote(locale, language)
	if note == "" {
		return nil
	}
	return []ui.Node{html.Span(html.Props{Class: "agent-reply-source-language", Data: map[string]string{"source-language": language}, Text: note})}
}
