package tokens

import (
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_005_SemanticTokenAdmission(t *testing.T) {
	colors := map[string]string{
		"accent": "#1d4f91", "accent-text": "#ffffff", "background": "#ffffff", "border": "#737887",
		"danger": "#a3123a", "danger-text": "#ffffff", "focus": "#1d4f91", "hover": "#163f73", "hover-text": "#ffffff",
		"info": "#1a4f66", "selection": "#dbeafe", "selection-text": "#1e3a8a", "success": "#166534",
		"surface": "#f4f5f8", "text": "#1a1d29", "text-muted": "#4a4f63", "warning": "#7a4a00", "warning-text": "#fff6e5",
	}
	if err := ValidateUIPolish005SemanticTokens(colors); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_UIPOLISH_005_SemanticTokenAdmissionRejectsMissingMalformedAndRaw(t *testing.T) {
	base := map[string]string{}
	for _, name := range UIPolish005SemanticNames() {
		base[name] = "#ffffff"
	}
	delete(base, "focus")
	if err := ValidateUIPolish005SemanticTokens(base); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing token error = %v", err)
	}
	base["focus"] = "white"
	if err := ValidateUIPolish005SemanticTokens(base); err == nil || !strings.Contains(err.Error(), "expected #rrggbb") {
		t.Fatalf("malformed token error = %v", err)
	}
	base["focus"] = "#ffffff"
	base["raw-purple"] = "#800080"
	if err := ValidateUIPolish005SemanticTokens(base); err == nil || !strings.Contains(err.Error(), "unrecognized") {
		t.Fatalf("raw token error = %v", err)
	}
}

func TestTodo_UIPOLISH_005_SemanticTokenNamesStable(t *testing.T) {
	names := SortedUIPolish005SemanticNames()
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("names not strictly sorted: %v", names)
		}
	}
}
