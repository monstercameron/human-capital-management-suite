package agentdocref

import (
	"errors"
	"strings"
	"testing"
)

func TestTodo_AGENTDOC_008(t *testing.T) {
	refs := []Reference{{DocumentID: "doc-policy", VersionMode: ModePinned, PinnedVersion: 1, Label: "Travel policy"}}
	if err := ValidateGuidance("Follow {{doc:doc-policy}}.\nAsk when uncertain.\t", refs); err != nil {
		t.Fatalf("valid guidance rejected: %v", err)
	}
	if err := ValidateGuidance("A reference may be available without a token.", refs); err != nil {
		t.Fatalf("unused reference rejected: %v", err)
	}
}

func TestTodo_AGENTDOC_008_Security(t *testing.T) {
	refs := []Reference{{DocumentID: "doc-policy", VersionMode: ModePinned, PinnedVersion: 1, Label: "Travel policy"}}
	tests := []struct {
		name string
		text string
		want error
	}{
		{name: "unknown document", text: "Follow {{doc:doc-secret}}.", want: ErrUnknownInstructionDocumentToken},
		{name: "too long", text: strings.Repeat("界", MaxGuidanceCharacters+1), want: ErrGuidanceTooLong},
		{name: "invalid UTF-8", text: string([]byte{0xff}), want: ErrGuidanceInvalidUTF8},
		{name: "control", text: "first\rsecond", want: ErrGuidanceControl},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGuidance(tt.text, refs)
			var typed *GuidanceValidationError
			if !errors.Is(err, tt.want) || !errors.As(err, &typed) {
				t.Fatalf("ValidateGuidance() error = %v, want typed %v", err, tt.want)
			}
		})
	}
	err := ValidateGuidance("Follow {{doc:doc-secret}}.", refs)
	if got := err.Error(); got != "These instructions mention a document that is not in the list below: doc-secret" {
		t.Fatalf("unknown token message = %q", got)
	}
}
