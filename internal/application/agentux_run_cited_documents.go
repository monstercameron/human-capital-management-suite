package application

import (
	"encoding/json"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

// personaRunSearchedDocuments reads the documents a document search returned
// to the run. Only identity and title are kept: the text itself has already
// gone to the model through the governed tool path and is not carried again.
func personaRunSearchedDocuments(toolOutput []byte) []personaRunSearchedDocument {
	var result struct {
		Hits []struct {
			DocumentID string
			VersionID  string
			Title      string
		}
	}
	if len(toolOutput) == 0 || json.Unmarshal(toolOutput, &result) != nil {
		return nil
	}
	out := make([]personaRunSearchedDocument, 0, len(result.Hits))
	for _, hit := range result.Hits {
		if strings.TrimSpace(hit.DocumentID) == "" || strings.TrimSpace(hit.VersionID) == "" || strings.TrimSpace(hit.Title) == "" {
			continue
		}
		out = append(out, personaRunSearchedDocument{DocumentID: hit.DocumentID, VersionID: hit.VersionID, Title: hit.Title})
	}
	return out
}

type personaRunSearchedDocument struct {
	DocumentID, VersionID, Title string
}

// personaRunCitedDocuments keeps the searched documents the sealed answer is
// grounded in. The sealed output's citations are the authority: a document
// the search returned but the validated answer does not cite is not listed
// as a source, and a citation with no matching searched document adds none.
func personaRunCitedDocuments(searched []personaRunSearchedDocument, citations []agentsecurity.Citation) []agentdocref.ResolvedDocument {
	if len(searched) == 0 || len(citations) == 0 {
		return nil
	}
	cited := make(map[string]struct{}, len(citations))
	for _, citation := range citations {
		if strings.HasPrefix(citation.SourceID, "document:") {
			cited[citation.SourceID] = struct{}{}
		}
	}
	var out []agentdocref.ResolvedDocument
	seen := make(map[string]struct{}, len(searched))
	for _, document := range searched {
		if _, ok := cited["document:"+document.DocumentID+"/version:"+document.VersionID]; !ok {
			continue
		}
		if _, duplicate := seen[document.DocumentID]; duplicate {
			continue
		}
		seen[document.DocumentID] = struct{}{}
		out = append(out, agentdocref.ResolvedDocument{Reference: agentdocref.Reference{DocumentID: document.DocumentID}, Title: document.Title})
	}
	return out
}
