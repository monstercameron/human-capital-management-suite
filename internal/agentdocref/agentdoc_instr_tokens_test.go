package agentdocref

import (
	"errors"
	"strings"
	"testing"
)

func TestAgentDocInstr_TokenParseAndValidation(t *testing.T) {
	text := "Read {{doc:doc-123}} before {{doc:doc-123}} and {{doc:doc-456}}."
	got := InstructionDocumentTokens(text)
	if len(got) != 2 || got[0] != "doc-123" || got[1] != "doc-456" {
		t.Fatalf("tokens = %#v", got)
	}
	err := ValidateInstructionDocumentTokens(text, []Reference{{DocumentID: "doc-123"}, {DocumentID: "doc-456"}})
	if err != nil {
		t.Fatalf("known tokens rejected: %v", err)
	}
}

func TestAgentDocInstr_RenderWithTitleAndUnreadable(t *testing.T) {
	refs := []Reference{{DocumentID: "doc-policy"}, {DocumentID: "doc-secret"}}
	text := "Use {{doc:doc-policy}} and {{doc:doc-secret}}."
	got := RenderInstructionDocumentTokens(text, refs, []InstructionDocumentTitle{{DocumentID: "doc-policy", Title: "Leave policy", Readable: true}}, "a document you cannot read")
	if got != `Use "Leave policy" and a document you cannot read.` {
		t.Fatalf("rendered instructions = %q", got)
	}
}

func TestAgentDocInstr_MalformedTokensStayText(t *testing.T) {
	text := "Keep {{doc:bad/id}} and {{doc:missing open."
	got := RenderInstructionDocumentTokens(text, nil, nil, "")
	if got != text {
		t.Fatalf("malformed tokens changed: %q", got)
	}
	if tokens := InstructionDocumentTokens(text); len(tokens) != 0 {
		t.Fatalf("malformed tokens parsed: %#v", tokens)
	}
}

func TestAgentDocInstr_UnknownReferenceRefused(t *testing.T) {
	err := ValidateInstructionDocumentTokens("Use {{doc:doc-unknown}}.", []Reference{{DocumentID: "doc-known"}})
	if !errors.Is(err, ErrUnknownInstructionDocumentToken) || !strings.Contains(err.Error(), "{{doc:doc-unknown}}") {
		t.Fatalf("unknown token error = %v", err)
	}
}
