package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

// An answer drawn from a searched document must name that document as a
// source. Before this the delivery request never carried the documents, so
// an agent described as answering "with citations" showed none.
func TestPersonaRunCitedDocumentsFollowTheSealedCitations(t *testing.T) {
	output := []byte(`{"Hits":[{"DocumentID":"doc-pto","VersionID":"docv-1","Title":"Paid time off policy","Markdown":"secret body"},{"DocumentID":"doc-sick","VersionID":"docv-9","Title":"Sick leave policy"},{"DocumentID":"","VersionID":"x","Title":"bad"}],"Total":3}`)
	searched := personaRunSearchedDocuments(output)
	if len(searched) != 2 {
		t.Fatalf("searched documents = %+v", searched)
	}
	citations := []agentsecurity.Citation{
		{SourceID: "chat:post-1"},
		{SourceID: "document:doc-pto/version:docv-1"},
		{SourceID: "document:doc-other/version:docv-1"},
	}
	cited := personaRunCitedDocuments(searched, citations)
	if len(cited) != 1 || cited[0].Reference.DocumentID != "doc-pto" || cited[0].Title != "Paid time off policy" || cited[0].Content != "" {
		t.Fatalf("cited documents = %+v", cited)
	}
	if got := personaRunCitedDocuments(searched, []agentsecurity.Citation{{SourceID: "chat:post-1"}}); len(got) != 0 {
		t.Fatalf("a document the sealed answer does not cite was listed: %+v", got)
	}
	if got := personaRunCitedDocuments(searched, []agentsecurity.Citation{{SourceID: "document:doc-pto/version:docv-2"}}); len(got) != 0 {
		t.Fatalf("a different version was listed as the source: %+v", got)
	}
	if got := personaRunSearchedDocuments([]byte(`not json`)); got != nil {
		t.Fatalf("malformed tool output produced documents: %+v", got)
	}
}

// A cited document without a numeric version still renders as a source; the
// renderer used to drop every document whose version number was unknown.
func TestPersonaReplyRendersASearchedSourceWithoutAVersionNumber(t *testing.T) {
	body := renderPersonaReplyWithAgentDocuments("Up to 40 hours carry over.", "tenant-a", "conversation-a", PersonaReplyOutputPolicy{},
		[]agentdocref.ResolvedDocument{{Reference: agentdocref.Reference{DocumentID: "doc-pto"}, Title: "Paid time off policy"}}, nil)
	if !strings.Contains(body, "Paid time off policy") || strings.Contains(body, "version 0") {
		t.Fatalf("source not rendered correctly: %q", body)
	}
}

// The delivery wrapper that adds the agent's referenced documents must keep
// the documents a search cited; it used to replace them, so answers found by
// search never showed a source.
func TestMergePersonaReplyDocumentsKeepsSearchedAndReferenced(t *testing.T) {
	searched := []agentdocref.ResolvedDocument{{Reference: agentdocref.Reference{DocumentID: "doc-pto"}, Title: "Paid time off policy"}, {Reference: agentdocref.Reference{DocumentID: "doc-sick"}, Title: "Sick leave policy"}}
	referenced := []agentdocref.ResolvedDocument{{Reference: agentdocref.Reference{DocumentID: "doc-pto"}, Version: 3, Title: "Paid time off policy"}}
	merged := mergePersonaReplyDocuments(searched, referenced)
	if len(merged) != 2 || merged[0].Reference.DocumentID != "doc-pto" || merged[0].Version != 3 || merged[1].Reference.DocumentID != "doc-sick" {
		t.Fatalf("merged documents = %+v", merged)
	}
	if got := mergePersonaReplyDocuments(searched, nil); len(got) != 2 {
		t.Fatalf("searched documents were dropped when the agent has no references: %+v", got)
	}
	if got := mergePersonaReplyDocuments(nil, nil); len(got) != 0 {
		t.Fatalf("empty merge = %+v", got)
	}
}
