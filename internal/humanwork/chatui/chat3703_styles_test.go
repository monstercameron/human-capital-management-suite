package chatui

import (
	"strings"
	"testing"
)

// TestChat3703RequiredBasisMarkerIsInStylesheet proves the Basis field on the
// admin country form is marked as required in the shipped stylesheet, and that
// the rule is registered in the chat stylesheet and targets the rendered field.
func TestChat3703RequiredBasisMarkerIsInStylesheet(t *testing.T) {
	want := `.chatlangadmin-form label[for="chatmapadmin-basis"]::after{content:" *"`
	if !strings.Contains(Chat3703Styles, want) {
		t.Fatalf("required marker rule missing from Chat3703Styles")
	}
	if !strings.Contains(ChatLane3Styles, Chat3703Styles) {
		t.Fatalf("Chat3703Styles is not registered in ChatLane3Styles")
	}
	if !strings.Contains(Stylesheet, want) {
		t.Fatalf("required marker rule not present in the chat Stylesheet")
	}
}
