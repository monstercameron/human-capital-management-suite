package application

import (
	"fmt"
	"strings"
)

// CHATBUG-049: asked for a list, an agent answered with one document title and
// nothing else. A reply that only names a document says nothing (CHATBUG-018),
// so when the run did search documents the server says what the agent can read
// in one sentence of its own, naming the documents the search returned. The
// sentence is the server's, in English, and states only what the search found;
// the documents it names are the ones the sealed answer then cites. A run with
// no searched documents has nothing to say and is refused as before.
const personaComposedListLimit = 10

// personaComposedListReply is that sentence, or "" when the search returned no
// document whose title can be named plainly.
func personaComposedListReply(searched []personaQualitySearchedDocument) string {
	seen := make(map[string]bool, len(searched))
	titles := make([]string, 0, len(searched))
	for _, document := range searched {
		title := strings.Join(strings.Fields(document.Title), " ")
		key := document.DocumentID
		if key == "" {
			key = title
		}
		// A title with brackets would read as a link or a marker to the reply checks.
		if title == "" || seen[key] || strings.ContainsAny(title, "[]()<>") {
			continue
		}
		seen[key] = true
		titles = append(titles, title)
		if len(titles) == personaComposedListLimit {
			break
		}
	}
	switch len(titles) {
	case 0:
		return ""
	case 1:
		return "I can read one document here: " + titles[0] + "."
	}
	return fmt.Sprintf("I can read %d documents here: %s.", len(titles), strings.Join(titles, ", "))
}
