package application

import (
	"encoding/json"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

type personaQualitySearchedDocument struct {
	DocumentID, VersionID, Title, SectionAnchor, SectionTitle, Markdown string
	Version                                                             uint64
}

func personaQualitySearchedDocuments(output []byte) []personaQualitySearchedDocument {
	var result struct {
		Hits []personaQualitySearchedDocument
	}
	if json.Unmarshal(output, &result) != nil {
		return nil
	}
	return result.Hits
}

func personaQualitySearchFailure(output []byte) string {
	var result struct {
		Hits        json.RawMessage
		Unavailable string
	}
	if json.Unmarshal(output, &result) != nil || result.Hits == nil {
		return ""
	}
	if result.Unavailable != "" {
		return "CONTEXT_UNAVAILABLE"
	}
	var hits []personaQualitySearchedDocument
	if json.Unmarshal(result.Hits, &hits) == nil && len(hits) == 0 {
		return "NO_RESULTS"
	}
	return ""
}

func personaQualityCitedDocuments(searched []personaQualitySearchedDocument, citations []agentsecurity.Citation) ([]agentdocref.ResolvedDocument, []PersonaReplyCitationDetail) {
	var documents []agentdocref.ResolvedDocument
	var details []PersonaReplyCitationDetail
	seen := map[string]bool{}
	for index, hit := range searched {
		if hit.DocumentID == "" || hit.VersionID == "" || hit.Title == "" {
			continue
		}
		for _, citation := range citations {
			key := citation.SourceID + "\x00" + citation.Location
			if citation.SourceID != "document:"+hit.DocumentID+"/version:"+hit.VersionID || seen[key] {
				continue
			}
			seen[key] = true
			anchor := hit.SectionAnchor
			sectionTitle := hit.SectionTitle
			// The sealed citation's section wins over the search's suggested hit.
			location := strings.TrimPrefix(citation.Location, "#")
			if _, section, ok := strings.Cut(location, "/section:"); ok {
				location = section
			}
			sections := documenthubstore.SplitSections(hit.Markdown)
			for _, section := range sections {
				if section.BlockID == location || "section:"+section.BlockID == location || "block:"+section.BlockID == location {
					anchor = section.BlockID
				}
			}
			for _, section := range sections {
				if section.BlockID == anchor {
					sectionTitle = section.Heading
				}
			}
			documents = append(documents, agentdocref.ResolvedDocument{Reference: agentdocref.Reference{DocumentID: hit.DocumentID, SectionAnchor: anchor}, Version: hit.Version, Title: hit.Title})
			details = append(details, PersonaReplyCitationDetail{DocumentID: hit.DocumentID, VersionID: hit.VersionID, SectionTitle: sectionTitle, SectionAnchor: anchor, Index: index + 1})
		}
	}
	return documents, details
}
