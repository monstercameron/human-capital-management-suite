package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestAgentDocInstr2_F1MentionTriggerAndChoice(t *testing.T) {
	text := "Follow @Paid time"
	state := personaInstructionMentionAt(text, len(text))
	if !state.Open || state.Query != "Paid time" || state.Start != len("Follow ") {
		t.Fatalf("mention state = %+v", state)
	}
	if state := personaInstructionMentionAt("Follow @Paid\nnow", len("Follow @Paid\nnow")); state.Open {
		t.Fatalf("newline mention stayed open: %+v", state)
	}
	if state := personaInstructionMentionAt("@"+string(make([]byte, 61)), 62); state.Open {
		t.Fatalf("long mention stayed open: %+v", state)
	}
	chosen, caret, refs := personaInstructionInsertDocument(text, len(text), nil, personaInstructionDocumentChoice{DocumentID: "doc-paid", Title: "Paid time", VersionMode: agentdocref.ModePinned, PinnedVersion: 4})
	if chosen != "Follow @[Paid time] " || caret != len(chosen) || len(refs) != 1 || refs[0].Label != "Paid time" {
		t.Fatalf("choice text=%q caret=%d refs=%+v", chosen, caret, refs)
	}
}

func TestAgentDocInstr2_F2AddModeRemoveState(t *testing.T) {
	text, _, refs := personaInstructionInsertDocument("Use @policy", len("Use @policy"), nil, personaInstructionDocumentChoice{DocumentID: "doc-policy", Title: "Policy", VersionMode: agentdocref.ModePinned, PinnedVersion: 3})
	refs = personaInstructionSetReferenceMode(refs, "doc-policy", agentdocref.ModeLatestPublished)
	if len(refs) != 1 || refs[0].VersionMode != agentdocref.ModeLatestPublished || refs[0].PinnedVersion != 0 {
		t.Fatalf("mode state = %+v", refs)
	}
	text, refs = personaInstructionRemoveReference(text, "doc-policy", refs)
	if text != "Use" || len(refs) != 0 {
		t.Fatalf("remove state text=%q refs=%+v", text, refs)
	}
}

func TestAgentDocInstr2_F3SubmitConvertsAndValidates(t *testing.T) {
	refs := []agentdocref.Reference{{DocumentID: "doc-policy", Label: "Policy", VersionMode: agentdocref.ModePinned, PinnedVersion: 2}}
	stored, kept, unknown := personaInstructionStoredForSubmit("Use @[Policy].", refs)
	if stored != "Use {{doc:doc-policy}}." || len(kept) != 1 || len(unknown) != 0 {
		t.Fatalf("stored=%q kept=%+v unknown=%v", stored, kept, unknown)
	}
	_, kept, unknown = personaInstructionStoredForSubmit("Use @[Other].", refs)
	if len(kept) != 0 || len(unknown) != 1 || unknown[0] != "Other" {
		t.Fatalf("unknown kept=%+v unknown=%v", kept, unknown)
	}
}

func TestAgentDocInstr2_F3SameTitleLabelsUseLocation(t *testing.T) {
	items := []productui.AgentDocumentSuggestion{{DocumentID: "people", Title: "Paid time off", Location: "People"}, {DocumentID: "finance", Title: "Paid time off", Location: "Finance"}}
	labels := personaDocumentSuggestionLabels(items)
	if labels["people"] != "Paid time off (People)" || labels["finance"] != "Paid time off (Finance)" {
		t.Fatalf("labels = %#v", labels)
	}
}
