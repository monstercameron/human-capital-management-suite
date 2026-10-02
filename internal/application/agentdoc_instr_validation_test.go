package application

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
)

func TestTodo_AGENTDOC_008_ProfileRefusesTokenWithoutReference(t *testing.T) {
	profile := personaDraftProfile()
	profile.Guidance = "Answer from {{doc:doc-missing}}."
	_, err := agentpersona.Seal(profile)
	if !errors.Is(err, agentdocref.ErrUnknownInstructionDocumentToken) || !strings.Contains(err.Error(), "doc-missing") {
		t.Fatalf("Seal error = %v", err)
	}
}

func TestTodo_AGENTDOC_008_ModelInstructionsRenderDocumentTitlesAndUnreadablePhrase(t *testing.T) {
	profile := personaDraftProfile()
	profile.Guidance = "Use {{doc:doc-policy}}. If {{doc:doc-secret}} is not readable, say so."
	profile.DocumentReferences = []agentdocref.Reference{{DocumentID: "doc-policy", VersionMode: agentdocref.ModePinned, PinnedVersion: 1, Label: "Policy"}, {DocumentID: "doc-secret", VersionMode: agentdocref.ModePinned, PinnedVersion: 1, Label: "Secret"}}

	got := renderPersonaGuidanceDocumentTokens(profile, []agentdocref.ResolvedDocument{{
		Reference: profile.DocumentReferences[0],
		Version:   1,
		Title:     "Leave policy",
		Content:   "Employees may take leave.",
	}})
	if got != `Use "Leave policy". If a document you cannot read is not readable, say so.` {
		t.Fatalf("rendered instructions = %q", got)
	}
}

func TestTodo_AGENTDOC_008_GuidanceChangeChangesProfileDigest(t *testing.T) {
	profile := personaDraftProfile()
	first, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	profile.Guidance = "Cite any referenced document title."
	second, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest == second.Digest || first.Profile.InstructionsDigest != second.Profile.InstructionsDigest {
		t.Fatalf("guidance edit did not force only the sealed profile digest: first=%s second=%s", first.Digest, second.Digest)
	}
}
