package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

func TestAgentUXQuality_SourceLinks(t *testing.T) {
	documents := []agentdocref.ResolvedDocument{{Reference: agentdocref.Reference{DocumentID: "pto", SectionAnchor: "carryover"}, Title: "Paid time off policy", Version: 1}}
	got := renderPersonaReplyWithAgentDocuments("40 hours carry over.", "tenant", "room", PersonaReplyOutputPolicy{}, documents, nil)
	if !strings.Contains(got, "[Paid time off policy · carryover · v1.0.0](/workspace/app/docs?document=pto#carryover)") {
		t.Fatalf("relative section source missing: %q", got)
	}
	if strings.Contains(got, "version 1") || displayVersion(42) != "v42.0.0" {
		t.Fatalf("semantic version display missing: %q", got)
	}
}

func TestAgentUXQuality_SectionCitation(t *testing.T) {
	searched := personaQualitySearchedDocuments([]byte(`{"Hits":[{"DocumentID":"pto","VersionID":"one","Title":"Paid time off policy","Version":1,"SectionAnchor":"carryover","Markdown":"## Carryover\n40 hours."},{"DocumentID":"holidays","VersionID":"two","Title":"Holiday guide"}]}`))
	documents, details := personaQualityCitedDocuments(searched, []agentsecurity.Citation{{SourceID: "document:pto/version:one", Location: "carryover"}})
	if len(documents) != 1 || documents[0].Reference.SectionAnchor != "carryover" || len(details) != 1 || details[0].SectionTitle != "Carryover" {
		t.Fatalf("cited section lost: %+v %+v", documents, details)
	}
	got := renderPersonaReplyWithAgentDocuments("40 hours [[1]].", "tenant", "room", PersonaReplyOutputPolicy{}, documents, nil, details...)
	if !strings.Contains(got, "[Paid time off policy · Carryover · v1.0.0](/workspace/app/docs?document=pto&version=one#carryover)") || strings.Contains(got, "Holiday guide") || strings.Contains(got, "[[1]]") {
		t.Fatalf("citation: %s", got)
	}
}

func TestAgentUXQuality_SourceLinks_Security(t *testing.T) {
	for _, target := range []string{"//evil.example/workspace/app/docs?document=pto", "/workspace/app/admin", "javascript:alert(1)", "https://evil.example/workspace/app/docs?document=pto"} {
		got := renderPersonaReplyText("[source]("+target+")", "tenant", "room", PersonaReplyOutputPolicy{})
		if strings.Contains(got, "](") {
			t.Fatalf("untrusted target survived: %q", got)
		}
	}
}

func TestAgentUXQuality_SearchFailures_Fault(t *testing.T) {
	for _, fixture := range []struct{ output, code string }{
		{`{"Hits":[],"Total":0}`, "NO_RESULTS"},
		{`{"Hits":null,"Total":0}`, "NO_RESULTS"},
		{`{"Hits":[],"Unavailable":"restricted"}`, "CONTEXT_UNAVAILABLE"},
		{`{"Hits":[{"DocumentID":"pto"}]}`, ""},
		{`{"unrelated":true}`, ""},
		{`broken`, ""},
	} {
		if got := personaQualitySearchFailure([]byte(fixture.output)); got != fixture.code {
			t.Fatalf("search classification %s = %s want %s", fixture.output, got, fixture.code)
		}
	}
}
