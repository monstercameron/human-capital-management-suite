package chatui

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Display-time cleaning of answers that are already stored. A stored agent
// answer carries bytes that are for the system, not for the reader: the token
// that restores "You asked in #general", the share address behind that block,
// the model's own trailing Sources list, and the readable-flag comment. None of
// it is ever printed; the stored bodies are not changed.

var (
	agentInternalLink      = regexp.MustCompile(`\[chat-agent-question(?:-context)?:[^\]]*\]\([^)]*\)`)
	agentInternalPartial   = regexp.MustCompile(`\[?chat-agent-question(?:-context)?:[A-Za-z0-9_=\-]*`)
	agentShareMarkdown     = regexp.MustCompile(`\[[^\]]*\]\(\s*[^)\s]*/chat/share/[^)]*\)`)
	agentShareAddress      = regexp.MustCompile(`\]?\(?(?:https?://[^\s/()]+)?/chat/share/[^\s)\]]*\)?`)
	agentQuestionFragment  = regexp.MustCompile(`#hcm-question=[A-Za-z0-9_=\-]*`)
	agentSourceFlag        = regexp.MustCompile(`[ \t]*<!--chat\.agent\.source\.readable:(?:true|false)-->`)
	agentSourcesTail       = regexp.MustCompile(`\n\nSources\n(?:[ \t]*- [^\n]*(?:\n|$))+\s*$`)
	agentBlankRun          = regexp.MustCompile(`\n{3,}`)
	agentMarkdownLink      = regexp.MustCompile(`\[[^\]]*\]\([^)]*\)`)
	agentMarkdownLinkParts = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)
	agentTitleVersion      = regexp.MustCompile(` \((?:v[0-9]+\.[0-9]+\.[0-9]+|version [0-9]+)(?:,.*)?\)$`)
	agentAnnouncementText  = regexp.MustCompile(`"Text"\s*:\s*("(?:[^"\\]|\\.)*")`)
)

// agentAnswerStripInternal removes the context token link, any other link whose
// target is a share address, and the readable-flag comments from an answer.
func agentAnswerStripInternal(body string) string {
	body = agentInternalLink.ReplaceAllString(body, "")
	body = agentShareMarkdown.ReplaceAllString(body, "")
	body = agentSourceFlag.ReplaceAllString(body, "")
	return strings.TrimSpace(agentBlankRun.ReplaceAllString(body, "\n\n"))
}

// agentAnswerPresentBody is the one shape an answer is shown in: its statement
// once, with no internal text, no trailing Sources list when the structured
// sources are shown in their own row, version labels in the display form, and
// each readable source's title a link inside the sentence.
func agentAnswerPresentBody(body string, sources []agentReplySource) string {
	body = agentAnswerStripInternal(body)
	if len(sources) > 0 {
		body = strings.TrimSpace(agentSourcesTail.ReplaceAllString(body, ""))
	}
	body = agentAnswerDisplayVersions(body)
	for _, source := range sources {
		if source.Href == "" {
			continue
		}
		body = agentAnswerLinkLegacyCitation(body, source)
	}
	return agentAnswerLinkTitles(body, sources)
}

func agentSourceBaseTitle(title string) string {
	title = strings.TrimSpace(strings.SplitN(title, " · ", 2)[0])
	return strings.TrimSpace(agentTitleVersion.ReplaceAllString(title, ""))
}

// agentAnswerLinkTitles makes the document title inside the answer's own
// sentence a link, for a source the reader may open. Text already inside a link
// is left as it is.
func agentAnswerLinkTitles(body string, sources []agentReplySource) string {
	for _, source := range sources {
		title := agentSourceBaseTitle(source.Title)
		if source.Href == "" || !source.Readable || len([]rune(title)) < 3 || strings.ContainsAny(title, "[]") {
			continue
		}
		link := "[" + title + "](" + source.Href + ")"
		var out strings.Builder
		last := 0
		for _, span := range agentMarkdownLink.FindAllStringIndex(body, -1) {
			out.WriteString(strings.ReplaceAll(body[last:span[0]], title, link))
			out.WriteString(body[span[0]:span[1]])
			last = span[1]
		}
		out.WriteString(strings.ReplaceAll(body[last:], title, link))
		body = out.String()
	}
	return body
}

// chatDisplayText is the text of a message as a result list or a saved row may
// show it: an announcement shows its sentence, an answer shows its statement,
// and no context token, share address, markup tag or data field is printed.
func chatDisplayText(text string) string {
	if strings.HasPrefix(text, AgentAnnouncementBodyPrefix) {
		if announcement, ok := DecodeAnnouncementMessageBody(text); ok {
			return chatDisplayText(announcement.Text)
		}
		if match := agentAnnouncementText.FindStringSubmatch(text); len(match) == 2 {
			var sentence string
			if json.Unmarshal([]byte(match[1]), &sentence) == nil {
				return chatDisplayText(sentence)
			}
		}
		return ""
	}
	text = agentAnswerStripInternal(text)
	text = agentInternalPartial.ReplaceAllString(text, "")
	text = agentShareAddress.ReplaceAllString(text, "")
	text = agentQuestionFragment.ReplaceAllString(text, "")
	text = strings.TrimSpace(agentSourcesTail.ReplaceAllString(strings.TrimSpace(text), ""))
	return strings.TrimSpace(agentBlankRun.ReplaceAllString(text, "\n\n"))
}

// agentAnswerPreviewBody is the answer text the embed and preview scanners see.
// A document that has its own row in Sources is a reference there, so its link
// in the sentence does not also open a preview card; a link to any other
// document in the answer is left for the scanners as it is.
func agentAnswerPreviewBody(body string, sources []agentReplySource) string {
	hrefs := make(map[string]bool, len(sources))
	for _, source := range sources {
		if source.Href != "" {
			hrefs[source.Href] = true
		}
	}
	if len(hrefs) == 0 {
		return body
	}
	return agentMarkdownLinkParts.ReplaceAllStringFunc(body, func(markup string) string {
		parts := agentMarkdownLinkParts.FindStringSubmatch(markup)
		if hrefs[parts[2]] {
			return parts[1]
		}
		return markup
	})
}
