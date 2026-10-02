package application

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var (
	statementMarkdownLink = regexp.MustCompile(`!?\[[^\]]*\]\([^)]*\)`)
	statementVersionToken = regexp.MustCompile(`(?i)\b(?:version\s+[0-9]+|v[0-9]+(?:\.[0-9]+){0,2})\b`)
)

// personaReplyHasStatement reports whether a reply says anything. Once the
// citation markers, links, the Sources list, version labels and the document
// and section titles are taken away, something with a letter or a digit must
// remain. A reply that is only a title, a link to the document or a citation
// marker names where an answer would be found and answers nothing.
func personaReplyHasStatement(text string, titles ...string) bool {
	if cut := strings.LastIndex(text, "\nSources\n"); cut >= 0 {
		text = text[:cut]
	}
	text = personaQualityCitationMarker.ReplaceAllString(text, " ")
	text = statementMarkdownLink.ReplaceAllString(text, " ")
	ordered := make([]string, 0, len(titles))
	for _, title := range titles {
		if title = strings.TrimSpace(title); title != "" {
			ordered = append(ordered, title)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	for _, title := range ordered {
		quoted := regexp.QuoteMeta(title)
		// A title with its version in brackets, as older answers cited it.
		text = regexp.MustCompile(`(?i)\(\s*`+quoted+`\s*(?:,[^)]*)?\)`).ReplaceAllString(text, " ")
		text = regexp.MustCompile(`(?i)`+quoted).ReplaceAllString(text, " ")
	}
	text = statementVersionToken.ReplaceAllString(text, " ")
	for _, char := range text {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			return true
		}
	}
	return false
}

// personaReplyStatementTitles lists the document and section titles of the
// documents an answer was searched from.
func personaReplyStatementTitles(searched []personaQualitySearchedDocument) []string {
	titles := make([]string, 0, 2*len(searched))
	for _, document := range searched {
		titles = append(titles, document.Title, document.SectionTitle)
	}
	return titles
}
