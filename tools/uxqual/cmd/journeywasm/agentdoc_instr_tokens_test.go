package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestAgentDocInstr_InsertingDocumentAddsTokenAndReference(t *testing.T) {
	text, caret, refs := personaInstructionInsertDocument("Read @pol before answering.", len("Read @pol"), nil, personaInstructionDocumentChoice{DocumentID: "doc-policy", Title: "Policy", VersionMode: agentdocref.ModePinned, PinnedVersion: 3})
	if text != "Read @[Policy] before answering." || caret != len("Read @[Policy] ") {
		t.Fatalf("inserted text=%q caret=%d", text, caret)
	}
	if len(refs) != 1 || refs[0].DocumentID != "doc-policy" || refs[0].PinnedVersion != 3 {
		t.Fatalf("refs=%+v", refs)
	}
}

func TestAgentDocInstr_RemovingReferenceRemovesToken(t *testing.T) {
	refs := []agentdocref.Reference{{DocumentID: "doc-policy", VersionMode: agentdocref.ModePinned, PinnedVersion: 3, Label: "Policy"}}
	text, refs := personaInstructionRemoveReference("Read @[Policy] first.", "doc-policy", refs)
	if text != "Read first." || len(refs) != 0 {
		t.Fatalf("removed text=%q refs=%+v", text, refs)
	}
}

func TestAgentDocInstr_SubmitCarriesInstructionsWithTokensAndReferences(t *testing.T) {
	refs := []agentdocref.Reference{{DocumentID: "doc-policy", VersionMode: agentdocref.ModeLatestPublished, Label: "Policy"}}
	got := personaInstructionReferencesForSubmit("Use @[Policy].", refs)
	if len(got) != 1 || got[0].DocumentID != "doc-policy" || got[0].VersionMode != agentdocref.ModeLatestPublished {
		t.Fatalf("submit refs=%+v", got)
	}
}

func TestTodo_AGENTDOC_008_BrowserSubmitPayload(t *testing.T) {
	guidance := "Use {{doc:doc-policy}}."
	references := []agentdocref.Reference{{DocumentID: "doc-policy", VersionMode: agentdocref.ModeLatestPublished, Label: "Policy"}}
	request := productui.PersonaAdminCommandRequest{Action: "CREATE_VERSION", PersonaID: "policy-helper", Instructions: &guidance, DocumentReferences: &references}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"instructions":"Use {{doc:doc-policy}}."`) || !strings.Contains(string(encoded), `"document_references":[{"document_id":"doc-policy"`) {
		t.Fatalf("guidance submit payload = %s", encoded)
	}
	empty := ""
	request.Instructions = &empty
	encoded, err = json.Marshal(request)
	if err != nil || !strings.Contains(string(encoded), `"instructions":""`) {
		t.Fatalf("explicit guidance clear payload = %s, err=%v", encoded, err)
	}
}
