package application

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

func TestAgentUXQuality_ExactCitations(t *testing.T) {
	validator, admission, run, _, source := personaRunOutputFixture(t)
	for _, document := range []struct{ id, text string }{{"pto", "# Paid time off policy\n## Carryover\n40 hours."}, {"holiday", "# 2026 holiday guide\n12 holidays."}} {
		datum, err := source.authority.Gateway.Observe(agentsecurity.SourceDocument, document.text, agentsecurity.KindObservation, agentsecurity.Citation{SourceID: "document:" + document.id + "/version:one", Location: "document:" + document.id + "/version:one", Digest: personaRunBytesDigest([]byte(document.text))})
		if err != nil {
			t.Fatal(err)
		}
		source.authority.Grounding = append(source.authority.Grounding, datum)
	}
	result, err := validator.ValidateAndPersistPersonaOutput(context.Background(), admission, run, agentmodel.ModelResult{Text: "40 hours carry over [[1:carryover]].", Finish: agentmodel.FinishComplete})
	if err != nil {
		t.Fatal(err)
	}
	documents := 0
	for _, citation := range result.Citations() {
		if strings.HasPrefix(citation.SourceID, "document:") {
			documents++
			if citation.SourceID != "document:pto/version:one" || !strings.HasSuffix(citation.Location, "/section:carryover") {
				t.Fatalf("unused document or wrong section sealed: %+v", citation)
			}
		}
	}
	if documents != 1 || result.Digest() == "" {
		t.Fatalf("sealed citation count=%d", documents)
	}
	for _, text := range []string{"Wrong [[3]].", "Wrong [[1:secret]]."} {
		_, err = validator.ValidateAndPersistPersonaOutput(context.Background(), admission, run, agentmodel.ModelResult{Text: text, Finish: agentmodel.FinishComplete})
		if !errors.Is(err, agentsecurity.ErrMissingCitation) {
			t.Fatalf("forged citation %q: %v", text, err)
		}
	}
}

func TestAgentUXQuality_CitationSchema_Security(t *testing.T) {
	for _, text := range []string{"Read [[1]].", "Read [[2:carryover]]."} {
		if err := validatePersonaChatReply(text); err != nil {
			t.Fatalf("closed marker rejected: %v", err)
		}
	}
	for _, text := range []string{"Read [1].", "Read [[1]] [open](https://evil).", "Read [[1:bad anchor]].", "Read [[0]]."} {
		if err := validatePersonaChatReply(text); err == nil {
			t.Fatalf("unsafe citation accepted: %s", text)
		}
	}
}

func TestAgentUXQuality_ExactCitations_Integration(t *testing.T) {
	service := documentServiceFixture(t)
	ctx := context.Background()
	const tenant = "tenant-a"
	pto, _ := deployedSearchFixture(t, service, ctx, tenant, "owner", "Paid time off policy", "# Paid time off policy\n## Carryover\n40 hours carry over under this policy.")
	holiday, _ := deployedSearchFixture(t, service, ctx, tenant, "owner", "2026 holiday guide", "# 2026 holiday guide\nThe holiday policy names twelve company holidays.")
	for _, documentID := range []string{pto, holiday} {
		if err := service.ShareDocument(ctx, tenant, "owner", documentID, "alice", ""); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := service.store.SearchLexical(ctx, tenant, "policy", "person", "alice")
	if err != nil || len(hits) != 2 {
		t.Fatalf("real two-document search: %+v %v", hits, err)
	}
	validator, admission, run, _, source := personaRunOutputFixture(t)
	var searched []personaQualitySearchedDocument
	index := 0
	for position, hit := range hits {
		version, err := service.store.ReadVersion(ctx, tenant, hit.DocumentID, hit.VersionID, "person", "alice")
		if err != nil {
			t.Fatal(err)
		}
		id := "document:" + hit.DocumentID + "/version:" + hit.VersionID
		datum, err := source.authority.Gateway.Observe(agentsecurity.SourceDocument, version.Markdown, agentsecurity.KindObservation, agentsecurity.Citation{SourceID: id, Location: id, Digest: personaRunBytesDigest([]byte(version.Markdown))})
		if err != nil {
			t.Fatal(err)
		}
		source.authority.Grounding = append(source.authority.Grounding, datum)
		searched = append(searched, personaQualitySearchedDocument{DocumentID: hit.DocumentID, VersionID: hit.VersionID, Title: hit.Title, Markdown: version.Markdown, Version: 1})
		if hit.DocumentID == pto {
			index = position + 1
		}
	}
	if index == 0 {
		t.Fatal("PTO not in authorized search")
	}
	answer := "40 hours carry over [[" + strconv.Itoa(index) + ":carryover]]."
	sealed, err := validator.ValidateAndPersistPersonaOutput(ctx, admission, run, agentmodel.ModelResult{Text: answer, Finish: agentmodel.FinishComplete})
	if err != nil {
		t.Fatal(err)
	}
	documents, details := personaQualityCitedDocuments(searched, sealed.Citations())
	if len(documents) != 1 || documents[0].Reference.DocumentID != pto || documents[0].Reference.SectionAnchor != "carryover" {
		t.Fatalf("search results leaked into sealed sources: %+v", documents)
	}
	markup := renderPersonaReplyWithAgentDocuments(answer, tenant, "general", PersonaReplyOutputPolicy{}, documents, nil, details...)
	if strings.Contains(markup, "2026 holiday guide") || !strings.Contains(markup, "#carryover)") || !strings.Contains(markup, "v1.0.0") || strings.Contains(markup, "[[") {
		t.Fatalf("sealed source rendering: %s", markup)
	}
}
