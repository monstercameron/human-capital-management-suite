package chatui

import (
	"regexp"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Old answer bodies stay immutable. Only their displayed version label changes.
func agentAnswerDisplayVersions(text string) string {
	return regexp.MustCompile(`(?i)\bversion ([0-9]+)\b`).ReplaceAllStringFunc(text, func(label string) string { return chat.DisplayVersionLabel(strings.ToLower(label)) })
}

func agentAnswerSourceUnavailable(locale string) string {
	switch {
	case strings.HasPrefix(locale, "de"):
		return "Sie können dieses Dokument nicht öffnen"
	case strings.HasPrefix(locale, "ar"):
		return "لا يمكنك فتح هذا المستند"
	default:
		return "You cannot open this document"
	}
}

func agentAnswerLinkLegacyCitation(body string, source agentReplySource) string {
	title := strings.TrimSpace(strings.SplitN(source.Title, " · ", 2)[0])
	title = regexp.MustCompile(` \(v[0-9]+\.[0-9]+\.[0-9]+(?:,.*)?\)$`).ReplaceAllString(title, "")
	pattern := regexp.MustCompile(`\(` + regexp.QuoteMeta(title) + `, v[0-9]+\.[0-9]+\.[0-9]+(?:, [^)]*)?\)`)
	return pattern.ReplaceAllStringFunc(body, func(string) string { return "[" + source.Title + "](" + source.Href + ")" })
}

func agentAnswerStopLabel(locale string) string {
	if strings.HasPrefix(locale, "de") {
		return "Stoppen"
	}
	if strings.HasPrefix(locale, "ar") {
		return "أوقف"
	}
	return "Stop"
}
