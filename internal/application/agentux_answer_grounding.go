package application

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

var personaQualityCitationMarker = regexp.MustCompile(`\[\[([1-9][0-9]*)(?::([^\[\]\s:]+))?\]\]`)

// Markers select existing, gateway-validated evidence; they cannot introduce
// a source or a section that is absent from the authorized search result.
func personaQualitySelectedGrounding(authority PersonaRunChatReplyAuthority, text string) ([]agentsecurity.Datum, error) {
	var context, documents []agentsecurity.Datum
	var parts []agentsecurity.AnswerPart
	for _, datum := range authority.Grounding {
		answer, err := authority.Gateway.BuildAnswer([]agentsecurity.Datum{datum})
		if err != nil || len(answer.Parts) != 1 || len(answer.Parts[0].Citations) != 1 {
			return nil, agentsecurity.ErrInvalidDatum
		}
		part := answer.Parts[0]
		if strings.HasPrefix(part.Citations[0].SourceID, "document:") {
			documents = append(documents, datum)
			parts = append(parts, part)
		} else {
			context = append(context, datum)
		}
	}
	markers := personaQualityCitationMarker.FindAllStringSubmatch(text, -1)
	if len(markers) == 0 {
		// Older plain replies have no markers. A single document is unambiguous;
		// with several documents, only an explicitly named title is selected.
		for index, part := range parts {
			blocks := documenthubstore.DeriveBlocks(part.Text)
			if len(parts) == 1 || len(blocks) > 0 && strings.Contains(text, blocks[0].Heading) {
				context = append(context, documents[index])
			}
		}
		if len(documents) > 0 && len(context) == len(authority.Grounding)-len(documents) {
			return nil, agentsecurity.ErrMissingCitation
		}
		return context, nil
	}
	seen := map[string]bool{}
	for _, marker := range markers {
		index, err := strconv.Atoi(marker[1])
		if err != nil || index < 1 || index > len(documents) {
			return nil, agentsecurity.ErrMissingCitation
		}
		key := marker[1] + ":" + marker[2]
		if seen[key] {
			continue
		}
		seen[key] = true
		datum := documents[index-1]
		if marker[2] != "" {
			part := parts[index-1]
			found := false
			for _, block := range documenthubstore.DeriveBlocks(part.Text) {
				found = found || block.ID == marker[2]
			}
			if !found || part.Kind != agentsecurity.KindObservation {
				return nil, agentsecurity.ErrMissingCitation
			}
			citation := part.Citations[0]
			citation.Location += "/section:" + marker[2]
			datum, err = authority.Gateway.Observe(agentsecurity.SourceDocument, part.Text, agentsecurity.KindObservation, citation)
			if err != nil {
				return nil, err
			}
		}
		context = append(context, datum)
	}
	return context, nil
}
