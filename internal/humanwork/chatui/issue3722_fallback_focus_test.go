package chatui

import (
	"os"
	"strings"
	"testing"
)

func TestIssue3722_MessageFallbackSelector(t *testing.T) {
	if got := issue3722MessageSelector(""); got != "" {
		t.Fatalf("empty id must give no selector, got %q", got)
	}
	if got, want := issue3722MessageSelector("abc-1"), `.message[data-message-id="abc-1"]`; got != want {
		t.Fatalf("selector = %q, want %q", got, want)
	}
	if got := issue3722MessageSelector(`a"]b`); !strings.Contains(got, `a\"]b`) {
		t.Fatalf("id is not escaped: %q", got)
	}
}

// The restore path uses the fallback when the remembered opener has left the page.
func TestIssue3722_RestoreFocusFallsBackToTheMessage(t *testing.T) {
	src, err := os.ReadFile("agentux_chat5_layers_js.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	start := strings.Index(text, "func restoreChatLayerFocus(")
	end := strings.Index(text, "func rememberedChatLayerRect(")
	if start < 0 || end < start {
		t.Fatal("restoreChatLayerFocus not found")
	}
	if !strings.Contains(text[start:end], "issue3722MessageSelector(") {
		t.Fatal("restoreChatLayerFocus does not fall back to the opener's message")
	}
}
