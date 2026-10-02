package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// The retry selector must not match the picker root, which carries the retry
// label as an attribute of the same name.
func TestAgentDocPickerRetrySelectorMatchesOnlyTheButton(t *testing.T) {
	if !strings.HasPrefix(personaDocumentPickerRetrySelector, "button[") {
		t.Fatalf("retry selector %q can match the picker root", personaDocumentPickerRetrySelector)
	}
	_ = productui.AgentDocumentPickerFailed
}
