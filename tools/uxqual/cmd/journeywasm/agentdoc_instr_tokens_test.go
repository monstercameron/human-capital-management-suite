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

// An agent can carry reference documents its instructions never mention (the
// server allows it). Saving such an agent must send those documents again.
func TestTodo_AGENTDOC_005_UnmentionedReferencesSurviveSave(t *testing.T) {
	refs := []agentdocref.Reference{
		{DocumentID: "doc-policy", VersionMode: agentdocref.ModePinned, PinnedVersion: 3, Label: "Policy"},
		{DocumentID: "doc-handbook", VersionMode: agentdocref.ModeLatestPublished, Label: "Handbook"},
	}
	stored, kept, unknown := personaInstructionStoredForSubmit("Answer briefly.", refs)
	if stored != "Answer briefly." || len(unknown) != 0 {
		t.Fatalf("stored=%q unknown=%v", stored, unknown)
	}
	if len(kept) != 2 || kept[0].DocumentID != "doc-policy" || kept[0].PinnedVersion != 3 || kept[1].DocumentID != "doc-handbook" {
		t.Fatalf("references sent with an instruction text that mentions none = %+v", kept)
	}
	// One mentioned, one not: both are sent, and only the mention becomes a token.
	stored, kept, unknown = personaInstructionStoredForSubmit("Follow @[Policy].", refs)
	if stored != "Follow {{doc:doc-policy}}." || len(kept) != 2 || len(unknown) != 0 {
		t.Fatalf("stored=%q kept=%+v unknown=%v", stored, kept, unknown)
	}
}

// Typing in the instructions removes a document only when its mention was in
// the text before the keystroke and is gone after it.
func TestTodo_AGENTDOC_005_TypingKeepsUnmentionedDocuments(t *testing.T) {
	refs := []agentdocref.Reference{
		{DocumentID: "doc-policy", VersionMode: agentdocref.ModePinned, PinnedVersion: 3, Label: "Policy"},
		{DocumentID: "doc-handbook", VersionMode: agentdocref.ModeLatestPublished, Label: "Handbook"},
	}
	if dropped := personaInstructionDroppedMentions("Answer briefly.", "Answer briefly!", refs); len(dropped) != 0 {
		t.Fatalf("a keystroke dropped documents that were never mentioned: %v", dropped)
	}
	dropped := personaInstructionDroppedMentions("Follow @[Policy] closely.", "Follow closely.", refs)
	if len(dropped) != 1 || dropped[0] != "doc-policy" {
		t.Fatalf("deleting the Policy mention dropped %v", dropped)
	}
	if dropped := personaInstructionDroppedMentions("Follow @[Policy].", "Follow @[Policy] and be kind.", refs); len(dropped) != 0 {
		t.Fatalf("typing beside a mention dropped %v", dropped)
	}
}

// Removing one document chip takes its mention out of the instructions and
// leaves every other character, line breaks and indentation included.
func TestTodo_AGENTDOC_008_RemovingChipKeepsLineBreaks(t *testing.T) {
	refs := []agentdocref.Reference{
		{DocumentID: "doc-policy", VersionMode: agentdocref.ModePinned, PinnedVersion: 3, Label: "Policy"},
		{DocumentID: "doc-handbook", VersionMode: agentdocref.ModeLatestPublished, Label: "Handbook"},
	}
	text := "Rules:\n\n1. Follow @[Policy] first.\n2. Then read @[Handbook].\n\n    Keep answers short.\n"
	got, left := personaInstructionRemoveReference(text, "doc-policy", refs)
	want := "Rules:\n\n1. Follow first.\n2. Then read @[Handbook].\n\n    Keep answers short.\n"
	if got != want {
		t.Fatalf("text after removing the Policy chip = %q, want %q", got, want)
	}
	if len(left) != 1 || left[0].DocumentID != "doc-handbook" {
		t.Fatalf("references after removing the Policy chip = %+v", left)
	}
	// A mention at the start or end of a line leaves no stray space behind.
	got, _ = personaInstructionRemoveReference("@[Policy] applies.\nSee @[Policy]\nDone.", "doc-policy", refs[:1])
	if got != "applies.\nSee\nDone." {
		t.Fatalf("edge mentions removed = %q", got)
	}
	// The unmentioned document is still attached after another chip is removed.
	_, left = personaInstructionRemoveReference("Follow @[Policy].", "doc-policy", refs)
	if len(left) != 1 || left[0].DocumentID != "doc-handbook" {
		t.Fatalf("unmentioned reference after removing another chip = %+v", left)
	}
}
